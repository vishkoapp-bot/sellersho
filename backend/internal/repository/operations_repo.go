package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"github.com/drup/platform/internal/jobrunner"
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
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM orders WHERE NOT EXISTS(SELECT 1 FROM background_job_candidate_retry b WHERE b.job='unpaid-order-expiry' AND b.candidate=orders.id AND b.next_retry_at>clock_timestamp()) AND NOT EXISTS(SELECT 1 FROM order_expiry_review_state e WHERE e.order_id=orders.id AND e.next_retry_at>clock_timestamp()) AND payment_status='pending' AND status IN ('pending','processing') AND NOT inventory_captured AND payment_expires_at<=NOW() ORDER BY payment_expires_at,id COLLATE "C" LIMIT 200`)
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
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
		candidateCtx, candidateCancel := context.WithTimeout(ctx, 300*time.Millisecond)
		o, err := r.expireUnpaidOrder(candidateCtx, id)
		contextErr := candidateCtx.Err() // capture before our own cancellation
		candidateCancel()
		if err != nil {
			failures = append(failures, err)
			var scheduleErr error
			if expiryCandidateTransient(err, contextErr) {
				_, scheduleErr = r.db.ExecContext(ctx, `INSERT INTO background_job_candidate_retry(job,candidate,next_retry_at,reason) VALUES('unpaid-order-expiry',$1,clock_timestamp()+INTERVAL '5 seconds','transient') ON CONFLICT(job,candidate) DO UPDATE SET next_retry_at=EXCLUDED.next_retry_at,reason=EXCLUDED.reason`, id)
			} else {
				_, scheduleErr = r.db.ExecContext(ctx, `INSERT INTO order_expiry_review_state(order_id,next_retry_at,reason) VALUES($1,clock_timestamp()+INTERVAL '5 minutes','candidate_rejected') ON CONFLICT(order_id) DO UPDATE SET next_retry_at=EXCLUDED.next_retry_at,reason=EXCLUDED.reason`, id)
			}
			if scheduleErr != nil {
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

// Every leaf must be an ordinary cancellation or known PostgreSQL retry.
// Driver rollback/connection sentinels additionally require the candidate's
// captured context to have ended; arbitrary errors or our own cleanup cancel
// do not provide that evidence. Semantic metadata failures stay review-held.
func expiryCandidateTransient(err, contextErr error) bool {
	candidateEnded := contextErr == context.Canceled || contextErr == context.DeadlineExceeded
	var transient func(error) bool
	transient = func(part error) bool {
		if part == nil || errors.Is(part, jobrunner.ErrMetadata) {
			return false
		}
		knownSQL := false
		if state, ok := part.(interface{ SQLState() string }); ok {
			switch state.SQLState() {
			case "57014", "55P03", "40001", "40P01":
				knownSQL = true
			default:
				return false
			}
		}
		if joined, ok := part.(interface{ Unwrap() []error }); ok {
			children := joined.Unwrap()
			if len(children) == 0 {
				return knownSQL
			}
			for _, child := range children {
				if !transient(child) {
					return false
				}
			}
			return true
		}
		if wrapped, ok := part.(interface{ Unwrap() error }); ok {
			if child := wrapped.Unwrap(); child != nil {
				return transient(child)
			}
		}
		switch part {
		case context.DeadlineExceeded, context.Canceled:
			return true
		case sql.ErrTxDone, driver.ErrBadConn:
			return candidateEnded
		}
		return knownSQL
	}
	return transient(err)
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
	res, err := db.ExecContext(ctx, `WITH due AS (SELECT id FROM qc_discrepancies WHERE status='open' AND responded_at IS NULL AND response_due_at<=NOW() AND escalated_at IS NULL ORDER BY response_due_at,id LIMIT 100 FOR UPDATE SKIP LOCKED) UPDATE qc_discrepancies q SET escalated_at=NOW() FROM due WHERE q.id=due.id`)
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
	rows, err := db.QueryContext(ctx, `SELECT s.id FROM suppliers s LEFT JOIN supplier_sla_metrics m ON m.supplier_id=s.id WHERE NOT EXISTS(SELECT 1 FROM background_job_candidate_retry r WHERE r.job='supplier-sla' AND r.candidate=s.id AND r.next_retry_at>clock_timestamp()) ORDER BY m.assessed_at ASC NULLS FIRST,s.id LIMIT 100`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	var total int64
	var failures []error
	for _, id := range ids {
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
		candidateCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		n, err := refreshSupplierSLACandidate(candidateCtx, db, id)
		cancel()
		if err != nil {
			failures = append(failures, err)
			if retryErr := RecordJobCandidateRetry(ctx, db, "supplier-sla", id); retryErr != nil {
				failures = append(failures, retryErr)
			}
			continue
		}
		total += n
	}
	return total, errors.Join(failures...)
}
func refreshSupplierSLACandidate(ctx context.Context, db *sql.DB, supplierID string) (int64, error) {
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
 WHERE ss.supplier_id=$1 AND ss.created_at>=NOW()-INTERVAL '90 days' AND o.payment_status='paid' AND o.status<>'cancelled'
 AND (ss.shipped_at IS NOT NULL OR ss.status='failed' OR ss.created_at+make_interval(days=>GREATEST(1,LEAST(30,COALESCE((SELECT MAX(p.preparation_days) FROM order_items oi JOIN products p ON p.id=oi.product_id WHERE oi.order_id=ss.order_id AND p.supplier_id=ss.supplier_id),1))))<=NOW()) GROUP BY ss.supplier_id)
 INSERT INTO supplier_sla_metrics(supplier_id,samples,on_time,score,eligible)
 SELECT s.id,COALESCE(n,0),COALESCE(good,0),CASE WHEN n>0 THEN good*100.0/n ELSE 0 END,
 COALESCE(n>0 AND good*100.0/n>98 AND s.approval_status='approved' AND u.is_active,FALSE)
 FROM suppliers s LEFT JOIN samples ON samples.supplier_id=s.id LEFT JOIN users u ON u.id=s.owner_user_id WHERE s.id=$1
 ON CONFLICT(supplier_id) DO UPDATE SET samples=EXCLUDED.samples,on_time=EXCLUDED.on_time,score=EXCLUDED.score,eligible=EXCLUDED.eligible,assessed_at=NOW()`, supplierID)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO supplier_shipping_methods(owner_user_id,method,status,requested_at,decided_at,decided_by)
 SELECT s.owner_user_id::text,'direct','enabled',NOW(),NOW(),'system:sla' FROM supplier_sla_metrics m JOIN suppliers s ON s.id=m.supplier_id WHERE m.supplier_id=$1 AND m.eligible AND s.owner_user_id IS NOT NULL
 ON CONFLICT(owner_user_id,method) DO UPDATE SET status='enabled',decided_at=NOW(),decided_by='system:sla' WHERE supplier_shipping_methods.decided_by='system:sla' OR (supplier_shipping_methods.status='requested' AND COALESCE(supplier_shipping_methods.decided_by,'')='')`, supplierID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE supplier_shipping_methods sm SET status='disabled',decided_at=NOW() FROM suppliers s JOIN supplier_sla_metrics m ON m.supplier_id=s.id WHERE s.id=$1 AND sm.owner_user_id=s.owner_user_id::text AND sm.method='direct' AND sm.decided_by='system:sla' AND NOT m.eligible`, supplierID)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

func RecordOperationsJob(ctx context.Context, db *sql.DB, job string, started time.Time, n int64, runErr error) error {
	message := ""
	if runErr != nil {
		message = "job_failed"
	}
	_, err := db.ExecContext(ctx, `INSERT INTO operations_job_runs(job,affected,error,started_at,finished_at) VALUES($1,$2,$3,$4,NOW())`, job, n, message, started)
	return err
}

// Each cleanup table has a finite oldest-first pass and never waits on a row
// held by a live request. No retention beyond the existing expiry predicates.
func CleanupExpiredAds(ctx context.Context, db *sql.DB) (int64, error) {
	var total int64
	for _, q := range []string{
		`WITH expired AS (SELECT ctid FROM ad_request_limits WHERE bucket<NOW()-INTERVAL '10 minutes' ORDER BY bucket LIMIT 2000 FOR UPDATE SKIP LOCKED) DELETE FROM ad_request_limits a USING expired e WHERE a.ctid=e.ctid`,
		`WITH expired AS (SELECT ctid FROM ad_fraud_keys WHERE expires_at<NOW()-INTERVAL '1 hour' ORDER BY expires_at LIMIT 2000 FOR UPDATE SKIP LOCKED) DELETE FROM ad_fraud_keys a USING expired e WHERE a.ctid=e.ctid`,
	} {
		res, err := db.ExecContext(ctx, q)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
