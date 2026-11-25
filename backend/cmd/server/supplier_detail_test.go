package main

// Item 6 (admin supplier detail) handler tests. Modelled on
// TestSupplierSuspensionAdminHandlers: direct handler calls via
// httptest with an admin claims context, against the disposable DB
// from suspensionTestSetup (TEAM04_TEST_DATABASE_URL; skips without
// it). The suspension DDL lacks the tables/columns the detail
// endpoint reads (product_variants, pending_approvals,
// supplier_warehouses, platform_settings, users.email,
// suppliers.tapin_token), so detailTestSeed adds them here — the
// shared suspension DDL is left untouched.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

const supplierDetailExtraDDL = `
ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT;
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS tapin_token TEXT;
CREATE TABLE IF NOT EXISTS product_variants (
	id TEXT  DEFAULT gen_random_uuid() PRIMARY KEY,
	product_id TEXT NOT NULL,
	spnc TEXT DEFAULT '',
	variant_name TEXT DEFAULT ''
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
	combination_key text,
	created_at timestamp with time zone NOT NULL DEFAULT now(),
	height_mm integer,
	inventory_mode text NOT NULL DEFAULT 'legacy'::text,
	is_active boolean NOT NULL DEFAULT true,
	is_default boolean NOT NULL DEFAULT false,
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
	stock integer NOT NULL DEFAULT 0,
	supplier_base_cost bigint,
	tax_class text,
	weight_grams integer,
	width_mm integer);
CREATE TABLE IF NOT EXISTS pending_approvals (
	id TEXT PRIMARY KEY,
	entity_type TEXT NOT NULL,
	target_id TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	requested_data JSONB NOT NULL DEFAULT '{}'::jsonb,
	requested_by TEXT NOT NULL,
	status TEXT  DEFAULT 'pending'::character varying NOT NULL,
	rejection_reason TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	reviewed_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS supplier_warehouses (
	id TEXT PRIMARY KEY,
	supplier_id TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	province_name TEXT NOT NULL DEFAULT '',
	city_name TEXT NOT NULL DEFAULT '',
	address TEXT DEFAULT '',
	postal_code TEXT DEFAULT '',
	phone TEXT DEFAULT '',
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	tapin_city_id integer,
	tapin_province_id integer,
	updated_at timestamp with time zone NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS platform_settings (
	id INT  DEFAULT 1 PRIMARY KEY,
	supplier_contract_version INT NOT NULL DEFAULT 2
,
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
INSERT INTO platform_settings (id, supplier_contract_version) VALUES (1, 3)
	ON CONFLICT (id) DO UPDATE SET supplier_contract_version = 3;
-- product_variants / pending_approvals / supplier_warehouses /
-- supplier_shipping_methods are NOT in suspensionDDL's DROP list,
-- so they survive across tests sharing this disposable DB: clear
-- the seeded rows before re-inserting, or the second test in the
-- package trips a duplicate key (and ListSupplierShippingMethods
-- would return the row twice).
DELETE FROM product_variants;
DELETE FROM pending_approvals;
DELETE FROM supplier_warehouses;
DELETE FROM supplier_shipping_methods;
UPDATE users SET email = 'owner-a@example.com' WHERE id = 'user-owner-1';
UPDATE suppliers SET tapin_shop_id = 'shop-1', tapin_token = 'tok-secret' WHERE id = 'sup-a';
UPDATE suppliers SET tapin_shop_id = 'shop-2', tapin_token = NULL WHERE id = 'sup-b';
INSERT INTO products (id, title, slug, category_slug, supplier_id, original_price, supplier_base_cost, stock, is_active, spn_number)
	VALUES ('prod-a2', 'کالای الف ۲', 'prod-a2', 'digital', 'sup-a', 150000, 80000, 0, FALSE, 112);
INSERT INTO product_variants (id, product_id, spnc, variant_name) VALUES
	('11111111-1111-4111-8111-111111111111', 'prod-a1', 'SPNC-A1', 'آبی'),
	('22222222-2222-4222-8222-222222222222', 'prod-a1', 'SPNC-A2', 'قرمز');
INSERT INTO pending_approvals (id, entity_type, target_id, title, requested_data, requested_by, status) VALUES
	('prop-pending', 'proposed_product', 'prop-pending', 'پیشنهاد در انتظار', '{}', 'sup-a', 'pending'),
	('prop-approved', 'proposed_product', 'prop-approved', 'پیشنهاد تأییدشده', '{}', 'sup-a', 'approved'),
	('prop-rejected', 'proposed_product', 'prop-rejected', 'پیشنهاد ردشده', '{}', 'sup-a', 'rejected');
INSERT INTO supplier_warehouses (id, supplier_id, name, province_name, city_name, is_active)
	VALUES ('swh-a1', 'sup-a', 'انبار اصفهان', 'اصفهان', 'اصفهان', TRUE);
INSERT INTO supplier_shipping_methods (owner_user_id, method, status)
	VALUES ('user-owner-1', 'direct', 'enabled');
INSERT INTO orders (id, order_number, payment_status, status, total_amount) VALUES ('ord-1', '1001', 'paid', 'delivered', 200000);
INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost) VALUES ('11111111-1111-1111-1111-111111111111', 'ord-1', 'prod-a1', 2, 100000);
INSERT INTO order_item_origins(id,order_id,order_item_id,origin_type,supplier_id,quantity)
	VALUES('detail-origin','ord-1','11111111-1111-1111-1111-111111111111','legacy','sup-a',2);
INSERT INTO supplier_shipments (id, order_id, supplier_id, shipping_address, customer_phone, customer_name, status)
	VALUES ('ship-1', 'ord-1', 'sup-a', '{}', '09120000009', 'مشتری', 'shipped');
INSERT INTO payout_requests (id, store_id, supplier_id, recipient_type, recipient_name, shaba, bank_name, amount, status)
	VALUES ('pay-1', NULL, 'sup-a', 'supplier', 'مالک الف', 'IR000000000000000000000001', 'بانک', 50000, 'pending');
`

func detailTestSetup(t *testing.T) (*sql.DB, *SupplierHandlers) {
	t.Helper()
	db := suspensionTestSetup(t)
	if _, err := db.Exec(supplierDetailExtraDDL); err != nil {
		t.Fatalf("detail extra DDL/seed failed: %v", err)
	}
	repo := repository.NewSupplierRepository(db)
	// TapinRepository attached: exercises the strict (shop AND token)
	// path through GetSupplierCredentials, as in main().
	h := NewSupplierHandlers(repo, nil, false).WithTapinRepository(repository.NewTapinRepository(db))
	return db, h
}

func detailTestCtx() context.Context {
	return context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: "admin-1", Role: "admin"})
}

func TestAdminSupplierDetailHandler(t *testing.T) {
	db, h := detailTestSetup(t)
	ctx := detailTestCtx()

	// Two audit rows (suspend + reactivate) so history is exercised;
	// sup-a ends approved again.
	repo := repository.NewSupplierRepository(db)
	if err := repo.SuspendSupplier(ctx, "sup-a", "admin-1", "تخلف از قرارداد"); err != nil {
		t.Fatalf("suspend for history seed: %v", err)
	}
	if err := repo.ReactivateSupplier(ctx, "sup-a", "admin-1"); err != nil {
		t.Fatalf("reactivate for history seed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/suppliers/sup-a", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.HandleAdminSupplierDetail(rec, req, "sup-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("detail handler = %d, body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "tapin_token") || strings.Contains(body, "tapinToken") || strings.Contains(body, "tok-secret") {
		t.Fatalf("detail response leaks the Tapin token: %s", body)
	}
	var out struct {
		Supplier map[string]interface{} `json:"supplier"`
		Counts   struct {
			Products       int `json:"products"`
			ActiveProducts int `json:"activeProducts"`
			Variants       int `json:"variants"`
			Proposals      struct {
				Pending  int `json:"pending"`
				Approved int `json:"approved"`
				Rejected int `json:"rejected"`
			} `json:"proposals"`
			Orders struct {
				Paid                    int            `json:"paid"`
				DirectShipmentsByStatus map[string]int `json:"directShipmentsByStatus"`
			} `json:"orders"`
		} `json:"counts"`
		Warehouses      []map[string]interface{} `json:"warehouses"`
		Members         []map[string]interface{} `json:"members"`
		ShippingMethods []map[string]interface{} `json:"shippingMethods"`
		Tapin           struct {
			ShopID    string `json:"shopId"`
			Connected bool   `json:"connected"`
		} `json:"tapin"`
		Finance struct {
			CurrencyUnit string `json:"currencyUnit"`
			Balance      struct {
				Earned    int64 `json:"earned"`
				Reserved  int64 `json:"reserved"`
				Available int64 `json:"available"`
			} `json:"balance"`
			Settlements []map[string]interface{} `json:"settlements"`
		} `json:"finance"`
		History []map[string]interface{} `json:"history"`
		Gaps    []string                 `json:"gaps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode detail response: %v (body %s)", err, body)
	}

	// Record row: stored fields, plus owner contact and the derived
	// direct-shipping flag.
	if out.Supplier["name"] != "تأمین‌کننده الف" {
		t.Errorf("supplier.name = %v", out.Supplier["name"])
	}
	owner, _ := out.Supplier["owner"].(map[string]interface{})
	if owner == nil || owner["fullName"] != "مالک الف" || owner["mobile"] != "09120000001" || owner["email"] != "owner-a@example.com" || owner["isActive"] != true {
		t.Errorf("supplier.owner = %v, want مالک الف / 09120000001 / owner-a@example.com / active", out.Supplier["owner"])
	}
	if out.Supplier["supportsDirectShipping"] != true {
		t.Errorf("supportsDirectShipping = %v, want true (direct method enabled)", out.Supplier["supportsDirectShipping"])
	}
	// Signed contract 1 (COALESCE default in the seed) vs platform 3.
	if out.Supplier["signedContractVersion"] == nil {
		t.Errorf("signedContractVersion missing")
	}
	if out.Supplier["platformContractVersion"] == nil || out.Supplier["platformContractVersion"].(float64) != 3 {
		t.Errorf("platformContractVersion = %v, want 3", out.Supplier["platformContractVersion"])
	}

	// Live-computed counts — never the stored total_products column.
	if out.Counts.Products != 2 || out.Counts.ActiveProducts != 1 || out.Counts.Variants != 2 {
		t.Errorf("counts products/active/variants = %d/%d/%d, want 2/1/2", out.Counts.Products, out.Counts.ActiveProducts, out.Counts.Variants)
	}
	if out.Counts.Proposals.Pending != 1 || out.Counts.Proposals.Approved != 1 || out.Counts.Proposals.Rejected != 1 {
		t.Errorf("counts proposals = %+v, want 1/1/1", out.Counts.Proposals)
	}
	if out.Counts.Orders.Paid != 1 || out.Counts.Orders.DirectShipmentsByStatus["shipped"] != 1 {
		t.Errorf("counts orders = %+v, want paid 1, shipped 1", out.Counts.Orders)
	}

	if len(out.Warehouses) != 1 || out.Warehouses[0]["cityName"] != "اصفهان" {
		t.Errorf("warehouses = %v, want one in اصفهان", out.Warehouses)
	}
	if len(out.Members) != 1 || out.Members[0]["fullName"] != "عضو تیم الف" {
		t.Errorf("members = %v, want the seeded team member", out.Members)
	}
	if len(out.ShippingMethods) != 1 || out.ShippingMethods[0]["method"] != "direct" {
		t.Errorf("shippingMethods = %v, want the direct row", out.ShippingMethods)
	}

	// Tapin strict: shop AND token both stored for sup-a.
	if out.Tapin.ShopID != "shop-1" || !out.Tapin.Connected {
		t.Errorf("tapin = %+v, want shop-1 connected", out.Tapin)
	}

	// Finance in TOMAN: earned 2×100000 delivered, reserved 50000
	// pending payout, available 150000. One pending settlement.
	if out.Finance.CurrencyUnit != "TOMAN" {
		t.Errorf("finance.currencyUnit = %q, want TOMAN", out.Finance.CurrencyUnit)
	}
	if out.Finance.Balance.Earned != 200000 || out.Finance.Balance.Reserved != 50000 || out.Finance.Balance.Available != 150000 {
		t.Errorf("finance.balance = %+v, want earned 200000 reserved 50000 available 150000", out.Finance.Balance)
	}
	if len(out.Finance.Settlements) != 1 {
		t.Errorf("settlements = %d rows, want 1", len(out.Finance.Settlements))
	}

	// History: exactly the two audit rows; approve/reject are
	// honestly flagged as unrecoverable in gaps.
	if len(out.History) != 2 {
		t.Errorf("history = %d rows, want 2 (suspend + reactivate)", len(out.History))
	}
	actions := map[string]bool{}
	for _, hrow := range out.History {
		actions[hrow["action"].(string)] = true
	}
	if !actions["supplier_suspend"] || !actions["supplier_reactivate"] {
		t.Errorf("history actions = %v, want supplier_suspend + supplier_reactivate", actions)
	}
	foundGap := false
	for _, g := range out.Gaps {
		if strings.Contains(g, "تأیید/رد") {
			foundGap = true
		}
	}
	if !foundGap {
		t.Errorf("gaps = %v, want the approve/reject-history gap note", out.Gaps)
	}
}

func TestAdminSupplierDetailHandlerNotFoundAndMethod(t *testing.T) {
	_, h := detailTestSetup(t)
	ctx := detailTestCtx()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/suppliers/sup-missing", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.HandleAdminSupplierDetail(rec, req, "sup-missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id = %d, want 404", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/suppliers/sup-a", strings.NewReader(`{}`)).WithContext(ctx)
	rec = httptest.NewRecorder()
	h.HandleAdminSupplierDetail(rec, req, "sup-a")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST detail = %d, want 405", rec.Code)
	}
}

func TestAdminSupplierDetailTapinStrictWithoutToken(t *testing.T) {
	_, h := detailTestSetup(t)
	ctx := detailTestCtx()

	// sup-b has a shop id but NO token: strict connected must be false.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/suppliers/sup-b", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.HandleAdminSupplierDetail(rec, req, "sup-b")
	if rec.Code != http.StatusOK {
		t.Fatalf("detail sup-b = %d, body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Tapin struct {
			ShopID    string `json:"shopId"`
			Connected bool   `json:"connected"`
		} `json:"tapin"`
		Counts struct {
			Products int `json:"products"`
			Variants int `json:"variants"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Tapin.ShopID != "shop-2" || out.Tapin.Connected {
		t.Errorf("tapin sup-b = %+v, want shop-2 NOT connected (token missing)", out.Tapin)
	}
	if out.Counts.Products != 1 || out.Counts.Variants != 0 {
		t.Errorf("counts sup-b = %+v, want products 1 variants 0", out.Counts)
	}
}

func readMainGoForDetailDispatch(t *testing.T) (string, error) {
	t.Helper()
	b, err := os.ReadFile("main.go")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func suppliersBlockForDetailDispatch(t *testing.T, src string) string {
	t.Helper()
	marker := `mux.HandleFunc("/api/v1/admin/suppliers/",`
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatal("admin suppliers block not found in main.go")
	}
	rest := src[start+len(marker):]
	end := strings.Index(rest, "mux.HandleFunc(")
	if end < 0 {
		t.Fatal("admin suppliers block has no following mux.HandleFunc")
	}
	return rest[:end]
}

// TestAdminSupplierDetailDispatchDoesNotSwallowActions guards the
// in-closure dispatch in main.go at the HTTP-shape level: the
// GET-single branch must match only a bare segment, so the POST
// .../<id>/approve|reject|suspend|reactivate branches can never be
// swallowed by it. The branch is plain source (no mux pattern), so
// the check reads the actual guard out of main.go and evaluates it
// against the shapes that matter, and asserts it sits after the
// last POST branch in the block.
func TestAdminSupplierDetailDispatchDoesNotSwallowActions(t *testing.T) {
	src, err := readMainGoForDetailDispatch(t)
	if err != nil {
		t.Fatal(err)
	}
	block := suppliersBlockForDetailDispatch(t, src)
	guard := `r.Method == http.MethodGet && sub != "" && !strings.Contains(sub, "/")`
	if !strings.Contains(block, guard) {
		t.Fatalf("suppliers block lacks the GET-single guard %q", guard)
	}
	lastPost := strings.LastIndex(block, `r.Method == http.MethodPost`)
	detailAt := strings.Index(block, "HandleAdminSupplierDetail(w, r, sub)")
	if detailAt < 0 || detailAt < lastPost {
		t.Fatalf("GET-single branch must come after every POST branch in the suppliers block (detailAt=%d, lastPost=%d)", detailAt, lastPost)
	}
	// Guard semantics: bare id matches, any suffixed action path does not.
	matches := func(method, sub string) bool {
		return method == http.MethodGet && sub != "" && !strings.Contains(sub, "/")
	}
	cases := []struct {
		method, sub string
		want        bool
	}{
		{http.MethodGet, "sup-a", true},
		{http.MethodGet, "sup-a/approve", false},
		{http.MethodGet, "sup-a/reject", false},
		{http.MethodGet, "sup-a/suspend", false},
		{http.MethodGet, "sup-a/reactivate", false},
		{http.MethodPost, "sup-a", false},
		{http.MethodGet, "", false},
	}
	for _, c := range cases {
		if got := matches(c.method, c.sub); got != c.want {
			t.Errorf("detail guard(%s, %q) = %v, want %v", c.method, c.sub, got, c.want)
		}
	}
}
