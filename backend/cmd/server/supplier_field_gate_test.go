package main

// Supplier-field gate tests (FIX 14, auth-keyed). The decision-table
// and viewer-resolution tests run anywhere; the HTTP and
// assemble-level tests run ONLY when TEAM04_TEST_DATABASE_URL is
// set to a DISPOSABLE local Postgres (never the live database);
// otherwise they skip.
//
// Behaviour covered: on a dropshipper store view (?storeSlug=...),
// the supplier cost fields (supplierId, supplierName, baseCostB,
// platformFeeF and, on detail, supplierWarranty) are served ONLY to
// the store's owner, the product's supplier, or a platform admin.
// Anonymous callers and unrelated sessions get the central shape.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

func gateStrPtr(s string) *string { return &s }

func TestCanSeeSupplierFieldsDecisionTable(t *testing.T) {
	store := &domain.Store{ID: "store-shopx", Slug: "shopx", OwnerType: "dropshipper", OwnerUserID: gateStrPtr("user-owner")}
	ownerless := &domain.Store{ID: "store-x", Slug: "x", OwnerType: "dropshipper"}
	cases := []struct {
		name    string
		viewer  supplierFieldsViewer
		store   *domain.Store
		product string
		want    bool
	}{
		{"anonymous", supplierFieldsViewer{}, store, "sup-1", false},
		{"customer", supplierFieldsViewer{userID: "user-customer"}, store, "sup-1", false},
		{"store owner", supplierFieldsViewer{userID: "user-owner"}, store, "sup-1", true},
		{"other seller", supplierFieldsViewer{userID: "user-other"}, store, "sup-1", false},
		{"product supplier", supplierFieldsViewer{userID: "user-supplier", supplierID: "sup-1"}, store, "sup-1", true},
		{"other supplier", supplierFieldsViewer{userID: "user-supplier", supplierID: "sup-2"}, store, "sup-1", false},
		{"admin", supplierFieldsViewer{userID: "user-admin", isAdmin: true}, store, "sup-1", true},
		{"owner without a store view", supplierFieldsViewer{userID: "user-owner"}, nil, "sup-1", false},
		{"owner of ownerless store", supplierFieldsViewer{userID: "user-owner"}, ownerless, "sup-1", false},
		{"supplier of unassigned product", supplierFieldsViewer{userID: "user-supplier", supplierID: "sup-1"}, store, "", false},
	}
	for _, tc := range cases {
		if got := tc.viewer.canSeeSupplierFields(tc.store, tc.product); got != tc.want {
			t.Errorf("%s: canSeeSupplierFields = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestResolveSupplierFieldsViewer(t *testing.T) {
	// No claims in context: the zero viewer.
	if v := resolveSupplierFieldsViewer(context.Background(), nil); v != (supplierFieldsViewer{}) {
		t.Fatalf("anonymous viewer = %+v, want zero", v)
	}
	// Claims without a supplier repository: identity and admin come
	// from the token alone.
	adminCtx := context.WithValue(context.Background(), userClaimsKey,
		&auth.UserClaims{UserID: "user-admin", Capabilities: []string{"admin"}})
	if v := resolveSupplierFieldsViewer(adminCtx, nil); v.userID != "user-admin" || !v.isAdmin || v.supplierID != "" {
		t.Fatalf("admin viewer = %+v", v)
	}
	// Legacy role-only token: admin via HasCapability's role fallback.
	legacyCtx := context.WithValue(context.Background(), userClaimsKey,
		&auth.UserClaims{UserID: "user-legacy", Role: "admin"})
	if v := resolveSupplierFieldsViewer(legacyCtx, nil); !v.isAdmin {
		t.Fatalf("legacy admin viewer = %+v, want isAdmin", v)
	}
	// A plain seller is neither admin nor (without the repository) a
	// resolved supplier.
	sellerCtx := context.WithValue(context.Background(), userClaimsKey,
		&auth.UserClaims{UserID: "user-owner", Capabilities: []string{"seller"}})
	if v := resolveSupplierFieldsViewer(sellerCtx, nil); v.userID != "user-owner" || v.isAdmin || v.supplierID != "" {
		t.Fatalf("seller viewer = %+v", v)
	}
}

func supplierGateRepos(t *testing.T) (*repository.CatalogRepository, *repository.StoreRepository, *repository.SupplierRepository) {
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
		`DROP TABLE IF EXISTS supplier_members, suppliers, users, search_queries, store_products, store_product_overrides, store_niche_overrides, stores, product_images, product_variant_images, product_variants, product_niches, pending_approvals, products CASCADE`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, slug VARCHAR(255) UNIQUE NOT NULL, category_slug VARCHAR(100), supplier_id VARCHAR(50), original_price BIGINT NOT NULL, supplier_base_cost BIGINT NOT NULL, platform_margin BIGINT NOT NULL DEFAULT 0, is_original BOOLEAN NOT NULL DEFAULT TRUE, stock INT NOT NULL DEFAULT 0, image_url TEXT, specs JSONB DEFAULT '{}'::jsonb, description TEXT, warehouse_name VARCHAR(100), preparation_days INT DEFAULT 1, supplier_warranty VARCHAR(100), is_featured BOOLEAN DEFAULT FALSE, is_special_deal BOOLEAN DEFAULT FALSE, fulfillment_method VARCHAR(50), supply_option VARCHAR(50), is_active BOOLEAN NOT NULL DEFAULT TRUE, brand TEXT NOT NULL DEFAULT '', spn_number BIGINT, length_mm INT, width_mm INT, height_mm INT, weight_grams INT, shipping_class TEXT NOT NULL DEFAULT 'standard', tax_class TEXT NOT NULL DEFAULT 'standard', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_niches (product_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, PRIMARY KEY (product_id, niche_id))`,
		`CREATE TABLE product_variants (id UUID PRIMARY KEY, product_id VARCHAR(50) NOT NULL, spnc VARCHAR(12) NOT NULL UNIQUE, spnc_number INT, variant_name TEXT NOT NULL DEFAULT 'x', attributes JSONB NOT NULL DEFAULT '{}', is_default BOOLEAN NOT NULL DEFAULT FALSE, stock INT NOT NULL DEFAULT 0, supplier_base_cost BIGINT, weight_grams INT, length_mm INT, width_mm INT, height_mm INT, sale_price BIGINT, sale_from TIMESTAMPTZ, sale_to TIMESTAMPTZ, shipping_class TEXT, tax_class TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_images (id UUID PRIMARY KEY, product_id VARCHAR(50) NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_variant_images (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), variant_id UUID NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE store_products (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, position INT NOT NULL DEFAULT 0, PRIMARY KEY (store_id, product_id))`,
		`CREATE TABLE stores (id VARCHAR(50) PRIMARY KEY, name VARCHAR(255) NOT NULL DEFAULT '', slug VARCHAR(255) NOT NULL DEFAULT '', owner_type VARCHAR(50) NOT NULL DEFAULT 'dropshipper', owner_name VARCHAR(255) NOT NULL DEFAULT '', owner_user_id VARCHAR(50), profit_margin_percent DOUBLE PRECISION NOT NULL DEFAULT 0, custom_store_margin_override DOUBLE PRECISION, is_margin_locked_by_admin BOOLEAN NOT NULL DEFAULT FALSE, active_plan_id VARCHAR(50) NOT NULL DEFAULT '', referral_code VARCHAR(100) NOT NULL DEFAULT '', referrer_seller_id VARCHAR(50), signed_contract_version INT NOT NULL DEFAULT 0, custom_domain VARCHAR(255) NOT NULL DEFAULT '', total_sales BIGINT NOT NULL DEFAULT 0, seller_deals_widget_config JSONB, template_id VARCHAR(100) NOT NULL DEFAULT '', template_config JSONB NOT NULL DEFAULT '{}'::jsonb, logo TEXT NOT NULL DEFAULT '', panel_config JSONB, tagline TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '', status VARCHAR(50) NOT NULL DEFAULT 'active', active_categories TEXT[] NOT NULL DEFAULT '{}', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE store_product_overrides (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, override_value DOUBLE PRECISION NOT NULL DEFAULT 0, mode VARCHAR(20) NOT NULL DEFAULT 'percent')`,
		`CREATE TABLE store_niche_overrides (store_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, override_value DOUBLE PRECISION NOT NULL DEFAULT 0, mode VARCHAR(20) NOT NULL DEFAULT 'percent')`,
		`CREATE TABLE search_queries (id BIGSERIAL PRIMARY KEY, term_normalized TEXT NOT NULL, store_slug TEXT NOT NULL, result_count INTEGER NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE pending_approvals (id VARCHAR(50) PRIMARY KEY, entity_type VARCHAR(50) NOT NULL, target_id VARCHAR(50) NOT NULL, title TEXT NOT NULL, requested_data JSONB NOT NULL DEFAULT '{}', requested_by VARCHAR(50) NOT NULL, status VARCHAR(50) NOT NULL, rejection_reason TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), reviewed_at TIMESTAMPTZ)`,
		`CREATE TABLE users (id VARCHAR(50) PRIMARY KEY, is_active BOOLEAN NOT NULL DEFAULT TRUE)`,
		`CREATE TABLE suppliers (id VARCHAR(50) PRIMARY KEY, name VARCHAR(255) NOT NULL DEFAULT '', owner_user_id VARCHAR(50), approval_status VARCHAR(20) NOT NULL DEFAULT 'approved')`,
		`CREATE TABLE supplier_members (supplier_id VARCHAR(50) NOT NULL, user_id VARCHAR(50) NOT NULL, role_key VARCHAR(50) NOT NULL DEFAULT 'owner', permissions JSONB NOT NULL DEFAULT '[]'::jsonb, is_active BOOLEAN NOT NULL DEFAULT TRUE)`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL failed: %v", err)
		}
	}
	seed := []string{
		`INSERT INTO users (id, is_active) VALUES ('user-owner', TRUE), ('user-supplier', TRUE), ('user-other', TRUE), ('user-admin', TRUE), ('user-customer', TRUE)`,
		`INSERT INTO suppliers (id, name, owner_user_id) VALUES ('sup-1', 'تأمین‌کننده تست', 'user-supplier')`,
		`INSERT INTO stores (id,name,slug,owner_type,owner_user_id,panel_config,seller_deals_widget_config) VALUES ('store-shopx','فروشگاه ایکس','shopx','dropshipper','user-owner','{}'::jsonb,'null'::jsonb)`,
		`INSERT INTO stores (id,name,slug,owner_type,panel_config,seller_deals_widget_config) VALUES ('store-central','مرکزی','central','central','{}'::jsonb,'null'::jsonb)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number) VALUES ('p-1','دستبند طلا','gold-bracelet','beauty','sup-1',72500000,58000000,0,TRUE,5,'img','desc','انبار مرکزی تهران',1,'۱۸ ماه گارانتی شرکتی',FALSE,FALSE,'warehouse','1day',TRUE,'نشر دمو',20000001)`,
		`INSERT INTO store_products VALUES ('store-shopx','p-1',0)`,
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}
	return repository.NewCatalogRepository(db), repository.NewStoreRepository(db), repository.NewSupplierRepository(db)
}

func gateClaimsCtx(claims *auth.UserClaims) context.Context {
	if claims == nil {
		return context.Background()
	}
	return context.WithValue(context.Background(), userClaimsKey, claims)
}

func assertSupplierFieldsHidden(t *testing.T, item map[string]json.RawMessage) {
	t.Helper()
	for _, k := range []string{"supplierId", "supplierName", "baseCostB"} {
		if _, ok := item[k]; ok {
			t.Fatalf("field %q must be absent for an unrelated caller, item %s", k, item["id"])
		}
	}
	var fee int64
	if raw, ok := item["platformFeeF"]; ok {
		if err := json.Unmarshal(raw, &fee); err != nil {
			t.Fatalf("platformFeeF decode: %v", err)
		}
	}
	if fee != 0 {
		t.Fatalf("platformFeeF = %d for an unrelated caller, want 0", fee)
	}
}

func assertSupplierFieldsShown(t *testing.T, item map[string]json.RawMessage) {
	t.Helper()
	var sid, name string
	var cost int64
	if err := json.Unmarshal(item["supplierId"], &sid); err != nil || sid != "sup-1" {
		t.Fatalf("supplierId = %q (err %v), want sup-1", sid, err)
	}
	if err := json.Unmarshal(item["supplierName"], &name); err != nil || name != "تأمین‌کننده تست" {
		t.Fatalf("supplierName = %q (err %v)", name, err)
	}
	if err := json.Unmarshal(item["baseCostB"], &cost); err != nil || cost != 58000000 {
		t.Fatalf("baseCostB = %d (err %v), want 58000000", cost, err)
	}
}

func TestSupplierFieldsGateHTTP(t *testing.T) {
	catalogRepo, storeRepo, supplierRepo := supplierGateRepos(t)

	callList := func(claims *auth.UserClaims) []map[string]json.RawMessage {
		ctx := gateClaimsCtx(claims)
		handler := func(w http.ResponseWriter, r *http.Request) {
			handleProductsList(w, r, ctx, catalogRepo, storeRepo, nil, supplierRepo)
		}
		rec := team04Get(t, handler, "/api/v1/products/?storeSlug=shopx")
		if rec.Code != http.StatusOK {
			t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
		}
		// PERF-01: paged envelope; the bare-array shape is retired.
		var envelope struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("list decode: %v", err)
		}
		list := envelope.Items
		if len(list) != 1 {
			t.Fatalf("list len = %d, want the store's single product", len(list))
		}
		return list
	}
	callDetail := func(claims *auth.UserClaims) map[string]json.RawMessage {
		ctx := gateClaimsCtx(claims)
		handler := func(w http.ResponseWriter, r *http.Request) {
			handleProductDetail(w, r, ctx, catalogRepo, storeRepo, nil, "p-1", supplierRepo)
		}
		rec := team04Get(t, handler, "/api/v1/products/p-1?storeSlug=shopx")
		if rec.Code != http.StatusOK {
			t.Fatalf("detail status = %d, body %s", rec.Code, rec.Body.String())
		}
		var detail map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatalf("detail decode: %v", err)
		}
		return detail
	}

	owner := &auth.UserClaims{UserID: "user-owner", Capabilities: []string{"seller"}}
	supplier := &auth.UserClaims{UserID: "user-supplier", Capabilities: []string{"supplier"}}
	admin := &auth.UserClaims{UserID: "user-admin", Capabilities: []string{"admin"}}
	otherSeller := &auth.UserClaims{UserID: "user-other", Capabilities: []string{"seller"}}
	customer := &auth.UserClaims{UserID: "user-customer", Capabilities: []string{"customer"}}

	// List: anonymous and unrelated sessions see the central shape;
	// owner, supplier and admin see the cost fields.
	assertSupplierFieldsHidden(t, callList(nil)[0])
	assertSupplierFieldsHidden(t, callList(customer)[0])
	assertSupplierFieldsHidden(t, callList(otherSeller)[0])
	assertSupplierFieldsShown(t, callList(owner)[0])
	assertSupplierFieldsShown(t, callList(supplier)[0])
	assertSupplierFieldsShown(t, callList(admin)[0])

	// Detail: same gate, plus supplierWarranty follows it.
	anonDetail := callDetail(nil)
	assertSupplierFieldsHidden(t, anonDetail)
	var warranty string
	if err := json.Unmarshal(anonDetail["supplierWarranty"], &warranty); err != nil || warranty != "" {
		t.Fatalf("anonymous supplierWarranty = %q (err %v), want empty", warranty, err)
	}
	ownerDetail := callDetail(owner)
	assertSupplierFieldsShown(t, ownerDetail)
	if err := json.Unmarshal(ownerDetail["supplierWarranty"], &warranty); err != nil || warranty != "۱۸ ماه گارانتی شرکتی" {
		t.Fatalf("owner supplierWarranty = %q (err %v)", warranty, err)
	}
}

// TestSupplierFieldsGateAssemble pins the non-pointer fields with a
// non-zero platform pct (the HTTP harness runs on default settings,
// whose pct is 0): hidden means platformFeeF 0 and an empty
// supplierWarranty, shown means the real fee, and the customer price
// is identical either way.
func TestSupplierFieldsGateAssemble(t *testing.T) {
	catalogRepo, storeRepo, _ := supplierGateRepos(t)
	ctx := context.Background()
	store, err := storeRepo.GetStoreBySlug(ctx, "shopx")
	if err != nil || store == nil {
		t.Fatalf("store lookup: %v", err)
	}
	prod, err := catalogRepo.GetProductByID(ctx, "p-1")
	if err != nil || prod == nil {
		t.Fatalf("product lookup: %v", err)
	}
	settings := &pricing.PlatformPricingSettings{
		PlatformPct: 20, CentralProfitPct: 10,
		MaxProfitMargin: 35, MinProfitMargin: 5, DefaultMargin: 20,
	}
	hidden, err := assembleProductDetail(ctx, catalogRepo, nil, nil, store, settings, *prod, false)
	if err != nil {
		t.Fatalf("assemble hidden: %v", err)
	}
	shown, err := assembleProductDetail(ctx, catalogRepo, nil, nil, store, settings, *prod, true)
	if err != nil {
		t.Fatalf("assemble shown: %v", err)
	}
	if hidden.BaseCostB != nil || hidden.SupplierID != nil || hidden.SupplierName != nil {
		t.Fatalf("hidden detail carries supplier pointers: %+v", hidden)
	}
	if hidden.PlatformFeeF != 0 {
		t.Fatalf("hidden platformFeeF = %d, want 0", hidden.PlatformFeeF)
	}
	if hidden.SupplierWarranty != "" {
		t.Fatalf("hidden supplierWarranty = %q, want empty", hidden.SupplierWarranty)
	}
	// R = 58,000,000 x 1.20 = 69,600,000, so F = R - C = 11,600,000.
	if shown.PlatformFeeF != 11600000 {
		t.Fatalf("shown platformFeeF = %d, want 11600000", shown.PlatformFeeF)
	}
	if shown.BaseCostB == nil || *shown.BaseCostB != 58000000 {
		t.Fatalf("shown baseCostB = %v, want 58000000", shown.BaseCostB)
	}
	if shown.SupplierWarranty != "۱۸ ماه گارانتی شرکتی" {
		t.Fatalf("shown supplierWarranty = %q", shown.SupplierWarranty)
	}
	if hidden.Price != shown.Price {
		t.Fatalf("gate changed the customer price: hidden %d, shown %d", hidden.Price, shown.Price)
	}
}

// TestSupplierFieldsGateThroughMiddleware exercises the production
// wiring: OptionalAuthMiddleware in front of the list handler, with
// identity arriving as a real Bearer token (not a hand-built
// context). A missing or invalid token degrades to anonymous and
// still gets a 200 with the gated shape.
func TestSupplierFieldsGateThroughMiddleware(t *testing.T) {
	catalogRepo, storeRepo, supplierRepo := supplierGateRepos(t)
	jwtService := auth.NewJWTService("supplier-gate-test-secret")
	wrapped := OptionalAuthMiddleware(jwtService, func(w http.ResponseWriter, r *http.Request) {
		handleProductsList(w, r, r.Context(), catalogRepo, storeRepo, nil, supplierRepo)
	})
	call := func(token string) []map[string]json.RawMessage {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/products/?storeSlug=shopx", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
		}
		// PERF-01: paged envelope; the bare-array shape is retired.
		var envelope struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode: %v", err)
		}
		list := envelope.Items
		if len(list) != 1 {
			t.Fatalf("list len = %d, want 1", len(list))
		}
		return list
	}

	assertSupplierFieldsHidden(t, call("")[0])
	assertSupplierFieldsHidden(t, call("not-a-real-token")[0])

	ownerToken, err := jwtService.GenerateTokenWithCapabilities("user-owner", "09000000000", "Owner", "seller", []string{"seller"}, time.Hour)
	if err != nil {
		t.Fatalf("mint owner token: %v", err)
	}
	assertSupplierFieldsShown(t, call(ownerToken)[0])

	customerToken, err := jwtService.GenerateTokenWithCapabilities("user-customer", "09000000001", "Customer", "customer", []string{"customer"}, time.Hour)
	if err != nil {
		t.Fatalf("mint customer token: %v", err)
	}
	assertSupplierFieldsHidden(t, call(customerToken)[0])
}
