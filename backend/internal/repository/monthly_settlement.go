package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/drup/platform/internal/pricing"
	"github.com/google/uuid"
)

var ErrInvalidSettlementPeriod = errors.New("invalid monthly settlement period")
var ErrSettlementReconciliation = errors.New("monthly settlement reconciliation required")

const monthlyBatchLimit = 25

// The aggregate projection is one MVCC statement. Historical completed totals
// remain accepted snapshots; new progress reports current committed item effects.
const monthlyCycleSelect = `SELECT c.period,c.settlement_day,c.status,
 CASE WHEN c.progress_initialized THEN m.n ELSE c.total_stores END,
 CASE WHEN c.progress_initialized THEN m.gross ELSE c.total_gross END,
 CASE WHEN c.progress_initialized THEN m.net ELSE c.total_net END,
 CASE WHEN c.progress_initialized THEN m.skipped ELSE c.skipped_stores END,
 c.created_at,c.created_by,c.progress_initialized,w.candidates,w.done,w.pending,w.blocked
 FROM monthly_settlement_cycles c
 LEFT JOIN LATERAL (SELECT count(*) n,COALESCE(sum(gross_amount),0) gross,COALESCE(sum(net_amount),0) net,
 count(*) FILTER(WHERE status='skipped_no_bank_details') skipped FROM monthly_settlement_items WHERE cycle_period=c.period) m ON true
 LEFT JOIN LATERAL (SELECT count(*) candidates,count(*) FILTER(WHERE state='done') done,
 count(*) FILTER(WHERE state='pending') pending,count(*) FILTER(WHERE state='blocked') blocked
 FROM monthly_settlement_progress WHERE cycle_period=c.period) w ON true`

func scanMonthlyCycle(row interface{ Scan(...any) error }) (*MonthlySettlementCycle, error) {
	var c MonthlySettlementCycle
	err := row.Scan(&c.Period, &c.SettlementDay, &c.Status, &c.TotalStores, &c.TotalGross, &c.TotalNet, &c.SkippedStores,
		&c.CreatedAt, &c.CreatedBy, &c.ProgressKnown, &c.CandidateStores, &c.CompletedStores, &c.PendingStores, &c.BlockedStores)
	if err != nil {
		return nil, err
	}
	if c.TotalGross < 0 || c.TotalNet < 0 || c.TotalGross > pricing.MaxTransportMoney || c.TotalNet > pricing.MaxTransportMoney {
		return nil, pricing.ErrInvalidMoney
	}
	return &c, nil
}

func (r *FinanceRepository) GetMonthlySettlementCycle(ctx context.Context, period string) (*MonthlySettlementCycle, error) {
	return scanMonthlyCycle(r.db.QueryRowContext(ctx, monthlyCycleSelect+` WHERE c.period=$1`, period))
}

// Running batches commit independently. Detail totals and item rows must share
// one repeatable-read snapshot rather than disagree across two committed batches.
func (r *FinanceRepository) GetMonthlySettlementDetail(ctx context.Context, period string) (*MonthlySettlementCycle, []MonthlySettlementItem, error) {
	cycle, page, err := r.GetMonthlySettlementDetailPage(ctx, period, ListPageOptions{Limit: MaxListLimit})
	if err != nil {
		return nil, nil, err
	}
	return cycle, page.Items, nil
}

// GetMonthlySettlementDetailPage returns a settlement cycle with one cursor
// page of its items. The cycle totals and the page share one repeatable-read
// transaction, so they agree with each other. PERF-02 Step 7.
func (r *FinanceRepository) GetMonthlySettlementDetailPage(ctx context.Context, period string, opts ListPageOptions) (*MonthlySettlementCycle, ListPage[MonthlySettlementItem], error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, ListPage[MonthlySettlementItem]{}, err
	}
	defer tx.Rollback()
	cycle, err := scanMonthlyCycle(tx.QueryRowContext(ctx, monthlyCycleSelect+` WHERE c.period=$1`, period))
	if err != nil {
		return nil, ListPage[MonthlySettlementItem]{}, err
	}
	items, err := listMonthlyItemsPage(ctx, tx, period, opts)
	if err != nil {
		return nil, ListPage[MonthlySettlementItem]{}, err
	}
	if err = tx.Commit(); err != nil {
		return nil, ListPage[MonthlySettlementItem]{}, err
	}
	return cycle, items, nil
}

// Initialization has no account locks and no financial amounts. Its manifest is
// committed atomically with the cycle, so restart never needs an in-memory cursor.
func (r *FinanceRepository) initializeMonthlyCycle(ctx context.Context, period string, day int, by string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO monthly_settlement_cycles(period,settlement_day,status,created_by,progress_initialized)
 VALUES($1,$2,'running',$3,true) ON CONFLICT(period) DO NOTHING`, period, day, by)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO monthly_settlement_progress(cycle_period,store_id)
 SELECT $1,id FROM stores WHERE owner_type<>'central' ORDER BY id`, period); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return n == 1, nil
}

func (r *FinanceRepository) RunMonthlySettlementForPeriod(ctx context.Context, period, by string) (*MonthlySettlementCycle, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	month, err := time.Parse("2006-01", period)
	if err != nil || month.Format("2006-01") != period {
		return nil, ErrInvalidSettlementPeriod
	}
	settings, err := r.GetSettlementSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings.SettlementDay == nil {
		return nil, ErrSettlementNotConfigured
	}
	created, err := r.initializeMonthlyCycle(ctx, period, effectiveSettlementDay(*settings.SettlementDay, month.Year(), month.Month()), by)
	if err != nil {
		return nil, err
	}
	c, err := r.GetMonthlySettlementCycle(ctx, period)
	if err != nil {
		return nil, err
	}
	if c.Status == "completed" {
		c.AlreadyCompleted = !created
		return c, nil
	}
	if !c.ProgressKnown {
		return nil, ErrSettlementReconciliation
	}
	return r.processMonthlyBatch(ctx, period)
}

func (r *FinanceRepository) runMonthlyScheduled(ctx context.Context, now time.Time, by string) (*MonthlySettlementCycle, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	settings, err := r.GetSettlementSettings(ctx)
	if err != nil {
		return nil, err
	}
	// An explicitly unset setting retains its existing pause behavior.
	if settings.SettlementDay == nil {
		return nil, ErrSettlementNotConfigured
	}
	due, day, inWindow := settlementWindowPeriod(now, *settings.SettlementDay)
	if inWindow {
		if _, err = r.initializeMonthlyCycle(ctx, due, day, by); err != nil {
			return nil, err
		}
	}
	var period string
	err = r.db.QueryRowContext(ctx, `SELECT period FROM monthly_settlement_cycles WHERE status<>'completed'
 ORDER BY last_attempted_at NULLS FIRST,period LIMIT 1`).Scan(&period)
	if err == sql.ErrNoRows {
		if inWindow {
			return r.GetMonthlySettlementCycle(ctx, due)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Yield even when reading this cycle exposes invalid historical totals or
	// missing progress. Such a cycle cannot monopolize all later worker ticks.
	if _, err := r.db.ExecContext(ctx, `UPDATE monthly_settlement_cycles SET last_attempted_at=NOW() WHERE period=$1 AND status<>'completed'`, period); err != nil {
		return nil, err
	}
	c, err := r.GetMonthlySettlementCycle(ctx, period)
	if err != nil {
		return nil, err
	}
	if !c.ProgressKnown {
		return nil, ErrSettlementReconciliation
	}
	return r.processMonthlyBatch(ctx, period)
}

func (r *FinanceRepository) processMonthlyBatch(ctx context.Context, period string) (*MonthlySettlementCycle, error) {
	if _, err := r.db.ExecContext(ctx, `UPDATE monthly_settlement_cycles SET last_attempted_at=NOW() WHERE period=$1 AND status<>'completed'`, period); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT store_id FROM monthly_settlement_progress WHERE cycle_period=$1 AND state<>'done'
 AND next_attempt_at<=NOW() ORDER BY next_attempt_at,store_id LIMIT $2`, period, monthlyBatchLimit)
	if err != nil {
		return nil, err
	}
	var accounts []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		accounts = append(accounts, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range accounts {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err = r.settleMonthlyAccount(ctx, period, id); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			state, reason, delay := "pending", "temporary_error", 5
			if errors.Is(err, ErrFinancePrincipalUnresolved) || errors.Is(err, pricing.ErrInvalidMoney) || errors.Is(err, ErrSettlementReconciliation) {
				state, reason, delay = "blocked", "reconciliation_required", 60
			}
			if errors.Is(err, context.DeadlineExceeded) || isMonthlyBusy(err) {
				reason = "busy"
			}
			if errors.Is(err, sql.ErrNoRows) {
				state, reason = "done", "ineligible"
			}
			// Separate bounded metadata-only transaction; never acquire an account
			// after this work row and never overwrite a concurrent successful finish.
			recordCtx, stop := context.WithTimeout(ctx, 250*time.Millisecond)
			_, recordErr := r.db.ExecContext(recordCtx, `UPDATE monthly_settlement_progress SET state=$3,reason=$4,
 attempts=attempts+1,last_attempted_at=NOW(),next_attempt_at=NOW()+$5*INTERVAL '1 second'
 WHERE cycle_period=$1 AND store_id=$2 AND state<>'done'`, period, id, state, reason, delay)
			stop()
			if recordErr != nil {
				return nil, recordErr
			}
		}
	}
	// No account locks in this finalizer. Completed is terminal, including when
	// an older concurrent statement wakes after a newer finalizer completed.
	_, err = r.db.ExecContext(ctx, `WITH work AS (SELECT count(*) FILTER(WHERE state='pending') pending,count(*) FILTER(WHERE state='blocked') blocked
 FROM monthly_settlement_progress WHERE cycle_period=$1), items AS (
 SELECT count(*) n,COALESCE(sum(gross_amount),0) gross,COALESCE(sum(net_amount),0) net,
 count(*) FILTER(WHERE status='skipped_no_bank_details') skipped FROM monthly_settlement_items WHERE cycle_period=$1)
 UPDATE monthly_settlement_cycles c SET status=CASE WHEN items.gross>$2 OR items.net>$2 THEN 'blocked' WHEN work.pending+work.blocked=0 THEN 'completed'
 WHEN work.pending=0 THEN 'blocked' ELSE 'running' END,total_stores=items.n,total_gross=items.gross,total_net=items.net,skipped_stores=items.skipped
 FROM work,items WHERE c.period=$1 AND c.progress_initialized AND c.status<>'completed'`, period, pricing.MaxTransportMoney)
	if err != nil {
		return nil, err
	}
	return r.GetMonthlySettlementCycle(ctx, period)
}

func isMonthlyBusy(err error) bool {
	var state interface{ SQLState() string }
	return errors.As(err, &state) && (state.SQLState() == "55P03" || state.SQLState() == "57014")
}

func (r *FinanceRepository) settleMonthlyAccount(parent context.Context, period, id string) error {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT set_config('lock_timeout','100ms',true),set_config('statement_timeout','750ms',true)`); err != nil {
		return err
	}
	var locks TransactionLockSet
	if err = LockAccountsTx(ctx, tx, &locks, FinancialAccount{Type: "seller", ID: id}); err != nil {
		return err
	}
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM monthly_settlement_progress WHERE cycle_period=$1 AND store_id=$2`, period, id).Scan(&state); err != nil {
		return err
	}
	if state == "done" {
		return nil
	}
	var ownerType, name string
	if err = tx.QueryRowContext(ctx, `SELECT owner_type,name FROM stores WHERE id=$1`, id).Scan(&ownerType, &name); err != nil {
		return err
	}
	reason := "ineligible"
	var amount int64
	var payoutID string
	if ownerType != "central" {
		var existing bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM monthly_settlement_items WHERE cycle_period=$1 AND store_id=$2)`, period, id).Scan(&existing); err != nil {
			return err
		}
		if existing {
			return ErrSettlementReconciliation
		}
		amount, err = BalanceTx(ctx, tx, FinancialAccount{Type: "seller", ID: id})
		if err != nil {
			return err
		}
		if amount > pricing.MaxTransportMoney {
			return pricing.ErrInvalidMoney
		}
		reason = "no_balance"
		if amount > 0 {
			var shaba, bank string
			err = tx.QueryRowContext(ctx, `SELECT shaba,bank_name FROM payout_requests WHERE store_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, id).Scan(&shaba, &bank)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			status := "skipped_no_bank_details"
			reason = "missing_bank"
			var link any
			if strings.TrimSpace(shaba) != "" {
				payoutID = "pay-req-" + uuid.NewString()
				link = payoutID
				status, reason = "pending_payout", "reserved"
				if _, err = tx.ExecContext(ctx, `INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status,available_balance,created_at)
 VALUES($1,$2,'seller',$3,$4,$5,$6,'pending',$6,NOW())`, payoutID, id, name, shaba, bank, amount); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO monthly_settlement_items(id,cycle_period,store_id,gross_amount,fee_amount,net_amount,payout_request_id,status)
 VALUES($1,$2,$3,$4,0,$4,$5,$6)`, "msi-"+uuid.NewString(), period, id, amount, link, status); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE monthly_settlement_progress SET state='done',reason=$3,attempts=attempts+1,last_attempted_at=NOW()
 WHERE cycle_period=$1 AND store_id=$2`, period, id, reason); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if reason == "missing_bank" {
		notifySettlementSkipped(ctx, r.db, id, period, amount)
	}
	if reason == "reserved" {
		notifySettlementCreated(ctx, r.db, id, period, payoutID, amount)
	}
	return nil
}
