package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/lib/pq"
	"math/rand/v2"
	"time"
)

var (
	ErrCouponExhausted         = errors.New("coupon_exhausted")
	ErrCouponInvalid           = errors.New("coupon_invalid")
	ErrCouponChanged           = errors.New("coupon_changed")
	ErrCouponEvidence          = errors.New("coupon_evidence_required")
	ErrCouponLifecycleRequired = errors.New("coupon_lifecycle_required")
)

func markCouponWriterTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT set_config('sellersho.fin007_writer','v1',true)`)
	return err
}
func orderHasCouponTx(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	var has bool
	err := tx.QueryRowContext(ctx, `SELECT btrim(COALESCE(discount_code,''))<>'' FROM orders WHERE id=$1`, id).Scan(&has)
	return has, err
}

// Only immutable migration-origin observations explain an unresolved legacy
// identity. Their INSERT fence is installed after backfill and rejects every
// runtime writer, including a caller that merely knows the v1 marker.
func unresolvedOrderCouponIssueTx(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	var found bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM orders o JOIN coupon_reconciliation_issues i ON i.order_id=o.id AND i.store_id=o.store_id AND i.code=upper(btrim(o.discount_code)) WHERE o.id=$1 AND i.reason IN ('missing_identity','ambiguous_identity') AND i.source_snapshot->>'order_id'=o.id AND i.source_snapshot->>'store_id'=o.store_id AND i.source_snapshot->>'code'=upper(btrim(o.discount_code)))`, id).Scan(&found)
	return found, err
}
func assertCouponDispatchAllowedTx(ctx context.Context, tx *sql.Tx, id string) error {
	has, err := orderHasCouponTx(ctx, tx, id)
	if err != nil || !has {
		return err
	}
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM order_coupon_reservations WHERE order_id=$1`, id).Scan(&state); err != nil {
		return fmt.Errorf("%w: %w", ErrCouponEvidence, err)
	}
	if state == "released" {
		return ErrCouponEvidence
	}
	return nil
}
func lockOrderCouponTx(ctx context.Context, tx *sql.Tx, set *TransactionLockSet, id string) error {
	has, err := orderHasCouponTx(ctx, tx, id)
	if err != nil || !has {
		return err
	}
	var coupon string
	if err = tx.QueryRowContext(ctx, `SELECT coupon_id FROM order_coupon_reservations WHERE order_id=$1`, id).Scan(&coupon); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			legacy, e := unresolvedOrderCouponIssueTx(ctx, tx, id)
			if e != nil {
				return e
			}
			if legacy {
				return markCouponWriterTx(ctx, tx)
			}
		}
		return fmt.Errorf("%w: %w", ErrCouponEvidence, err)
	}
	if err = LockTransactionRowsTx(ctx, tx, set, TransactionRowLock{Table: "seller_discount_codes", ID: coupon}); err != nil {
		return err
	}
	return markCouponWriterTx(ctx, tx)
}

// Creation holds existing FK parents compatibly, never upgrades their guards,
// and may insert only a new private order between principals and coupon.
func lockCouponCreationParentsTx(ctx context.Context, tx *sql.Tx, order domain.Order, shipments []domain.SupplierShipment, late bool) error {
	var plan []TransactionRowLock
	add := func(table, id string) {
		if id != "" {
			plan = append(plan, TransactionRowLock{Table: table, ID: id})
		}
	}
	if !late {
		add("stores", order.StoreID)
		if order.ReferrerSellerID != nil {
			add("stores", *order.ReferrerSellerID)
		}
		if order.CustomerUserID != nil {
			add("users", *order.CustomerUserID)
		}
		for _, item := range order.Items {
			if item.Routing != "" {
				add("suppliers", item.SupplierID)
			}
		}
		for _, sh := range shipments {
			if sh.OrderID != order.ID {
				return ErrCouponEvidence
			}
			add("suppliers", sh.SupplierID)
		}
	} else {
		for _, item := range order.Items {
			add("products", item.ProductID)
			if item.VariantID != nil {
				add("product_variants", *item.VariantID)
			}
			if item.Routing != "" {
				if item.WarehouseID != nil {
					add("warehouses", *item.WarehouseID)
				}
				if item.SupplierWarehouseID != nil {
					add("supplier_warehouses", *item.SupplierWarehouseID)
				}
				for _, sp := range item.OriginSplits {
					if sp.WarehouseID != nil {
						add("warehouses", *sp.WarehouseID)
					}
					if sp.SupplierWarehouseID != nil {
						add("supplier_warehouses", *sp.SupplierWarehouseID)
					}
				}
			}
		}
	}
	locks, err := normalizeTransactionLocks(plan)
	if err != nil {
		return err
	}
	for _, l := range locks {
		var id string
		if err = tx.QueryRowContext(ctx, `SELECT id FROM `+l.Table+` WHERE id=$1 FOR KEY SHARE`, l.ID).Scan(&id); err != nil {
			return err
		}
	}
	if late {

		for _, item := range order.Items {
			if item.Routing != "" && item.SupplierID != "" {
				var valid bool
				if err = tx.QueryRowContext(ctx, `SELECT supplier_id=$2 FROM products WHERE id=$1`, item.ProductID, item.SupplierID).Scan(&valid); err != nil {
					return err
				}
				if !valid {
					return ErrCouponChanged
				}
				splits := append([]domain.OrderItemOriginSplit(nil), item.OriginSplits...)
				splits = append(splits, domain.OrderItemOriginSplit{SupplierWarehouseID: item.SupplierWarehouseID})
				for _, sp := range splits {
					if sp.SupplierWarehouseID != nil {
						if err = tx.QueryRowContext(ctx, `SELECT supplier_id=$2 FROM supplier_warehouses WHERE id=$1`, *sp.SupplierWarehouseID, item.SupplierID).Scan(&valid); err != nil {
							return err
						}
						if !valid {
							return ErrCouponChanged
						}
					}
				}
			}
			if item.VariantID != nil {
				var valid bool
				if err = tx.QueryRowContext(ctx, `SELECT product_id=$2 FROM product_variants WHERE id=$1`, *item.VariantID, item.ProductID).Scan(&valid); err != nil {
					return err
				}
				if !valid {
					return ErrCouponChanged
				}
			}
		}
	}
	return nil
}
func reserveOrderCouponTx(ctx context.Context, tx *sql.Tx, o domain.Order) error {
	if normalizeDiscountCode(o.DiscountCode) == "" {
		if o.CouponDiscountAmount != nil && *o.CouponDiscountAmount != 0 {
			return ErrCouponEvidence
		}
		return nil
	}
	if o.PaymentStatus != "pending" || (o.Status != "pending" && o.Status != "processing") || o.InventoryCaptured {
		return ErrCouponEvidence
	}
	if o.CouponID == "" || o.CouponRevision <= 0 || o.CouponDiscountAmount == nil || *o.CouponDiscountAmount <= 0 || *o.CouponDiscountAmount > o.SellerDiscountAmount || (o.SellerDiscountAmount < 0 || o.SellerDiscountAmount > pricing.MaxTransportMoney) {
		return ErrCouponEvidence
	}
	bounded, cancel := couponAdmissionContext(ctx)
	defer cancel()
	var set TransactionLockSet
	if err := LockTransactionRowsTx(bounded, tx, &set, TransactionRowLock{Table: "seller_discount_codes", ID: o.CouponID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCouponChanged
		}
		return err
	}
	var store, code string
	var revision int64
	var active, window, blocked bool
	var count int64
	var max sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT store_id,code,revision,is_active,starts_at<=clock_timestamp() AND ends_at>clock_timestamp(),used_count,max_uses,EXISTS(SELECT 1 FROM coupon_reconciliation_issues i WHERE i.store_id=c.store_id AND i.code=upper(btrim(c.code)) AND i.reason IN ('missing_identity','ambiguous_identity')) FROM seller_discount_codes c WHERE id=$1`, o.CouponID).Scan(&store, &code, &revision, &active, &window, &count, &max, &blocked); err != nil {
		return err
	}
	if store != o.StoreID || normalizeDiscountCode(code) != normalizeDiscountCode(o.DiscountCode) || revision != o.CouponRevision {
		return ErrCouponChanged
	}
	if !active || !window || blocked {
		return ErrCouponInvalid
	}
	if max.Valid && count >= max.Int64 {
		return ErrCouponExhausted
	}

	// Creation has already set the transaction-local writer marker before its
	// private order INSERT. Keep all three subordinate writes in one statement;
	// the coupon guard remains held through deferred consistency and COMMIT.
	var admitted bool
	err := tx.QueryRowContext(ctx, `WITH instant AS MATERIALIZED(SELECT clock_timestamp() at),
 added AS(INSERT INTO order_coupon_reservations(order_id,coupon_id,store_id,code,seller_discount_amount,coupon_discount_amount,coupon_revision,state,evidence_kind,reason,created_at)
 SELECT $1,$2,$3,$4,$5,$6,$7,'reserved','admission','checkout',instant.at FROM instant,seller_discount_codes c WHERE c.id=$2 AND c.starts_at<=instant.at AND c.ends_at>instant.at RETURNING order_id),
 counter AS(UPDATE seller_discount_codes SET used_count=used_count+1 WHERE id=$2 AND EXISTS(SELECT 1 FROM added) RETURNING id),
 event AS(INSERT INTO order_coupon_events(order_id,event_kind,new_state,reason) SELECT order_id,'reserved','reserved','checkout' FROM added RETURNING order_id)
 SELECT EXISTS(SELECT 1 FROM event)`, o.ID, o.CouponID, o.StoreID, normalizeDiscountCode(o.DiscountCode), o.SellerDiscountAmount, *o.CouponDiscountAmount, o.CouponRevision).Scan(&admitted)
	if err != nil {
		var pg *pq.Error
		if errors.As(err, &pg) && pg.Code == "23514" && pg.Message == "FIN007 invalid admission window" {
			return fmt.Errorf("%w: %w", ErrCouponInvalid, err)
		}
		return err
	}
	if !admitted {
		return ErrCouponInvalid
	}
	return nil
}

// These transitions consume preheld order/coupon guards and do not acquire locks.
func transitionOrderCouponTx(ctx context.Context, tx *sql.Tx, id, state, reason string) error {
	var before, coupon string
	if err := tx.QueryRowContext(ctx, `SELECT state,coupon_id FROM order_coupon_reservations WHERE order_id=$1`, id).Scan(&before, &coupon); err != nil {
		return fmt.Errorf("%w: %w", ErrCouponEvidence, err)
	}
	if before == state || before == "consumed" || before == "released" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE order_coupon_reservations SET state=$2,reason=$3,updated_at=clock_timestamp() WHERE order_id=$1`, id, state, reason); err != nil {
		return err
	}
	if state == "released" {
		if _, err := tx.ExecContext(ctx, `UPDATE seller_discount_codes SET used_count=used_count-1 WHERE id=$1 AND used_count>0`, coupon); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO order_coupon_events(order_id,event_kind,prior_state,new_state,reason) VALUES($1,$2,$3,$2,$4) ON CONFLICT(order_id,event_kind) DO NOTHING`, id, state, before, reason)
	return err
}
func confirmOrderCouponTx(ctx context.Context, tx *sql.Tx, id string, verified bool) (bool, error) {
	has, err := orderHasCouponTx(ctx, tx, id)
	if err != nil || !has {
		return !has, err
	}
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM order_coupon_reservations WHERE order_id=$1`, id).Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			legacy, e := unresolvedOrderCouponIssueTx(ctx, tx, id)
			if e != nil {
				return false, e
			}
			if legacy {
				return false, nil
			}
		}
		return false, fmt.Errorf("%w: %w", ErrCouponEvidence, err)
	}
	if state == "released" {
		return false, nil
	}
	if state == "consumed" {
		return true, nil
	}
	var receipt bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_payment_receipts r JOIN order_payment_verifications v ON v.id=r.verification_id JOIN payment_transactions p ON p.id=r.payment_id WHERE r.order_id=$1 AND v.order_id=r.order_id AND p.order_id=r.order_id AND r.amount_toman=v.amount_toman AND r.customer_id IS NOT DISTINCT FROM v.customer_id AND r.funding_kind=v.funding_kind AND p.amount=r.amount_toman AND r.currency='TOMAN')`, id).Scan(&receipt); err != nil {
		return false, err
	}
	if verified && !receipt {
		return false, ErrCouponEvidence
	}
	next := "review_hold"
	reason := "unverified_payment_evidence"
	if receipt {
		next = "consumed"
		reason = "verified_payment_receipt"
	}
	return true, transitionOrderCouponTx(ctx, tx, id, next, reason)
}
func cancelOrderCouponTx(ctx context.Context, tx *sql.Tx, id, reason string) error {
	switch reason {
	case "customer_cancel", "payment_expiry", "admission_rejected", "late_payment":
	default:
		return ErrCouponEvidence
	}
	has, err := orderHasCouponTx(ctx, tx, id)
	if err != nil || !has {
		return err
	}
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM order_coupon_reservations WHERE order_id=$1`, id).Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			legacy, e := unresolvedOrderCouponIssueTx(ctx, tx, id)
			if e != nil {
				return e
			}
			if legacy {
				return nil
			}
		}
		return fmt.Errorf("%w: %w", ErrCouponEvidence, err)
	}
	if state == "consumed" || state == "released" {
		return nil
	}
	var cancelled, captured, paid, receipt, inquiry, booked bool
	if err = tx.QueryRowContext(ctx, `SELECT status='cancelled',inventory_captured,payment_status<>'pending',EXISTS(SELECT 1 FROM order_payment_receipts WHERE order_id=$1),EXISTS(SELECT 1 FROM order_payment_verifications WHERE order_id=$1),EXISTS(SELECT 1 FROM payment_transactions WHERE order_id=$1 AND status<>'pending') FROM orders WHERE id=$1`, id).Scan(&cancelled, &captured, &paid, &receipt, &inquiry, &booked); err != nil {
		return err
	}
	if !cancelled {
		return ErrCouponEvidence
	}
	next := "review_hold"
	if receipt {
		next = "consumed"
	} else if !captured && !paid && !inquiry && !booked && state != "review_hold" {
		next = "released"
	}
	return transitionOrderCouponTx(ctx, tx, id, next, reason)
}

func couponTransactionRetryable(err error) bool {
	for _, terminal := range []error{context.Canceled, context.DeadlineExceeded, ErrCouponExhausted, ErrCouponInvalid, ErrCouponChanged, ErrCouponEvidence, ErrCouponLifecycleRequired, ErrTransactionLockOrder} {
		if errors.Is(err, terminal) {
			return false
		}
	}
	var pg *pq.Error
	return errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01" || pg.Code == "55P03")
}

func couponRetryPause(ctx context.Context) error {
	timer := time.NewTimer(time.Duration(15+rand.IntN(20)) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Admission consumes its caller's existing budget. The fallback bounds legacy
// callers that provide no deadline; it does not replace the HTTP API budget.
func couponAdmissionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, bounded := ctx.Deadline(); bounded {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, 5*time.Second)
}
