package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/drup/platform/internal/repository"
	"github.com/lib/pq"
)

// Notification inbox read paths.
//
// The SQL + envelope live here in shared helpers so the customer
// handlers (handleT01Notifications / handleT01NotificationsRead in
// customer_handlers.go), the supplier handlers below, and the
// seller/admin/warehouse handlers (role_notifications_handlers.go)
// stay thin wrappers over one implementation. The envelope is the
// same on every role route: {success, notifications, unreadCount},
// including when the outbox table is missing (empty list,
// unreadCount 0) — the missing-table 500 on mark-all (P1-15) is
// fixed by the table guard in markNotificationsReadForUser below.

// supplierNotificationFilter starts after the recipient argument ($1).
// These three body templates distinguish rows whose kind is shared across roles.
func supplierNotificationFilter() (string, []any) {
	return ` AND (
		kind = ANY($2)
		OR (kind = $3 AND body LIKE $4)
		OR (kind = $5 AND body LIKE $6)
		OR (kind = $7 AND body LIKE $8)
	)`, []any{
		pq.Array([]string{
			repository.NotifKindProposalRejected,
			repository.NotifKindProposalApproved,
			repository.NotifKindSupplierApproved,
			repository.NotifKindSupplierRejected,
			repository.NotifKindApprovalRejected,
			repository.NotifKindTicketReply,
			repository.NotifKindOrderPlaced,
			repository.NotifKindPaymentConfirmed,
			repository.NotifKindLowStock,
			repository.NotifKindProfitReleased,
			repository.NotifKindFinanceAdjustment,
			repository.NotifKindPayoutReviewed,
			repository.NotifKindPayoutTransfer,
			repository.NotifKindSupplierReturnReview,
			repository.NotifKindSupplierParcelCharge,
			repository.NotifKindReturnQCCharged,
			repository.NotifKindReturnShipped,
			repository.NotifKindStorageFeeCharged,
		}),
		repository.NotifKindDeliveryConfirmed, "%سود فروشنده پس از پایان مهلت مرجوعی آزاد می‌شود%",
		repository.NotifKindOrderCancelled, "%آماده‌سازی آن را متوقف کنید%",
		repository.NotifKindDisputeResolved, "%مسئولیت با تأمین‌کننده اعلام شد%",
	}
}

// The seller drawer covers all owned stores. Every current writer targets
// specific recipients, so the global set is empty and every included event
// is individual. A future platform-wide announcement belongs in the global set.
func sellerNotificationFilter() (string, []any) {
	// A supplier who owns the store can receive only the supplier-body delivery copy.
	const ownedStore = `EXISTS (SELECT 1 FROM stores
		WHERE stores.id = (notification_outbox.payload->>'store_id') AND stores.owner_user_id = $1::uuid)`
	return ` AND (
		kind = ANY($2)
		OR (kind = $3 AND (body LIKE $4 OR (body LIKE $5 AND ` + ownedStore + `)))
		OR (kind = $6 AND body LIKE $7)
		OR (kind = $8 AND payload->>'event' = 'order_cancelled')
		OR (kind = $9 AND payload->>'target_kind' = 'seller')
		OR (kind = ANY($10) AND ` + ownedStore + `)
		OR (kind = $11 AND EXISTS (SELECT 1 FROM support_tickets
			WHERE support_tickets.id = (notification_outbox.payload->>'ticket_id')
			AND requester_role = 'seller' AND requester_user_id = $1::uuid))
		OR (kind = $12 AND payload->>'entity_type' IN ('store_name','store_slug','custom_domain')
			AND EXISTS (SELECT 1 FROM pending_approvals pa
				WHERE pa.id = (notification_outbox.payload->>'approval_id')
				AND pa.target_id IN (SELECT id FROM stores WHERE owner_user_id = $1::uuid)))
	)`, []any{
		pq.Array([]string{
			repository.NotifKindOrderPlaced,
			repository.NotifKindPaymentConfirmed,
			repository.NotifKindProfitReleased,
			repository.NotifKindLowStock,
			repository.NotifKindSettlementCreated,
			repository.NotifKindSettlementSkipped,
		}),
		repository.NotifKindDeliveryConfirmed,
		"%سود شما پس از پایان مهلت مرجوعی آزاد می‌شود%",
		"%سود فروشنده پس از پایان مهلت مرجوعی آزاد می‌شود%",
		repository.NotifKindDisputeResolved, "%مسئولیت با فروشنده اعلام شد%",
		repository.NotifKindOrderCancelled,
		repository.NotifKindFinanceAdjustment,
		pq.Array([]string{repository.NotifKindPayoutReviewed, repository.NotifKindPayoutTransfer}),
		repository.NotifKindTicketReply,
		repository.NotifKindApprovalRejected,
	}
}

func listSellerNotifications(ctx context.Context, db *sql.DB, userID string) ([]map[string]interface{}, int, bool, error) {
	filter, args := sellerNotificationFilter()
	return listNotificationsWithFilter(ctx, db, userID, filter, args...)
}

func markSellerNotificationsRead(ctx context.Context, db *sql.DB, userID string, ids []string) (int64, error) {
	filter, args := sellerNotificationFilter()
	return markNotificationsReadWithFilter(ctx, db, userID, ids, filter, args...)
}

func listSupplierNotifications(ctx context.Context, db *sql.DB, userID string) ([]map[string]interface{}, int, bool, error) {
	filter, args := supplierNotificationFilter()
	return listNotificationsWithFilter(ctx, db, userID, filter, args...)
}

func markSupplierNotificationsRead(ctx context.Context, db *sql.DB, userID string, ids []string) (int64, error) {
	filter, args := supplierNotificationFilter()
	return markNotificationsReadWithFilter(ctx, db, userID, ids, filter, args...)
}

// listNotificationsForUser reads the outbox rows addressed to one
// recipient, newest first (LIMIT 100), plus the unread count
// (status <> 'read'). The third return value reports whether the
// notification_outbox table exists at all; when it does not, the
// caller answers with an empty list instead of an error.
func listNotificationsForUser(ctx context.Context, db *sql.DB, userID string) ([]map[string]interface{}, int, bool, error) {
	return listNotificationsWithFilter(ctx, db, userID, "")
}

func listNotificationsWithFilter(ctx context.Context, db *sql.DB, userID, kindFilter string, kindArgs ...any) ([]map[string]interface{}, int, bool, error) {
	if !t01TableExists(ctx, db, "notification_outbox") {
		return nil, 0, false, nil
	}
	filter := warehouseNotificationFilter(ctx, userID) + kindFilter
	args := append([]any{userID}, kindArgs...)
	var unreadCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_outbox
		WHERE recipient_user_id = $1::uuid AND status <> 'read'`+filter, args...).Scan(&unreadCount); err != nil {
		return nil, 0, true, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, kind, title, body, payload, status, created_at, sent_at FROM notification_outbox
		WHERE recipient_user_id = $1::uuid`+filter+` ORDER BY created_at DESC LIMIT 100`, args...)
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
// P1-15: the table-existence guard covers BOTH paths — the old
// mark-all path ran its UPDATE unguarded and 500d when
// notification_outbox did not exist, while the single-id path
// already guarded. A missing table now answers success + updated 0.
func markNotificationsReadForUser(ctx context.Context, db *sql.DB, userID string, ids []string) (int64, error) {
	return markNotificationsReadWithFilter(ctx, db, userID, ids, "")
}

func markNotificationsReadWithFilter(ctx context.Context, db *sql.DB, userID string, ids []string, kindFilter string, kindArgs ...any) (int64, error) {
	if !t01TableExists(ctx, db, "notification_outbox") {
		return 0, nil
	}
	filter := warehouseNotificationFilter(ctx, userID) + kindFilter
	args := append([]any{userID}, kindArgs...)
	if len(ids) == 0 {
		// Mark-all (live client POSTs with no body). Scoped to the
		// caller only; pending rows are included because no
		// pending->sent worker exists in this tree.
		res, err := db.ExecContext(ctx,
			`UPDATE notification_outbox SET status = 'read', read_at = NOW()
			WHERE recipient_user_id = $1::uuid AND status IN ('pending', 'sent')`+filter, args...)
		if err != nil {
			return 0, err
		}
		updated, _ := res.RowsAffected()
		return updated, nil
	}
	var updated int64
	// The ID is bound after the recipient and any filter parameters.
	args = append(args, nil)
	idPredicate := fmt.Sprintf(" AND id = $%d", len(args))
	for _, id := range ids {
		args[len(args)-1] = id
		res, err := db.ExecContext(ctx,
			`UPDATE notification_outbox SET status = 'read', read_at = NOW()
			WHERE recipient_user_id = $1::uuid AND status IN ('pending', 'sent')`+filter+idPredicate, args...)
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
// owner's notifications through this route.
func handleSupplierNotifications(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	items, unreadCount, exists, err := listSupplierNotifications(notificationContext(r), db, userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !exists {
		// P1-15: the missing-table envelope matches the normal one
		// (success + unreadCount), so clients never have to branch.
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "notifications": []interface{}{}, "unreadCount": 0})
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
	updated, err := markSupplierNotificationsRead(notificationContext(r), db, userID, ids)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": updated})
}
