package main

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

	_ "github.com/lib/pq"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

// Gaps 19b + 20 handler coverage, DB-gated like the other suites in
// this package (TEAM04_TEST_DATABASE_URL; skips when unset). The DDL
// is the minimal faithful subset of the real schema the exercised
// handlers and repo functions touch.

const supplierGapsDDL = `
DROP TABLE IF EXISTS warranty_property_requests, warranty_properties, inventory_balances,
	product_variants, product_niches, niches, product_images, products,
	pending_approvals, platform_settings, suppliers, users CASCADE;
CREATE TABLE users (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	mobile VARCHAR(15) UNIQUE NOT NULL,
	full_name VARCHAR(100) NOT NULL DEFAULT '',
	role VARCHAR(20) NOT NULL DEFAULT 'customer',
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	username VARCHAR(50) DEFAULT '',
	password_hash VARCHAR(255) DEFAULT '',
	email VARCHAR(254),
	failed_login_attempts INT NOT NULL DEFAULT 0,
	locked_until TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	warehouse_city character varying(50) NOT NULL DEFAULT 'تهران'::character varying);
CREATE TABLE suppliers (
	id VARCHAR(50) PRIMARY KEY,
	owner_user_id UUID NOT NULL,
	name VARCHAR(200) NOT NULL,
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
	signed_contract_version INT DEFAULT 1 ,
	tapin_shop_id VARCHAR(50),
	warehouse_city_confirmed BOOLEAN DEFAULT FALSE,
	warehouse_city_source VARCHAR(30),
	default_markup_type VARCHAR(20),
	default_markup_value DECIMAL(10,2),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	suspended_at timestamp with time zone,
	suspended_by text,
	suspension_reason text,
	tapin_token text);
CREATE TABLE platform_settings (
	id INT  DEFAULT 1 PRIMARY KEY,
	supplier_contract_version INT
 DEFAULT 2 ,
	-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	early_withdrawal_fee_pct NUMERIC,
	early_withdrawal_minimum_toman BIGINT NOT NULL DEFAULT 1000000 CONSTRAINT platform_settings_early_withdrawal_minimum_toman_check CHECK (early_withdrawal_minimum_toman BETWEEN 1 AND 9007199254740991),
	platform_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
	settlement_day INTEGER,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	attribution_model character varying(20) NOT NULL DEFAULT 'last_touch'::character varying,
	base_fee_f bigint NOT NULL DEFAULT 100000,
	central_campaign_budget bigint NOT NULL DEFAULT 0,
	central_profit_pct double precision NOT NULL DEFAULT 10,
	central_store_markup_percent numeric(5,2) NOT NULL DEFAULT 10,
	cookie_days integer NOT NULL DEFAULT 30,
	default_item_weight_grams integer NOT NULL DEFAULT 500,
	default_margin numeric(5,2) NOT NULL DEFAULT 20.00,
	default_markup_type character varying(10) NOT NULL DEFAULT 'percent'::character varying,
	default_markup_value numeric(10,2) NOT NULL DEFAULT 30,
	delivery_followup_trigger_days integer NOT NULL DEFAULT 10,
	delivery_hold_period_days integer NOT NULL DEFAULT 20,
	increase_cap_pct double precision NOT NULL DEFAULT 10,
	increase_locked boolean NOT NULL DEFAULT true,
	ippanel_api_key text NOT NULL DEFAULT ''::text,
	ippanel_base_url text NOT NULL DEFAULT 'https://edge.ippanel.com/v1'::text,
	ippanel_enabled boolean NOT NULL DEFAULT false,
	ippanel_pattern_code text NOT NULL DEFAULT ''::text,
	ippanel_sender text NOT NULL DEFAULT '+983000505'::text,
	max_dropshipper_margin_percent numeric(5,2) NOT NULL DEFAULT 100,
	max_profit_margin numeric(5,2) NOT NULL DEFAULT 35.00,
	min_profit_margin numeric(5,2) NOT NULL DEFAULT 5.00,
	packaging_tariffs jsonb,
	product_fee_overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
	referral_commission_percent numeric(5,2) NOT NULL DEFAULT 2.00,
	referral_is_enabled boolean NOT NULL DEFAULT true,
	referral_subject_to_delivery_lock boolean NOT NULL DEFAULT true,
	return_window_days integer NOT NULL DEFAULT 7,
	seller_contract_text text NOT NULL DEFAULT ''::text,
	seller_contract_version integer NOT NULL DEFAULT 2,
	shipping_base_rate bigint NOT NULL DEFAULT 0,
	shipping_per_kg bigint NOT NULL DEFAULT 0,
	storage_fee_enabled boolean NOT NULL DEFAULT false,
	storage_fee_free_days integer NOT NULL DEFAULT 30,
	storage_fee_per_kg_day bigint NOT NULL DEFAULT 2000,
	storage_fee_per_m3_day bigint NOT NULL DEFAULT 8000,
	stuck_order_expiry_enabled boolean NOT NULL DEFAULT false,
	supplier_contract_text text NOT NULL DEFAULT ''::text,
	supplier_fee_overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
	supplier_return_postal_charge bigint,
	tapin_default_box_id text NOT NULL DEFAULT '4'::text,
	tapin_platform_shop_id text,
	tapin_platform_token text,
	tapin_tariff_url text,
	trial_catalog_quota integer NOT NULL DEFAULT 25,
	updated_at timestamp with time zone NOT NULL DEFAULT now(),
	volumetric_divisor integer NOT NULL DEFAULT 5000,
	wallet_min_withdrawal bigint NOT NULL DEFAULT 0,
	wallet_spend_enabled boolean NOT NULL DEFAULT true,
	wallet_withdraw_enabled boolean NOT NULL DEFAULT true,
	warehouse_tariffs jsonb NOT NULL DEFAULT '{}'::jsonb);
INSERT INTO platform_settings (id, supplier_contract_version) VALUES (1, 7);
CREATE TABLE pending_approvals (
	id VARCHAR(50) PRIMARY KEY,
	entity_type VARCHAR(30) NOT NULL,
	target_id VARCHAR(50) NOT NULL DEFAULT '',
	title VARCHAR(200) NOT NULL DEFAULT '',
	requested_data JSONB NOT NULL DEFAULT '{}'::jsonb,
	requested_by VARCHAR(100) NOT NULL DEFAULT '',
	status VARCHAR(20) NOT NULL DEFAULT 'pending',
	rejection_reason TEXT,
	reviewed_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE products (
	id VARCHAR(50) PRIMARY KEY,
	title VARCHAR(255) DEFAULT '',
	slug VARCHAR(255) DEFAULT '',
	category_slug VARCHAR(100) DEFAULT '',
	supplier_id VARCHAR(50),
	original_price BIGINT DEFAULT 0,
	supplier_base_cost BIGINT DEFAULT 0,
	platform_margin BIGINT DEFAULT 0,
	is_original BOOLEAN DEFAULT TRUE,
	stock INT DEFAULT 0,
	image_url TEXT DEFAULT '',
	description TEXT DEFAULT '',
	warehouse_name VARCHAR(200) DEFAULT '',
	preparation_days INT DEFAULT 0,
	supplier_warranty VARCHAR(200) DEFAULT '',
	specs JSONB DEFAULT '{}'::jsonb,
	fulfillment_method VARCHAR(30) DEFAULT '',
	barcode VARCHAR(100),
	brand VARCHAR(100) DEFAULT '',
	length_mm INT,
	width_mm INT,
	height_mm INT,
	weight_grams INT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	measurement_status VARCHAR(255) NOT NULL DEFAULT 'unverified',
	-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	measurement_version INTEGER NOT NULL DEFAULT 0,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	attribute_schema jsonb,
	base_price_override bigint,
	implicit_agreement boolean NOT NULL DEFAULT false,
	implicit_agreement_until timestamp with time zone,
	is_active boolean NOT NULL DEFAULT true,
	is_featured boolean DEFAULT false,
	is_special_deal boolean DEFAULT false,
	markup_type character varying(10),
	markup_value numeric(10,2),
	measurement_revision_id text,
	measurement_source character varying(20),
	origin_supplier_warehouse_id text,
	packaging_height_mm integer,
	packaging_length_mm integer,
	packaging_width_mm integer,
	product_type text NOT NULL DEFAULT 'legacy'::text,
	rating numeric(3,2) NOT NULL DEFAULT 0,
	reviews_count integer NOT NULL DEFAULT 0,
	schema_version integer NOT NULL DEFAULT 0,
	shipping_class text NOT NULL DEFAULT 'standard'::text,
	spn character varying(20),
	spn_number bigint,
	supply_option character varying(20) NOT NULL DEFAULT '1day'::character varying,
	tax_class text NOT NULL DEFAULT 'standard'::text,
	warranty_property_id character varying(60));
CREATE TABLE product_images (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	product_id VARCHAR(50) NOT NULL,
	image_url TEXT NOT NULL,
	sort_order INT DEFAULT 0
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	created_at timestamp with time zone NOT NULL DEFAULT now());
CREATE TABLE niches (
	id VARCHAR(50) PRIMARY KEY,
	category_slug VARCHAR(100) DEFAULT ''
,
	-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	title VARCHAR(255) NOT NULL DEFAULT '',
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	badge character varying(50),
	created_at timestamp with time zone NOT NULL DEFAULT now(),
	description text,
	slug character varying(100));
CREATE TABLE product_niches (
	product_id VARCHAR(50) NOT NULL,
	niche_id VARCHAR(50) NOT NULL,
	PRIMARY KEY (product_id, niche_id)
);
CREATE TABLE product_variants (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	product_id VARCHAR(50) NOT NULL,
	spnc VARCHAR(20),
	variant_name VARCHAR(200) DEFAULT '',
	attributes JSONB DEFAULT '{}'::jsonb,
	is_default BOOLEAN DEFAULT FALSE,
	stock INT DEFAULT 0,
	supplier_base_cost BIGINT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	combination_key text,
	height_mm integer,
	inventory_mode text NOT NULL DEFAULT 'legacy'::text,
	is_active boolean NOT NULL DEFAULT true,
	length_mm integer,
	max_cart_qty integer,
	measurement_revision_id text,
	measurement_source text,
	measurement_status text NOT NULL DEFAULT 'unverified'::text,
	measurement_version integer NOT NULL DEFAULT 0,
	min_cart_qty integer NOT NULL DEFAULT 1,
	sale_from timestamp with time zone,
	sale_price bigint,
	sale_to timestamp with time zone,
	selection jsonb NOT NULL DEFAULT '{}'::jsonb,
	shipping_class text,
	spnc_number integer,
	tax_class text,
	weight_grams integer,
	width_mm integer);
CREATE TABLE inventory_balances (
	id VARCHAR(80) PRIMARY KEY,
	product_id VARCHAR(50) NOT NULL,
	supplier_id VARCHAR(50),
	owner_type VARCHAR(20) NOT NULL,
	location_type VARCHAR(20) NOT NULL,
	location_key VARCHAR(150) NOT NULL,
	warehouse_name VARCHAR(100),
	status VARCHAR(30) NOT NULL,
	quantity INT NOT NULL DEFAULT 0,
	unit_tracked BOOLEAN NOT NULL DEFAULT FALSE,
	reconciled BOOLEAN NOT NULL DEFAULT FALSE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	warehouse_id TEXT,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	bin_code character varying(30),
	supplier_warehouse_id text,
	variant_id uuid);
CREATE UNIQUE INDEX IF NOT EXISTS inventory_balances_unallocated_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS inventory_balances_sku_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL;
-- TEST-02 round 4: DROP TABLE inventory_balances above also drops its
-- triggers. Recreate them (the trigger FUNCTIONS survive the drop):
-- trg_inventory_balances_sync_stock keeps product_variants.stock and
-- products.stock in sync with sellable balances (migrations
-- 202610170001 + 202611110003); inventory_assign_origin stamps the
-- origin warehouse on insert (migration 202611140002). Without the
-- sync trigger, INV07-style SKU stock assertions read stale 0s.
DROP TRIGGER IF EXISTS trg_inventory_balances_sync_stock ON inventory_balances;
CREATE TRIGGER trg_inventory_balances_sync_stock AFTER INSERT OR UPDATE OR DELETE ON inventory_balances FOR EACH ROW EXECUTE FUNCTION sync_product_stock_projection();
DROP TRIGGER IF EXISTS inventory_assign_origin ON inventory_balances;
CREATE TRIGGER inventory_assign_origin BEFORE INSERT ON inventory_balances FOR EACH ROW EXECUTE FUNCTION inventory_assign_origin();
CREATE TABLE warranty_properties (
	id VARCHAR(60) PRIMARY KEY,
	name VARCHAR(200) NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	status VARCHAR(20) NOT NULL DEFAULT 'active',
	created_by VARCHAR(50),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX uq_warranty_properties_name ON warranty_properties (lower(trim(name)));
CREATE TABLE warranty_property_requests (
	id VARCHAR(60) PRIMARY KEY,
	supplier_id VARCHAR(50),
	product_id VARCHAR(50),
	proposed_name VARCHAR(200) DEFAULT '',
	description TEXT DEFAULT '',
	image_urls JSONB NOT NULL DEFAULT '[]'::jsonb,
	status VARCHAR(20) NOT NULL DEFAULT 'pending',
	rejection_reason TEXT,
	approved_property_id VARCHAR(60),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	reviewed_at TIMESTAMPTZ
);
`

type supplierGapsFixture struct {
	db           *sql.DB
	ctx          context.Context
	supplierRepo *repository.SupplierRepository
	adminRepo    *repository.AdminRepository
	jwtService   *auth.JWTService
}

func supplierGapsSetup(t *testing.T) *supplierGapsFixture {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEAM04B_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set TEAM04_TEST_DATABASE_URL to run the supplier-gaps handler tests")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, supplierGapsDDL); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE products ADD COLUMN IF NOT EXISTS schema_version INT DEFAULT 0, ADD COLUMN IF NOT EXISTS origin_supplier_warehouse_id TEXT, ADD COLUMN IF NOT EXISTS shipping_class TEXT DEFAULT 'standard', ADD COLUMN IF NOT EXISTS tax_class TEXT DEFAULT 'standard';
		ALTER TABLE product_variants ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT TRUE;
		ALTER TABLE inventory_balances ADD COLUMN IF NOT EXISTS variant_id UUID;
		ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS tapin_token TEXT;`); err != nil {
		t.Fatal(err)
	}
	return &supplierGapsFixture{
		db:           db,
		ctx:          ctx,
		supplierRepo: repository.NewSupplierRepository(db),
		adminRepo:    repository.NewAdminRepository(db),
		jwtService:   auth.NewJWTService("supplier-gaps-test-secret"),
	}
}

func (f *supplierGapsFixture) seedProposal(t *testing.T, id string) {
	t.Helper()
	if _, err := f.db.ExecContext(f.ctx, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES('gap-source','sup-1','Approval source','Test','Test') ON CONFLICT(id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	data := map[string]interface{}{"supplierBaseCost": float64(800), "stock": float64(3), "categorySlug": "digital", "imageUrls": []interface{}{}, "supplierWarehouseId": "gap-source"}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO pending_approvals (id, entity_type, target_id, title, requested_data, requested_by, status)
		VALUES ($1, 'proposed_product', $1, 'کالای تستی', $2, 'sup-1', 'pending')`, id, string(raw)); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
	var decisions []repository.FieldDecision
	for key, value := range repository.ReviewLeaves(data) {
		decisions = append(decisions, repository.FieldDecision{Key: key, Decision: "approved", ValueHash: repository.ReviewValueHash(value)})
	}
	if err := f.supplierRepo.SaveFieldReview(f.ctx, id, "test-admin", decisions); err != nil {
		t.Fatal(err)
	}
}

func (f *supplierGapsFixture) productCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM products`).Scan(&n); err != nil {
		t.Fatalf("count products: %v", err)
	}
	return n
}

func (f *supplierGapsFixture) approvalRow(t *testing.T, id string) (string, sql.NullString) {
	t.Helper()
	var status string
	var reason sql.NullString
	if err := f.db.QueryRowContext(f.ctx, `
		SELECT status, rejection_reason FROM pending_approvals WHERE id = $1`, id).Scan(&status, &reason); err != nil {
		t.Fatalf("read approval: %v", err)
	}
	return status, reason
}

// GAP 20a: the dedicated approve route honours {approved:false}, and
// the pre-existing no-field call keeps approving (back-compat).
func TestDedicatedApproveRouteHonoursApprovedFlag(t *testing.T) {
	f := supplierGapsSetup(t)
	handlers := NewSupplierHandlers(f.supplierRepo, f.jwtService, false)
	call := func(id, body string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/approve/"+id, strings.NewReader(body))
		rec := httptest.NewRecorder()
		handlers.HandleApproveProposal(rec, req)
		return rec.Code, rec.Body.String()
	}

	// {approved:false} → rejected with the reason, no product.
	f.seedProposal(t, "pa-ded-1")
	code, body := call("pa-ded-1", `{"approved":false,"reason":"قیمت نامعتبر"}`)
	if code != http.StatusOK || !strings.Contains(body, `"status":"rejected"`) {
		t.Fatalf("dedicated reject = %d %s", code, body)
	}
	if n := f.productCount(t); n != 0 {
		t.Fatalf("products after reject = %d, want 0", n)
	}
	if status, reason := f.approvalRow(t, "pa-ded-1"); status != "rejected" || !reason.Valid || reason.String != "قیمت نامعتبر" {
		t.Fatalf("approval row = (%q, %+v), want rejected + reason", status, reason)
	}

	// No field at all (the current admin UI) → still approves.
	f.seedProposal(t, "pa-ded-2")
	code, body = call("pa-ded-2", `{}`)
	if code != http.StatusOK || !strings.Contains(body, `"status":"approved"`) {
		t.Fatalf("dedicated approve (empty body) = %d %s", code, body)
	}
	if n := f.productCount(t); n != 1 {
		t.Fatalf("products after approve = %d, want 1", n)
	}

	// Explicit {approved:true} → approves.
	f.seedProposal(t, "pa-ded-3")
	code, _ = call("pa-ded-3", `{"approved":true}`)
	if code != http.StatusOK {
		t.Fatalf("dedicated approve (true) = %d", code)
	}
	if n := f.productCount(t); n != 2 {
		t.Fatalf("products after second approve = %d, want 2", n)
	}
}

// GAP 20b: the generic inbox review runs proposals through the SAME
// ApproveProposal/RejectProposal pipeline (product created exactly
// once; a second review is the guard's conflict), while other entity
// types keep their generic behaviour exactly.
func TestGenericInboxReviewRoutesProposalsThroughPipeline(t *testing.T) {
	f := supplierGapsSetup(t)
	review := func(id, body string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/approvals/"+id+"/review", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handleReviewApproval(rec, req, f.ctx, id, f.adminRepo, f.supplierRepo)
		return rec.Code, rec.Body.String()
	}

	// Approve a proposal via the inbox → product created.
	f.seedProposal(t, "pa-gen-1")
	code, body := review("pa-gen-1", `{"status":"approved"}`)
	if code != http.StatusOK {
		t.Fatalf("inbox approve = %d %s", code, body)
	}
	var approved map[string]interface{}
	if err := json.Unmarshal([]byte(body), &approved); err != nil || approved["productId"] == "" || approved["productId"] == nil {
		t.Fatalf("inbox approve body = %s (err %v), want productId", body, err)
	}
	if n := f.productCount(t); n != 1 {
		t.Fatalf("products after inbox approve = %d, want 1", n)
	}
	if status, _ := f.approvalRow(t, "pa-gen-1"); status != "approved" {
		t.Fatalf("approval status = %q, want approved", status)
	}

	// Second review (the other route racing) → conflict, still ONE product.
	code, _ = review("pa-gen-1", `{"status":"approved"}`)
	if code != http.StatusConflict {
		t.Fatalf("second inbox approve = %d, want 409", code)
	}
	if n := f.productCount(t); n != 1 {
		t.Fatalf("products after second approve = %d, want exactly 1", n)
	}

	// Reject a proposal via the inbox → rejected + reason, no product.
	f.seedProposal(t, "pa-gen-2")
	code, body = review("pa-gen-2", `{"status":"rejected","rejectionReason":"توضیح ناقص"}`)
	if code != http.StatusOK || !strings.Contains(body, `"status":"rejected"`) {
		t.Fatalf("inbox reject = %d %s", code, body)
	}
	if status, reason := f.approvalRow(t, "pa-gen-2"); status != "rejected" || !reason.Valid || reason.String != "توضیح ناقص" {
		t.Fatalf("inbox-rejected row = (%q, %+v)", status, reason)
	}
	if n := f.productCount(t); n != 1 {
		t.Fatalf("products after inbox reject = %d, want still 1", n)
	}

	// A warranty_property review keeps the generic behaviour exactly:
	// property row created, approval flipped, NO product involved.
	warrantyData := `{"proposed_name":"ویژگی آزمایشی","description":"توضیح کامل ویژگی آزمایشی","warranty_request_id":"wreq-1","supplier_id":"sup-1"}`
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO warranty_property_requests (id, supplier_id, proposed_name, description, status)
		VALUES ('wreq-1', 'sup-1', 'ویژگی آزمایشی', 'توضیح کامل ویژگی آزمایشی', 'pending')`); err != nil {
		t.Fatalf("seed warranty request: %v", err)
	}
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO pending_approvals (id, entity_type, target_id, title, requested_data, requested_by, status)
		VALUES ('pa-w-1', 'warranty_property', 'pa-w-1', 'ویژگی آزمایشی', $1, 'sup-1', 'pending')`, warrantyData); err != nil {
		t.Fatalf("seed warranty approval: %v", err)
	}
	code, body = review("pa-w-1", `{"status":"approved"}`)
	if code != http.StatusOK {
		t.Fatalf("warranty review = %d %s", code, body)
	}
	var propStatus string
	if err := f.db.QueryRowContext(f.ctx, `
		SELECT status FROM warranty_properties WHERE id = 'WPROP-pa-w-1'`).Scan(&propStatus); err != nil {
		t.Fatalf("warranty property row missing: %v", err)
	}
	if propStatus != "active" {
		t.Fatalf("warranty property status = %q, want active", propStatus)
	}
	var reqStatus, reqProp string
	if err := f.db.QueryRowContext(f.ctx, `
		SELECT status, COALESCE(approved_property_id, '') FROM warranty_property_requests WHERE id = 'wreq-1'`).Scan(&reqStatus, &reqProp); err != nil {
		t.Fatalf("read warranty request: %v", err)
	}
	if reqStatus != "approved" || reqProp != "WPROP-pa-w-1" {
		t.Fatalf("warranty request = (%q, %q), want (approved, WPROP-pa-w-1)", reqStatus, reqProp)
	}
	if status, _ := f.approvalRow(t, "pa-w-1"); status != "approved" {
		t.Fatalf("warranty approval status = %q, want approved", status)
	}
	if n := f.productCount(t); n != 1 {
		t.Fatalf("products after warranty review = %d, want still 1 (no product path)", n)
	}
}
