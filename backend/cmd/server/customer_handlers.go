package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// Team 01 (Phase 2, live parity): customer APIs (MASTER-AUDIT D06, D07)
// and logout (D08).
//
// Confirmed existence evidence (Phase-1 team report §3.2): unauthenticated
// probes return 401 for wishlist/addresses/customer-*/notifications/
// views paths, 400 for /reviews without productId, and 405 for GET
// /newsletter/subscribe (POST only). Roles and bodies for the 401
// routes are unverified; the "customer" role is used here because the
// live frontend calls these from the customer panel, and every
// unverified body choice is marked TODO at its handler.

// --- wishlist (D06) ---------------------------------------------------------

// handleT01Wishlist serves /api/v1/wishlist and is registered for both
// /wishlist and /customer/wishlist-style callers through main.go.
// Table: customer_wishlists (user_id, store_id, product_id) — created
// by migration 202610310002, given its per-store dimension by
// 202611050002. Wishlists are PER STORE (owner doctrine: every
// dropshipper store is a separate site with its own wishlist), so
// every operation is scoped by the store the caller names: the
// storeSlug query param on GET, the storeSlug JSON field on
// POST/DELETE. A missing slug means the central store
// ('store-central'), preserving the pre-split behaviour for legacy
// callers; an unknown slug fails closed with 404, mirroring order
// creation — a heart must never land in the wrong store's list.
// TODO: the exact live response envelope is unverified.
func handleT01Wishlist(w http.ResponseWriter, r *http.Request, db *sql.DB, storeRepo *repository.StoreRepository) {
	if t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		storeID, ok := t01WishlistStoreID(w, r, storeRepo, r.URL.Query().Get("storeSlug"))
		if !ok {
			return
		}
		if !t01TableExists(ctx, db, "customer_wishlists") {
			t01WriteJSON(w, http.StatusOK, map[string]interface{}{"wishlist": []string{}})
			return
		}
		rows, err := db.QueryContext(ctx,
			`SELECT product_id FROM customer_wishlists WHERE user_id = $1 AND store_id = $2 ORDER BY created_at DESC`, userID, storeID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				writeInternalError(w, err)
				return
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"wishlist": ids})
	case http.MethodPost:
		var req struct {
			ProductID string `json:"productId"`
			StoreSlug string `json:"storeSlug"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.ProductID) == "" {
			http.Error(w, `{"error":"productId is required"}`, http.StatusBadRequest)
			return
		}
		storeID, ok := t01WishlistStoreID(w, r, storeRepo, req.StoreSlug)
		if !ok {
			return
		}
		if !t01TableExists(ctx, db, "customer_wishlists") {
			t01Stub(w, r, "D06", "customer_wishlists table is absent in this database.")
			return
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO customer_wishlists (user_id, store_id, product_id) VALUES ($1, $2, $3) ON CONFLICT (user_id, store_id, product_id) DO NOTHING`,
			userID, storeID, req.ProductID); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "productId": req.ProductID})
	case http.MethodDelete:
		var req struct {
			ProductID string `json:"productId"`
			StoreSlug string `json:"storeSlug"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.ProductID) == "" {
			http.Error(w, `{"error":"productId is required"}`, http.StatusBadRequest)
			return
		}
		storeID, ok := t01WishlistStoreID(w, r, storeRepo, req.StoreSlug)
		if !ok {
			return
		}
		if _, err := db.ExecContext(ctx,
			`DELETE FROM customer_wishlists WHERE user_id = $1 AND store_id = $2 AND product_id = $3`, userID, storeID, req.ProductID); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "productId": req.ProductID})
	default:
		writeMethodNotAllowed(w)
	}
}

// t01WishlistStoreID resolves the wishlist's store dimension. An
// empty slug is the central store (legacy callers named no store).
// Any other slug must resolve through the store repo; an unknown one
// fails closed with 404 — the same rule order creation applies.
func t01WishlistStoreID(w http.ResponseWriter, r *http.Request, storeRepo *repository.StoreRepository, slug string) (string, bool) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "store-central", true
	}
	if storeRepo == nil {
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return "", false
	}
	store, err := storeRepo.GetStoreBySlug(r.Context(), slug)
	if err != nil {
		writeInternalError(w, err)
		return "", false
	}
	if store == nil {
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return "", false
	}
	return store.ID, true
}

// --- addresses (D06) ---------------------------------------------------------
//
// The address book is user_addresses (migration 202610230006, the
// checkout address-book table behind D63). A second table,
// customer_addresses (migration 202610310002), also exists; which one
// the live handleAddresses reads is unverified (TODO), so this handler
// uses user_addresses and never writes customer_addresses.

type t01Address struct {
	ID            string  `json:"id"`
	Label         *string `json:"label,omitempty"`
	ReceiverName  string  `json:"receiverFullName"`
	ReceiverPhone *string `json:"receiverPhone,omitempty"`
	ProvinceName  string  `json:"provinceName"`
	CityName      string  `json:"cityName"`
	PostalCode    *string `json:"postalCode,omitempty"`
	AddressDetail string  `json:"addressDetail"`
	IsDefault     bool    `json:"isDefault"`
}

func t01QueryAddresses(w http.ResponseWriter, r *http.Request, db *sql.DB, userID string) []t01Address {
	rows, err := db.QueryContext(r.Context(),
		`SELECT id, label, receiver_full_name, receiver_phone, province_name, city_name,
			postal_code, address_detail, is_default FROM user_addresses
		WHERE user_id = $1::uuid ORDER BY is_default DESC, created_at ASC`, userID)
	if err != nil {
		writeInternalError(w, err)
		return nil
	}
	defer rows.Close()
	out := []t01Address{}
	for rows.Next() {
		var a t01Address
		if err := rows.Scan(&a.ID, &a.Label, &a.ReceiverName, &a.ReceiverPhone, &a.ProvinceName,
			&a.CityName, &a.PostalCode, &a.AddressDetail, &a.IsDefault); err != nil {
			writeInternalError(w, err)
			return nil
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return nil
	}
	return out
}

// handleT01Addresses serves GET/POST /api/v1/addresses and
// /api/v1/customer/addresses (both prefixes are registered to it).
func handleT01Addresses(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		addrs := t01QueryAddresses(w, r, db, userID)
		if addrs == nil {
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"addresses": addrs})
	case http.MethodPost:
		var req struct {
			Label         string `json:"label"`
			ReceiverName  string `json:"receiverFullName"`
			ReceiverPhone string `json:"receiverPhone"`
			ProvinceName  string `json:"provinceName"`
			CityName      string `json:"cityName"`
			PostalCode    string `json:"postalCode"`
			AddressDetail string `json:"addressDetail"`
			IsDefault     bool   `json:"isDefault"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.ReceiverName) == "" || strings.TrimSpace(req.ProvinceName) == "" ||
			strings.TrimSpace(req.CityName) == "" || strings.TrimSpace(req.AddressDetail) == "" {
			http.Error(w, `{"error":"receiverFullName, provinceName, cityName and addressDetail are required"}`, http.StatusBadRequest)
			return
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer tx.Rollback()
		if req.IsDefault {
			if _, err := tx.ExecContext(ctx,
				`UPDATE user_addresses SET is_default = FALSE WHERE user_id = $1::uuid`, userID); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		id := uuid.NewString()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_addresses (id, user_id, label, receiver_full_name, receiver_phone,
				province_name, city_name, postal_code, address_detail, is_default)
			VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10)`,
			id, userID, nullIfEmpty(req.Label), req.ReceiverName, nullIfEmpty(req.ReceiverPhone),
			req.ProvinceName, req.CityName, nullIfEmpty(req.PostalCode), req.AddressDetail, req.IsDefault); err != nil {
			writeInternalError(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": id})
	default:
		writeMethodNotAllowed(w)
	}
}

// handleT01AddressByID serves DELETE /api/v1/addresses/<id> and
// /api/v1/customer/addresses/<id>. Deleting the default address promotes
// the oldest remaining address (migration 202610230006 invariant).
func handleT01AddressByID(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if !t01MethodOnly(w, r, http.MethodDelete) || t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer tx.Rollback()
	var wasDefault bool
	err = tx.QueryRowContext(ctx,
		`DELETE FROM user_addresses WHERE id = $1 AND user_id = $2::uuid RETURNING is_default`,
		id, userID).Scan(&wasDefault)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"Address not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if wasDefault {
		_, _ = tx.ExecContext(ctx,
			`UPDATE user_addresses SET is_default = TRUE WHERE id = (
				SELECT id FROM user_addresses WHERE user_id = $1::uuid ORDER BY created_at ASC LIMIT 1)`, userID)
	}
	if err := tx.Commit(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func nullIfEmpty(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// --- reviews (D06) ------------------------------------------------------------

// handleT01Reviews serves GET /api/v1/reviews (public). The probe
// confirms the exact behaviour: 400 "productId is required" without
// productId. Table: product_reviews (migration 202610310002).
func handleT01Reviews(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	productID := strings.TrimSpace(r.URL.Query().Get("productId"))
	if productID == "" {
		// FIX 14b: live answers this 400 as application/json (probed
		// 2026-11-03); http.Error would serve text/plain.
		t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "productId is required"})
		return
	}
	rows, err := db.QueryContext(r.Context(),
		`SELECT id, product_id, user_id, rating, comment, status, created_at
		FROM product_reviews WHERE product_id = $1 AND status = 'published' ORDER BY created_at DESC`,
		productID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	type review struct {
		ID        string    `json:"id"`
		ProductID string    `json:"productId"`
		UserID    string    `json:"userId"`
		Rating    int       `json:"rating"`
		Comment   string    `json:"comment"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"createdAt"`
	}
	out := []review{}
	for rows.Next() {
		var rv review
		if err := rows.Scan(&rv.ID, &rv.ProductID, &rv.UserID, &rv.Rating, &rv.Comment, &rv.Status, &rv.CreatedAt); err != nil {
			writeInternalError(w, err)
			return
		}
		out = append(out, rv)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	// FIX 14: live returns the RAW array (probed 2026-11-03:
	// GET /reviews?productId=... -> []), not {"reviews":[...]}.
	t01WriteJSON(w, http.StatusOK, out)
}

// handleT01CustomerReviews serves GET/POST /api/v1/customer/reviews.
func handleT01CustomerReviews(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		rows, err := db.QueryContext(ctx,
			`SELECT id, product_id, rating, comment, status, created_at FROM product_reviews
			WHERE user_id = $1 ORDER BY created_at DESC`, userID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var id, productID, comment, status string
			var rating int
			var created time.Time
			if err := rows.Scan(&id, &productID, &rating, &comment, &status, &created); err != nil {
				writeInternalError(w, err)
				return
			}
			out = append(out, map[string]interface{}{
				"id": id, "productId": productID, "rating": rating,
				"comment": comment, "status": status, "createdAt": created.Format(time.RFC3339),
			})
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		// FIX 14 (reverted for this endpoint): only the public /reviews
		// raw-array shape is live-proven; /customer/reviews keeps its
		// previous {"reviews": [...]} wrapper (live shape unprobed).
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"reviews": out})
	case http.MethodPost:
		var req struct {
			ProductID string `json:"productId"`
			Rating    int    `json:"rating"`
			Comment   string `json:"comment"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.ProductID) == "" || req.Rating < 1 || req.Rating > 5 {
			http.Error(w, `{"error":"productId and a rating between 1 and 5 are required"}`, http.StatusBadRequest)
			return
		}
		id := "rev-" + uuid.NewString()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO product_reviews (id, product_id, user_id, rating, comment) VALUES ($1, $2, $3, $4, $5)`,
			id, req.ProductID, userID, req.Rating, req.Comment); err != nil {
			if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
				http.Error(w, `{"error":"Review already exists for this product"}`, http.StatusConflict)
				return
			}
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": id})
	default:
		writeMethodNotAllowed(w)
	}
}

// --- notifications (D06) -------------------------------------------------------
//
// Table: notification_outbox (migration 202610180005). The migration
// header notes it was not applied to prod at write time, so reads are
// guarded: a database without the table returns an empty list.
// TODO: the exact live notification envelope is unverified.

func handleT01Notifications(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	if !t01TableExists(ctx, db, "notification_outbox") {
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"notifications": []interface{}{}})
		return
	}
	var unreadCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_outbox
		WHERE recipient_user_id = $1::uuid AND status <> 'read'`, userID).Scan(&unreadCount); err != nil {
		writeInternalError(w, err)
		return
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, kind, title, body, payload, status, created_at, sent_at FROM notification_outbox
		WHERE recipient_user_id = $1::uuid ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, kind, title, body, payload, status string
		var created time.Time
		var sentAt sql.NullTime
		if err := rows.Scan(&id, &kind, &title, &body, &payload, &status, &created, &sentAt); err != nil {
			writeInternalError(w, err)
			return
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
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "notifications": out, "unreadCount": unreadCount})
}

// handleT01NotificationsRead serves POST /api/v1/notifications/read.
// Per migration 202610180005, read flips 'sent' -> 'read'.
// TODO: the exact live read contract (single id vs ids vs mark-all)
// is unverified; {id} and {ids: [...]} are both accepted.
func handleT01NotificationsRead(w http.ResponseWriter, r *http.Request, db *sql.DB) {
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
	if len(ids) == 0 {
		// Mark-all (live client POSTs /notifications/read with no body).
		// Scoped to the caller only; pending rows are included because no
		// pending->sent worker exists in this tree (see GET above).
		res, err := db.ExecContext(r.Context(),
			`UPDATE notification_outbox SET status = 'read', read_at = NOW()
			WHERE recipient_user_id = $1::uuid AND status IN ('pending', 'sent')`, userID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		updated, _ := res.RowsAffected()
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": updated})
		return
	}
	if !t01TableExists(r.Context(), db, "notification_outbox") {
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": 0})
		return
	}
	var updated int64
	for _, id := range ids {
		res, err := db.ExecContext(r.Context(),
			`UPDATE notification_outbox SET status = 'read', read_at = NOW()
			WHERE id = $1 AND recipient_user_id = $2::uuid AND status IN ('pending', 'sent')`, id, userID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		n, _ := res.RowsAffected()
		updated += n
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": updated})
}

// --- view events (D06/D51) ------------------------------------------------------
//
// POST /api/v1/views/events records a storefront view into
// store_view_daily / product_view_daily with the dedupe rule of
// migration 202610230002: one daily token counts at most ONE product
// view per product and ONE store view per store per Tehran day.
// Only the SHA-256 hash of the client token is stored; no IP and no
// user id are recorded. The route is public (anonymous view counting);
// the token is a client daily token, not an auth JWT.

func handleT01ViewEvent(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	var req struct {
		StoreSlug string `json:"storeSlug"`
		ProductID string `json:"productId"`
		Kind      string `json:"kind"`
		Token     string `json:"token"`
	}
	// Accept the daily-token field under its alternate names.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	_ = json.Unmarshal(raw["storeSlug"], &req.StoreSlug)
	_ = json.Unmarshal(raw["productId"], &req.ProductID)
	_ = json.Unmarshal(raw["kind"], &req.Kind)
	for _, k := range []string{"token", "dailyToken", "clientToken", "viewToken"} {
		if v, ok := raw[k]; ok {
			_ = json.Unmarshal(v, &req.Token)
			if req.Token != "" {
				break
			}
		}
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		if req.ProductID != "" {
			kind = "product"
		} else {
			kind = "store"
		}
	}
	if kind == "store" && strings.TrimSpace(req.StoreSlug) == "" {
		http.Error(w, `{"error":"storeSlug is required for store views"}`, http.StatusBadRequest)
		return
	}
	if kind == "product" && strings.TrimSpace(req.ProductID) == "" {
		http.Error(w, `{"error":"productId is required for product views"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	day := t01TehranDay(time.Now())
	target := req.StoreSlug
	if kind == "product" {
		target = req.ProductID
	}
	counted := true
	if strings.TrimSpace(req.Token) != "" {
		res, err := db.ExecContext(ctx,
			`INSERT INTO view_dedupe_daily (token_hash, kind, target_id, day) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
			t01HashToken(req.Token), kind, target, day)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		n, _ := res.RowsAffected()
		counted = n > 0
	}
	if counted {
		switch kind {
		case "store":
			_, err := db.ExecContext(ctx,
				`INSERT INTO store_view_daily (store_slug, day, views) VALUES ($1, $2, 1)
				ON CONFLICT (store_slug, day) DO UPDATE SET views = store_view_daily.views + 1`,
				req.StoreSlug, day)
			if err != nil {
				writeInternalError(w, err)
				return
			}
		case "product":
			// TODO: product events without a storeSlug are counted
			// under an empty store slug; the live mapping of a
			// product view to a store slug is unverified.
			_, err := db.ExecContext(ctx,
				`INSERT INTO product_view_daily (store_slug, product_id, day, views) VALUES ($1, $2, $3, 1)
				ON CONFLICT (store_slug, product_id, day) DO UPDATE SET views = product_view_daily.views + 1`,
				req.StoreSlug, req.ProductID, day)
			if err != nil {
				writeInternalError(w, err)
				return
			}
		}
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "counted": counted})
}

// --- newsletter (D06) ------------------------------------------------------------
//
// POST only: the live probe returns 405 for GET /newsletter/subscribe.
// Table: newsletter_subscribers (migration 202610200004), one row per
// (store, mobile). Storefront templates send storeSlug, which is
// resolved to the store id (unknown slug → 400, never a silent
// fall back to central); storeId is still accepted for compatibility
// and storeSlug wins when both are present. When neither is given,
// the central store is used.

func t01CentralStoreID(ctx context.Context, db *sql.DB) string {
	var id string
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM stores WHERE owner_type = 'central' ORDER BY id LIMIT 1`).Scan(&id); err != nil {
		return ""
	}
	return id
}

// t01StoreIDBySlug resolves a store slug to its id with the same
// case-insensitive match StoreRepository.GetStoreBySlug performs.
// The T01 handlers receive only *sql.DB (no repo is in scope here),
// so the lookup runs on it directly, mirroring t01CentralStoreID.
func t01StoreIDBySlug(ctx context.Context, db *sql.DB, slug string) (string, bool, error) {
	var id string
	err := db.QueryRowContext(ctx,
		`SELECT id FROM stores WHERE LOWER(slug) = LOWER($1)`, slug).Scan(&id)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

func t01RandomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return uuid.NewString()
	}
	return hex.EncodeToString(b)
}

func handleT01NewsletterSubscribe(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	var req struct {
		Mobile    string `json:"mobile"`
		StoreID   string `json:"storeId"`
		StoreSlug string `json:"storeSlug"`
		Email     string `json:"email"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	mobile := t01NormalizeMobile(req.Mobile)
	if mobile == "" {
		http.Error(w, `{"error":"mobile is required"}`, http.StatusBadRequest)
		return
	}
	storeID := strings.TrimSpace(req.StoreID)
	if slug := strings.TrimSpace(req.StoreSlug); slug != "" {
		// storeSlug (sent by every storefront template) wins over
		// storeId and resolves fail-closed: an unknown slug is a
		// 400, never a silent fall back to the central store —
		// that fall back is what used to misfile every storefront
		// subscription under central.
		resolved, found, err := t01StoreIDBySlug(r.Context(), db, slug)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if !found {
			http.Error(w, `{"error":"unknown storeSlug"}`, http.StatusBadRequest)
			return
		}
		storeID = resolved
	}
	if storeID == "" {
		storeID = t01CentralStoreID(r.Context(), db)
	}
	if storeID == "" {
		http.Error(w, `{"error":"storeId is required"}`, http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	id := "sub-" + uuid.NewString()
	if _, err := db.ExecContext(r.Context(),
		`INSERT INTO newsletter_subscribers (id, store_id, mobile, email, status, source, unsubscribe_token)
		VALUES ($1, $2, $3, $4, 'active', 'footer', $5)
		ON CONFLICT (store_id, mobile) DO UPDATE SET status = 'active', unsubscribed_at = NULL, email = EXCLUDED.email`,
		id, storeID, mobile, nullIfEmpty(email), t01RandomToken()); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "storeId": storeID, "mobile": mobile})
}

func handleT01NewsletterUnsubscribe(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	var req struct {
		Token   string `json:"token"`
		Mobile  string `json:"mobile"`
		StoreID string `json:"storeId"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	var res sql.Result
	var err error
	if strings.TrimSpace(req.Token) != "" {
		res, err = db.ExecContext(ctx,
			`UPDATE newsletter_subscribers SET status = 'unsubscribed', unsubscribed_at = NOW()
			WHERE unsubscribe_token = $1 AND status = 'active'`, req.Token)
	} else if strings.TrimSpace(req.Mobile) != "" && strings.TrimSpace(req.StoreID) != "" {
		res, err = db.ExecContext(ctx,
			`UPDATE newsletter_subscribers SET status = 'unsubscribed', unsubscribed_at = NOW()
			WHERE store_id = $1 AND mobile = $2 AND status = 'active'`,
			req.StoreID, t01NormalizeMobile(req.Mobile))
	} else {
		http.Error(w, `{"error":"token or mobile+storeId is required"}`, http.StatusBadRequest)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	n, _ := res.RowsAffected()
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": n})
}

// --- geo (D06) --------------------------------------------------------------------
//
// Served from tapin_geo_snapshots (migration 202610230006): kind
// 'tree' for provinces, 'cities:<province_code>' for cities. The live
// probe returns 401 for /geo/provinces, so these routes require a
// valid token (any role).
//
// Merge-phase backend: the picker (ProvinceCityPicker, live dC port)
// reads {available, items:[{code, name}]}. The stored tree payload is
// [{code, title, cities:[{code, title}]}] — NOT {code, name} — so both
// handlers map title -> name (trimmed) and report available only when
// a non-empty list was actually produced. Cities fall back to the
// tree's nested list when no per-province snapshot row exists (on
// current data only the tree row exists; per-province rows were only
// "reserved" and never populated). A missing/empty dataset yields
// available:false, which is exactly the picker's text-input
// degradation — the live unavailable behavior.

// t01GeoMapItem maps one raw snapshot item to the picker's {code,
// name} shape. Code is kept as-is (number or string); name comes
// from title, then name, then label, trimmed. Items without a code
// or without a usable name are skipped: the picker matches by name
// and resolves by code, so a nameless or codeless entry is useless.
func t01GeoMapItem(raw map[string]interface{}) (map[string]interface{}, bool) {
	code, hasCode := raw["code"]
	if !hasCode || code == nil {
		return nil, false
	}
	name := ""
	for _, key := range []string{"title", "name", "label"} {
		if s, ok := raw[key].(string); ok && strings.TrimSpace(s) != "" {
			name = strings.TrimSpace(s)
			break
		}
	}
	if name == "" {
		return nil, false
	}
	return map[string]interface{}{"code": code, "name": name}, true
}

// t01GeoMapList maps a snapshot payload to picker items. A JSON list
// maps directly; a JSON object with a single list-valued field
// (provinces/items/children) unwraps one level. Anything else yields
// no items.
func t01GeoMapList(payload []byte) []map[string]interface{} {
	var asList []map[string]interface{}
	if err := json.Unmarshal(payload, &asList); err == nil {
		out := []map[string]interface{}{}
		for _, m := range asList {
			if item, ok := t01GeoMapItem(m); ok {
				out = append(out, item)
			}
		}
		return out
	}
	var asObject map[string]interface{}
	if err := json.Unmarshal(payload, &asObject); err != nil {
		return nil
	}
	for _, key := range []string{"provinces", "items", "children"} {
		nested, ok := asObject[key].([]interface{})
		if !ok {
			continue
		}
		out := []map[string]interface{}{}
		for _, n := range nested {
			if m, ok := n.(map[string]interface{}); ok {
				if item, ok := t01GeoMapItem(m); ok {
					out = append(out, item)
				}
			}
		}
		return out
	}
	return nil
}

// t01GeoCodeString renders a snapshot code for comparison: numbers
// and strings compare equal when their text matches ("1" == 1).
func t01GeoCodeString(code interface{}) string {
	switch v := code.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

// t01GeoProvinceParam decodes the picker's province query value. The
// picker sends province=<JSON-encoded code>, so a string code arrives
// quoted ("12") while a numeric code arrives bare (12). Both decode
// to the bare code text used for snapshot-kind and tree-node matching.
func t01GeoProvinceParam(r *http.Request) string {
	raw := strings.TrimSpace(r.URL.Query().Get("province"))
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("provinceCode"))
	}
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("provinceId"))
	}
	if raw == "" {
		return ""
	}
	var asString string
	if strings.HasPrefix(raw, "\"") && json.Unmarshal([]byte(raw), &asString) == nil {
		return strings.TrimSpace(asString)
	}
	return raw
}

// t01GeoTreePayload loads the 'tree' snapshot payload, or nil when
// the row is missing or empty.
func t01GeoTreePayload(ctx context.Context, db *sql.DB) []byte {
	var payload []byte
	if err := db.QueryRowContext(ctx,
		`SELECT payload FROM tapin_geo_snapshots WHERE kind = 'tree'`).Scan(&payload); err != nil || len(payload) == 0 {
		return nil
	}
	return payload
}

func handleT01GeoProvinces(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	items := []map[string]interface{}{}
	if payload := t01GeoTreePayload(r.Context(), db); payload != nil {
		items = t01GeoMapList(payload)
	}
	// available is true only when a non-empty list was produced; an
	// absent/empty dataset is the picker's text-input degradation.
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"available": len(items) > 0, "provinces": items})
}

func handleT01GeoCities(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	province := t01GeoProvinceParam(r)
	if province == "" {
		http.Error(w, `{"error":"province is required"}`, http.StatusBadRequest)
		return
	}
	items := []map[string]interface{}{}
	// 1) Per-province snapshot row, when populated.
	var payload []byte
	if err := db.QueryRowContext(r.Context(),
		`SELECT payload FROM tapin_geo_snapshots WHERE kind = $1`, "cities:"+province).Scan(&payload); err == nil && len(payload) > 0 {
		items = t01GeoMapList(payload)
	}
	// 2) Fallback: the tree's nested city list for the province node.
	// Per-province rows were only "reserved" and never populated, so
	// on current data this is the path that actually serves cities.
	if len(items) == 0 {
		if tree := t01GeoTreePayload(r.Context(), db); tree != nil {
			var nodes []map[string]interface{}
			if err := json.Unmarshal(tree, &nodes); err == nil {
				for _, node := range nodes {
					if t01GeoCodeString(node["code"]) != province {
						continue
					}
					if nested, ok := node["cities"].([]interface{}); ok {
						for _, n := range nested {
							if m, ok := n.(map[string]interface{}); ok {
								if item, ok := t01GeoMapItem(m); ok {
									items = append(items, item)
								}
							}
						}
					}
					break
				}
			}
		}
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"available": len(items) > 0, "cities": items})
}

// --- customer wallet (D07) ----------------------------------------------------------
//
// Kept SEPARATE from the seller wallet. Tables: customer_wallets
// (balance, locked_balance; migrations 202610160006, 202610200002)
// and customer_withdrawal_requests (lifecycle pending -> approved ->
// paid; pending|approved -> rejected; pending -> cancelled).
// This handler never assumes wallet_amount/gateway_amount on orders:
// those order-snapshot columns belong to the orders team (D38).

var t01ShebaRe = regexp.MustCompile(`^IR[0-9]{24}$`)

func t01CustomerWalletBalance(ctx context.Context, db *sql.DB, userID string) (balance, locked int64, err error) {
	err = db.QueryRowContext(ctx,
		`SELECT balance, locked_balance FROM customer_wallets WHERE customer_id = $1::uuid`, userID).Scan(&balance, &locked)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return balance, locked, err
}

func handleT01CustomerWallet(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	balance, locked, err := t01CustomerWalletBalance(r.Context(), db, userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	available := balance - locked
	if available < 0 {
		available = 0
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"balance":       balance,
		"lockedBalance": locked,
		"available":     available,
	})
}

// handleT01CustomerWalletQuote answers GET/POST /customer/wallet/quote.
// TODO: the exact live quote contract is unverified; the quote reports
// the requested amount against the available balance with no fee,
// because no customer-withdrawal fee exists in the wallet migrations
// (fees belong to the seller early-withdrawal path).
func handleT01CustomerWalletQuote(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var amount int64
	if r.Method == http.MethodPost {
		var req struct {
			Amount int64 `json:"amount"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		amount = req.Amount
	} else if r.Method == http.MethodGet {
		if v := strings.TrimSpace(r.URL.Query().Get("amount")); v != "" {
			if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
				amount = parsed
			}
		}
	} else {
		writeMethodNotAllowed(w)
		return
	}
	balance, locked, err := t01CustomerWalletBalance(r.Context(), db, userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	available := balance - locked
	if available < 0 {
		available = 0
	}
	withdrawEnabled := true
	if t01ColumnExists(r.Context(), db, "platform_settings", "wallet_withdraw_enabled") {
		var enabled bool
		if err := db.QueryRowContext(r.Context(),
			`SELECT wallet_withdraw_enabled FROM platform_settings WHERE id = 1`).Scan(&enabled); err == nil {
			withdrawEnabled = enabled
		}
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"amount":          amount,
		"available":       available,
		"sufficient":      withdrawEnabled && amount > 0 && amount <= available,
		"fee":             0,
		"net":             amount,
		"withdrawEnabled": withdrawEnabled,
	})
}

func handleT01CustomerWithdrawals(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		rows, err := db.QueryContext(ctx,
			`SELECT id, customer_name, customer_mobile, account_holder_name, sheba, bank_name,
				amount, status, rejection_reason, created_at
			FROM customer_withdrawal_requests WHERE customer_id = $1::uuid ORDER BY created_at DESC`, userID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var id, name, mobile, holder, sheba, bank, status string
			var rejection sql.NullString
			var amount int64
			var created time.Time
			if err := rows.Scan(&id, &name, &mobile, &holder, &sheba, &bank, &amount, &status, &rejection, &created); err != nil {
				writeInternalError(w, err)
				return
			}
			out = append(out, map[string]interface{}{
				"id": id, "customerName": name, "customerMobile": mobile,
				"accountHolderName": holder, "sheba": sheba, "bankName": bank,
				"amount": amount, "status": status, "rejectionReason": rejection.String,
				"createdAt": created.Format(time.RFC3339),
			})
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"withdrawals": out})
	case http.MethodPost:
		var req struct {
			Amount            int64  `json:"amount"`
			Sheba             string `json:"sheba"`
			BankName          string `json:"bankName"`
			AccountHolderName string `json:"accountHolderName"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		sheba := strings.ToUpper(strings.TrimSpace(req.Sheba))
		if req.Amount <= 0 || !t01ShebaRe.MatchString(sheba) || strings.TrimSpace(req.BankName) == "" ||
			strings.TrimSpace(req.AccountHolderName) == "" {
			http.Error(w, `{"error":"amount, a valid IR sheba, bankName and accountHolderName are required"}`, http.StatusBadRequest)
			return
		}
		balance, locked, err := t01CustomerWalletBalance(ctx, db, userID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		available := balance - locked
		if req.Amount > available {
			http.Error(w, `{"error":"Amount exceeds available balance"}`, http.StatusBadRequest)
			return
		}
		// The caller's display name/mobile come from the users row; the
		// withdrawal row requires both, so they are read, never invented.
		var custName, custMobile string
		if err := db.QueryRowContext(ctx,
			`SELECT COALESCE(full_name, ''), COALESCE(mobile, '') FROM users WHERE id = $1::uuid`, userID).Scan(&custName, &custMobile); err != nil {
			writeInternalError(w, err)
			return
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer tx.Rollback()
		res, err := tx.ExecContext(ctx,
			`UPDATE customer_wallets SET locked_balance = locked_balance + $1
			WHERE customer_id = $2::uuid AND balance - locked_balance >= $1`, req.Amount, userID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		// No wallet row yet means zero available; the amount check above
		// already rejected that case, but fail closed if nothing locked.
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, `{"error":"Amount exceeds available balance"}`, http.StatusBadRequest)
			return
		}
		id := "cwd-" + uuid.NewString()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO customer_withdrawal_requests (id, customer_id, customer_name, customer_mobile,
				account_holder_name, sheba, bank_name, amount, status)
			VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8, 'pending')`,
			id, userID, custName, custMobile, req.AccountHolderName, sheba, req.BankName, req.Amount); err != nil {
			writeInternalError(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": id, "status": "pending"})
	default:
		writeMethodNotAllowed(w)
	}
}

// --- logout (D08) ---------------------------------------------------------------
//
// POST /api/v1/auth/logout with a Bearer <redacted> (live frontend contract:
// fetch POST + Bearer, errors swallowed by the caller).
// TODO (U52): server-side revocation semantics are UNVERIFIED. The live
// binary contains no "blacklist" string and no session table, while
// Redis holds jwt:revoked:* keys of unknown origin. This endpoint
// therefore returns the confirmed contract ({success: true}) and does
// NOT invent a blacklist, and does NOT claim the token is revoked: the
// token remains valid until expiry unless a real revocation mechanism
// is built and proven.
func handleT01Logout(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) {
		return
	}
	// AuthMiddleware (applied in main.go) already proved the Bearer token
	// is valid; db is unused here but kept in the signature for symmetry.
	_ = db
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
