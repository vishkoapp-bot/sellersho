package main

import (
	"context"
	"database/sql"
	"errors"
	"github.com/drup/platform/internal/jobrunner"
	"net/http"
	"strconv"
	"time"
)

// Team 01 (Phase 2, live parity): admin insight APIs (MASTER-AUDIT D13).
//
// Existence is confirmed (live binary; frontend calls; probes 401 for
// overview and overview/summary), but the live frontend's expected
// response schema is not documented in the Phase-1 report for most of
// these endpoints. Each handler below therefore aggregates from the
// underlying tables and its shape is marked TODO. Money-out figures
// follow Team 02's D32 rule: only payout_requests in
// completed/transfer_confirmed count as money out; approved alone
// never does.

// t01Count returns COUNT(*) for a guarded table query, or 0.
func t01Count(ctx context.Context, db *sql.DB, query string, args ...interface{}) int64 {
	var n int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0
	}
	return n
}

func t01Sum(ctx context.Context, db *sql.DB, query string, args ...interface{}) int64 {
	var n sql.NullInt64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil || !n.Valid {
		return 0
	}
	return n.Int64
}

// handleT01AdminOverview serves GET /api/v1/admin/overview: platform
// headline counts. TODO: exact live overview shape is unverified.
func handleT01AdminOverview(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"users":          t01Count(ctx, db, `SELECT COUNT(*) FROM users`),
		"stores":         t01Count(ctx, db, `SELECT COUNT(*) FROM stores`),
		"products":       t01Count(ctx, db, `SELECT COUNT(*) FROM products`),
		"orders":         t01Count(ctx, db, `SELECT COUNT(*) FROM orders`),
		"sales":          t01Sum(ctx, db, `SELECT COALESCE(SUM(payable_amount), 0) FROM orders WHERE status != 'cancelled'`),
		"pendingPayouts": t01Count(ctx, db, `SELECT COUNT(*) FROM payout_requests WHERE status = 'pending'`),
		"openTickets":    t01Count(ctx, db, `SELECT COUNT(*) FROM support_tickets WHERE status IN ('open', 'assigned', 'pending')`),
	})
}

// handleT01AdminOverviewSub serves GET /api/v1/admin/overview/<sub> for
// sub in summary|trends|funnel|leaders|alerts. Live contract is
// bundle-proven (index-MERGED.js adminApi): every sub-response wraps its
// payload in {"data": ...} (see the per-case notes). The previous shapes
// (bare summary object, {trends:[{day,orders,sales}]},
// {funnel:[{status,count}]}, {leaders:[{storeId,orders,sales}]}, alert
// count object) did not match live and crashed the rebuilt overview tab
// (Fix68): the tab reads res.data, got undefined, and the funnel footer
// read .disputed of undefined. Each sub-response below is computed from
// real tables; nothing is fabricated.
func handleT01AdminOverviewSub(w http.ResponseWriter, r *http.Request, db *sql.DB, sub string) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	switch sub {
	case "summary":
		// Live contract: {data: {todayGMV, todayOrders, todayProfit,
		// d7GMV, d7Orders, d7Profit, d30GMV, d30Profit, totalGMV,
		// totalProfit, activeStores, activeSuppliers}}. GMV is
		// SUM(payable_amount), profit is SUM(platform_profit); both
		// exclude cancelled orders. Windows use the database clock.
		var todayOrders, todayGMV, todayProfit int64
		var d7Orders, d7GMV, d7Profit int64
		var d30GMV, d30Profit int64
		var totalGMV, totalProfit int64
		err := db.QueryRowContext(ctx, `
			SELECT
				COUNT(*) FILTER (WHERE DATE(created_at) = CURRENT_DATE AND status <> 'cancelled'),
				COALESCE(SUM(payable_amount) FILTER (WHERE DATE(created_at) = CURRENT_DATE AND status <> 'cancelled'), 0),
				COALESCE(SUM(platform_profit) FILTER (WHERE DATE(created_at) = CURRENT_DATE AND status <> 'cancelled'), 0),
				COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '7 days' AND status <> 'cancelled'),
				COALESCE(SUM(payable_amount) FILTER (WHERE created_at >= NOW() - INTERVAL '7 days' AND status <> 'cancelled'), 0),
				COALESCE(SUM(platform_profit) FILTER (WHERE created_at >= NOW() - INTERVAL '7 days' AND status <> 'cancelled'), 0),
				COALESCE(SUM(payable_amount) FILTER (WHERE created_at >= NOW() - INTERVAL '30 days' AND status <> 'cancelled'), 0),
				COALESCE(SUM(platform_profit) FILTER (WHERE created_at >= NOW() - INTERVAL '30 days' AND status <> 'cancelled'), 0),
				COALESCE(SUM(payable_amount) FILTER (WHERE status <> 'cancelled'), 0),
				COALESCE(SUM(platform_profit) FILTER (WHERE status <> 'cancelled'), 0)
			FROM orders`).Scan(&todayOrders, &todayGMV, &todayProfit, &d7Orders, &d7GMV, &d7Profit, &d30GMV, &d30Profit, &totalGMV, &totalProfit)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
			"todayGMV":        todayGMV,
			"todayOrders":     todayOrders,
			"todayProfit":     todayProfit,
			"d7GMV":           d7GMV,
			"d7Orders":        d7Orders,
			"d7Profit":        d7Profit,
			"d30GMV":          d30GMV,
			"d30Profit":       d30Profit,
			"totalGMV":        totalGMV,
			"totalProfit":     totalProfit,
			"activeStores":    t01Count(ctx, db, `SELECT COUNT(*) FROM stores WHERE status <> 'suspended'`),
			"activeSuppliers": t01Count(ctx, db, `SELECT COUNT(DISTINCT supplier_id) FROM products WHERE supplier_id IS NOT NULL`),
		}})
	case "trends":
		// Live contract: {data: [{gmv, orders}]} — one point per day,
		// oldest first, zero-filled so the range always has `days`
		// points. The ?days= param (live passes 30) is honoured,
		// clamped to 1..90. Cancelled orders are excluded, matching
		// the summary's GMV definition.
		days := 30
		if q := r.URL.Query().Get("days"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n >= 1 && n <= 90 {
				days = n
			}
		}
		rows, err := db.QueryContext(ctx, `
			SELECT TO_CHAR(d::date, 'YYYY-MM-DD') AS day,
				COUNT(o.id) AS orders,
				COALESCE(SUM(o.payable_amount), 0) AS gmv
			FROM generate_series(CURRENT_DATE - (INTERVAL '1 day' * ($1 - 1)), CURRENT_DATE, INTERVAL '1 day') AS d
			LEFT JOIN orders o ON DATE(o.created_at) = d::date AND o.status <> 'cancelled'
			GROUP BY d ORDER BY d ASC`, days)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		points := []map[string]interface{}{}
		for rows.Next() {
			var day time.Time
			var orders, gmv int64
			if err := rows.Scan(&day, &orders, &gmv); err != nil {
				writeInternalError(w, err)
				return
			}
			_ = day
			points = append(points, map[string]interface{}{"gmv": gmv, "orders": orders})
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"data": points})
	case "funnel":
		// Live contract: {data: {placed, packed, shipped, delivered,
		// confirmed, disputed, cancelled}}. orders.status values in
		// this database are processing/packed/shipped/
		// delivered_confirmed/cancelled, so the mapping is: placed <-
		// processing, packed <- packed, shipped <- shipped, delivered
		// <- delivered_confirmed, cancelled <- cancelled; confirmed
		// counts orders with confirmed_at set, disputed counts orders
		// with disputed_at set. All seven keys are always present.
		statusCounts := map[string]int64{}
		rows, err := db.QueryContext(ctx, `SELECT status, COUNT(*) FROM orders GROUP BY status`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		for rows.Next() {
			var status string
			var n int64
			if err := rows.Scan(&status, &n); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			statusCounts[status] = n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		var confirmed, disputed int64
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE confirmed_at IS NOT NULL`).Scan(&confirmed)
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE disputed_at IS NOT NULL`).Scan(&disputed)
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
			"placed":    statusCounts["processing"],
			"packed":    statusCounts["packed"],
			"shipped":   statusCounts["shipped"],
			"delivered": statusCounts["delivered_confirmed"],
			"confirmed": confirmed,
			"disputed":  disputed,
			"cancelled": statusCounts["cancelled"],
		}})
	case "leaders":
		// Live contract: {data: {suppliers: [{id, name, orders, gmv}],
		// stores: [{id, name, orders, gmv}]}} ordered by gmv desc.
		// Store gmv is SUM(payable_amount) over non-cancelled orders;
		// supplier gmv is SUM(final_unit_price * quantity) over
		// order items of non-cancelled orders, via products.supplier_id.
		storeLeaders := []map[string]interface{}{}
		rows, err := db.QueryContext(ctx, `
			SELECT s.id, s.name, COUNT(o.id) AS orders,
				COALESCE(SUM(o.payable_amount), 0) AS gmv
			FROM orders o JOIN stores s ON s.id = o.store_id
			WHERE o.status <> 'cancelled'
			GROUP BY s.id, s.name ORDER BY gmv DESC LIMIT 10`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		for rows.Next() {
			var id, name string
			var orders, gmv int64
			if err := rows.Scan(&id, &name, &orders, &gmv); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			storeLeaders = append(storeLeaders, map[string]interface{}{"id": id, "name": name, "orders": orders, "gmv": gmv})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		supplierLeaders := []map[string]interface{}{}
		rows, err = db.QueryContext(ctx, `
			SELECT p.supplier_id, sp.name, COUNT(DISTINCT oi.order_id) AS orders,
				COALESCE(SUM(oi.final_unit_price * oi.quantity), 0) AS gmv
			FROM order_items oi
			JOIN orders o ON o.id = oi.order_id AND o.status <> 'cancelled'
			JOIN products p ON p.id = oi.product_id
			JOIN suppliers sp ON sp.id = p.supplier_id
			GROUP BY p.supplier_id, sp.name ORDER BY gmv DESC LIMIT 10`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		for rows.Next() {
			var id, name string
			var orders, gmv int64
			if err := rows.Scan(&id, &name, &orders, &gmv); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			supplierLeaders = append(supplierLeaders, map[string]interface{}{"id": id, "name": name, "orders": orders, "gmv": gmv})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
			"suppliers": supplierLeaders,
			"stores":    storeLeaders,
		}})
	case "alerts":
		// Live contract: {data: [{id, title, count, severity, tab,
		// items?, amount?}]}. Only rows with count > 0 are returned,
		// so the tab shows its "all quiet" empty state when nothing
		// is waiting. Row titles and tab targets are candidate-side
		// choices: live's alert titles come from live's backend, not
		// from the bundle, so they are unproven; tabs are the
		// candidate admin's existing tab ids.
		alerts := []map[string]interface{}{}
		addAlert := func(id, title string, count, amount int64, hasAmount bool, severity, tab string, items []string) {
			if count <= 0 {
				return
			}
			row := map[string]interface{}{"id": id, "title": title, "count": count, "severity": severity, "tab": tab}
			if hasAmount {
				row["amount"] = amount
			}
			if len(items) > 0 {
				row["items"] = items
			}
			alerts = append(alerts, row)
		}
		var stockItems []string
		stockRows, err := db.QueryContext(ctx, `SELECT title FROM products WHERE stock = 0 ORDER BY created_at DESC LIMIT 3`)
		if err == nil {
			for stockRows.Next() {
				var t string
				if err := stockRows.Scan(&t); err == nil {
					stockItems = append(stockItems, t)
				}
			}
			stockRows.Close()
		}
		addAlert("stockouts", "اتمام موجودی", t01Count(ctx, db, `SELECT COUNT(*) FROM products WHERE stock = 0`), 0, false, "medium", "catalog", stockItems)
		pendingPayoutCount := t01Count(ctx, db, `SELECT COUNT(*) FROM payout_requests WHERE status = 'pending'`)
		pendingPayoutSum := t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM payout_requests WHERE status = 'pending'`)
		addAlert("pendingPayouts", "تسویه‌های در انتظار", pendingPayoutCount, pendingPayoutSum, true, "high", "finance-payouts", nil)
		addAlert("openDisputes", "اختلاف‌های باز", t01Count(ctx, db, `SELECT COUNT(*) FROM disputes WHERE status = 'pending'`), 0, false, "high", "support-disputes", nil)
		addAlert("shipmentsAwaitingReceive", "محموله‌های در انتظار دریافت", t01Count(ctx, db, `SELECT COUNT(*) FROM inbound_shipments WHERE status = 'in_transit'`), 0, false, "medium", "warehouses", nil)
		addAlert("returnsAwaitingQC", "مرجوعی‌های در انتظار بررسی", t01Count(ctx, db, `SELECT COUNT(*) FROM customer_returns WHERE status = 'requested'`), 0, false, "medium", "support-returns", nil)
		addAlert("pendingStores", "فروشگاه‌های در انتظار تأیید", t01Count(ctx, db, `SELECT COUNT(*) FROM stores WHERE status = 'pending'`), 0, false, "medium", "stores-list", nil)
		addAlert("pendingProposals", "کالاهای پیشنهادی در انتظار بررسی", t01Count(ctx, db, `SELECT COUNT(*) FROM pending_approvals WHERE entity_type = 'proposed_product' AND status = 'pending'`), 0, false, "low", "catalog", nil)
		addAlert("stuckPayments", "پرداخت‌های ناموفق یا معلق", t01Count(ctx, db, `SELECT COUNT(*) FROM orders WHERE payment_status = 'pending' AND created_at < NOW() - INTERVAL '24 hours'`), 0, false, "high", "finance-pending-payments", nil)
		lockedCount := t01Count(ctx, db, `SELECT COUNT(*) FROM orders WHERE status = 'delivered_confirmed' AND COALESCE(profit_released, false) = false`)
		lockedSum := t01Sum(ctx, db, `SELECT COALESCE(SUM(dropshipper_profit), 0) FROM orders WHERE status = 'delivered_confirmed' AND COALESCE(profit_released, false) = false`)
		addAlert("profitLockBacklog", "سودهای در انتظار آزادسازی", lockedCount, lockedSum, true, "low", "finance-payouts", nil)
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"data": alerts})
	default:
		http.NotFound(w, r)
	}
}

// handleT01AdminGrowth serves GET /api/v1/admin/growth/<area> for area
// in campaigns|ads|fraud|jobs|referral|sms. TODO: exact live shapes
// are unverified. Fraud has no dedicated table in the migrations, so
// it reports ad_event_rejections (D67) counts instead of inventing a
// fraud model.
func handleT01AdminGrowth(w http.ResponseWriter, r *http.Request, db *sql.DB, area string) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	switch area {
	case "campaigns":
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"sellerCampaigns":    t01Count(ctx, db, `SELECT COUNT(*) FROM seller_campaigns`),
			"supplierCampaigns":  t01Count(ctx, db, `SELECT COUNT(*) FROM supplier_campaigns`),
			"supplierPromotions": t01Count(ctx, db, `SELECT COUNT(*) FROM supplier_promotions`),
		})
	case "ads":
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"sellerCampaigns":   t01Count(ctx, db, `SELECT COUNT(*) FROM seller_campaigns`),
			"supplierCampaigns": t01Count(ctx, db, `SELECT COUNT(*) FROM supplier_campaigns`),
			"rejectedEvents":    t01Count(ctx, db, `SELECT COUNT(*) FROM ad_event_rejections`),
		})
	case "fraud", "ads/fraud":
		// Live fraud-signal shape (AdminDashboardView-MERGED.js ad
		// tab, first field read at byte 359080): the live tab reads
		// the response object directly (no .data envelope):
		// baselineCtr, campaigns[{campaignId, name, flagged, ctr,
		// ctrVsBaseline, clicks, flagReason}], anomalyRule,
		// velocity[{campaignId, ip, ipMasked, clicksPerMinute,
		// campaignName, minute}], velocityNote, ipTrackingNote,
		// totalRejections7d, rejectionTracking,
		// rejections[{reason, reasonFa, count}], rejectionNote.
		// t01FraudShape computes what candidate tables prove and
		// leaves the unprovable parts empty rather than inventing
		// them (flag threshold, server note strings, reasonFa
		// mapping, per-IP velocity). The old {rejectedEvents}
		// envelope matched no live field and is dropped; no
		// candidate frontend read it.
		t01WriteJSON(w, http.StatusOK, t01FraudShape(ctx, db))
	case "jobs":
		// Live scheduler-state shape (AdminDashboardView-MERGED.js):
		// GET /admin/growth/jobs is tried first by the live jobs
		// tab (hs.jobs, byte 334273) and GET /admin/jobs is its
		// fallback (Na.list, byte 60078); both answer {jobs: [...]}
		// and the tab normalizer (byte 445526+) reads id, schedule,
		// startedAt, lastRunAt, lastReleased, lastRunError,
		// runCount, running, lastRunDurationMs,
		// consecutiveFailures, failureCount, failureRatePct,
		// recentRuns[{at, durationMs, error, released}]. This case
		// and GET /admin/jobs share t01SchedulerJobs so the two
		// endpoints agree.
		jobs, err := t01SchedulerJobs(ctx, db)
		if err != nil {
			writeInternalError(w, err)
			break
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"jobs": jobs})
	case "referral":
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"bonuses":        t01Count(ctx, db, `SELECT COUNT(*) FROM referral_bonuses`),
			"bonusTotal":     t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM referral_bonuses`),
			"referredStores": t01Count(ctx, db, `SELECT COUNT(*) FROM stores WHERE referrer_seller_id IS NOT NULL`),
		})
	case "sms":
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"total":  t01Count(ctx, db, `SELECT COUNT(*) FROM sms_logs`),
			"sent":   t01Count(ctx, db, `SELECT COUNT(*) FROM sms_logs WHERE status = 'sent'`),
			"failed": t01Count(ctx, db, `SELECT COUNT(*) FROM sms_logs WHERE status = 'failed'`),
		})
	default:
		http.NotFound(w, r)
	}
}

// t01SchedulerJobs builds the live scheduler-state job list from
// job_runs (migration 202610310008: id, job_id, started_at,
// released, confirmed, duration_ms, error, created_at). Field
// provenance, all from AdminDashboardView-MERGED.js:
//   - id / schedule / startedAt / lastRunAt / lastReleased /
//     lastRunError / runCount / running / lastRunDurationMs /
//     consecutiveFailures / failureCount / failureRatePct /
//     recentRuns: the jobs-tab normalizer (byte 445526+).
//   - schedule "every 60 seconds" for delivery-lock-releaser:
//     the tab schedule map (byte 445227) plus the worker
//     60s ticker (internal/worker/scheduler.go). Any other
//     job_id gets schedule "" (no live schedule proven).
//   - startedAt is the earliest recorded run (MIN started_at):
//     the live field exists but its derivation is unproven.
//   - running is always false: job_runs carries no in-flight
//     state, and no worker running-flag is exposed to this
//     package; the tab also tracks manual runs client-side.
//   - recentRuns are the latest 5 job_runs rows (live history
//     is service memory cleared on restart; the candidate
//     reads the persisted rows instead, so the live
//     recentRunsNote memory wording is NOT reproduced).
//   - confirmed is intentionally NOT emitted: the live job
//     object has no confirmed field (only lastReleased).
//
// A missing job_runs table yields an empty list, matching the
// live tab empty state, not a 500.
func t01SchedulerJobs(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	jobs := []map[string]interface{}{}
	if !t01TableExists(ctx, db, "job_runs") {
		return jobs, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT job_id, started_at, released, duration_ms, error
		FROM job_runs ORDER BY job_id ASC, started_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type t01Run struct {
		at         time.Time
		released   int64
		durationMs int64
		errText    string
	}
	grouped := map[string][]t01Run{}
	var order []string
	for rows.Next() {
		var jobID string
		var run t01Run
		if err := rows.Scan(&jobID, &run.at, &run.released, &run.durationMs, &run.errText); err != nil {
			return nil, err
		}
		if _, ok := grouped[jobID]; !ok {
			order = append(order, jobID)
		}
		grouped[jobID] = append(grouped[jobID], run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, jobID := range order {
		runs := grouped[jobID]
		latest := runs[0]
		var lastErr interface{}
		if latest.errText != "" {
			lastErr = latest.errText
		}
		consecutive := 0
		for _, run := range runs {
			if run.errText == "" {
				break
			}
			consecutive++
		}
		failures := 0
		for _, run := range runs {
			if run.errText != "" {
				failures++
			}
		}
		var rate interface{}
		if len(runs) > 0 {
			rate = float64(failures) / float64(len(runs)) * 100
		}
		recent := []map[string]interface{}{}
		for i, run := range runs {
			if i >= 5 {
				break
			}
			var runErr interface{}
			if run.errText != "" {
				runErr = run.errText
			}
			recent = append(recent, map[string]interface{}{
				"at": run.at.Format(time.RFC3339), "durationMs": run.durationMs,
				"error": runErr, "released": run.released,
			})
		}
		schedule := ""
		if jobID == "delivery-lock-releaser" {
			schedule = "every 60 seconds"
		}
		jobs = append(jobs, map[string]interface{}{
			"id": jobID, "schedule": schedule,
			"startedAt":    runs[len(runs)-1].at.Format(time.RFC3339),
			"lastRunAt":    latest.at.Format(time.RFC3339),
			"lastReleased": latest.released, "lastRunError": lastErr,
			"runCount": len(runs), "running": false,
			"lastRunDurationMs":   latest.durationMs,
			"consecutiveFailures": consecutive, "failureCount": failures,
			"failureRatePct": rate, "recentRuns": recent,
		})
	}
	return jobs, nil
}

// t01SchedulerJob returns one scheduler-state job, or found=false.
func t01SchedulerJob(ctx context.Context, db *sql.DB, jobID string) (map[string]interface{}, bool, error) {
	jobs, err := t01SchedulerJobs(ctx, db)
	if err != nil {
		return nil, false, err
	}
	for _, job := range jobs {
		if job["id"] == jobID {
			return job, true, nil
		}
	}
	return nil, false, nil
}

// handleT01AdminJobs serves GET /api/v1/admin/jobs -> {jobs: [...]}.
func handleT01AdminJobs(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	jobs, err := t01SchedulerJobs(r.Context(), db)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"jobs": jobs})
}

// handleT01AdminJobRun serves POST /api/v1/admin/jobs/{id}/run
// (live Na.runNow, AdminDashboardView-MERGED.js byte 60150): no
// request body; answers {job: <scheduler-state job>}. Only the
// known scheduler job can be run, so any other id 404s (live
// behaviour for unknown ids is unproven). The cycle runs
// synchronously so the returned job reflects the just-finished
// run; a concurrent 60s tick cannot double-release (release is
// guarded by profit_released = FALSE). A nil run answers 503.
// This handler never invents a run row: the returned state is
// re-read from job_runs after the cycle. Whether live runs the
// cycle synchronously or queued is unproven; the synchronous
// read-back is what the live frontend needs, since it replaces
// the job from the response and toasts lastReleased.
func handleT01AdminJobRun(w http.ResponseWriter, r *http.Request, db *sql.DB, jobID string, run func(context.Context) error) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	if jobID != "delivery-lock-releaser" {
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}
	if run == nil {
		http.Error(w, `{"error":"scheduler unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if err := run(r.Context()); err != nil {
		if errors.Is(err, jobrunner.ErrBusy) {
			http.Error(w, `{"error":"job busy"}`, http.StatusConflict)
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, `{"error":"job cancelled"}`, http.StatusRequestTimeout)
		} else {
			http.Error(w, `{"error":"job failed; review operations"}`, http.StatusServiceUnavailable)
		}
		return
	}
	job, found, err := t01SchedulerJob(r.Context(), db, jobID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !found {
		http.Error(w, `{"error":"job state unavailable"}`, http.StatusInternalServerError)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"job": job})
}

// t01FraudShape builds the live fraud-signal envelope from real
// candidate tables. baselineCtr is the pooled 30-day CTR over
// seller_campaign_events (live label: 30-day average; pooled vs
// mean-of-campaigns is unproven). Campaign CTRs use the same
// 30-day window so ctrVsBaseline stays coherent. flagged is
// always false and flagReason empty because the live flag
// threshold is unproven and must not be invented; anomalyRule,
// velocityNote, ipTrackingNote and rejectionNote are live
// server strings no bundle string proves, so they are empty.
// velocity is empty: seller_campaign_events carries no IP
// column, so per-IP clicks-per-minute cannot be computed.
// rejections group ad_event_rejections (last 7 days) by reason;
// reasonFa repeats the raw reason because the live Persian
// mapping is unproven. rejectionTracking is true only when
// the table exists (live shows rejectionNote otherwise).
func t01FraudShape(ctx context.Context, db *sql.DB) map[string]interface{} {
	campaigns := []map[string]interface{}{}
	baselineCtr := 0.0
	if t01TableExists(ctx, db, "seller_campaigns") && t01TableExists(ctx, db, "seller_campaign_events") {
		var imp, clk int64
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE event_type = 'impression'), COUNT(*) FILTER (WHERE event_type = 'click') FROM seller_campaign_events WHERE occurred_at >= NOW() - INTERVAL '30 days'`).Scan(&imp, &clk); err == nil && imp > 0 {
			baselineCtr = float64(clk) / float64(imp) * 100
		}
		rows, err := db.QueryContext(ctx, `SELECT c.id::text, c.name, COUNT(*) FILTER (WHERE e.event_type = 'impression'), COUNT(*) FILTER (WHERE e.event_type = 'click') FROM seller_campaigns c LEFT JOIN seller_campaign_events e ON e.campaign_id = c.id AND e.occurred_at >= NOW() - INTERVAL '30 days' GROUP BY c.id, c.name ORDER BY c.name`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, name string
				var cImp, cClk int64
				if err := rows.Scan(&id, &name, &cImp, &cClk); err != nil {
					break
				}
				ctr := 0.0
				if cImp > 0 {
					ctr = float64(cClk) / float64(cImp) * 100
				}
				ctrVs := 0.0
				if baselineCtr > 0 {
					ctrVs = ctr / baselineCtr
				}
				campaigns = append(campaigns, map[string]interface{}{
					"campaignId": id, "name": name, "flagged": false,
					"ctr": ctr, "ctrVsBaseline": ctrVs, "clicks": cClk,
					"flagReason": "",
				})
			}
		}
	}
	rejections := []map[string]interface{}{}
	totalRej7 := int64(0)
	tracking := false
	if t01TableExists(ctx, db, "ad_event_rejections") {
		tracking = true
		totalRej7 = t01Count(ctx, db, `SELECT COUNT(*) FROM ad_event_rejections WHERE occurred_at >= NOW() - INTERVAL '7 days'`)
		rows, err := db.QueryContext(ctx, `SELECT reason, COUNT(*) FROM ad_event_rejections WHERE occurred_at >= NOW() - INTERVAL '7 days' GROUP BY reason ORDER BY COUNT(*) DESC, reason`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var reason string
				var n int64
				if err := rows.Scan(&reason, &n); err != nil {
					break
				}
				rejections = append(rejections, map[string]interface{}{
					"reason": reason, "reasonFa": reason, "count": n,
				})
			}
		}
	}
	return map[string]interface{}{
		"baselineCtr": baselineCtr, "campaigns": campaigns,
		"anomalyRule": "", "velocity": []map[string]interface{}{},
		"velocityNote": "", "ipTrackingNote": "",
		"totalRejections7d": totalRej7, "rejectionTracking": tracking,
		"rejections": rejections, "rejectionNote": "",
	}
}

// handleT01AdminSupportTickets serves GET /api/v1/admin/support/tickets.
func handleT01AdminSupportTickets(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	rows, err := db.QueryContext(r.Context(),
		`SELECT id, requester_user_id, requester_role, subject, status, priority,
			related_order_number, created_at, updated_at
		FROM support_tickets ORDER BY created_at DESC LIMIT $1`, t01Limit(r))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, role, subject, status, priority string
		var requester sql.NullString
		var orderNo sql.NullString
		var created, updated time.Time
		if err := rows.Scan(&id, &requester, &role, &subject, &status, &priority, &orderNo, &created, &updated); err != nil {
			writeInternalError(w, err)
			return
		}
		out = append(out, map[string]interface{}{
			"id": id, "requesterUserId": requester.String, "requesterRole": role,
			"subject": subject, "status": status, "priority": priority,
			"relatedOrderNumber": orderNo.String,
			"createdAt":          created.Format(time.RFC3339), "updatedAt": updated.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"tickets": out})
}

// handleT01AdminSupportMetrics serves GET
// /api/v1/admin/support/metrics/<area> for area in tickets|returns|disputes.
func handleT01AdminSupportMetrics(w http.ResponseWriter, r *http.Request, db *sql.DB, area string) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	var table, statusCol string
	switch area {
	case "tickets":
		table, statusCol = "support_tickets", "status"
	case "returns":
		table, statusCol = "customer_returns", "status"
	case "disputes":
		table, statusCol = "disputes", "status"
	default:
		http.NotFound(w, r)
		return
	}
	if !t01TableExists(ctx, db, table) {
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"total": 0, "byStatus": []interface{}{}})
		return
	}
	rows, err := db.QueryContext(ctx,
		`SELECT `+statusCol+`, COUNT(*) FROM `+table+` GROUP BY `+statusCol+` ORDER BY COUNT(*) DESC`)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	byStatus := []map[string]interface{}{}
	var total int64
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			writeInternalError(w, err)
			return
		}
		total += n
		byStatus = append(byStatus, map[string]interface{}{"status": status, "count": n})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"total": total, "byStatus": byStatus})
}

// handleT01AdminStoreIntel serves GET /api/v1/admin/store-intel/<area>
// for area in stores|catalog|warehouse. (store-intel/suppliers is
// already implemented by Team 06 and is not duplicated here.)
// TODO: exact live shapes are unverified.
func handleT01AdminStoreIntel(w http.ResponseWriter, r *http.Request, db *sql.DB, area string) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	switch area {
	case "stores":
		rows, err := db.QueryContext(ctx,
			`SELECT s.id, s.name, s.slug, s.status, COUNT(o.id) AS orders,
				COALESCE(SUM(o.payable_amount), 0) AS sales
			FROM stores s LEFT JOIN orders o ON o.store_id = s.id AND o.status != 'cancelled'
			GROUP BY s.id, s.name, s.slug, s.status ORDER BY sales DESC LIMIT $1`, t01Limit(r))
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var id, name, slug, status string
			var orders, sales int64
			if err := rows.Scan(&id, &name, &slug, &status, &orders, &sales); err != nil {
				writeInternalError(w, err)
				return
			}
			out = append(out, map[string]interface{}{
				"id": id, "name": name, "slug": slug, "status": status,
				"orders": orders, "sales": sales,
			})
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"stores": out})
	case "catalog":
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"products":            t01Count(ctx, db, `SELECT COUNT(*) FROM products`),
			"activeProducts":      t01Count(ctx, db, `SELECT COUNT(*) FROM products WHERE is_active = TRUE`),
			"categories":          t01Count(ctx, db, `SELECT COUNT(*) FROM categories`),
			"missingMeasurements": t01Count(ctx, db, `SELECT COUNT(*) FROM products WHERE length_mm IS NULL OR width_mm IS NULL OR height_mm IS NULL OR weight_grams IS NULL`),
		})
	case "warehouse":
		warehouses, _, err := t01ListRows(ctx, db, "warehouses", "ORDER BY priority ASC, id ASC", t01Limit(r))
		if err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"warehouses": warehouses,
			"inbounds":   t01Count(ctx, db, `SELECT COUNT(*) FROM warehouse_inbounds`),
		})
	default:
		http.NotFound(w, r)
	}
}

// handleT01AdminContentHealth serves GET /api/v1/admin/content/health:
// active vs total counts for the public content family (D01).
// TODO: exact live shape is unverified.
func handleT01AdminContentHealth(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"banners":           t01Count(ctx, db, `SELECT COUNT(*) FROM banners`),
		"activeBanners":     t01Count(ctx, db, `SELECT COUNT(*) FROM banners WHERE is_active = TRUE`),
		"sections":          t01Count(ctx, db, `SELECT COUNT(*) FROM homepage_sections`),
		"activeSections":    t01Count(ctx, db, `SELECT COUNT(*) FROM homepage_sections WHERE is_active = TRUE`),
		"staticPages":       t01Count(ctx, db, `SELECT COUNT(*) FROM static_pages`),
		"activeStaticPages": t01Count(ctx, db, `SELECT COUNT(*) FROM static_pages WHERE is_active = TRUE`),
		"siteContentKeys":   t01Count(ctx, db, `SELECT COUNT(*) FROM site_content`),
		"templates":         t01Count(ctx, db, `SELECT COUNT(*) FROM store_templates`),
	})
}

// handleT01AdminReferralNetwork serves GET
// /api/v1/admin/referral-network. TODO: exact live shape is unverified.
func handleT01AdminReferralNetwork(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	rows, err := db.QueryContext(ctx,
		`SELECT referrer_store_id, COUNT(*) AS referred, COALESCE(SUM(amount), 0) AS bonuses
		FROM referral_bonuses GROUP BY referrer_store_id ORDER BY referred DESC LIMIT 50`)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	nodes := []map[string]interface{}{}
	for rows.Next() {
		var referrer string
		var referred, bonuses int64
		if err := rows.Scan(&referrer, &referred, &bonuses); err != nil {
			writeInternalError(w, err)
			return
		}
		nodes = append(nodes, map[string]interface{}{
			"referrerStoreId": referrer, "referredStores": referred, "bonuses": bonuses,
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	referralEnabled := false
	if t01ColumnExists(ctx, db, "platform_settings", "referral_is_enabled") {
		var enabled sql.NullBool
		if err := db.QueryRowContext(ctx,
			`SELECT referral_is_enabled FROM platform_settings WHERE id = 1`).Scan(&enabled); err == nil && enabled.Valid {
			referralEnabled = enabled.Bool
		}
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"network": nodes,
		"enabled": referralEnabled,
	})
}

// handleT01AdminAdSummary serves GET /api/v1/admin/ad-summary.
// TODO: exact live shape is unverified.
func handleT01AdminAdSummary(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"sellerCampaigns":    t01Count(ctx, db, `SELECT COUNT(*) FROM seller_campaigns`),
		"supplierCampaigns":  t01Count(ctx, db, `SELECT COUNT(*) FROM supplier_campaigns`),
		"rejectedEvents":     t01Count(ctx, db, `SELECT COUNT(*) FROM ad_event_rejections`),
		"supplierPromotions": t01Count(ctx, db, `SELECT COUNT(*) FROM supplier_promotions`),
	})
}

// handleT01AdminAdsStats serves GET /api/v1/admin/ads/stats.
// TODO: exact live shape is unverified; ad_events table existence is
// guarded (the ads event handler exists in source, its table is not
// confirmed in the migrations read for this task).
func handleT01AdminAdsStats(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	events := int64(0)
	if t01TableExists(ctx, db, "ad_events") {
		events = t01Count(ctx, db, `SELECT COUNT(*) FROM ad_events`)
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"events":            events,
		"sellerCampaigns":   t01Count(ctx, db, `SELECT COUNT(*) FROM seller_campaigns`),
		"supplierCampaigns": t01Count(ctx, db, `SELECT COUNT(*) FROM supplier_campaigns`),
		"rejectedEvents":    t01Count(ctx, db, `SELECT COUNT(*) FROM ad_event_rejections`),
	})
}
