package main

// Gap 12 (real supplier suspension) regression tests. These run ONLY
// when TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local Postgres
// (never the live database); otherwise they skip. The DDL is the
// minimal faithful subset of the real schema the exercised repo
// functions and handlers touch (post-migration shape: suppliers
// carries suspended_at / suspended_by / suspension_reason from
// 202611050007).
//
// Covered: the suspend/reactivate state guards (pending/rejected ->
// conflict, approved -> ok, double-suspend -> conflict), the central
// (ownerless) record guard, audit rows, users.is_active untouched,
// panel access denied for owner AND team member while suspended, the
// catalog predicate (list scoped + unscoped, category counts,
// supplier-less legacy products stay), public detail 404 vs the
// ungated repo read, whole-order rejection at creation, and the
// settlement freeze.

import (
	"context"
	"database/sql"
	"errors"
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

const suspensionDDL = `
DROP TABLE IF EXISTS order_item_origins, supplier_shipments, ledger_entries, order_items, orders,
	payout_requests, supplier_shipping_methods, store_products, stores,
	product_niches, categories, products, audit_logs, supplier_members,
	suppliers, users CASCADE;
CREATE TABLE users (
	id TEXT PRIMARY KEY,
	mobile VARCHAR(15) NOT NULL DEFAULT '',
	full_name VARCHAR(100) NOT NULL DEFAULT '',
	role VARCHAR(20) NOT NULL DEFAULT 'customer',
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	username VARCHAR(50) DEFAULT '',
	password_hash VARCHAR(255) DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE suppliers (
	id VARCHAR(50) PRIMARY KEY,
	owner_user_id TEXT,
	name VARCHAR(200) NOT NULL DEFAULT '',
	logo_url TEXT,
	rating DECIMAL(3,2) DEFAULT 0,
	total_products INT DEFAULT 0,
	delivery_score INT DEFAULT 100,
	warehouse_city VARCHAR(100) DEFAULT '',
	description TEXT,
	badge VARCHAR(50) DEFAULT '',
	supports_direct_shipping BOOLEAN DEFAULT FALSE,
	approval_status VARCHAR(20) DEFAULT 'pending_approval',
	approved_by VARCHAR(50),
	approved_at TIMESTAMPTZ,
	rejection_reason TEXT,
	signed_contract_version INT,
	tapin_shop_id VARCHAR(50),
	warehouse_city_confirmed BOOLEAN DEFAULT FALSE,
	warehouse_city_source VARCHAR(30),
	default_markup_type VARCHAR(20),
	default_markup_value DECIMAL(10,2),
	suspended_at TIMESTAMPTZ,
	suspended_by TEXT,
	suspension_reason TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE supplier_members (
	id TEXT PRIMARY KEY,
	supplier_id VARCHAR(50) NOT NULL,
	user_id TEXT NOT NULL,
	role_key VARCHAR(50) NOT NULL DEFAULT 'manager',
	permissions JSONB NOT NULL DEFAULT '[]'::jsonb,
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE audit_logs (
	id TEXT PRIMARY KEY,
	timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	operator TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL DEFAULT '',
	details TEXT NOT NULL DEFAULT ''
);
CREATE TABLE products (
	id VARCHAR(50) PRIMARY KEY,
	title VARCHAR(255) NOT NULL DEFAULT '',
	slug VARCHAR(255) NOT NULL DEFAULT '',
	category_slug VARCHAR(100) DEFAULT '',
	supplier_id VARCHAR(50),
	original_price BIGINT DEFAULT 0,
	supplier_base_cost BIGINT DEFAULT 0,
	platform_margin DOUBLE PRECISION DEFAULT 0,
	is_original BOOLEAN DEFAULT FALSE,
	stock INT DEFAULT 0,
	image_url TEXT,
	description TEXT,
	warehouse_name VARCHAR(100) DEFAULT '',
	preparation_days INT DEFAULT 0,
	supplier_warranty VARCHAR(100),
	is_featured BOOLEAN DEFAULT FALSE,
	is_special_deal BOOLEAN DEFAULT FALSE,
	fulfillment_method VARCHAR(30) DEFAULT 'warehouse',
	supply_option VARCHAR(30) DEFAULT '1day',
	is_active BOOLEAN DEFAULT TRUE,
	brand VARCHAR(100),
	spn_number BIGINT,
	specs JSONB,
	length_mm INT, width_mm INT, height_mm INT, weight_grams INT,
	shipping_class TEXT NOT NULL DEFAULT 'standard', tax_class TEXT NOT NULL DEFAULT 'standard',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE product_niches (product_id VARCHAR(50), niche_id VARCHAR(50));
CREATE TABLE categories (id VARCHAR(50) PRIMARY KEY, slug VARCHAR(100) NOT NULL DEFAULT '');
CREATE TABLE stores (id VARCHAR(50) PRIMARY KEY, name VARCHAR(255) NOT NULL DEFAULT '', slug VARCHAR(255) NOT NULL DEFAULT '', owner_type VARCHAR(50) NOT NULL DEFAULT 'dropshipper', owner_name VARCHAR(255) NOT NULL DEFAULT '', owner_user_id VARCHAR(50), profit_margin_percent DOUBLE PRECISION NOT NULL DEFAULT 0, custom_store_margin_override DOUBLE PRECISION, is_margin_locked_by_admin BOOLEAN NOT NULL DEFAULT FALSE, active_plan_id VARCHAR(50) NOT NULL DEFAULT '', referral_code VARCHAR(100) NOT NULL DEFAULT '', referrer_seller_id VARCHAR(50), signed_contract_version INT NOT NULL DEFAULT 0, custom_domain VARCHAR(255) NOT NULL DEFAULT '', total_sales BIGINT NOT NULL DEFAULT 0, seller_deals_widget_config JSONB, template_id VARCHAR(100) NOT NULL DEFAULT '', template_config JSONB NOT NULL DEFAULT '{}'::jsonb, logo TEXT NOT NULL DEFAULT '');
CREATE TABLE store_products (store_id VARCHAR(50), product_id VARCHAR(50));
CREATE TABLE supplier_shipping_methods (
	owner_user_id TEXT NOT NULL,
	method VARCHAR(20) NOT NULL,
	status VARCHAR(20) NOT NULL DEFAULT 'disabled',
	requested_at TIMESTAMPTZ,
	decided_at TIMESTAMPTZ,
	decided_by TEXT
);
CREATE TABLE payout_requests (
	id TEXT PRIMARY KEY,
	store_id TEXT,
	supplier_id VARCHAR(50),
	recipient_type VARCHAR(20) DEFAULT 'supplier',
	recipient_name TEXT DEFAULT '',
	shaba TEXT DEFAULT '',
	bank_name TEXT DEFAULT '',
	amount BIGINT DEFAULT 0,
	status VARCHAR(20) DEFAULT 'pending',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	processed_at TIMESTAMPTZ,
	transfer_reference TEXT
);
CREATE TABLE orders (
	id TEXT PRIMARY KEY,
	customer_user_id TEXT,
	order_number TEXT DEFAULT '',
	store_id TEXT DEFAULT '',
	store_name TEXT DEFAULT '',
	total_amount BIGINT DEFAULT 0,
	discount_amount BIGINT DEFAULT 0,
	payable_amount BIGINT DEFAULT 0,
	payment_method TEXT DEFAULT '',
	payment_status TEXT DEFAULT 'pending',
	customer_full_name TEXT DEFAULT '',
	customer_phone TEXT DEFAULT '',
	customer_province TEXT DEFAULT '',
	customer_city TEXT DEFAULT '',
	customer_address TEXT DEFAULT '',
	customer_postal_code TEXT DEFAULT '',
	status TEXT DEFAULT 'processing',
	tracking_code TEXT DEFAULT '',
	shipping_cost BIGINT DEFAULT 0,
	dropshipper_profit BIGINT DEFAULT 0,
	platform_profit BIGINT DEFAULT 0,
	referral_commission BIGINT DEFAULT 0,
	referral_commission_released BOOLEAN DEFAULT FALSE,
	referrer_seller_id TEXT,
	profit_released BOOLEAN DEFAULT FALSE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	fulfillment_method TEXT DEFAULT 'warehouse',
	wallet_amount BIGINT DEFAULT 0,
	gateway_amount BIGINT DEFAULT 0,
	platform_addon_amount BIGINT DEFAULT 0,
	supplier_addon_amount BIGINT DEFAULT 0,
	inventory_captured BOOLEAN DEFAULT FALSE,
	packaging_length_mm INT, packaging_width_mm INT, packaging_height_mm INT,
	dispute_reason TEXT, carrier TEXT, tracking_number TEXT, shipping_label TEXT,
	packed_at TIMESTAMPTZ, box_size TEXT, weight_grams INT,
	idempotency_key TEXT, return_window_ends_at TIMESTAMPTZ,
	seller_discount_amount BIGINT DEFAULT 0, discount_code TEXT DEFAULT '',
	shipping_quote_id TEXT
);
CREATE TABLE order_items (
	id UUID PRIMARY KEY,
	order_id TEXT NOT NULL,
	product_id VARCHAR(50) NOT NULL,
	quantity INT NOT NULL DEFAULT 1,
	wholesale_cost BIGINT DEFAULT 0,
	platform_fee BIGINT DEFAULT 0,
	seller_margin BIGINT DEFAULT 0,
	final_unit_price BIGINT DEFAULT 0,
	discount_amount BIGINT DEFAULT 0,
	variant_id TEXT, variant_name TEXT, variant_spnc TEXT
);
CREATE TABLE order_item_origins (
	id TEXT PRIMARY KEY,
	order_id TEXT NOT NULL,
	order_item_id UUID,
	leg_index INT DEFAULT 0,
	origin_type TEXT DEFAULT 'warehouse',
	supplier_id TEXT,
	warehouse_id TEXT,
	supplier_warehouse_id TEXT,
	origin_label TEXT DEFAULT '',
	quantity INT DEFAULT 0
);
CREATE TABLE ledger_entries (
	id TEXT PRIMARY KEY,
	type VARCHAR(20) NOT NULL DEFAULT 'debit',
	amount BIGINT NOT NULL DEFAULT 0,
	target_account TEXT NOT NULL DEFAULT '',
	target_type TEXT NOT NULL DEFAULT '',
	reason TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE supplier_shipments (
	id TEXT PRIMARY KEY,
	order_id TEXT NOT NULL,
	supplier_id VARCHAR(50),
	shipping_address JSONB DEFAULT '{}'::jsonb,
	customer_phone TEXT DEFAULT '',
	customer_name TEXT DEFAULT '',
	status TEXT DEFAULT 'pending',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO users (id, mobile, full_name, role, is_active) VALUES
	('user-owner-1', '09120000001', 'مالک الف', 'supplier', TRUE),
	('user-owner-2', '09120000002', 'مالک ب', 'supplier', TRUE),
	('user-member-1', '09120000003', 'عضو تیم الف', 'supplier', TRUE),
	('user-pending', '09120000004', 'در انتظار', 'supplier', TRUE),
	('user-rejected', '09120000005', 'ردشده', 'supplier', FALSE),
	('user-cust', '09120000009', 'مشتری', 'customer', TRUE);
INSERT INTO suppliers (id, owner_user_id, name, approval_status) VALUES
	('sup-a', 'user-owner-1', 'تأمین‌کننده الف', 'approved'),
	('sup-b', 'user-owner-2', 'تأمین‌کننده ب', 'approved'),
	('sup-pending', 'user-pending', 'تأمین‌کننده در انتظار', 'pending_approval'),
	('sup-rejected', 'user-rejected', 'تأمین‌کننده ردشده', 'rejected'),
	('sup-central', NULL, 'تأمین‌کننده مرکزی پلتفرم', 'approved');
INSERT INTO supplier_members (id, supplier_id, user_id, role_key, permissions, is_active) VALUES
	('mem-1', 'sup-a', 'user-member-1', 'manager', '["supplier.manage"]'::jsonb, TRUE);
INSERT INTO categories (id, slug) VALUES ('cat-digital', 'digital');
INSERT INTO products (id, title, slug, category_slug, supplier_id, original_price, supplier_base_cost, stock, is_active, spn_number) VALUES
	('prod-a1', 'کالای الف', 'prod-a1', 'digital', 'sup-a', 200000, 100000, 10, TRUE, 111),
	('prod-b1', 'کالای ب', 'prod-b1', 'digital', 'sup-b', 300000, 150000, 10, TRUE, 222),
	('prod-legacy', 'کالای قدیمی', 'prod-legacy', 'digital', 'sup-ghost', 100000, 50000, 10, TRUE, 333);
INSERT INTO stores (id, name, slug, owner_type) VALUES ('store-central', 'مرکزی', 'central', 'central');
`

func suspensionTestSetup(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB suspension test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(suspensionDDL); err != nil {
		t.Fatalf("DDL failed: %v", err)
	}
	return db
}

func TestSupplierSuspensionGuardsAndAudit(t *testing.T) {
	db := suspensionTestSetup(t)
	repo := repository.NewSupplierRepository(db)
	ctx := context.Background()

	if err := repo.SuspendSupplier(ctx, "sup-pending", "admin-1", "x"); !errors.Is(err, repository.ErrSupplierStateConflict) {
		t.Fatalf("suspend pending: got %v, want ErrSupplierStateConflict", err)
	}
	if err := repo.SuspendSupplier(ctx, "sup-rejected", "admin-1", "x"); !errors.Is(err, repository.ErrSupplierStateConflict) {
		t.Fatalf("suspend rejected: got %v, want ErrSupplierStateConflict", err)
	}
	if err := repo.SuspendSupplier(ctx, "sup-missing", "admin-1", "x"); !errors.Is(err, repository.ErrSupplierStateConflict) {
		t.Fatalf("suspend missing: got %v, want ErrSupplierStateConflict", err)
	}
	if err := repo.ReactivateSupplier(ctx, "sup-a", "admin-1"); !errors.Is(err, repository.ErrSupplierStateConflict) {
		t.Fatalf("reactivate non-suspended: got %v, want ErrSupplierStateConflict", err)
	}
	// The central record is the OWNERLESS suppliers row; it can be
	// neither suspended nor reactivated.
	if err := repo.SuspendSupplier(ctx, "sup-central", "admin-1", "x"); !errors.Is(err, repository.ErrCentralSupplierProtected) {
		t.Fatalf("suspend central: got %v, want ErrCentralSupplierProtected", err)
	}
	if err := repo.ReactivateSupplier(ctx, "sup-central", "admin-1"); !errors.Is(err, repository.ErrCentralSupplierProtected) {
		t.Fatalf("reactivate central: got %v, want ErrCentralSupplierProtected", err)
	}

	if err := repo.SuspendSupplier(ctx, "sup-a", "admin-1", "تخلف از قرارداد"); err != nil {
		t.Fatalf("suspend approved: %v", err)
	}
	if err := repo.SuspendSupplier(ctx, "sup-a", "admin-1", "again"); !errors.Is(err, repository.ErrSupplierStateConflict) {
		t.Fatalf("double suspend: got %v, want ErrSupplierStateConflict", err)
	}

	// DTO carries the suspension bookkeeping; owner account untouched.
	list, err := repo.ListSuppliersForAdminDetail(ctx)
	if err != nil {
		t.Fatalf("admin detail: %v", err)
	}
	var supAStatus string
	var suspReason interface{}
	for _, s := range list {
		if s.ID == "sup-a" {
			supAStatus = s.ApprovalStatus
			if s.SuspendedAt == nil || s.SuspendedBy == nil || *s.SuspendedBy != "admin-1" || s.SuspensionReason == nil || *s.SuspensionReason != "تخلف از قرارداد" {
				t.Fatalf("DTO suspension fields wrong: %+v", s)
			}
			suspReason = *s.SuspensionReason
		}
	}
	if supAStatus != "suspended" || suspReason == nil {
		t.Fatalf("sup-a status = %q, want suspended", supAStatus)
	}
	var ownerActive bool
	if err := db.QueryRow(`SELECT is_active FROM users WHERE id = 'user-owner-1'`).Scan(&ownerActive); err != nil || !ownerActive {
		t.Fatalf("owner is_active changed by suspend: %v %v", ownerActive, err)
	}

	if err := repo.ReactivateSupplier(ctx, "sup-a", "admin-1"); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	list, err = repo.ListSuppliersForAdminDetail(ctx)
	if err != nil {
		t.Fatalf("admin detail after reactivate: %v", err)
	}
	for _, s := range list {
		if s.ID == "sup-a" {
			if s.ApprovalStatus != "approved" || s.SuspendedAt != nil || s.SuspendedBy != nil || s.SuspensionReason != nil {
				t.Fatalf("reactivation did not restore/clear: %+v", s)
			}
		}
	}
	if err := db.QueryRow(`SELECT is_active FROM users WHERE id = 'user-owner-1'`).Scan(&ownerActive); err != nil || !ownerActive {
		t.Fatalf("owner is_active changed by reactivate: %v %v", ownerActive, err)
	}

	// Exactly two audit rows, admin operator, supplier category.
	rows, err := db.Query(`SELECT operator, action, category, details FROM audit_logs ORDER BY timestamp, action`)
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var op, act, cat, det string
		if err := rows.Scan(&op, &act, &cat, &det); err != nil {
			t.Fatalf("audit scan: %v", err)
		}
		if op != "admin:admin-1" || cat != "supplier" || !strings.Contains(det, `"supplier_id":"sup-a"`) {
			t.Fatalf("audit row shape wrong: %s %s %s %s", op, act, cat, det)
		}
		actions = append(actions, act)
	}
	if len(actions) != 2 {
		t.Fatalf("audit rows = %v, want [supplier_suspend supplier_reactivate]", actions)
	}
}

func TestSupplierSuspensionPanelAccess(t *testing.T) {
	db := suspensionTestSetup(t)
	repo := repository.NewSupplierRepository(db)
	ctx := context.Background()

	assertAccess := func(userID string, want bool) {
		t.Helper()
		_, _, ok, err := repo.GetSupplierAccess(ctx, userID)
		if err != nil {
			t.Fatalf("access %s: %v", userID, err)
		}
		if ok != want {
			t.Fatalf("access %s = %v, want %v", userID, ok, want)
		}
	}
	assertAccess("user-owner-1", true)
	assertAccess("user-member-1", true)

	if err := repo.SuspendSupplier(ctx, "sup-a", "admin-1", "x"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	assertAccess("user-owner-1", false)
	assertAccess("user-member-1", false)
	// The other supplier's owner is unaffected.
	assertAccess("user-owner-2", true)

	if err := repo.ReactivateSupplier(ctx, "sup-a", "admin-1"); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	assertAccess("user-owner-1", true)
	assertAccess("user-member-1", true)
}

func TestSupplierSuspensionCatalogGate(t *testing.T) {
	db := suspensionTestSetup(t)
	supRepo := repository.NewSupplierRepository(db)
	catRepo := repository.NewCatalogRepository(db)
	ctx := context.Background()

	contains := func(filter repository.ProductFilter, want string) bool {
		t.Helper()
		products, err := catRepo.GetProducts(ctx, filter)
		if err != nil {
			t.Fatalf("GetProducts: %v", err)
		}
		for _, p := range products {
			if p.ID == want {
				return true
			}
		}
		return false
	}
	counts := func() int {
		t.Helper()
		m, err := catRepo.GetCategoryProductCounts(ctx)
		if err != nil {
			t.Fatalf("counts: %v", err)
		}
		return m["cat-digital"]
	}

	if !contains(repository.ProductFilter{}, "prod-a1") || counts() != 3 {
		t.Fatalf("pre-suspend catalog wrong")
	}
	if err := supRepo.SuspendSupplier(ctx, "sup-a", "admin-1", "x"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if contains(repository.ProductFilter{}, "prod-a1") {
		t.Fatalf("suspended product still listed unscoped")
	}
	if contains(repository.ProductFilter{StoreScoped: true, StoreProductIDs: []string{"prod-a1", "prod-b1"}}, "prod-a1") {
		t.Fatalf("suspended product still listed in a store assortment")
	}
	if !contains(repository.ProductFilter{}, "prod-b1") || !contains(repository.ProductFilter{}, "prod-legacy") {
		t.Fatalf("clean / supplier-less legacy products must stay listed")
	}
	if counts() != 2 {
		t.Fatalf("category count = %d, want 2 (suspended hidden, legacy kept)", counts())
	}
	if suspended, err := catRepo.IsSupplierSuspended(ctx, "sup-a"); err != nil || !suspended {
		t.Fatalf("IsSupplierSuspended(sup-a) = %v, %v", suspended, err)
	}
	if suspended, err := catRepo.IsSupplierSuspended(ctx, "sup-ghost"); err != nil || suspended {
		t.Fatalf("IsSupplierSuspended(sup-ghost) = %v, %v", suspended, err)
	}

	if err := supRepo.ReactivateSupplier(ctx, "sup-a", "admin-1"); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if !contains(repository.ProductFilter{}, "prod-a1") || counts() != 3 {
		t.Fatalf("reactivation did not restore the catalog")
	}
}

func TestSupplierSuspensionPublicDetail404(t *testing.T) {
	db := suspensionTestSetup(t)
	supRepo := repository.NewSupplierRepository(db)
	catRepo := repository.NewCatalogRepository(db)
	storeRepo := repository.NewStoreRepository(db)
	pricingRepo := repository.NewPricingRepository(db)
	ctx := context.Background()

	detail := func(subPath string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/products/"+subPath, nil)
		rec := httptest.NewRecorder()
		handleProductDetail(rec, req, ctx, catRepo, storeRepo, pricingRepo, subPath, supRepo)
		return rec.Code
	}
	if code := detail("prod-a1"); code != http.StatusOK {
		t.Fatalf("detail before suspend = %d, want 200", code)
	}
	if err := supRepo.SuspendSupplier(ctx, "sup-a", "admin-1", "x"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if code := detail("prod-a1"); code != http.StatusNotFound {
		t.Fatalf("detail after suspend = %d, want 404", code)
	}
	if code := detail("spn-111"); code != http.StatusNotFound {
		t.Fatalf("spn detail after suspend = %d, want 404", code)
	}
	if code := detail("prod-b1"); code != http.StatusOK {
		t.Fatalf("clean product detail = %d, want 200", code)
	}
	// The repo read itself stays ungated (admin/history/warehouse use).
	p, err := catRepo.GetProductByID(ctx, "prod-a1")
	if err != nil || p == nil {
		t.Fatalf("GetProductByID must still resolve a suspended supplier's product: %v %v", p, err)
	}
}

func TestSupplierSuspensionOrderGate(t *testing.T) {
	db := suspensionTestSetup(t)
	supRepo := repository.NewSupplierRepository(db)
	catRepo := repository.NewCatalogRepository(db)
	storeRepo := repository.NewStoreRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	shipmentRepo := repository.NewSupplierShipmentRepository(db)
	pricingRepo := repository.NewPricingRepository(db)
	ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: "user-cust", Mobile: "09120000009", Role: "customer"})

	create := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handleCreateOrder(rec, req, ctx, orderRepo, catRepo, storeRepo, shipmentRepo, pricingRepo, supRepo, db)
		return rec
	}
	info := `"customerInfo":{"fullName":"مشتری","phone":"09120000009","province":"قم","city":"قم","address":"x","postalCode":"3711111111"}`
	suspendedBody := `{"storeId":"central",` + info + `,"items":[{"productId":"prod-b1","quantity":1},{"productId":"prod-a1","quantity":1}]}`
	cleanBody := `{"storeId":"central",` + info + `,"items":[{"productId":"prod-b1","quantity":1}]}`

	if err := supRepo.SuspendSupplier(ctx, "sup-a", "admin-1", "x"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	rec := create(suspendedBody)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Product prod-a1 is unavailable because its supplier is suspended") {
		t.Fatalf("suspended cart: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := create(cleanBody); rec.Code != http.StatusCreated {
		t.Fatalf("clean cart: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := supRepo.ReactivateSupplier(ctx, "sup-a", "admin-1"); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if rec := create(suspendedBody); rec.Code != http.StatusCreated {
		t.Fatalf("cart after reactivate: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSupplierSuspensionSettlementFreeze(t *testing.T) {
	db := suspensionTestSetup(t)
	repo := repository.NewSupplierRepository(db)
	ctx := context.Background()

	// Seed earned balance for sup-b: one paid + delivered order line.
	if _, err := db.Exec(`INSERT INTO orders (id, payment_status, status) VALUES ('ord-paid-1', 'paid', 'delivered')`); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost) VALUES (gen_random_uuid(), 'ord-paid-1', 'prod-b1', 1, 500000)`); err != nil {
		t.Fatalf("seed order item: %v", err)
	}

	if _, err := repo.RequestSettlement(ctx, "sup-b", 100000, "IR000000000000000000000000", "بانک", "مالک ب"); err != nil {
		t.Fatalf("settlement while approved: %v", err)
	}
	if err := repo.SuspendSupplier(ctx, "sup-b", "admin-1", "x"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if _, err := repo.RequestSettlement(ctx, "sup-b", 100000, "IR000000000000000000000000", "بانک", "مالک ب"); !errors.Is(err, repository.ErrSupplierSuspended) {
		t.Fatalf("settlement while suspended: got %v, want ErrSupplierSuspended", err)
	}
	// Balance truth is untouched by suspension: Earned still accrues.
	bal, err := repo.GetSettlementBalance(ctx, "sup-b")
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if bal.Earned != 500000 {
		t.Fatalf("earned = %d, want 500000 (suspension must not rewrite accounting)", bal.Earned)
	}
	if err := repo.ReactivateSupplier(ctx, "sup-b", "admin-1"); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if _, err := repo.RequestSettlement(ctx, "sup-b", 100000, "IR000000000000000000000000", "بانک", "مالک ب"); err != nil {
		t.Fatalf("settlement after reactivate: %v", err)
	}
}

func TestSupplierSuspensionAdminHandlers(t *testing.T) {
	db := suspensionTestSetup(t)
	repo := repository.NewSupplierRepository(db)
	h := NewSupplierHandlers(repo, nil, false)
	ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: "admin-1", Role: "admin"})

	call := func(handler func(http.ResponseWriter, *http.Request, string), id, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/suppliers/"+id, strings.NewReader(body)).WithContext(ctx)
		rec := httptest.NewRecorder()
		handler(rec, req, id)
		return rec
	}
	if rec := call(h.HandleAdminSupplierSuspend, "sup-a", `{"reason":"تخلف"}`); rec.Code != http.StatusOK {
		t.Fatalf("suspend handler = %d, body %s", rec.Code, rec.Body.String())
	}
	if rec := call(h.HandleAdminSupplierSuspend, "sup-a", `{"reason":"دوباره"}`); rec.Code != http.StatusConflict {
		t.Fatalf("double suspend handler = %d, want 409", rec.Code)
	}
	if rec := call(h.HandleAdminSupplierSuspend, "sup-central", `{"reason":"x"}`); rec.Code != http.StatusConflict {
		t.Fatalf("central suspend handler = %d, want 409", rec.Code)
	}
	if rec := call(h.HandleAdminSupplierReactivate, "sup-a", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("reactivate handler = %d, body %s", rec.Code, rec.Body.String())
	}
	if rec := call(h.HandleAdminSupplierReactivate, "sup-a", `{}`); rec.Code != http.StatusConflict {
		t.Fatalf("double reactivate handler = %d, want 409", rec.Code)
	}
}
