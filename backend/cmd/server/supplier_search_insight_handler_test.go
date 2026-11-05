package main

// Supplier search-insight endpoint permission tests (D12). They run
// ONLY when TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local
// Postgres (never the live database); otherwise they skip. The
// endpoint stays behind RequireSupplierAccess with the default
// supplier.manage permission: the owner and a supplier.manage member
// get 200, a products.manage-only member gets 403, and the 501 stub
// is gone (the response always carries the three section keys).

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

func TestSupplierSearchInsightPermissions(t *testing.T) {
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping DB integration test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()
	ddl := []string{
		`DROP TABLE IF EXISTS order_items, orders, search_queries, products, categories, supplier_members, suppliers, users CASCADE`,
		`CREATE TABLE users (id VARCHAR(50) PRIMARY KEY, is_active BOOLEAN NOT NULL DEFAULT TRUE)`,
		`CREATE TABLE suppliers (id VARCHAR(50) PRIMARY KEY, owner_user_id VARCHAR(50), approval_status VARCHAR(30) NOT NULL DEFAULT 'approved')`,
		`CREATE TABLE supplier_members (supplier_id VARCHAR(50) NOT NULL, user_id VARCHAR(50) NOT NULL, role_key VARCHAR(50) NOT NULL DEFAULT '', permissions JSONB NOT NULL DEFAULT '[]', is_active BOOLEAN NOT NULL DEFAULT TRUE)`,
		`CREATE TABLE categories (id VARCHAR(50) PRIMARY KEY, title TEXT NOT NULL, slug VARCHAR(100) NOT NULL)`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, slug VARCHAR(255) UNIQUE NOT NULL, category_slug VARCHAR(100), supplier_id VARCHAR(50), image_url TEXT, brand TEXT NOT NULL DEFAULT '', is_active BOOLEAN NOT NULL DEFAULT TRUE)`,
		`CREATE TABLE search_queries (id BIGSERIAL PRIMARY KEY, term_normalized TEXT NOT NULL, store_slug TEXT NOT NULL, result_count INTEGER NOT NULL, catalog_result_count INTEGER, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE orders (id VARCHAR(50) PRIMARY KEY, payment_status VARCHAR(20) NOT NULL DEFAULT 'pending', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE order_items (id VARCHAR(50) PRIMARY KEY, order_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, quantity INT NOT NULL DEFAULT 0, wholesale_cost BIGINT NOT NULL DEFAULT 0)`,
		`INSERT INTO users (id) VALUES ('si-owner'), ('si-mgr'), ('si-prod')`,
		`INSERT INTO suppliers (id, owner_user_id) VALUES ('sup-si', 'si-owner')`,
		`INSERT INTO supplier_members (supplier_id, user_id, role_key, permissions) VALUES ('sup-si', 'si-mgr', 'manager', '["products.manage","supplier.manage"]'::jsonb)`,
		`INSERT INTO supplier_members (supplier_id, user_id, role_key, permissions) VALUES ('sup-si', 'si-prod', 'products', '["products.manage"]'::jsonb)`,
		`INSERT INTO categories VALUES ('cat-si','پوشاک ورزشی','sportswear')`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,image_url,brand,is_active) VALUES ('psi1','کفش رانینگ تست','psi1','sportswear','sup-si','','برند تست',TRUE)`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := db.Exec(`INSERT INTO search_queries (term_normalized, store_slug, result_count, catalog_result_count) VALUES ('کفش رانینگ', 'central', 4, 4)`); err != nil {
			t.Fatalf("search seed: %v", err)
		}
	}

	repo := repository.NewSupplierRepository(db)
	jwtService := auth.NewJWTService("test-secret-search-insight")
	handlers := &SupplierHandlers{supplierRepo: repo}
	wrapped := RequireSupplierAccess(jwtService, repo)(handlers.HandleSupplierSearchInsight)

	call := func(userID string) *httptest.ResponseRecorder {
		token, err := jwtService.GenerateToken(userID, "09120000000", "Test", "supplier", time.Hour)
		if err != nil {
			t.Fatalf("token: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/supplier/search-insight?days=30", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		return rec
	}

	// Owner: 200, three section keys, own demand surfaced.
	rec := call("si-owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("owner body not JSON: %v", err)
	}
	for _, key := range []string{"windowDays", "topDemand", "unmetInScope", "ownProductTrend"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("response missing key %q (body: %s)", key, rec.Body.String())
		}
	}
	var insight struct {
		TopDemand []struct {
			Term string `json:"term"`
		} `json:"topDemand"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &insight); err != nil {
		t.Fatalf("decode insight: %v", err)
	}
	if len(insight.TopDemand) != 1 || insight.TopDemand[0].Term != "کفش رانینگ" {
		t.Fatalf("owner topDemand = %+v, want [کفش رانینگ]", insight.TopDemand)
	}

	// supplier.manage member: 200.
	if rec := call("si-mgr"); rec.Code != http.StatusOK {
		t.Fatalf("supplier.manage member status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	// products.manage-only member: 403 (permission not loosened).
	if rec := call("si-prod"); rec.Code != http.StatusForbidden {
		t.Fatalf("products.manage-only member status = %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
}
