package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

func handleSupplierTickets(w http.ResponseWriter, r *http.Request, stores *repository.StoreRepository, suppliers *repository.SupplierRepository) {
	handleAccountSupportTickets(w, r, stores, suppliers)
}

func handleAccountSupportTickets(w http.ResponseWriter, r *http.Request, stores *repository.StoreRepository, suppliers *repository.SupplierRepository) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var input struct {
		repository.SupportReferenceInput
		Subject string `json:"subject"`
		Message string `json:"message"`
	}
	if r.Method == http.MethodPost && !decodeSupportJSON(w, r, &input) {
		return
	}
	requestKey := ""
	if r.Method == http.MethodPost {
		keys := r.Header.Values("Idempotency-Key")
		if len(keys) > 1 || (len(keys) == 1 && len(keys[0]) != 36) {
			writeSupportError(w, repository.ErrStoreInput)
			return
		}
		if len(keys) == 1 {
			requestKey = keys[0]
		}
	}
	actor, ok := supportActor(w, r, suppliers, input.SupportReferenceInput)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// Preserve the response envelope of existing seller clients posting only
	// subject and message. New clients request a category and get the new ID.
	legacySellerPost := r.Method == http.MethodPost && actor.Role == "seller" && input.Kind == ""
	if r.Method == http.MethodPost && !legacySellerPost {
		id, err := stores.CreateSupportTicketWithKey(r.Context(), actor, input.SupportReferenceInput, input.Subject, input.Message, requestKey)
		if err != nil {
			writeSupportError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
		return
	}
	opts, err := parseListPage(r)
	if err != nil {
		http.Error(w, `{"error":"Invalid limit"}`, http.StatusBadRequest)
		return
	}
	if opts.Cursor != "" {
		if _, _, err := repository.DecodeCreatedAtCursor(opts.Cursor); err != nil {
			http.Error(w, `{"error":"cursor is invalid"}`, http.StatusBadRequest)
			return
		}
	}
	if legacySellerPost {
		if _, err := stores.CreateSupportTicketWithKey(r.Context(), actor, input.SupportReferenceInput, input.Subject, input.Message, requestKey); err != nil {
			writeSupportError(w, err)
			return
		}
	}
	page, err := stores.SupportTicketsPage(r.Context(), actor, opts)
	if err != nil {
		writeSupportError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]any{"tickets": page.Items, "nextCursor": page.NextCursor, "hasMore": page.HasMore, "limit": page.Limit})
}

func handleSellerTicketContext(w http.ResponseWriter, r *http.Request, stores *repository.StoreRepository) {
	handleAccountTicketContext(w, r, stores, nil)
}

func handleSupplierTicketContext(w http.ResponseWriter, r *http.Request, stores *repository.StoreRepository, suppliers *repository.SupplierRepository) {
	handleAccountTicketContext(w, r, stores, suppliers)
}

func handleAccountTicketContext(w http.ResponseWriter, r *http.Request, stores *repository.StoreRepository, suppliers *repository.SupplierRepository) {
	if !t01MethodOnly(w, r, http.MethodGet) {
		return
	}
	q := r.URL.Query()
	input := repository.SupportReferenceInput{Kind: q.Get("kind"), ReferenceID: q.Get("referenceId"), ReferenceType: q.Get("referenceType"), ReferenceCode: q.Get("referenceCode"), StoreID: q.Get("storeId")}
	if input.Kind == "" || input.Kind == "general" {
		writeSupportError(w, repository.ErrStoreInput)
		return
	}
	actor, ok := supportActor(w, r, suppliers, input)
	if !ok {
		return
	}
	ref, err := stores.ResolveSupportReference(r.Context(), actor, input)
	if err != nil {
		writeSupportError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	t01WriteJSON(w, http.StatusOK, ref)
}

func supportActor(w http.ResponseWriter, r *http.Request, suppliers *repository.SupplierRepository, input repository.SupportReferenceInput) (repository.SupportActor, bool) {
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return repository.SupportActor{}, false
	}
	if suppliers == nil {
		return repository.SupportActor{UserID: userID, Role: "seller"}, true
	}
	account, permissions, active, err := suppliers.GetSupplierAccess(r.Context(), userID)
	if err != nil {
		writeInternalError(w, err)
		return repository.SupportActor{}, false
	}
	permission := ""
	switch input.Kind {
	case "order", "return", "shipment":
		permission = "orders.manage"
		if input.ReferenceType == "stock_return" || input.ReferenceType == "inbound" || input.ReferenceType == "carton" {
			permission = "inventory.manage"
		}
	case "financial":
		permission = "finance.view"
	}
	if !active || account == "" || (permission != "" && !slices.Contains(permissions, permission) &&
		!slices.Contains(permissions, "supplier.manage") && !(permission == "finance.view" && slices.Contains(permissions, "finance.manage"))) {
		http.Error(w, `{"error":"به این بخش از حساب تأمین‌کننده دسترسی نداری."}`, http.StatusForbidden)
		return repository.SupportActor{}, false
	}
	return repository.SupportActor{UserID: userID, Role: "supplier", AccountID: account}, true
}

func decodeSupportJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil || decoder.Decode(new(any)) != io.EOF {
		writeSupportError(w, repository.ErrStoreInput)
		return false
	}
	return true
}

func writeSupportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrSupportContextNotFound), errors.Is(err, sql.ErrNoRows):
		http.Error(w, `{"error":"این مورد برای حساب شما پیدا نشد."}`, http.StatusNotFound)
	case errors.Is(err, repository.ErrStoreInput):
		http.Error(w, `{"error":"اطلاعات درخواست پشتیبانی معتبر نیست."}`, http.StatusBadRequest)
	case errors.Is(err, repository.ErrSupportRequestConflict):
		http.Error(w, `{"error":"این درخواست با متن یا مرجع دیگری ثبت شده است. پیام‌های بخش پشتیبانی را بررسی کن."}`, http.StatusConflict)
	default:
		writeInternalError(w, err)
	}
}

func handleAdminSupportTicket(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if id == "" || len(id) > 50 || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		var requester, status string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(requester_user_id::text,''),status FROM support_tickets
			WHERE id=$1 AND (NOT $2 OR section='finance')`, id, supportStaffFinanceOnly(r)).Scan(&requester, &status); err != nil {
			writeSupportError(w, err)
			return
		}
		rows, err := db.QueryContext(ctx, `SELECT id,COALESCE(author_user_id::text,''),body,is_internal,created_at
			FROM support_ticket_messages WHERE ticket_id=$1 ORDER BY created_at DESC,id DESC LIMIT 50`, id)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		type message struct {
			ID          string    `json:"id"`
			Body        string    `json:"body"`
			IsInternal  bool      `json:"isInternal"`
			IsRequester bool      `json:"isRequester"`
			CreatedAt   time.Time `json:"createdAt"`
		}
		messages := []message{}
		for rows.Next() {
			var m message
			var author string
			if err := rows.Scan(&m.ID, &author, &m.Body, &m.IsInternal, &m.CreatedAt); err != nil {
				writeInternalError(w, err)
				return
			}
			m.IsRequester = requester != "" && author == requester
			messages = append(messages, m)
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		slices.Reverse(messages)
		t01WriteJSON(w, http.StatusOK, map[string]any{"messages": messages, "status": status})
	case http.MethodPatch:
		var input struct {
			Status string `json:"status"`
		}
		if !decodeSupportJSON(w, r, &input) {
			return
		}
		switch input.Status {
		case "open", "assigned", "pending", "resolved", "closed":
		default:
			writeSupportError(w, repository.ErrStoreInput)
			return
		}
		var status string
		if err := db.QueryRowContext(ctx, `UPDATE support_tickets SET status=$2,updated_at=NOW()
			WHERE id=$1 AND (NOT $3 OR section='finance') RETURNING status`, id, input.Status, supportStaffFinanceOnly(r)).Scan(&status); err != nil {
			writeSupportError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]string{"status": status})
	default:
		writeMethodNotAllowed(w)
	}
}

func supportStaffFinanceOnly(r *http.Request) bool {
	claims, _ := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	return claims != nil && !claims.HasCapability("admin") && !claims.HasCapability("support_specialist") &&
		(claims.HasCapability("finance_specialist") || claims.HasCapability("finance"))
}
