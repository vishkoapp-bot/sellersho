package main

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

// Team 01 (Phase 2, live parity): warehouse routes, backend part only
// (MASTER-AUDIT D17). Warehouse business logic belongs to the
// orders/warehouse scope; this file only registers routes that are
// missing, wired to existing order/shipment queries where the shape is
// confirmed, and marks the rest TODO. The source putaway/queue and
// putaway/tasks routes are NOT removed.

// handleT01WarehouseMe serves GET /api/v1/warehouse/me (probe 401,
// frontend call). The confirmed contract is only the caller's identity
// as a warehouse user; which warehouse (if any) is assigned to the
// caller is unverified (TODO), so warehouseId is null rather than
// guessed.
func handleT01WarehouseMe(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	var role string
	if claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil {
		role = claims.Role
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"userId":      t01ClaimsUserID(r),
		"role":        role,
		"warehouseId": nil,
	})
}

// handleT01WarehouseOrdersReady serves GET
// /api/v1/warehouse/orders/ready-to-pack and ready-to-ship, wired to
// the existing repository queries (same ReadyToPickOrder shape as the
// source ready-to-pick route): picked orders wait for packing, packed
// orders wait for carrier handover.
func handleT01WarehouseOrdersReady(w http.ResponseWriter, r *http.Request, ctx context.Context, repo *repository.WarehouseRepository, kind string) {
	if !t01MethodOnly(w, r, http.MethodGet) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var orders []repository.ReadyToPickOrder
	var err error
	switch kind {
	case "pack":
		orders, err = repo.GetOrdersReadyToPack(ctx)
	case "ship":
		orders, err = repo.GetOrdersReadyToShip(ctx)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if orders == nil {
		orders = []repository.ReadyToPickOrder{}
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"orders":  orders,
	})
}

// handleT01WarehouseShipments serves GET /api/v1/warehouse/shipments/
// (slash pattern: the live probe 301-redirects the bare path to the
// slash path, which is how Go's ServeMux treats a registered subtree
// pattern). It lists shipping legs; TODO: the exact live shipment
// shape is unverified.
func handleT01WarehouseShipments(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	rows, _, err := t01ListRows(r.Context(), db, "order_shipping_legs", "ORDER BY id ASC", t01Limit(r))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"shipments": rows})
}

// handleT01WarehouseReturnBacks serves GET
// /api/v1/warehouse/return-backs: customer returns that are approved
// or received and therefore on their way back to (or at) the
// warehouse. TODO: the exact live return-back filter is unverified;
// refunded/completed/rejected returns are excluded.
func handleT01WarehouseReturnBacks(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	rows, err := db.QueryContext(r.Context(),
		`SELECT id, order_id, customer_id, status, reason, admin_note, created_at, updated_at
		FROM customer_returns WHERE status IN ('approved', 'received') ORDER BY created_at DESC LIMIT $1`, t01Limit(r))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, orderID, customerID, status, reason, note string
		var created, updated interface{}
		if err := rows.Scan(&id, &orderID, &customerID, &status, &reason, &note, &created, &updated); err != nil {
			writeInternalError(w, err)
			return
		}
		out = append(out, map[string]interface{}{
			"id": id, "orderId": orderID, "customerId": customerID, "status": status,
			"reason": reason, "adminNote": note, "createdAt": created, "updatedAt": updated,
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"returnBacks": out})
}

// handleT01WarehousePutaway serves GET /api/v1/warehouse/putaway (the
// live base route, without /queue and /tasks, which remain registered
// separately). TODO: the exact live base semantics are unverified;
// the putaway queue list (existing repo query) is returned rather
// than an invented payload.
func handleT01WarehousePutaway(w http.ResponseWriter, r *http.Request, ctx context.Context, repo *repository.WarehouseRepository) {
	if !t01MethodOnly(w, r, http.MethodGet) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	queue, err := repo.GetPutawayQueue(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if queue == nil {
		queue = []repository.PutawayQueueItem{}
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"queue":   queue,
	})
}
