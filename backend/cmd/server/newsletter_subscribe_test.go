package main

// Newsletter subscribe store-attribution regression tests. These run
// ONLY when TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local
// Postgres (never the live database); otherwise they skip.
//
// Bug covered: POST /api/v1/newsletter/subscribe decoded only
// {mobile, storeId, email}, so the storeSlug every storefront
// template sends was silently discarded and EVERY subscription was
// filed under the central store. The fix resolves storeSlug to the
// store id (unknown slug → 400, fail-closed; slug wins over
// storeId) and keeps the central default when neither is given.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/testdb"

	_ "github.com/lib/pq"
)

func t01NewsletterTestDB(t *testing.T) *sql.DB {
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
		`DROP TABLE IF EXISTS newsletter_subscribers CASCADE`,
		`DROP TABLE IF EXISTS stores CASCADE`,
		`CREATE TABLE stores (id VARCHAR(50) PRIMARY KEY, slug VARCHAR(255) NOT NULL DEFAULT '', owner_type VARCHAR(50) NOT NULL DEFAULT 'dropshipper')`,
		`CREATE TABLE newsletter_subscribers (id VARCHAR(50) PRIMARY KEY, store_id VARCHAR(50) NOT NULL, mobile VARCHAR(15) NOT NULL, email VARCHAR(254), status VARCHAR(12) NOT NULL DEFAULT 'active', source VARCHAR(20) NOT NULL DEFAULT 'footer', unsubscribe_token VARCHAR(64) NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), unsubscribed_at TIMESTAMPTZ, UNIQUE (store_id, mobile))`,
		`INSERT INTO stores (id, slug, owner_type) VALUES ('store-central', 'central', 'central')`,
		`INSERT INTO stores (id, slug, owner_type) VALUES ('store-shopx', 'shopx', 'dropshipper')`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL failed: %v", err)
		}
	}
	return db
}

func t01NewsletterPost(t *testing.T, db *sql.DB, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/newsletter/subscribe", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handleT01NewsletterSubscribe(rec, req, db)
	return rec
}

func t01NewsletterCount(t *testing.T, db *sql.DB, storeID, mobile string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM newsletter_subscribers WHERE store_id = $1 AND mobile = $2`, storeID, mobile).Scan(&n); err != nil {
		t.Fatalf("count subscribers: %v", err)
	}
	return n
}

func t01NewsletterRespStoreID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	id, _ := resp["storeId"].(string)
	return id
}

func TestNewsletterSubscribeStoreSlug(t *testing.T) {
	db := t01NewsletterTestDB(t)

	// A valid storeSlug files the subscription under that store,
	// not central.
	rec := t01NewsletterPost(t, db, `{"mobile":"09120000001","storeSlug":"shopx"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("storeSlug subscribe status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := t01NewsletterRespStoreID(t, rec); got != "store-shopx" {
		t.Fatalf("storeSlug subscribe storeId = %q, want store-shopx", got)
	}
	if n := t01NewsletterCount(t, db, "store-shopx", "09120000001"); n != 1 {
		t.Fatalf("rows under store-shopx = %d, want 1", n)
	}
	if n := t01NewsletterCount(t, db, "store-central", "09120000001"); n != 0 {
		t.Fatalf("rows under store-central = %d, want 0", n)
	}

	// Slug matching is case-insensitive, like GetStoreBySlug.
	rec = t01NewsletterPost(t, db, `{"mobile":"09120000002","storeSlug":"SHOPX"}`)
	if rec.Code != http.StatusOK || t01NewsletterRespStoreID(t, rec) != "store-shopx" {
		t.Fatalf("uppercase slug: status = %d, body %s", rec.Code, rec.Body.String())
	}

	// An unknown slug is a 400 (fail-closed) and records nothing.
	rec = t01NewsletterPost(t, db, `{"mobile":"09120000003","storeSlug":"no-such-store"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown slug status = %d, want 400, body %s", rec.Code, rec.Body.String())
	}
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM newsletter_subscribers WHERE mobile = '09120000003'`).Scan(&total); err != nil {
		t.Fatalf("count unknown-slug rows: %v", err)
	}
	if total != 0 {
		t.Fatalf("unknown slug recorded %d rows, want 0", total)
	}

	// Neither slug nor id (the central Footer) keeps the central
	// default.
	rec = t01NewsletterPost(t, db, `{"mobile":"09120000004"}`)
	if rec.Code != http.StatusOK || t01NewsletterRespStoreID(t, rec) != "store-central" {
		t.Fatalf("no-slug subscribe: status = %d, body %s", rec.Code, rec.Body.String())
	}

	// storeSlug wins when both are present.
	rec = t01NewsletterPost(t, db, `{"mobile":"09120000005","storeId":"store-shopx","storeSlug":"central"}`)
	if rec.Code != http.StatusOK || t01NewsletterRespStoreID(t, rec) != "store-central" {
		t.Fatalf("slug-wins subscribe: status = %d, body %s", rec.Code, rec.Body.String())
	}
	if n := t01NewsletterCount(t, db, "store-shopx", "09120000005"); n != 0 {
		t.Fatalf("slug-wins rows under store-shopx = %d, want 0", n)
	}
}
