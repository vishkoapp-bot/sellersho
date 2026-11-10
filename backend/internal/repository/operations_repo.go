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
	ID       string
	Products []string
}

func (r *OrderRepository) PaymentDeadline(ctx context.Context, id string) *sql.Row {
	return r.db.QueryRowContext(ctx, `SELECT payment_expires_at FROM orders WHERE id=$1`, id)
}

// The order row is the serialization point shared with MarkPaymentPaid.
// No stock is added: these unpaid orders have never captured any stock.
func (r *OrderRepository) ExpireUnpaidOrders(ctx context.Context) ([]ExpiredOrder, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM orders WHERE payment_status='pending' AND status IN ('pending','processing') AND NOT inventory_captured AND payment_expires_at<=NOW() ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 200`)
	if err != nil {
		return nil, err
	}
	out := []ExpiredOrder{}
	for rows.Next() {
		var o ExpiredOrder
		if err = rows.Scan(&o.ID); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		o := &out[i]
		pr, e := tx.QueryContext(ctx, `SELECT DISTINCT product_id FROM order_items WHERE order_id=$1`, o.ID)
		if e != nil {
			return nil, e
		}
		for pr.Next() {
			var id string
			if e = pr.Scan(&id); e != nil {
				pr.Close()
				return nil, e
			}
			o.Products = append(o.Products, id)
		}
		e = pr.Err()
		pr.Close()
		if e != nil {
			return nil, e
		}
		if _, err = tx.ExecContext(ctx, `UPDATE orders SET status='cancelled',payment_status='cancelled',updated_at=NOW() WHERE id=$1`, o.ID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE payment_transactions SET status='failed' WHERE order_id=$1 AND status='pending'`, o.ID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE supplier_shipments SET status='cancelled',updated_at=NOW() WHERE order_id=$1 AND status IN ('pending','processing')`, o.ID); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
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
