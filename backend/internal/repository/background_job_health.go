package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type JobHealth struct {
	AutomaticRetry     int64      `json:"automaticRetry"`
	Job                string     `json:"job"`
	Running            bool       `json:"running"`
	Missing            bool       `json:"missing"`
	Stale              bool       `json:"stale"`
	Disabled           bool       `json:"disabled"`
	Error              string     `json:"error"`
	LastSuccess        *time.Time `json:"lastSuccess"`
	Started            *time.Time `json:"started"`
	Finished           *time.Time `json:"finished"`
	Cutover            *time.Time `json:"cutover"`
	Runs               int64      `json:"runs"`
	Affected           int64      `json:"affected"`
	Ready              int64      `json:"ready"`
	Review             int64      `json:"review"`
	Capped             bool       `json:"capped"`
	OldestReadySeconds int64      `json:"oldestReadySeconds"`
}

// The panel always enumerates expected jobs, even before their first run.
// Counts stop at 1001 (shown as 1000+) and never hide review rows in ready work.
func BackgroundJobHealth(ctx context.Context, db *sql.DB) ([]JobHealth, error) {
	definitions := []struct {
		id              string
		period, timeout time.Duration
	}{
		{"unpaid-order-expiry", 5 * time.Second, 3 * time.Second}, {"ad-cleanup", 10 * time.Second, 2 * time.Second},
		{"qc-response-deadline", 30 * time.Second, 2 * time.Second}, {"supplier-sla", 30 * time.Second, 3 * time.Second},
		{"delivery-lock-releaser", time.Minute, 10 * time.Second}, {"monthly-settlement", time.Minute, 4 * time.Second},
	}
	out := make([]JobHealth, 0, len(definitions))
	for _, d := range definitions {
		h := JobHealth{Job: d.id}
		var lease *time.Time
		var owner sql.NullString
		err := db.QueryRowContext(ctx, `SELECT owner,lease_until,started_at,finished_at,last_success_at,last_error,disabled,runs,affected,cutover_at FROM background_job_state WHERE job=$1`, d.id).Scan(&owner, &lease, &h.Started, &h.Finished, &h.LastSuccess, &h.Error, &h.Disabled, &h.Runs, &h.Affected, &h.Cutover)
		if errors.Is(err, sql.ErrNoRows) {
			h.Missing = true
		} else if err != nil {
			return nil, err
		}
		h.Running = owner.Valid && lease != nil && lease.After(time.Now())
		if d.id == "monthly-settlement" {
			if err = db.QueryRowContext(ctx, `SELECT COALESCE((SELECT settlement_day IS NULL FROM platform_settings ORDER BY id LIMIT 1),TRUE)`).Scan(&h.Disabled); err != nil {
				return nil, err
			}
		}
		h.Stale = !h.Disabled && (h.LastSuccess == nil || time.Since(*h.LastSuccess) > 3*d.period+d.timeout)
		if !h.Running && h.Started != nil && (h.Finished == nil || h.Finished.Before(*h.Started)) {
			h.Error = "outcome_missing"
		}
		if err = jobBacklog(ctx, db, &h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}
func jobBacklog(ctx context.Context, db *sql.DB, h *JobHealth) error {
	var ready, review string
	switch h.Job {
	case "unpaid-order-expiry":
		eligible := ` FROM orders o WHERE payment_status='pending' AND status IN ('pending','processing') AND NOT inventory_captured`
		ready = `SELECT payment_expires_at AS due` + eligible + ` AND payment_expires_at<=NOW() AND NOT EXISTS(SELECT 1 FROM order_expiry_review_state r WHERE r.order_id=o.id AND r.next_retry_at>clock_timestamp()) ORDER BY payment_expires_at LIMIT 1001`
		review = `SELECT 1` + eligible + ` AND (payment_expires_at IS NULL OR EXISTS(SELECT 1 FROM order_expiry_review_state r WHERE r.order_id=o.id AND r.next_retry_at>clock_timestamp())) LIMIT 1001`
	case "qc-response-deadline":
		ready = `SELECT response_due_at AS due FROM qc_discrepancies WHERE status='open' AND responded_at IS NULL AND response_due_at<=NOW() AND escalated_at IS NULL ORDER BY response_due_at LIMIT 1001`
		review = `SELECT 1 FROM qc_discrepancies WHERE status='open' AND responded_at IS NULL AND escalated_at IS NOT NULL LIMIT 1001`
	case "delivery-lock-releaser":
		ready = `SELECT due FROM (SELECT shipped_at+INTERVAL '20 days' AS due FROM orders o WHERE status IN ('shipped','delivered_unconfirmed') AND NOT profit_released AND shipped_at<=NOW()-INTERVAL '20 days' AND NOT EXISTS(SELECT 1 FROM background_job_candidate_retry r WHERE r.job='delivery-confirm' AND r.candidate=o.id AND r.next_retry_at>clock_timestamp()) UNION ALL SELECT return_window_ends_at AS due FROM orders o WHERE status='delivered_confirmed' AND NOT profit_released AND return_window_ends_at<=NOW() AND NOT EXISTS(SELECT 1 FROM background_job_candidate_retry r WHERE r.job='delivery-release' AND r.candidate=o.id AND r.next_retry_at>clock_timestamp())) q ORDER BY due LIMIT 1001`
		review = `SELECT 1 FROM orders o WHERE NOT profit_released AND ((status='delivered_confirmed' AND return_window_ends_at IS NULL) OR (status IN ('shipped','delivered_unconfirmed','delivered_confirmed') AND EXISTS(SELECT 1 FROM background_job_candidate_retry r WHERE r.job IN ('delivery-confirm','delivery-release') AND r.candidate=o.id AND r.next_retry_at>clock_timestamp()))) LIMIT 1001`
	case "monthly-settlement":
		ready = `SELECT p.next_attempt_at AS due FROM monthly_settlement_progress p JOIN monthly_settlement_cycles c ON c.period=p.cycle_period WHERE c.status<>'completed' AND p.state='pending' AND p.next_attempt_at<=NOW() ORDER BY p.next_attempt_at LIMIT 1001`
		review = `SELECT 1 FROM monthly_settlement_progress p JOIN monthly_settlement_cycles c ON c.period=p.cycle_period WHERE c.status<>'completed' AND p.state='blocked' LIMIT 1001`
	case "supplier-sla":
		ready = `SELECT COALESCE(m.assessed_at+INTERVAL '30 seconds',s.created_at) AS due FROM suppliers s LEFT JOIN supplier_sla_metrics m ON m.supplier_id=s.id WHERE (m.assessed_at IS NULL OR m.assessed_at<=NOW()-INTERVAL '30 seconds') AND NOT EXISTS(SELECT 1 FROM background_job_candidate_retry r WHERE r.job='supplier-sla' AND r.candidate=s.id AND r.next_retry_at>clock_timestamp()) ORDER BY due LIMIT 1001`
		review = `SELECT 1 FROM background_job_candidate_retry r JOIN suppliers s ON s.id=r.candidate WHERE r.job='supplier-sla' AND r.next_retry_at>clock_timestamp() LIMIT 1001`
	case "ad-cleanup":
		ready = `SELECT due FROM (SELECT bucket+INTERVAL '10 minutes' AS due FROM ad_request_limits WHERE bucket<NOW()-INTERVAL '10 minutes' UNION ALL SELECT expires_at+INTERVAL '1 hour' AS due FROM ad_fraud_keys WHERE expires_at<NOW()-INTERVAL '1 hour')q ORDER BY due LIMIT 1001`
	}
	if ready != "" {
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(GREATEST(0,EXTRACT(EPOCH FROM clock_timestamp()-MIN(due)))::bigint,0) FROM (`+ready+`) q`).Scan(&h.Ready, &h.OldestReadySeconds); err != nil {
			return err
		}
	}
	if review != "" {
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+review+`) q`).Scan(&h.Review); err != nil {
			return err
		}
	}
	if h.Job == "unpaid-order-expiry" {
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT 1 FROM background_job_candidate_retry b JOIN orders o ON o.id=b.candidate WHERE b.job='unpaid-order-expiry' AND b.reason='transient' AND b.next_retry_at>clock_timestamp() AND o.payment_status='pending' AND o.status IN ('pending','processing') AND NOT o.inventory_captured AND o.payment_expires_at<=NOW() LIMIT 1001)q`).Scan(&h.AutomaticRetry); err != nil {
			return err
		}
	}
	h.Capped = h.Ready > 1000 || h.Review > 1000 || h.AutomaticRetry > 1000
	return nil
}
