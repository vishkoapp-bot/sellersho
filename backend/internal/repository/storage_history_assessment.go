package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/drup/platform/internal/pricing"
	"github.com/google/uuid"
)

const storageAssessmentDiscoverySQL = `SELECT id FROM warehouse_storage_receipt_lots WHERE id COLLATE "C">$1::text ORDER BY id COLLATE "C" LIMIT $2`

const storageAssessmentPositions = 100
const storageAssessmentDays = 32

type storageAssessmentLot struct {
	ID, Product, Supplier, Variant, Warehouse, GoodsClass string
	History                                               storageReceiptHistory
	Remaining                                             int64
	LastFee, LastAssessed                                 sql.NullTime
	Closed                                                sql.NullTime
}

const storageAssessmentLotColumns = `p.id,p.product_id,p.supplier_id,l.variant_id,p.warehouse_id,p.goods_class,l.initial_quantity,l.held_from,l.billing_start_at,l.free_days,l.unit_day_toman,p.qty,p.last_fee_date,p.last_assessed_date,p.closed_at`

func scanStorageAssessmentLot(row interface{ Scan(...any) error }) (storageAssessmentLot, error) {
	var p storageAssessmentLot
	err := row.Scan(&p.ID, &p.Product, &p.Supplier, &p.Variant, &p.Warehouse, &p.GoodsClass, &p.History.Quantity, &p.History.HeldFrom, &p.History.BillingStart, &p.History.FreeDays, &p.History.UnitDay, &p.Remaining, &p.LastFee, &p.LastAssessed, &p.Closed)
	return p, err
}

// Only certified receipt facts can create new charges. Old merged positions
// retain their original charges and explicit unresolved audit, never guessed debt.
// Each pass is bounded; MoreWork requests another idempotent pass, not a large
// request holding all warehouse accounts/positions for an unbounded history scan.
func (r *SupplierRepository) runStorageHistoryAssessment(ctx context.Context, at time.Time) (StorageFeeRunSummary, error) {
	summary := StorageFeeRunSummary{VerifiedReceiptHistoryOnly: true}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return summary, err
	}
	defer tx.Rollback()
	var lockSet TransactionLockSet
	if err = LockTransactionRowsTx(ctx, tx, &lockSet, TransactionRowLock{Table: "platform_settings", ID: "1"}); err != nil {
		return summary, err
	}
	var enabled bool
	// Configuration guard is acquired before every account/product/work guard.
	if err = tx.QueryRowContext(ctx, `SELECT storage_fee_enabled FROM platform_settings WHERE id=1`).Scan(&enabled); err != nil {
		return summary, err
	}
	if !enabled {
		return summary, ErrStorageFeeDisabled
	}
	var available bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('warehouse_storage_receipt_lots') IS NOT NULL AND to_regclass('warehouse_storage_lot_departures') IS NOT NULL`).Scan(&available); err != nil {
		return summary, err
	}
	if !available {
		return summary, ErrStorageReceiptSource
	}
	cutoff := tehranDay(at).AddDate(0, 0, -1)
	var frontier string
	var cyclePending bool
	if err = tx.QueryRowContext(ctx, `SELECT last_receipt_id,cycle_pending FROM warehouse_storage_assessment_cursor WHERE id=1`).Scan(&frontier, &cyclePending); err != nil {
		return summary, err
	}
	rows, err := tx.QueryContext(ctx, storageAssessmentDiscoverySQL, frontier, storageAssessmentPositions+1)
	if err != nil {
		return summary, err
	}
	var receiptIDs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return summary, err
		}
		receiptIDs = append(receiptIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return summary, err
	}
	moreReceipts := len(receiptIDs) > storageAssessmentPositions
	if moreReceipts {
		receiptIDs = receiptIDs[:storageAssessmentPositions]
	}
	summary.ReceiptsExamined = len(receiptIDs)
	summary.MoreWork = moreReceipts
	var lots []storageAssessmentLot
	for _, id := range receiptIDs {
		p, e := scanStorageAssessmentLot(tx.QueryRowContext(ctx, `SELECT `+storageAssessmentLotColumns+` FROM warehouse_storage_positions p JOIN warehouse_storage_receipt_lots l ON l.id=p.id WHERE p.id=$2 AND l.history_status='verified' AND NOT storage_receipt_is_blocked(l.id) AND (p.last_assessed_date IS NULL OR p.last_assessed_date<LEAST($1::date,COALESCE((p.closed_at AT TIME ZONE 'Asia/Tehran')::date,$1::date)))`, cutoff, id))
		if e == sql.ErrNoRows {
			continue
		}
		if e != nil {
			return summary, e
		}
		lots = append(lots, p)
	}
	pendingDayWork := false
	// Bounded unresolved-measurement sample is separate from the eligible work
	// cursor, so unresolved rows cannot starve all later verified receipts.
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM(SELECT id FROM warehouse_storage_receipt_lots WHERE history_status='unresolved_measurement' ORDER BY id LIMIT $1) s`, storageAssessmentPositions+1).Scan(&summary.PositionsSkippedNoMeasurement); err != nil {
		return summary, err
	}
	if summary.PositionsSkippedNoMeasurement > storageAssessmentPositions {
		summary.PositionsSkippedNoMeasurement = storageAssessmentPositions
		summary.UnresolvedSampleLimited = true
	}
	var plan []TransactionRowLock
	for _, p := range lots {
		plan = append(plan, TransactionRowLock{Table: "suppliers", ID: p.Supplier}, TransactionRowLock{Table: "products", ID: p.Product}, TransactionRowLock{Table: "warehouse_storage_positions", ID: p.ID})
		if p.Variant != "" {
			plan = append(plan, TransactionRowLock{Table: "product_variants", ID: p.Variant})
		}
	}
	if err = LockTransactionRowsTx(ctx, tx, &lockSet, plan...); err != nil {
		return summary, err
	}
	chargedBySupplier := map[string]int64{}
	positionsBySupplier := map[string]int{}
	for _, discovered := range lots {
		p, e := scanStorageAssessmentLot(tx.QueryRowContext(ctx, `SELECT `+storageAssessmentLotColumns+` FROM warehouse_storage_positions p JOIN warehouse_storage_receipt_lots l ON l.id=p.id WHERE p.id=$1 AND l.history_status='verified' AND NOT storage_receipt_is_blocked(l.id) AND (p.product_id,p.supplier_id,p.warehouse_id)=(l.product_id,l.supplier_id,l.warehouse_id)`, discovered.ID))
		if e != nil {
			return summary, e
		}
		if p.Product != discovered.Product || p.Supplier != discovered.Supplier || p.Variant != discovered.Variant || p.Warehouse != discovered.Warehouse {
			return summary, ErrFinanceAccountChanged
		}
		remaining := p.History.Quantity
		e = tx.QueryRowContext(ctx, `SELECT remaining_quantity_after FROM warehouse_storage_lot_departures WHERE position_id=$1 ORDER BY effective_at DESC,ordinal DESC LIMIT 1`, p.ID).Scan(&remaining)
		if e != nil && e != sql.ErrNoRows {
			return summary, e
		}
		if remaining < 0 || remaining > p.History.Quantity || remaining != p.Remaining {
			return summary, ErrStorageReceiptSource
		}
		through := cutoff
		if p.Closed.Valid && tehranDay(p.Closed.Time).Before(through) {
			through = tehranDay(p.Closed.Time)
		}
		if p.LastAssessed.Valid && !p.LastAssessed.Time.Before(through) {
			continue
		}
		first := tehranDay(p.History.BillingStart).AddDate(0, 0, p.History.FreeDays)
		if heldDay := tehranDay(p.History.HeldFrom); heldDay.After(first) {
			first = heldDay
		}
		if p.LastAssessed.Valid && p.LastAssessed.Time.AddDate(0, 0, 1).After(first) {
			first = p.LastAssessed.Time.AddDate(0, 0, 1)
		}
		if p.LastFee.Valid && p.LastFee.Time.AddDate(0, 0, 1).After(first) {
			first = p.LastFee.Time.AddDate(0, 0, 1)
		}
		var chargedHere int64
		var lastCharged sql.NullTime
		lastExamined := through
		for day, n := first, 0; !day.After(through); day, n = day.AddDate(0, 0, 1), n+1 {
			if n >= storageAssessmentDays {
				lastExamined = day.AddDate(0, 0, -1)
				summary.MoreWork = true
				pendingDayWork = true
				break
			}
			boundary := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, storageFeeTehranLoc)
			before := p.History.Quantity
			e = tx.QueryRowContext(ctx, `SELECT remaining_quantity_after FROM warehouse_storage_lot_departures WHERE position_id=$1 AND effective_at<=$2 ORDER BY effective_at DESC,ordinal DESC LIMIT 1`, p.ID, boundary).Scan(&before)
			if e != nil && e != sql.ErrNoRows {
				return summary, e
			}
			if before < 0 || before > p.History.Quantity {
				return summary, ErrStorageReceiptSource
			}
			history := p.History
			if used := history.Quantity - before; used > 0 {
				history.Departures = []storageQuantityDeparture{{Quantity: used, At: boundary}}
			}
			quantity, amount, e := storageReceiptDayAmount(history, day)
			if e != nil {
				return summary, e
			}
			if amount == 0 {
				continue
			}
			result, e := tx.ExecContext(ctx, `INSERT INTO warehouse_storage_fee_charges(id,position_id,supplier_id,product_id,warehouse_id,fee_date,qty,fee_unit_day_toman,amount_toman,goods_class,estimated) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,FALSE) ON CONFLICT(position_id,fee_date) DO NOTHING`, "WSF-"+uuid.NewString(), p.ID, p.Supplier, p.Product, p.Warehouse, day, quantity, p.History.UnitDay, amount, p.GoodsClass)
			if e != nil {
				return summary, e
			}
			inserted, e := result.RowsAffected()
			if e != nil {
				return summary, e
			}
			if inserted == 0 {
				var matches bool
				if e = tx.QueryRowContext(ctx, `SELECT qty=$3 AND fee_unit_day_toman=$4 AND amount_toman=$5 AND supplier_id=$6 AND product_id=$7 AND warehouse_id=$8 FROM warehouse_storage_fee_charges WHERE position_id=$1 AND fee_date=$2`, p.ID, day, quantity, p.History.UnitDay, amount, p.Supplier, p.Product, p.Warehouse).Scan(&matches); e != nil || !matches {
					return summary, ErrStorageReceiptSource
				}
				continue
			}
			if e = pricing.AccumulateMoney(&chargedHere, amount, 1); e != nil {
				return summary, e
			}
			summary.ChargesCreated++
			lastCharged = sql.NullTime{Time: day, Valid: true}
		}
		if chargedHere > 0 {
			if e = pricing.AccumulateMoney(&summary.TotalCharged, chargedHere, 1); e != nil {
				return summary, e
			}
			summary.PositionsCharged++
			chargedBySupplier[p.Supplier] += chargedHere
			positionsBySupplier[p.Supplier]++
		}
		var feeDate any
		if lastCharged.Valid {
			feeDate = lastCharged.Time
		}
		if _, e = tx.ExecContext(ctx, `UPDATE warehouse_storage_positions SET last_assessed_date=GREATEST(COALESCE(last_assessed_date,$2::date),$2::date),last_fee_date=CASE WHEN $3::date IS NOT NULL THEN GREATEST(COALESCE(last_fee_date,$3::date),$3::date) ELSE last_fee_date END,updated_at=clock_timestamp() WHERE id=$1`, p.ID, lastExamined, feeDate); e != nil {
			return summary, e
		}
	}
	nextFrontier := ""
	nextCyclePending := false
	if moreReceipts {
		nextFrontier = receiptIDs[len(receiptIDs)-1]
		nextCyclePending = cyclePending || pendingDayWork
	}
	summary.MoreWork = summary.MoreWork || cyclePending
	if _, err = tx.ExecContext(ctx, `UPDATE warehouse_storage_assessment_cursor SET last_receipt_id=$1,cycle_pending=$2 WHERE id=1`, nextFrontier, nextCyclePending); err != nil {
		return summary, err
	}
	if err = tx.Commit(); err != nil {
		return summary, fmt.Errorf("commit receipt storage assessment: %w", err)
	}
	for supplier, total := range chargedBySupplier {
		notifyStorageFeeCharged(ctx, r.db, supplier, total, positionsBySupplier[supplier])
	}
	return summary, nil
}
