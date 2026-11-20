package main

import (
	"database/sql"
	"errors"
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

func handleT01TreasuryCommission(w http.ResponseWriter, r *http.Request, db *sql.DB, now time.Time) {
	days := t01TreasuryDays(r)
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, 1-days)
	end := today.AddDate(0, 0, 1)
	// The totals and daily buckets share a statement snapshot. Keep the
	// existing gross recorded-profit definition, not a bank-cash measure.
	rows, err := db.QueryContext(r.Context(), `
		SELECT TO_CHAR(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day,
		       COALESCE(SUM(platform_profit), 0),
		       COALESCE(SUM(platform_profit) FILTER (WHERE profit_released IS TRUE), 0),
		       COALESCE(SUM(platform_profit) FILTER (WHERE profit_released IS NOT TRUE), 0),
		       COUNT(*)
		FROM orders
		WHERE payment_status = 'paid' AND status != 'cancelled'
		  AND created_at >= $1 AND created_at < $2
		GROUP BY GROUPING SETS ((1), ())`, start, end)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	var total, released, unreleased, orderCount int64
	byDay := make(map[string]int64, days)
	for rows.Next() {
		var day sql.NullString
		var commission, paid, pending, count int64
		if err := rows.Scan(&day, &commission, &paid, &pending, &count); err != nil {
			writeInternalError(w, err)
			return
		}
		if day.Valid {
			byDay[day.String] = commission
		} else {
			total, released, unreleased, orderCount = commission, paid, pending, count
		}
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	trend := make([]map[string]interface{}, 0, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		trend = append(trend, map[string]interface{}{"date": day, "commission": byDay[day]})
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "days": days,
		"total": total, "releasedTotal": released, "unreleasedTotal": unreleased,
		"orderCount": orderCount, "trend": trend,
	})
}

func handleT01FinanceTreasury(w http.ResponseWriter, r *http.Request, db *sql.DB, area string) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	switch area {
	case "cash-position":
		// FIN-008: the cash position is computed by the set-based,
		// single-REPEATABLE-READ-snapshot implementation in
		// finance_treasury.go. The old allTimeIn-minus-everything
		// formula (paid orders treated as bank cash, pending payouts
		// double-subtracted, negative balances netted, silent-zero
		// reads) is retired; see D-01..D-17 in fin008-audit.md.
		handleTreasuryCashPosition(w, r, db)

	case "commission":
		handleT01TreasuryCommission(w, r, db, time.Now())
	case "payouts-summary":
		// FIN-008 D-15: a failed read fails the response instead of
		// silently rendering 0.
		pending, err := t01SumErr(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM payout_requests WHERE status = 'pending'`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		pendingCount, err := t01CountErr(ctx, db, `SELECT COUNT(*) FROM payout_requests WHERE status = 'pending'`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		// D32: only completed/transfer_confirmed are money out.
		paid, err := t01SumErr(ctx, db, `SELECT COALESCE(SUM(amount), 0) FROM payout_requests WHERE status IN ('completed', 'transfer_confirmed')`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		paidCount, err := t01CountErr(ctx, db, `SELECT COUNT(*) FROM payout_requests WHERE status IN ('completed', 'transfer_confirmed')`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		rejected, err := t01CountErr(ctx, db, `SELECT COUNT(*) FROM payout_requests WHERE status = 'rejected'`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":      true,
			"pending":      pending,
			"pendingCount": pendingCount,
			"paid":         paid,
			"paidCount":    paidCount,
			"rejected":     rejected,
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
		// cannot be computed honestly. FIN-008 D-15: fail the
		// response (the tab renders an error state) instead of
		// returning zeros with success:true.
		writeInternalError(w, errors.New("finance treasury profit-locks: neither v_fin_store_profit_lock nor orders.return_window_ends_at exists; locks cannot be computed"))
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
		// FIN-008 D-15: a failed read fails the response instead of
		// silently rendering 0.
		total, err := t01CountErr(ctx, db, `SELECT COUNT(*) FROM customer_returns`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		requested, err := t01CountErr(ctx, db, `SELECT COUNT(*) FROM customer_returns WHERE status = 'requested'`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		refunded, err := t01CountErr(ctx, db, `SELECT COUNT(*) FROM customer_returns WHERE status = 'refunded'`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"total":     total,
			"requested": requested,
			"refunded":  refunded,
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
	cycles, err := repository.NewFinanceRepository(db).ListMonthlyCycles(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"cycles": cycles})
}

func handleT01MonthlyDetail(w http.ResponseWriter, r *http.Request, db *sql.DB, period string) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	repo := repository.NewFinanceRepository(db)
	opts, perr := parseListPage(r)
	if perr != nil {
		http.Error(w, `{"error":"Invalid limit"}`, http.StatusBadRequest)
		return
	}
	cycle, page, err := repo.GetMonthlySettlementDetailPage(r.Context(), period, opts)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, `{"error":"Cycle not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		if isInvalidCursorError(err) {
			http.Error(w, `{"error":"cursor is invalid"}`, http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	items := []map[string]interface{}{}
	for _, it := range page.Items {
		items = append(items, map[string]interface{}{"id": it.ID, "storeId": it.StoreID, "grossAmount": it.GrossAmount, "feeAmount": it.FeeAmount, "netAmount": it.NetAmount, "payoutRequestId": it.PayoutRequestID, "status": it.Status})
	}
	t01WriteJSON(w, http.StatusOK, struct {
		*repository.MonthlySettlementCycle
		Items      []map[string]interface{} `json:"items"`
		NextCursor string                   `json:"nextCursor,omitempty"`
		HasMore    bool                     `json:"hasMore"`
		Limit      int                      `json:"limit"`
	}{cycle, items, page.NextCursor, page.HasMore, page.Limit})
}

// handleT01MonthlyRun starts/resumes the shared durable reservation service.
// A completed period stays sealed; partial work is acknowledged truthfully.
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
	cycle, err := repository.NewFinanceRepository(db).RunMonthlySettlementForPeriod(r.Context(), strings.TrimSpace(req.Period), t01ClaimsUserID(r))
	if errors.Is(err, repository.ErrInvalidSettlementPeriod) {
		http.Error(w, `{"error":"period (YYYY-MM) is required"}`, http.StatusBadRequest)
		return
	}
	if errors.Is(err, repository.ErrSettlementNotConfigured) {
		http.Error(w, `{"error":"monthly settlement is not configured by an admin"}`, http.StatusConflict)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if cycle.AlreadyCompleted {
		http.Error(w, `{"error":"Settlement cycle already exists for this period"}`, http.StatusConflict)
		return
	}
	status := http.StatusOK
	if cycle.Status != "completed" {
		status = http.StatusAccepted
	}
	t01WriteJSON(w, status, struct {
		*repository.MonthlySettlementCycle
		Success bool `json:"success"`
	}{cycle, true})
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
	var accountStoreID string
	if err := tx.QueryRowContext(ctx, `SELECT store_id FROM seller_early_withdrawals WHERE id=$1`, id).Scan(&accountStoreID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Withdrawal not found"}`, http.StatusNotFound)
		} else {
			writeInternalError(w, err)
		}
		return
	}
	var accountLocks repository.TransactionLockSet
	if err := repository.LockAccountsTx(ctx, tx, &accountLocks, repository.FinancialAccount{Type: "seller", ID: accountStoreID}); err != nil {
		writeInternalError(w, err)
		return
	}
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
