package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/repository"
)

// FIN-013 §2.4: the unknown-outcome refund work queue as an admin
// surface. Until FIN-013 the queue existed as data + index only —
// queryable in SQL, with no endpoint and no automated reconciler.
// These two handlers close the visibility half: the queue is listed
// oldest-first with its age, and a manual reconcile trigger exists.
// Execution itself lives in the refund-reconciliation job (worker) and
// in this trigger; both are inert until FIN-010 supplies a PSP refund
// adapter, and both say so explicitly instead of reporting a fake
// success.

// handleFIN013RefundUnknownQueue serves
// GET /api/v1/finance/refunds/unknown-queue?limit=25 (finance roles).
// The response carries obligation IDs, order IDs, amounts (Toman),
// purposes, destinations, latest-event states and ages — no provider
// reference, token, or customer PII.
func handleFIN013RefundUnknownQueue(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	limit := 25
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			limit = n
		}
	}
	started := time.Now()
	items, err := repository.NewOrderRepository(db).ListUnknownRefunds(r.Context(), limit)
	// FIN-013: per-query finance latency for the refund work queue
	// (audit §2.6 Q5) — one histogram sample per list call.
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	observability.Default.Observe("sql", "refund_work_queue", outcome, time.Since(started))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if items == nil {
		items = []repository.UnknownRefundQueueItem{}
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "items": items, "count": len(items)})
}

// handleFIN013RefundReconcile serves
// POST /api/v1/finance/refunds/reconcile (finance roles): a manual
// trigger for one bounded reconciler pass (25 obligations, the same
// bound as the scheduled job). Until FIN-010 wires a real PSP refund
// adapter there is no provider to reconcile against, so the trigger
// answers 503 with an explicit machine-readable reason instead of a
// 200 that reconciled nothing. When the adapter lands, this handler
// must pass it to ReconcileOrderRefunds the same way the worker's
// SetRefundProvider does — see docs/operations/
// finance-invariants-runbook.md ("manual reconcile trigger").
func handleFIN013RefundReconcile(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	http.Error(w, `{"error":"refund_provider_unavailable","message":"no PSP refund adapter is configured (FIN-010 blocked); unknown outcomes stay queued and visible"}`, http.StatusServiceUnavailable)
}
