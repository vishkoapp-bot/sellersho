package main

// Per-store wishlist regression tests. These run ONLY when
// TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local Postgres
// (never the live database); otherwise they skip.
//
// Doctrine covered: every dropshipper store is a separate site with
// its OWN wishlist. Before migration 202611050002 the wishlist was a
// single global list per user, so a product hearted inside one store
// surfaced — at central prices — everywhere. The handler now scopes
// every operation by the resolved store: two stores' lists are fully
// isolated, a missing storeSlug keeps the legacy central behaviour,
// and an unknown slug fails closed with 404 (the order-creation rule).

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

func wishlistTestSetup(t *testing.T) (*sql.DB, *repository.StoreRepository) {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB HTTP test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ddl := []string{
		`DROP TABLE IF EXISTS customer_wishlists, stores CASCADE`,
		`CREATE TABLE stores (id VARCHAR(50) PRIMARY KEY, name VARCHAR(255) NOT NULL DEFAULT '', slug VARCHAR(255) NOT NULL DEFAULT '', owner_type VARCHAR(50) NOT NULL DEFAULT 'dropshipper', owner_name VARCHAR(255) NOT NULL DEFAULT '', owner_user_id VARCHAR(50), profit_margin_percent DOUBLE PRECISION NOT NULL DEFAULT 0, custom_store_margin_override DOUBLE PRECISION, is_margin_locked_by_admin BOOLEAN NOT NULL DEFAULT FALSE, active_plan_id VARCHAR(50) NOT NULL DEFAULT '', referral_code VARCHAR(100) NOT NULL DEFAULT '', referrer_seller_id VARCHAR(50), signed_contract_version INT NOT NULL DEFAULT 0, custom_domain VARCHAR(255) NOT NULL DEFAULT '', total_sales BIGINT NOT NULL DEFAULT 0, seller_deals_widget_config JSONB, template_id VARCHAR(100) NOT NULL DEFAULT '', template_config JSONB NOT NULL DEFAULT '{}'::jsonb, logo TEXT NOT NULL DEFAULT '',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			active_categories text[] NOT NULL DEFAULT '{}'::text[],
			addon_section_title character varying(200),
			contract_signature_code text,
			description text,
			owner_phone text NOT NULL DEFAULT ''::text,
			panel_config jsonb NOT NULL DEFAULT '{}'::jsonb,
			plan_expires_at timestamp with time zone,
			rejection_reason text,
			related_section_title character varying(200),
			status character varying(20) NOT NULL DEFAULT 'active'::character varying,
			tagline text,
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		// Post-migration shape (202611050002 applied).
		`CREATE TABLE customer_wishlists (user_id TEXT NOT NULL, store_id TEXT NOT NULL DEFAULT 'store-central', product_id TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (user_id, store_id, product_id))`,
		`INSERT INTO stores (id, name, slug, owner_type) VALUES ('store-central', 'مرکزی', 'central', 'central'), ('store-mone', 'مونه', 'mone', 'dropshipper'), ('store-vishko', 'ویشکو', 'vishkostor', 'dropshipper')`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL failed: %v", err)
		}
	}
	return db, repository.NewStoreRepository(db)
}

func wishlistCall(t *testing.T, db *sql.DB, storeRepo *repository.StoreRepository, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	req = req.WithContext(context.WithValue(req.Context(), userClaimsKey, &auth.UserClaims{UserID: "user-1", Role: "customer"}))
	rec := httptest.NewRecorder()
	handleT01Wishlist(rec, req, db, storeRepo)
	return rec
}

func wishlistGet(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("wishlist GET status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Wishlist []string `json:"wishlist"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode wishlist response: %v (body %s)", err, rec.Body.String())
	}
	return out.Wishlist
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestWishlistPerStoreIsolation(t *testing.T) {
	db, storeRepo := wishlistTestSetup(t)

	// Heart one product inside mone, one inside vishkostor, one with
	// no store named (the legacy central caller).
	if rec := wishlistCall(t, db, storeRepo, http.MethodPost, "/api/v1/wishlist", `{"productId":"p-mone","storeSlug":"mone"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST mone: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := wishlistCall(t, db, storeRepo, http.MethodPost, "/api/v1/wishlist", `{"productId":"p-vishko","storeSlug":"vishkostor"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST vishkostor: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := wishlistCall(t, db, storeRepo, http.MethodPost, "/api/v1/wishlist", `{"productId":"p-central"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST central (no slug): status %d body %s", rec.Code, rec.Body.String())
	}

	mone := wishlistGet(t, wishlistCall(t, db, storeRepo, http.MethodGet, "/api/v1/wishlist?storeSlug=mone", ""))
	if !containsID(mone, "p-mone") || containsID(mone, "p-central") || containsID(mone, "p-vishko") {
		t.Fatalf("mone list = %v, want exactly its own heart", mone)
	}
	vishko := wishlistGet(t, wishlistCall(t, db, storeRepo, http.MethodGet, "/api/v1/wishlist?storeSlug=vishkostor", ""))
	if !containsID(vishko, "p-vishko") || containsID(vishko, "p-mone") || containsID(vishko, "p-central") {
		t.Fatalf("vishkostor list = %v, want exactly its own heart", vishko)
	}

	// Missing slug = central (back-compat), and the explicit central
	// slug resolves to the same list.
	central := wishlistGet(t, wishlistCall(t, db, storeRepo, http.MethodGet, "/api/v1/wishlist", ""))
	if !containsID(central, "p-central") || containsID(central, "p-mone") || containsID(central, "p-vishko") {
		t.Fatalf("central (no slug) list = %v, want exactly the legacy heart", central)
	}
	centralBySlug := wishlistGet(t, wishlistCall(t, db, storeRepo, http.MethodGet, "/api/v1/wishlist?storeSlug=central", ""))
	if !containsID(centralBySlug, "p-central") || len(centralBySlug) != len(central) {
		t.Fatalf("central (slug) list = %v, want the same list as no-slug %v", centralBySlug, central)
	}

	// Unknown slugs fail closed on every method.
	if rec := wishlistCall(t, db, storeRepo, http.MethodGet, "/api/v1/wishlist?storeSlug=ghost", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET unknown slug: status %d, want 404", rec.Code)
	}
	if rec := wishlistCall(t, db, storeRepo, http.MethodPost, "/api/v1/wishlist", `{"productId":"p-x","storeSlug":"ghost"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("POST unknown slug: status %d, want 404", rec.Code)
	}
	if rec := wishlistCall(t, db, storeRepo, http.MethodDelete, "/api/v1/wishlist", `{"productId":"p-mone","storeSlug":"ghost"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown slug: status %d, want 404", rec.Code)
	}

	// A delete scoped to one store never touches another store's list:
	// removing p-mone under vishkostor's slug is a no-op for mone.
	if rec := wishlistCall(t, db, storeRepo, http.MethodDelete, "/api/v1/wishlist", `{"productId":"p-mone","storeSlug":"vishkostor"}`); rec.Code != http.StatusOK {
		t.Fatalf("DELETE cross-store: status %d", rec.Code)
	}
	if mone = wishlistGet(t, wishlistCall(t, db, storeRepo, http.MethodGet, "/api/v1/wishlist?storeSlug=mone", "")); !containsID(mone, "p-mone") {
		t.Fatalf("mone list after cross-store delete = %v, want p-mone intact", mone)
	}
	if rec := wishlistCall(t, db, storeRepo, http.MethodDelete, "/api/v1/wishlist", `{"productId":"p-mone","storeSlug":"mone"}`); rec.Code != http.StatusOK {
		t.Fatalf("DELETE mone: status %d", rec.Code)
	}
	if mone = wishlistGet(t, wishlistCall(t, db, storeRepo, http.MethodGet, "/api/v1/wishlist?storeSlug=mone", "")); containsID(mone, "p-mone") {
		t.Fatalf("mone list after own delete = %v, want p-mone gone", mone)
	}
	if central = wishlistGet(t, wishlistCall(t, db, storeRepo, http.MethodGet, "/api/v1/wishlist", "")); !containsID(central, "p-central") {
		t.Fatalf("central list after mone delete = %v, want p-central intact", central)
	}
}
