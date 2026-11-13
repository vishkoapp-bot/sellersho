package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// Supplier notification read path (proposal_rejected outbox rows).
//
// The SQL + envelope live here in shared helpers so the customer
// handlers (handleT01Notifications / handleT01NotificationsRead in
// customer_handlers.go) and the supplier handlers below stay thin
// wrappers over one implementation. The customer envelope is
// byte-identical to before the extraction, including the
// missing-table behaviour (empty list, no "success" key) and the
// mark-all path running its UPDATE without a table-existence guard.

// listNotificationsForUser reads the outbox rows addressed to one
// recipient, newest first (LIMIT 100), plus the unread count
// (status <> 'read'). The third return value reports whether the
// notification_outbox table exists at all; when it does not, the
// caller answers with an empty list instead of an error.
func listNotificationsForUser(ctx context.Context, db *sql.DB, userID string) ([]map[string]interface{}, int, bool, error) {
	if !t01TableExists(ctx, db, "notification_outbox") {
		return nil, 0, false, nil
	}
	var unreadCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_outbox
		WHERE recipient_user_id = $1::uuid AND status <> 'read'`, userID).Scan(&unreadCount); err != nil {
		return nil, 0, true, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, kind, title, body, payload, status, created_at, sent_at FROM notification_outbox
		WHERE recipient_user_id = $1::uuid ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, 0, true, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, kind, title, body, payload, status string
		var created time.Time
		var sentAt sql.NullTime
		if err := rows.Scan(&id, &kind, &title, &body, &payload, &status, &created, &sentAt); err != nil {
			return nil, 0, true, err
		}
		item := map[string]interface{}{
			"id": id, "kind": kind, "title": title, "body": body,
			"payload": json.RawMessage(payload),
			"status":  status, "createdAt": created.Format(time.RFC3339),
		}
		if sentAt.Valid {
			item["sentAt"] = sentAt.Time.Format(time.RFC3339)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, true, err
	}
	return out, unreadCount, true, nil
}

// markNotificationsReadForUser flips outbox rows to 'read' for one
// recipient only. ids empty means mark-all (pending + sent rows);
// otherwise each id is flipped individually. The UPDATE is
// idempotent: already-read rows match nothing and report updated 0,
// which the client must treat as success, not an error.
func markNotificationsReadForUser(ctx context.Context, db *sql.DB, userID string, ids []string) (int64, error) {
	if len(ids) == 0 {
		// Mark-all (live client POSTs with no body). Scoped to the
		// caller only; pending rows are included because no
		// pending->sent worker exists in this tree.
		res, err := db.ExecContext(ctx,
			`UPDATE notification_outbox SET status = 'read', read_at = NOW()
			WHERE recipient_user_id = $1::uuid AND status IN ('pending', 'sent')`, userID)
		if err != nil {
			return 0, err
		}
		updated, _ := res.RowsAffected()
		return updated, nil
	}
	if !t01TableExists(ctx, db, "notification_outbox") {
		return 0, nil
	}
	var updated int64
	for _, id := range ids {
		res, err := db.ExecContext(ctx,
			`UPDATE notification_outbox SET status = 'read', read_at = NOW()
			WHERE id = $1 AND recipient_user_id = $2::uuid AND status IN ('pending', 'sent')`, id, userID)
		if err != nil {
			return updated, err
		}
		n, _ := res.RowsAffected()
		updated += n
	}
	return updated, nil
}

// handleSupplierNotifications serves GET /api/v1/supplier/notifications.
// The recipient is the caller's JWT user id, which is the supplier
// owner's id (RejectProposal addresses rows to
// suppliers.owner_user_id). Team members therefore do not see the
// owner's rejection notifications in v1 — by design.
func handleSupplierNotifications(w http.ResponseWriter, r *http.Request, db *sql.DB) {
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
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"notifications": []interface{}{}})
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "notifications": items, "unreadCount": unreadCount})
}

// handleSupplierNotificationsRead serves POST
// /api/v1/supplier/notifications/read. Accepts {id}, {ids}, or an
// empty body (= mark-all), scoped to the caller.
func handleSupplierNotificationsRead(w http.ResponseWriter, r *http.Request, db *sql.DB) {
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
