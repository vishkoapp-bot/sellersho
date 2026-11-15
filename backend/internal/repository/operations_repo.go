package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrPaymentExpired = errors.New("payment window has expired or order was cancelled")
var ErrQCResponseExpired = errors.New("مهلت ۴۸ساعته پاسخ به مغایرت تمام شده؛ پرونده برای بررسی مدیر ارجاع شده است")

type ExpiredOrder struct {
	ID             string
	Products       []string
	CustomerUserID string
	OrderNumber    string
}

func (r *OrderRepository) PaymentDeadline(ctx context.Context, id string) *sql.Row {
	return r.db.QueryRowContext(ctx, `SELECT payment_expires_at FROM orders WHERE id=$1`, id)
}

// Each order is one transaction: acquiring a later order's account after an
// earlier order's products would invert the global financial lock prefix.
// Committed results are returned even if a later order fails, for Redis cleanup.
func (r *OrderRepository) ExpireUnpaidOrders(ctx context.Context) ([]ExpiredOrder, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM orders WHERE NOT EXISTS(SELECT 1 FROM order_expiry_review_state e WHERE e.order_id=orders.id AND e.next_retry_at>clock_timestamp()) AND payment_status='pending' AND status IN ('pending','processing') AND NOT inventory_captured AND payment_expires_at<=NOW() ORDER BY id COLLATE "C" LIMIT 200`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []ExpiredOrder{}
	var failures []error
	for _, id := range ids {
		o, err := r.expireUnpaidOrder(ctx, id)
		if err != nil {
			failures = append(failures, err)
			if _, scheduleErr := r.db.ExecContext(ctx, `INSERT INTO order_expiry_review_state(order_id,next_retry_at,reason) VALUES($1,clock_timestamp()+INTERVAL '5 minutes','candidate_rejected') ON CONFLICT(order_id) DO UPDATE SET next_retry_at=EXCLUDED.next_retry_at,reason=EXCLUDED.reason`, id); scheduleErr != nil {
				failures = append(failures, scheduleErr)
			}
			continue
		}
		if o != nil {
			out = append(out, *o)
		}
	}
	return out, errors.Join(failures...)
}
func (r *OrderRepository) expireUnpaidOrder(ctx context.Context, id string) (*ExpiredOrder, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var o ExpiredOrder
	err = tx.QueryRowContext(ctx, `SELECT id,COALESCE(customer_user_id::text,''),order_number FROM orders WHERE id=$1 AND payment_status='pending' AND status IN ('pending','processing') AND NOT inventory_captured AND payment_expires_at<=NOW() FOR UPDATE SKIP LOCKED`, id).Scan(&o.ID, &o.CustomerUserID, &o.OrderNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = lockOrderFinanceTx(ctx, tx, id, true); err != nil {
		return nil, err
	}
	if err = assertOrderCancellationAllowedTx(ctx, tx, id); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT product_id FROM order_items WHERE order_id=$1`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p string
		if err = rows.Scan(&p); err != nil {
			rows.Close()
			return nil, err
		}
		o.Products = append(o.Products, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE orders SET status='cancelled',updated_at=NOW() WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err = synchronizeVerifiedPaymentFactsTx(ctx, tx, id); err != nil {
		return nil, err
	}
	if err = ensureCancellationRefundsTx(ctx, tx, id, "system:payment-expiry"); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE supplier_shipments SET status='cancelled',updated_at=NOW() WHERE order_id=$1 AND status IN ('pending','processing')`, id); err != nil {
		return nil, err
	}
	var confirmed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_payment_receipts WHERE order_id=$1)`, id).Scan(&confirmed); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	// The old expiry notice specifically says unpaid. Confirmed money must not
	// receive that misleading notice; its persisted refund summary is authoritative.
	if !confirmed {
		notifyExpiredOrder(ctx, r.db, o.ID, o.OrderNumber, o.CustomerUserID)
	}
	return &o, nil
}

// Expiry escalates for human review; silence never accepts a shortage or
// authorizes a financial adjustment. The predicate makes every retry safe.
func EscalateQCDeadlines(ctx context.Context, db *sql.DB) (int64, error) {
	res, err := db.ExecContext(ctx, `UPDATE qc_discrepancies SET escalated_at=NOW() WHERE status='open' AND responded_at IS NULL AND response_due_at<=NOW() AND escalated_at IS NULL`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Recalculate actual on-time dispatches over the last 90 days. No-history
// suppliers receive no automatic grant from the legacy default score of 100.
// Explicit administrative decisions are preserved; only system-owned grants
// are withdrawn automatically on degradation or account suspension.
func RefreshSupplierSLA(ctx context.Context, db *sql.DB) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(90261109)`); err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `WITH samples AS (
 SELECT ss.supplier_id,COUNT(*)::int AS n,
 COUNT(*) FILTER(WHERE ss.shipped_at IS NOT NULL AND ss.shipped_at<=ss.created_at+make_interval(days=>GREATEST(1,LEAST(30,COALESCE((SELECT MAX(p.preparation_days) FROM order_items oi JOIN products p ON p.id=oi.product_id WHERE oi.order_id=ss.order_id AND p.supplier_id=ss.supplier_id),1)))) AND ss.status<>'failed')::int AS good
 FROM supplier_shipments ss JOIN orders o ON o.id=ss.order_id
 WHERE ss.created_at>=NOW()-INTERVAL '90 days' AND o.payment_status='paid' AND o.status<>'cancelled'
 AND (ss.shipped_at IS NOT NULL OR ss.status='failed' OR ss.created_at+make_interval(days=>GREATEST(1,LEAST(30,COALESCE((SELECT MAX(p.preparation_days) FROM order_items oi JOIN products p ON p.id=oi.product_id WHERE oi.order_id=ss.order_id AND p.supplier_id=ss.supplier_id),1))))<=NOW()) GROUP BY ss.supplier_id)
 INSERT INTO supplier_sla_metrics(supplier_id,samples,on_time,score,eligible)
 SELECT s.id,COALESCE(n,0),COALESCE(good,0),CASE WHEN n>0 THEN good*100.0/n ELSE 0 END,
 COALESCE(n>0 AND good*100.0/n>98 AND s.approval_status='approved' AND u.is_active,FALSE)
 FROM suppliers s LEFT JOIN samples ON samples.supplier_id=s.id LEFT JOIN users u ON u.id=s.owner_user_id
 ON CONFLICT(supplier_id) DO UPDATE SET samples=EXCLUDED.samples,on_time=EXCLUDED.on_time,score=EXCLUDED.score,eligible=EXCLUDED.eligible,assessed_at=NOW()`)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO supplier_shipping_methods(owner_user_id,method,status,requested_at,decided_at,decided_by)
 SELECT s.owner_user_id::text,'direct','enabled',NOW(),NOW(),'system:sla' FROM supplier_sla_metrics m JOIN suppliers s ON s.id=m.supplier_id WHERE m.eligible AND s.owner_user_id IS NOT NULL
 ON CONFLICT(owner_user_id,method) DO UPDATE SET status='enabled',decided_at=NOW(),decided_by='system:sla' WHERE supplier_shipping_methods.decided_by='system:sla' OR (supplier_shipping_methods.status='requested' AND COALESCE(supplier_shipping_methods.decided_by,'')='')`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	_, err = tx.ExecContext(ctx, `UPDATE supplier_shipping_methods sm SET status='disabled',decided_at=NOW() FROM suppliers s JOIN supplier_sla_metrics m ON m.supplier_id=s.id WHERE sm.owner_user_id=s.owner_user_id::text AND sm.method='direct' AND sm.decided_by='system:sla' AND NOT m.eligible`)
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

func RecordOperationsJob(ctx context.Context, db *sql.DB, job string, started time.Time, n int64, runErr error) {
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	_, _ = db.ExecContext(ctx, `INSERT INTO operations_job_runs(job,affected,error,started_at,finished_at) VALUES($1,$2,$3,$4,NOW())`, job, n, message, started)
}
