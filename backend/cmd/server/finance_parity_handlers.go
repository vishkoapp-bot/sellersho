package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// Team 01 (Phase 2, live parity): finance APIs (MASTER-AUDIT D16) and
// the MANUAL monthly settlement engine (live only, NOT the
// feature/monthly-settlement branch).
//
// D32 rule (Team 02, binding here): money out is counted ONLY from
// payout_requests in completed/transfer_confirmed. "approved" is only
// a manual-transfer marker and is never counted as paid, and no
// ledger entry is written at approval time.
//
// MANUAL SETTLEMENT (live engine, from binary strings + migration
// 202611020002 + Team 02 §6):
//   GET  /finance/settlements/monthly        list cycles
//   GET  /finance/settlements/monthly/<period>  cycle detail + items
//   POST /finance/settlements/monthly/run    MANUAL trigger only
//   GET  /finance/settlements/pending        (already exists)
//   GET  /finance/settlements/approved       list approved payouts
//   GET  /finance/early-withdrawals          list
//   POST /finance/early-withdrawals/<id>/review  finance review
//   GET/PUT /admin/settings/settlement       (in admin_settings_handlers.go)
// Cycle items: fee_amount is ALWAYS 0 for monthly items; a store with
// withdrawable balance and bank details gets pending_payout plus a
// pending payout_request; a store without bank details gets
// skipped_no_bank_details and NO invented payout.
// Deliberately NOT implemented (branch-only or unverified):
//   - NO auto worker / sweep, NO seller early-withdrawal request
//     handler, NO settlement UI, NO hard-coded early minimum
//     1,000,000 (branch-only, U03), NO 24h SLA as live behaviour.
//   - The trigger question is U02: the run endpoint is manual by
//     construction; any auto-trigger is TODO U02.
//   - settlement_day=10 / fee 5% are SETTINGS DATA, not proven
//     behaviour; the run refuses to act while settlement_day is NULL
//     with the live message "monthly settlement is not configured by
//     an admin".
// Bank-details source assumption (TODO): no stores.shaba column exists
// in the migrations, so "bank details on record" is read from the
// store's most recent payout_requests row (shaba, bank_name). If the
// merged tree shows a different source, this must be replaced.

// --- treasury (D16) ------------------------------------------------------------
// Live contract (AdminDashboardView-MERGED.js, Nov 2 18:38 build):
//   getCashPosition(days=30) -> /finance/treasury/cash-position?days=N
//   getCommission(days=30)   -> /finance/treasury/commission?days=N
//   getProfitLocks()         -> /finance/treasury/profit-locks
//   getPayoutsSummary()      -> /finance/treasury/payouts-summary
//   getSupplierAging()       -> /finance/treasury/supplier-aging
//   getReconciliation()      -> /finance/treasury/reconciliation
//   getTreasuryReturns()     -> /finance/treasury/returns
// The live frontend stores each treasury response ONLY when
// response.success is true (e.g. `O && O.success ? O : null`), so
// every case below returns success:true. The previous bare
// {platformProfit, orders} commission shape and bare
// {cashIn, cashOut, net} cash shape are retired: against the live
// frontend they stored null and rendered the empty states.
// Bundle-proven commission fields: total, releasedTotal,
// unreleasedTotal, orderCount, trend[].commission, note (optional).
// Bundle-proven cash fields: allTimeIn, allTimeOut, overObligated,
// safeToSpend, period.{in,out,trend[].in,trend[].out}, heldOrderCount,
// heldInOrders, owedToStores, owedToSuppliers, pendingRefundCount,
// pendingRefunds, pendingCount, pendingPayouts, platformEarned, note,
// sellerInFlight, supplierInFlight. The bundle proves field NAMES and
// labels, not the SQL behind them: derivations marked INFERRED rest on
// those labels plus the D32 money-out rule (only payout_requests in
// completed/transfer_confirmed count as money out) and the existing
// table shapes, not on bundle SQL.

// t01TreasuryDays parses ?days= (the live UI offers 7/30, default 30).
// Unparsable or out-of-range values fall back to 30.
func t01TreasuryDays(r *http.Request) int {
	days := 30
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	return days
}

// t01DayLabels returns YYYY-MM-DD labels for the days window ending
// today (UTC), oldest first, for zero-filling daily trends.
func t01DayLabels(days int) []string {
	now := time.Now().UTC()
	out := make([]string, 0, days)
	for i := days - 1; i >= 0; i-- {
		out = append(out, now.AddDate(0, 0, -i).Format("2006-01-02"))
	}
	return out
}

func handleT01FinanceTreasury(w http.ResponseWriter, r *http.Request, db *sql.DB, area string) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	switch area {
	case "cash-position":
		days := t01TreasuryDays(r)
		cutoff := time.Now().AddDate(0, 0, -days)
		// INFERRED derivations (the bundle proves names/labels only).
		allTimeIn := t01Sum(ctx, db, `SELECT COALESCE(SUM(payable_amount), 0) FROM orders WHERE payment_status = 'paid' AND status != 'cancelled'`)
		paidOut := t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM payout_requests WHERE status IN ('completed', 'transfer_confirmed')`)
		customerPaid := t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM customer_withdrawal_requests WHERE status = 'paid'`)
		allTimeOut := paidOut + customerPaid
		periodIn := t01Sum(ctx, db, `SELECT COALESCE(SUM(payable_amount), 0) FROM orders WHERE payment_status = 'paid' AND status != 'cancelled' AND created_at >= $1`, cutoff)
		periodOut := t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM payout_requests WHERE status IN ('completed', 'transfer_confirmed') AND created_at >= $1`, cutoff) +
			t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM customer_withdrawal_requests WHERE status = 'paid' AND created_at >= $1`, cutoff)
		heldInOrders := t01Sum(ctx, db, `SELECT COALESCE(SUM(payable_amount), 0) FROM orders WHERE payment_status = 'paid' AND COALESCE(profit_released, FALSE) = FALSE AND status != 'cancelled'`)
		heldOrderCount := t01Count(ctx, db, `SELECT COUNT(*) FROM orders WHERE payment_status = 'paid' AND COALESCE(profit_released, FALSE) = FALSE AND status != 'cancelled'`)
		owedToStores := t01Sum(ctx, db, `SELECT COALESCE(SUM(dropshipper_profit), 0) FROM orders WHERE profit_released = TRUE AND status IN ('delivered', 'delivered_confirmed')`)
		owedToSuppliers := t01Sum(ctx, db, `SELECT COALESCE(SUM(payable_amount - platform_profit - dropshipper_profit), 0) FROM orders WHERE profit_released = TRUE AND status IN ('delivered', 'delivered_confirmed')`)
		pendingRefundCount := t01Count(ctx, db, `SELECT COUNT(*) FROM customer_withdrawal_requests WHERE status IN ('pending', 'approved')`)
		pendingRefunds := t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM customer_withdrawal_requests WHERE status IN ('pending', 'approved')`)
		pendingPayouts := t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM payout_requests WHERE status = 'pending'`)
		pendingCount := t01Count(ctx, db, `SELECT COUNT(*) FROM payout_requests WHERE status = 'pending'`)
		platformEarned := t01Sum(ctx, db, `SELECT COALESCE(SUM(platform_profit), 0) FROM orders WHERE payment_status = 'paid' AND status != 'cancelled'`)
		sellerInFlight := t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM payout_requests WHERE status = 'approved' AND recipient_type = 'seller'`)
		supplierInFlight := t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM payout_requests WHERE status = 'approved' AND recipient_type = 'supplier'`)
		// Live subtitle: safeToSpend = allTimeIn - allTimeOut -
		// pendingPayouts - heldInOrders - owedToStores -
		// owedToSuppliers. A negative result is shown as 0 with
		// overObligated set (INFERRED: the bundle proves the
		// overObligated flag and banner, not the negative display).
		safeRaw := allTimeIn - allTimeOut - pendingPayouts - heldInOrders - owedToStores - owedToSuppliers
		safeToSpend := safeRaw
		overObligated := false
		if safeRaw < 0 {
			safeToSpend = 0
			overObligated = true
		}
		// Daily period trend (INFERRED granularity: one bucket per
		// calendar day; the bundle proves only trend[].in/out).
		inByDay := map[string]int64{}
		if rows, err := db.QueryContext(ctx,
			`SELECT TO_CHAR(created_at, 'YYYY-MM-DD') AS day, COALESCE(SUM(payable_amount), 0)
			 FROM orders WHERE payment_status = 'paid' AND status != 'cancelled' AND created_at >= $1
			 GROUP BY TO_CHAR(created_at, 'YYYY-MM-DD') ORDER BY day`, cutoff); err == nil {
			for rows.Next() {
				var day string
				var v int64
				if err := rows.Scan(&day, &v); err == nil {
					inByDay[day] = v
				}
			}
			rows.Close()
		}
		outByDay := map[string]int64{}
		if rows, err := db.QueryContext(ctx,
			`SELECT TO_CHAR(created_at, 'YYYY-MM-DD') AS day, COALESCE(SUM(amount), 0)
			 FROM payout_requests WHERE status IN ('completed', 'transfer_confirmed') AND created_at >= $1
			 GROUP BY TO_CHAR(created_at, 'YYYY-MM-DD') ORDER BY day`, cutoff); err == nil {
			for rows.Next() {
				var day string
				var v int64
				if err := rows.Scan(&day, &v); err == nil {
					outByDay[day] += v
				}
			}
			rows.Close()
		}
		if rows, err := db.QueryContext(ctx,
			`SELECT TO_CHAR(created_at, 'YYYY-MM-DD') AS day, COALESCE(SUM(amount), 0)
			 FROM customer_withdrawal_requests WHERE status = 'paid' AND created_at >= $1
			 GROUP BY TO_CHAR(created_at, 'YYYY-MM-DD') ORDER BY day`, cutoff); err == nil {
			for rows.Next() {
				var day string
				var v int64
				if err := rows.Scan(&day, &v); err == nil {
					outByDay[day] += v
				}
			}
			rows.Close()
		}
		trend := []map[string]interface{}{}
		for _, day := range t01DayLabels(days) {
			trend = append(trend, map[string]interface{}{"date": day, "in": inByDay[day], "out": outByDay[day]})
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true, "days": days,
			"allTimeIn": allTimeIn, "allTimeOut": allTimeOut,
			"overObligated": overObligated, "safeToSpend": safeToSpend,
			"period": map[string]interface{}{"in": periodIn, "out": periodOut, "trend": trend},
			"heldOrderCount": heldOrderCount, "heldInOrders": heldInOrders,
			"owedToStores": owedToStores, "owedToSuppliers": owedToSuppliers,
			"pendingRefundCount": pendingRefundCount, "pendingRefunds": pendingRefunds,
			"pendingCount": pendingCount, "pendingPayouts": pendingPayouts,
			"platformEarned": platformEarned,
			"sellerInFlight": sellerInFlight, "supplierInFlight": supplierInFlight,
		})
	case "commission":
		days := t01TreasuryDays(r)
		cutoff := time.Now().AddDate(0, 0, -days)
		// Paid orders only (live label: "جمع سهم ثبت‌شده روی
		// سفارش‌های پرداخت‌شده"); cancelled orders excluded.
		// Released split (INFERRED): profit_released TRUE means
		// released, FALSE means still awaiting release, matching
		// the profit-locks classification used elsewhere.
		var total, released, unreleased, orderCount int64
		if err := db.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(platform_profit), 0),
			        COALESCE(SUM(CASE WHEN COALESCE(profit_released, FALSE) THEN platform_profit ELSE 0 END), 0),
			        COALESCE(SUM(CASE WHEN COALESCE(profit_released, FALSE) THEN 0 ELSE platform_profit END), 0),
			        COUNT(*)
			 FROM orders
			 WHERE payment_status = 'paid' AND status != 'cancelled' AND created_at >= $1`,
			cutoff).Scan(&total, &released, &unreleased, &orderCount); err != nil {
			writeInternalError(w, err)
			return
		}
		// Daily trend (INFERRED granularity: one bucket per
		// calendar day; the bundle proves only trend[].commission).
		byDay := map[string]int64{}
		rows, err := db.QueryContext(ctx,
			`SELECT TO_CHAR(created_at, 'YYYY-MM-DD') AS day, COALESCE(SUM(platform_profit), 0)
			 FROM orders
			 WHERE payment_status = 'paid' AND status != 'cancelled' AND created_at >= $1
			 GROUP BY TO_CHAR(created_at, 'YYYY-MM-DD') ORDER BY day`, cutoff)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		for rows.Next() {
			var day string
			var v int64
			if err := rows.Scan(&day, &v); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			byDay[day] = v
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			writeInternalError(w, err)
			return
		}
		rows.Close()
		ctrend := []map[string]interface{}{}
		for _, day := range t01DayLabels(days) {
			ctrend = append(ctrend, map[string]interface{}{"date": day, "commission": byDay[day]})
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true, "days": days,
			"total": total, "releasedTotal": released, "unreleasedTotal": unreleased,
			"orderCount": orderCount, "trend": ctrend,
		})
	case "payouts-summary":
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":      true,
			"pending":      t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM payout_requests WHERE status = 'pending'`),
			"pendingCount": t01Count(ctx, db, `SELECT COUNT(*) FROM payout_requests WHERE status = 'pending'`),
			// D32: only completed/transfer_confirmed are money out.
			"paid":      t01Sum(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM payout_requests WHERE status IN ('completed', 'transfer_confirmed')`),
			"paidCount": t01Count(ctx, db, `SELECT COUNT(*) FROM payout_requests WHERE status IN ('completed', 'transfer_confirmed')`),
			"rejected":  t01Count(ctx, db, `SELECT COUNT(*) FROM payout_requests WHERE status = 'rejected'`),
		})
	case "profit-locks":
		// Live shape: {success, totals:{locked, lockedCount,
		// releasable, releasableCount}, rule}. The view
		// v_fin_store_profit_lock (migration 202610190004) already
		// encodes the locked/releasable classification; totals are
		// its column sums. The live tab hides the card when both
		// totals are 0.
		lockRule := "سود هر سفارش تا پایان مهلت مرجوعی قفل می‌ماند؛ سود سفارش‌های تحویل قطعی‌شده که مهلت مرجوعی‌شان تمام شده، آماده آزادسازی است."
		if t01TableExists(ctx, db, "v_fin_store_profit_lock") {
			var locked, releasable, lockedCount, releasableCount int64
			err := db.QueryRowContext(ctx,
				`SELECT COALESCE(SUM(locked), 0), COALESCE(SUM(releasable), 0),
				        COALESCE(SUM(locked_count), 0), COALESCE(SUM(releasable_count), 0)
				 FROM v_fin_store_profit_lock`).Scan(&locked, &releasable, &lockedCount, &releasableCount)
			if err == nil {
				t01WriteJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"totals": map[string]interface{}{
						"locked": locked, "lockedCount": lockedCount,
						"releasable": releasable, "releasableCount": releasableCount,
					},
					"rule": lockRule,
				})
				return
			}
		}
		// Fallback when the view is absent: the same classification
		// computed from orders (paid, not cancelled, store present).
		if t01ColumnExists(ctx, db, "orders", "return_window_ends_at") {
			var locked, releasable, lockedCount, releasableCount int64
			err := db.QueryRowContext(ctx,
				`SELECT COALESCE(SUM(CASE WHEN profit_released = FALSE AND NOT (status = 'delivered_confirmed' AND return_window_ends_at IS NOT NULL AND return_window_ends_at <= NOW()) THEN dropshipper_profit ELSE 0 END), 0),
				        COALESCE(SUM(CASE WHEN profit_released = FALSE AND status = 'delivered_confirmed' AND return_window_ends_at IS NOT NULL AND return_window_ends_at <= NOW() THEN dropshipper_profit ELSE 0 END), 0),
				        COALESCE(COUNT(*) FILTER (WHERE profit_released = FALSE AND NOT (status = 'delivered_confirmed' AND return_window_ends_at IS NOT NULL AND return_window_ends_at <= NOW())), 0),
				        COALESCE(COUNT(*) FILTER (WHERE profit_released = FALSE AND status = 'delivered_confirmed' AND return_window_ends_at IS NOT NULL AND return_window_ends_at <= NOW()), 0)
				 FROM orders WHERE payment_status = 'paid' AND status != 'cancelled' AND store_id IS NOT NULL`).Scan(&locked, &releasable, &lockedCount, &releasableCount)
			if err != nil {
				writeInternalError(w, err)
				return
			}
			t01WriteJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"totals": map[string]interface{}{
					"locked": locked, "lockedCount": lockedCount,
					"releasable": releasable, "releasableCount": releasableCount,
				},
				"rule": lockRule,
			})
			return
		}
		// No view and no return-window column: the classification
		// cannot be computed honestly; the live tab hides the card
		// when both totals are 0.
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"totals": map[string]interface{}{
				"locked": 0, "lockedCount": 0, "releasable": 0, "releasableCount": 0,
			},
		})
	case "reconciliation":
		rows, err := db.QueryContext(ctx,
			`SELECT target_type, type, COUNT(*) AS entries, COALESCE(SUM(amount), 0) AS total
			FROM ledger_entries GROUP BY target_type, type ORDER BY target_type, type`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var targetType, typ string
			var entries, total int64
			if err := rows.Scan(&targetType, &typ, &entries, &total); err != nil {
				writeInternalError(w, err)
				return
			}
			out = append(out, map[string]interface{}{
				"targetType": targetType, "type": typ, "entries": entries, "total": total,
			})
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "reconciliation": out})
	case "returns":
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"total":     t01Count(ctx, db, `SELECT COUNT(*) FROM customer_returns`),
			"requested": t01Count(ctx, db, `SELECT COUNT(*) FROM customer_returns WHERE status = 'requested'`),
			"refunded":  t01Count(ctx, db, `SELECT COUNT(*) FROM customer_returns WHERE status = 'refunded'`),
		})
	case "supplier-aging":
		// Aging of supplier payout requests by creation age bucket.
		rows, err := db.QueryContext(ctx,
			`SELECT CASE
					WHEN created_at >= NOW() - INTERVAL '7 days' THEN '0-7d'
					WHEN created_at >= NOW() - INTERVAL '30 days' THEN '8-30d'
					ELSE '30d+'
				END AS bucket, COUNT(*) AS requests, COALESCE(SUM(amount), 0) AS total
			FROM payout_requests WHERE recipient_type = 'supplier'
			GROUP BY bucket ORDER BY bucket`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var bucket string
			var requests, total int64
			if err := rows.Scan(&bucket, &requests, &total); err != nil {
				writeInternalError(w, err)
				return
			}
			out = append(out, map[string]interface{}{"bucket": bucket, "requests": requests, "total": total})
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "aging": out})
	default:
		http.NotFound(w, r)
	}
}

// handleT01FinanceAdjustments serves GET /api/v1/finance/adjustments.
// TODO: no adjustments table exists in the migration chain, so this
// route is registered with the confirmed path/role and a stub.
func handleT01FinanceAdjustments(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	t01Stub(w, r, "D16", "No finance-adjustments table exists in the migrations; the live adjustments store is unverified.")
}

// handleT01FinanceRevenueMonthly serves GET
// /api/v1/finance/reports/revenue/monthly: revenue grouped by calendar
// month. (The existing /finance/reports/revenue route takes a period
// parameter and is untouched.)
func handleT01FinanceRevenueMonthly(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	rows, err := db.QueryContext(r.Context(),
		`SELECT TO_CHAR(created_at, 'YYYY-MM') AS month, COUNT(*) AS orders,
			COALESCE(SUM(payable_amount), 0) AS revenue,
			COALESCE(SUM(platform_profit), 0) AS platform_profit
		FROM orders WHERE status != 'cancelled'
		GROUP BY TO_CHAR(created_at, 'YYYY-MM') ORDER BY month DESC`)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var month string
		var orders, revenue, profit int64
		if err := rows.Scan(&month, &orders, &revenue, &profit); err != nil {
			writeInternalError(w, err)
			return
		}
		out = append(out, map[string]interface{}{
			"month": month, "orders": orders, "revenue": revenue, "platformProfit": profit,
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"months": out})
}

// --- manual monthly settlement ---------------------------------------------------

var t01PeriodRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}$`)

func handleT01MonthlyList(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	if !t01TableExists(r.Context(), db, "monthly_settlement_cycles") {
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"cycles": []interface{}{}})
		return
	}
	rows, err := db.QueryContext(r.Context(),
		`SELECT period, settlement_day, status, total_stores, total_gross, total_net, skipped_stores, created_at, created_by
		FROM monthly_settlement_cycles ORDER BY period DESC`)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var period, status, createdBy string
		var day, totalStores int
		var gross, net int64
		var skipped int
		var created time.Time
		if err := rows.Scan(&period, &day, &status, &totalStores, &gross, &net, &skipped, &created, &createdBy); err != nil {
			writeInternalError(w, err)
			return
		}
		out = append(out, map[string]interface{}{
			"period": period, "settlementDay": day, "status": status,
			"totalStores": totalStores, "totalGross": gross, "totalNet": net,
			"skippedStores": skipped, "createdAt": created.Format(time.RFC3339), "createdBy": createdBy,
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"cycles": out})
}

func handleT01MonthlyDetail(w http.ResponseWriter, r *http.Request, db *sql.DB, period string) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	var day, totalStores, skipped int
	var gross, net int64
	var status, createdBy string
	var created time.Time
	var cyclePeriod string
	err := db.QueryRowContext(ctx,
		`SELECT period, settlement_day, status, total_stores, total_gross, total_net, skipped_stores, created_at, created_by
		FROM monthly_settlement_cycles WHERE period = $1`, period).Scan(
		&cyclePeriod, &day, &status, &totalStores, &gross, &net, &skipped, &created, &createdBy)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"Cycle not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, store_id, gross_amount, fee_amount, net_amount, payout_request_id, status
		FROM monthly_settlement_items WHERE cycle_period = $1 ORDER BY store_id`, period)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id, storeID, itemStatus string
		var grossAmt, feeAmt, netAmt int64
		var payoutID sql.NullString
		if err := rows.Scan(&id, &storeID, &grossAmt, &feeAmt, &netAmt, &payoutID, &itemStatus); err != nil {
			writeInternalError(w, err)
			return
		}
		items = append(items, map[string]interface{}{
			"id": id, "storeId": storeID, "grossAmount": grossAmt, "feeAmount": feeAmt,
			"netAmount": netAmt, "payoutRequestId": payoutID.String, "status": itemStatus,
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"period": period, "settlementDay": day, "status": status,
		"totalStores": totalStores, "totalGross": gross, "totalNet": net,
		"skippedStores": skipped, "createdAt": created.Format(time.RFC3339), "createdBy": createdBy,
		"items": items,
	})
}

// handleT01MonthlyRun serves POST /api/v1/finance/settlements/monthly/run.
// MANUAL trigger only: the caller supplies {"period": "YYYY-MM"} and the
// run settles that month exactly once (period is the cycle PK).
// TODO (U02): whether live also runs this automatically is unverified;
// no worker is registered here.
func handleT01MonthlyRun(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	var req struct {
		Period string `json:"period"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	period := strings.TrimSpace(req.Period)
	if !t01PeriodRe.MatchString(period) {
		http.Error(w, `{"error":"period (YYYY-MM) is required"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	// Settlement must be configured by an admin: settlement_day NULL
	// means the engine stays inert (live message, Team 02 §6).
	var settlementDay sql.NullInt64
	if t01ColumnExists(ctx, db, "platform_settings", "settlement_day") {
		_ = db.QueryRowContext(ctx,
			`SELECT settlement_day FROM platform_settings WHERE id = 1`).Scan(&settlementDay)
	}
	if !settlementDay.Valid {
		http.Error(w, `{"error":"monthly settlement is not configured by an admin"}`, http.StatusConflict)
		return
	}
	finRepo := repository.NewFinanceRepository(db)
	createdBy := t01ClaimsUserID(r)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer tx.Rollback()
	// Advisory lock mirroring the automatic sweep
	// (repository.RunMonthlySettlement, same key 91827364): serialize
	// concurrent runs across the check-then-insert, so a second
	// concurrent run for the same period waits, then re-checks and
	// gets the clean 409 below instead of 500ing on the cycle-PK
	// insert. pg_advisory_xact_lock is released at commit/rollback
	// (the deferred Rollback covers the early-return paths). The key
	// is global, not per-period, so this also serializes a manual run
	// against a concurrent automatic sweep.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(91827364)`); err != nil {
		writeInternalError(w, err)
		return
	}
	var existing string
	err = tx.QueryRowContext(ctx,
		`SELECT period FROM monthly_settlement_cycles WHERE period = $1`, period).Scan(&existing)
	if err == nil {
		http.Error(w, `{"error":"Settlement cycle already exists for this period"}`, http.StatusConflict)
		return
	}
	if err != nil && err != sql.ErrNoRows {
		writeInternalError(w, err)
		return
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO monthly_settlement_cycles (period, settlement_day, status, created_by)
		VALUES ($1, $2, 'completed', $3)`, period, settlementDay.Int64, createdBy); err != nil {
		writeInternalError(w, err)
		return
	}
	// Candidate stores: dropshipper stores (central excluded: settling
	// the platform's own store to itself is meaningless; TODO: the live
	// store filter is unverified).
	storeRows, err := tx.QueryContext(ctx,
		`SELECT id, name FROM stores WHERE owner_type != 'central' ORDER BY id`)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	type storeRow struct{ id, name string }
	stores := []storeRow{}
	for storeRows.Next() {
		var s storeRow
		if err := storeRows.Scan(&s.id, &s.name); err != nil {
			storeRows.Close()
			writeInternalError(w, err)
			return
		}
		stores = append(stores, s)
	}
	if err := storeRows.Err(); err != nil {
		storeRows.Close()
		writeInternalError(w, err)
		return
	}
	storeRows.Close()

	var totalStores, skippedStores int
	var totalGross, totalNet int64
	for _, s := range stores {
		// Balance is read through the existing finance repo function
		// (Team 02 D31 formula, including early-withdrawal deductions),
		// outside the cycle transaction: it only reads.
		bal, err := finRepo.GetStoreBalance(ctx, s.id)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		available := bal.AvailableAmount
		if available <= 0 {
			continue
		}
		// Bank details on record: the store's most recent payout
		// request (TODO: source unverified, see file header).
		var shaba, bankName string
		err = tx.QueryRowContext(ctx,
			`SELECT shaba, bank_name FROM payout_requests WHERE store_id = $1 ORDER BY created_at DESC LIMIT 1`,
			s.id).Scan(&shaba, &bankName)
		itemID := "msi-" + uuid.NewString()
		if err == sql.ErrNoRows || strings.TrimSpace(shaba) == "" {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO monthly_settlement_items (id, cycle_period, store_id, gross_amount, fee_amount, net_amount, status)
				VALUES ($1, $2, $3, $4, 0, $4, 'skipped_no_bank_details')`,
				itemID, period, s.id, available); err != nil {
				writeInternalError(w, err)
				return
			}
			skippedStores++
			totalStores++
			totalGross += available
			totalNet += available
			continue
		}
		if err != nil {
			writeInternalError(w, err)
			return
		}
		// Pending payout request for the full (fee-free) amount. The
		// request stays 'pending' for the existing finance review /
		// transfer-confirmation flow (D32); no ledger entry is written
		// here, because approval alone never writes the ledger.
		// NOTE: recipient_type is 'seller' because the payout_requests CHECK
		// constraint (migration 000003) allows only ('seller','supplier');
		// finance_repo.go uses 'dropshipper' but no migration has legalised
		// that value yet, so it would fail at runtime. Revisit if the
		// schema team relaxes the constraint (supervisor: see report).
		payoutID := "pay-req-" + uuid.NewString()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO payout_requests (id, store_id, recipient_type, recipient_name, shaba, bank_name, amount, status, available_balance, created_at)
			VALUES ($1, $2, 'seller', $3, $4, $5, $6, 'pending', $6, NOW())`,
			payoutID, s.id, s.name, shaba, bankName, available); err != nil {
			writeInternalError(w, err)
			return
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO monthly_settlement_items (id, cycle_period, store_id, gross_amount, fee_amount, net_amount, payout_request_id, status)
			VALUES ($1, $2, $3, $4, 0, $4, $5, 'pending_payout')`,
			itemID, period, s.id, available, payoutID); err != nil {
			writeInternalError(w, err)
			return
		}
		totalStores++
		totalGross += available
		totalNet += available
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE monthly_settlement_cycles SET total_stores = $1, total_gross = $2, total_net = $3, skipped_stores = $4 WHERE period = $5`,
		totalStores, totalGross, totalNet, skippedStores, period); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "period": period, "status": "completed",
		"totalStores": totalStores, "totalGross": totalGross, "totalNet": totalNet,
		"skippedStores": skippedStores,
	})
}

// handleT01SettlementsApproved serves GET
// /api/v1/finance/settlements/approved: payout requests in 'approved'.
// NOTE (D32): approved is NOT paid; this list is a review queue, and
// money-out counting must still use completed/transfer_confirmed only.
func handleT01SettlementsApproved(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	rows, err := db.QueryContext(r.Context(),
		`SELECT id, store_id, recipient_type, recipient_name, shaba, bank_name, amount, status, created_at
		FROM payout_requests WHERE status = 'approved' ORDER BY created_at DESC LIMIT $1`, t01Limit(r))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, storeID, recipientType, recipientName, shaba, bankName, status string
		var amount int64
		var created time.Time
		if err := rows.Scan(&id, &storeID, &recipientType, &recipientName, &shaba, &bankName, &amount, &status, &created); err != nil {
			writeInternalError(w, err)
			return
		}
		out = append(out, map[string]interface{}{
			"id": id, "storeId": storeID, "recipientType": recipientType, "recipientName": recipientName,
			"shaba": shaba, "bankName": bankName, "amount": amount, "status": status,
			"createdAt": created.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"settlements": out})
}

// --- early withdrawals (live list/review only) -----------------------------------
// There is deliberately NO seller early-withdrawal request handler
// here: that endpoint comes from the un-deployed branch, not live.
// Review approves or rejects an existing seller_early_withdrawals row.
// On approval, a payout_requests row for the NET amount is created in
// status 'approved' (migration 202611020002 comment) in the same
// transaction; NO ledger entry is written at approval (D32: the ledger
// debit happens at transfer confirmation, via the existing finance
// flow). On rejection nothing moves and the pending hold releases
// automatically through the GetStoreBalance formula (pending gross no
// longer deducted). NO minimum amount and NO 24h deadline are enforced
// (both branch-only, U03).

func handleT01EarlyWithdrawalsList(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	q := `SELECT id, store_id, gross_amount, fee_pct, fee_amount, net_amount, shaba, bank_name,
		status, rejection_reason, payout_request_id, created_at, processed_at
		FROM seller_early_withdrawals WHERE 1 = 1`
	args := []interface{}{}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		args = append(args, status)
		q += ` AND status = $1`
	}
	if storeID := strings.TrimSpace(r.URL.Query().Get("storeId")); storeID != "" {
		args = append(args, storeID)
		q += fmt.Sprintf(` AND store_id = $%d`, len(args))
	}
	q += ` ORDER BY created_at DESC LIMIT ` + fmt.Sprintf(`%d`, t01Limit(r))
	rows, err := db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, storeID, shaba, bankName, status string
		var gross, feeAmt, net int64
		var feePct float64
		var rejection, payoutID sql.NullString
		var created time.Time
		var processed sql.NullTime
		if err := rows.Scan(&id, &storeID, &gross, &feePct, &feeAmt, &net, &shaba, &bankName,
			&status, &rejection, &payoutID, &created, &processed); err != nil {
			writeInternalError(w, err)
			return
		}
		out = append(out, map[string]interface{}{
			"id": id, "storeId": storeID, "grossAmount": gross, "feePct": feePct,
			"feeAmount": feeAmt, "netAmount": net, "shaba": shaba, "bankName": bankName,
			"status": status, "rejectionReason": rejection.String, "payoutRequestId": payoutID.String,
			"createdAt": created.Format(time.RFC3339), "processedAt": t01NullTime(processed),
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"withdrawals": out})
}

func handleT01EarlyWithdrawalReview(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	if req.Status != "approved" && req.Status != "rejected" {
		http.Error(w, `{"error":"status must be approved or rejected"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer tx.Rollback()
	var storeID, storeName, shaba, bankName, oldStatus string
	var netAmount int64
	err = tx.QueryRowContext(ctx,
		`SELECT w.store_id, s.name, w.net_amount, w.shaba, w.bank_name, w.status
		FROM seller_early_withdrawals w JOIN stores s ON s.id = w.store_id WHERE w.id = $1 FOR UPDATE`,
		id).Scan(&storeID, &storeName, &netAmount, &shaba, &bankName, &oldStatus)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"Withdrawal not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if oldStatus != "pending" {
		http.Error(w, `{"error":"Withdrawal is not pending"}`, http.StatusConflict)
		return
	}
	if req.Status == "rejected" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE seller_early_withdrawals SET status = 'rejected', rejection_reason = $1, processed_at = NOW() WHERE id = $2`,
			nullIfEmpty(req.Reason), id); err != nil {
			writeInternalError(w, err)
			return
		}
	} else {
		payoutID := "pay-req-" + uuid.NewString()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO payout_requests (id, store_id, recipient_type, recipient_name, shaba, bank_name, amount, status, available_balance, created_at)
			VALUES ($1, $2, 'seller', $3, $4, $5, $6, 'approved', $6, NOW())`,
			payoutID, storeID, storeName, shaba, bankName, netAmount); err != nil {
			writeInternalError(w, err)
			return
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE seller_early_withdrawals SET status = 'approved', payout_request_id = $1, processed_at = NOW() WHERE id = $2`,
			payoutID, id); err != nil {
			writeInternalError(w, err)
			return
		}
		// NOTE (D32): no ledger entry is written here. The ledger debit
		// is written by the existing transfer-confirmation flow when
		// finance confirms the manual transfer.
	}
	if err := tx.Commit(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id, "status": req.Status})
}
