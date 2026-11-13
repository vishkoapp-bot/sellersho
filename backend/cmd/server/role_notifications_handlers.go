package main

import (
	"database/sql"
	"net/http"
)

// Seller / admin / warehouse notification read paths (Phase 2, W2).
//
// All three roles read the SAME notification_outbox table through the
// shared helpers in supplier_notifications_handlers.go
// (listNotificationsForUser / markNotificationsReadForUser), with the
// SAME envelope as the customer and supplier routes:
// {success, notifications, unreadCount} on GET, {success, updated}
// on POST read. No new notification infra, no new kinds, no new
// migration — these endpoints only expose rows W1 writers address to
// the caller.
//
// Identity mapping (verified in code, not assumed):
//   - JWT UserID is users.id for every role: the OTP login mints the
//     token from user.ID (auth_handlers.go GenerateTokenWithCapabilities
//     call), and GenerateToken sets Capabilities=[role].
//   - Seller = the store owner: stores.owner_user_id is the seller's
//     user id (store repository), and the "seller" capability is
//     granted on store creation. W1 writers address order/finance
//     notices to that same id, so the seller sees them here.
//   - Admin = users with the admin capability (RequireRoles "admin"
//     also admits the super_admin capability). W1 writers address
//     dispute-filed etc. rows to the admin users' ids.
//   - Warehouse = users with role warehouse_staff or
//     logistics_specialist (same warehouseRoles gate as the other
//     /api/v1/warehouse/* routes; the dev seeder creates the central
//     warehouse operator as a users row with role warehouse_staff).
//     W1 writers address inbound/return-QC notices to those users'
//     ids. There is no separate warehouse-staff identity table to map
//     through — the users row IS the identity, so no mapping gap.
//
// Team-member scoping mirrors the supplier endpoint by design: the
// recipient is always the caller's JWT user id, never a shared queue.

func handleRoleNotifications(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	items, unreadCount, exists, err := listNotificationsForUser(r.Context(), db, userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !exists {
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "notifications": []interface{}{}, "unreadCount": 0})
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "notifications": items, "unreadCount": unreadCount})
}

func handleRoleNotificationsRead(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var req struct {
		ID  string   `json:"id"`
		IDs []string `json:"ids"`
	}
	if r.ContentLength != 0 && !t01DecodeJSON(w, r, &req) {
		return
	}
	ids := req.IDs
	if req.ID != "" {
		ids = append(ids, req.ID)
	}
	updated, err := markNotificationsReadForUser(r.Context(), db, userID, ids)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": updated})
}

// handleSellerNotifications serves GET /api/v1/seller/notifications:
// outbox rows addressed to the calling store owner.
func handleSellerNotifications(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	handleRoleNotifications(w, r, db)
}

// handleSellerNotificationsRead serves POST
// /api/v1/seller/notifications/read. Accepts {id}, {ids}, or an
// empty body (= mark-all), scoped to the caller.
func handleSellerNotificationsRead(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	handleRoleNotificationsRead(w, r, db)
}

// handleAdminNotifications serves GET /api/v1/admin/notifications:
// outbox rows addressed to the calling admin user.
func handleAdminNotifications(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	handleRoleNotifications(w, r, db)
}

// handleAdminNotificationsRead serves POST
// /api/v1/admin/notifications/read. Accepts {id}, {ids}, or an
// empty body (= mark-all), scoped to the caller.
func handleAdminNotificationsRead(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	handleRoleNotificationsRead(w, r, db)
}

// handleWarehouseNotifications serves GET
// /api/v1/warehouse/notifications: outbox rows addressed to the
// calling warehouse staff user.
func handleWarehouseNotifications(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	handleRoleNotifications(w, r, db)
}

// handleWarehouseNotificationsRead serves POST
// /api/v1/warehouse/notifications/read. Accepts {id}, {ids}, or an
// empty body (= mark-all), scoped to the caller.
func handleWarehouseNotificationsRead(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	handleRoleNotificationsRead(w, r, db)
}
