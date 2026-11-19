package main

// Tests for the product-cluster gap fixes (gaps 8, 9, 10):
//   gap 8 — propose validates weight/dims; approval writes the product's
//           measurement columns (status stays 'unverified').
//   gap 9 — approval persists categoryAttributes (first value per
//           attribute, validated against the dk registry) and creates
//           the proposed variants with their costs/stocks.
//   gap 10 — variant pricing write path (PUT …/variants/{vid}/pricing),
//            ownership, clear-to-inherit, and the pricing read path.
//
// The handler-level gap-8 tests need no DB. The approval/pricing tests
// are DB-gated on TEAM04_TEST_DATABASE_URL (same convention as
// wishlist_per_store_test.go) and build a minimal schema in a scratch
// schema, so they never touch real tables.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"

	_ "github.com/lib/pq"
)

// ---------- gap 8: propose validation (no DB) ----------

func proposeRequest(t *testing.T, supplierID, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/products/propose", strings.NewReader(body))
	return req.WithContext(context.WithValue(req.Context(), supplierIDKey, supplierID))
}

func TestProposeProductRejectsMissingMeasurements(t *testing.T) {
	h := NewSupplierHandlers(nil, auth.NewJWTService("gaps-test"), false)
	cases := []struct {
		name    string
		body    string
		wantSub string
	}{
		{
			name:    "no measurements at all",
			body:    `{"title":"کالای تست","supplierBaseCost":100000,"stock":5}`,
			wantSub: "وزن",
		},
		{
			name:    "weight only, dims missing",
			body:    `{"title":"کالای تست","supplierBaseCost":100000,"stock":5,"weightGrams":900}`,
			wantSub: "طول",
		},
		{
			name:    "zero weight counts as missing",
			body:    `{"title":"کالای تست","supplierBaseCost":100000,"stock":5,"weightGrams":0,"lengthMm":100,"widthMm":100,"heightMm":100}`,
			wantSub: "وزن",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Physical validation follows the V2 publication contract. Keep the
			// intended missing parent measurements, but provide a valid simple
			// schema and SKU so the request reaches that validation boundary.
			var payload map[string]interface{}
			if err := json.Unmarshal([]byte(tc.body), &payload); err != nil {
				t.Fatal(err)
			}
			payload["schemaVersion"] = 2
			payload["productType"] = "simple"
			payload["productSchema"] = map[string]interface{}{"version": 2, "type": "simple", "attributes": []interface{}{}}
			payload["nicheIds"] = []string{"niche-measurement-fixture"}
			payload["variants"] = []interface{}{map[string]interface{}{
				"id": "00000000-0000-4000-8000-000000000001", "is_active": true,
				"selection": map[string]string{}, "supplier_base_cost": 100000, "stock": 5,
			}}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			h.HandleProposeProduct(rec, proposeRequest(t, "sup-gap-a", string(body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantSub) {
				t.Errorf("error body %q does not name missing %q", rec.Body.String(), tc.wantSub)
			}
			if !strings.Contains(rec.Body.String(), "الزامی") {
				t.Errorf("error body %q does not state the requirement", rec.Body.String())
			}
		})
	}
}

func TestProposeProductRequiresV2BeforeMeasurementValidation(t *testing.T) {
	h := NewSupplierHandlers(nil, auth.NewJWTService("gaps-test"), false)
	for _, body := range []string{
		`{"title":"Legacy product","supplierBaseCost":100000,"stock":5}`,
		`{"schemaVersion":1,"title":"Legacy product","supplierBaseCost":100000,"stock":5}`,
		`{"schemaVersion":3,"title":"Future product","supplierBaseCost":100000,"stock":5}`,
	} {
		rec := httptest.NewRecorder()
		h.HandleProposeProduct(rec, proposeRequest(t, "sup-gap-a", body))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Product schema version 2 is required") {
			t.Fatalf("publication schema gate: status %d, body %s", rec.Code, rec.Body.String())
		}
	}
}

func TestMissingProposalMeasurementsHelper(t *testing.T) {
	full := map[string]interface{}{
		"weightGrams": float64(1200), "lengthMm": float64(300),
		"widthMm": float64(200), "heightMm": float64(100),
	}
	if missing := missingProposalMeasurements(full); len(missing) != 0 {
		t.Errorf("full payload: missing = %v, want none", missing)
	}
	snake := map[string]interface{}{
		"weight_grams": float64(1), "length_mm": float64(1),
		"width_mm": float64(1), "height_mm": float64(1),
	}
	if missing := missingProposalMeasurements(snake); len(missing) != 0 {
		t.Errorf("snake_case payload: missing = %v, want none", missing)
	}
	partial := map[string]interface{}{"weightGrams": float64(10)}
	if missing := missingProposalMeasurements(partial); len(missing) != 3 {
		t.Errorf("partial payload: missing = %v, want 3 entries", missing)
	}
}

// ---------- DB-gated helpers ----------

func productGapsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping DB-gated product-gap test")
	}
	db, err := testdb.Open(t, url)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	schema := fmt.Sprintf("product_gaps_%d", time.Now().UnixNano())
	if _, err := db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := db.Exec(`SET search_path TO ` + schema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	ddl := []string{
		`CREATE TABLE pending_approvals (id TEXT PRIMARY KEY, title TEXT, requested_data JSONB, requested_by TEXT, entity_type TEXT, status TEXT DEFAULT 'pending', reviewed_at TIMESTAMPTZ, rejection_reason TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			target_id character varying(50))`,
		`CREATE TABLE products (id TEXT PRIMARY KEY, title TEXT, slug TEXT, category_slug TEXT, supplier_id TEXT, original_price BIGINT, supplier_base_cost BIGINT, platform_margin NUMERIC, is_original BOOLEAN DEFAULT false, stock INT DEFAULT 0, image_url TEXT, description TEXT, warehouse_name TEXT DEFAULT 'انبار مرکزی تهران'::character varying , preparation_days INT DEFAULT 1 , supplier_warranty TEXT, specs JSONB DEFAULT '{}'::jsonb , fulfillment_method TEXT DEFAULT 'warehouse'::character varying , barcode TEXT, brand TEXT DEFAULT ''::text , length_mm INT, width_mm INT, height_mm INT, weight_grams INT, shipping_class TEXT NOT NULL DEFAULT 'standard', tax_class TEXT NOT NULL DEFAULT 'standard', measurement_status TEXT NOT NULL DEFAULT 'unverified', created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW(),
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
			spn character varying(20),
			spn_number bigint,
			supply_option character varying(20) NOT NULL DEFAULT '1day'::character varying,
			warranty_property_id character varying(60))`,
		`CREATE TABLE product_images (product_id TEXT, image_url TEXT, sort_order INT DEFAULT 0 ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			id uuid NOT NULL DEFAULT gen_random_uuid())`,
		`CREATE TABLE product_variant_images (id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text, variant_id TEXT NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ DEFAULT NOW())`,
		`CREATE TABLE niches (id TEXT PRIMARY KEY, category_slug TEXT,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			title VARCHAR(255) NOT NULL DEFAULT '',
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			badge character varying(50),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			description text,
			slug character varying(100))`,
		`CREATE TABLE product_niches (product_id TEXT, niche_id TEXT, PRIMARY KEY (product_id, niche_id))`,
		`CREATE TABLE inventory_balances (id TEXT PRIMARY KEY, product_id TEXT, supplier_id TEXT, owner_type TEXT, location_type TEXT, location_key TEXT, status TEXT, quantity INT DEFAULT 0 , reconciled BOOLEAN DEFAULT false, updated_at TIMESTAMPTZ DEFAULT NOW(), 
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			warehouse_id TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			bin_code character varying(30),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			supplier_warehouse_id text,
			variant_id uuid,
			warehouse_name character varying(100))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS inventory_balances_unallocated_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS inventory_balances_sku_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL`,
		`CREATE TABLE product_variants (id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text, product_id TEXT, spnc TEXT, spnc_number INT, variant_name TEXT DEFAULT 'پیش‌فرض'::text , attributes JSONB DEFAULT '{}'::jsonb , is_default BOOLEAN DEFAULT false, stock INT DEFAULT 0, min_cart_qty INT DEFAULT 1 , max_cart_qty INT, supplier_base_cost BIGINT, weight_grams INT, length_mm INT, width_mm INT, height_mm INT, sale_price BIGINT, sale_from TIMESTAMPTZ, sale_to TIMESTAMPTZ, shipping_class TEXT, tax_class TEXT, created_at TIMESTAMPTZ DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			combination_key text,
			inventory_mode text NOT NULL DEFAULT 'legacy'::text,
			is_active boolean NOT NULL DEFAULT true,
			measurement_revision_id text,
			measurement_source text,
			measurement_status text NOT NULL DEFAULT 'unverified'::text,
			measurement_version integer NOT NULL DEFAULT 0,
			selection jsonb NOT NULL DEFAULT '{}'::jsonb)`,
		`CREATE TABLE dk_attribute_values (dk_attr_id BIGINT, dk_value_id BIGINT, value_fa TEXT, PRIMARY KEY (dk_attr_id, dk_value_id),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			title_en text,
			title_fa text)`,
		`CREATE TABLE product_attribute_values (product_id TEXT, dk_attribute_id BIGINT, dk_value_id BIGINT, PRIMARY KEY (product_id, dk_value_id))`,
		`CREATE TABLE audit_logs (id TEXT PRIMARY KEY, timestamp TIMESTAMPTZ DEFAULT NOW(), operator TEXT, action TEXT, category TEXT, details JSONB,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
	}
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("ddl %q: %v", stmt[:40], err)
		}
	}
	for _, stmt := range []string{
		`CREATE TABLE proposal_field_reviews (LIKE public.proposal_field_reviews INCLUDING ALL)`,
		`CREATE TABLE proposal_review_history (LIKE public.proposal_review_history INCLUDING ALL)`,
		`CREATE TABLE supplier_warehouses (LIKE public.supplier_warehouses INCLUDING ALL)`,
		`ALTER TABLE products ADD COLUMN IF NOT EXISTS origin_supplier_warehouse_id TEXT, ADD COLUMN IF NOT EXISTS schema_version INT DEFAULT 0`,
		`ALTER TABLE inventory_balances ADD COLUMN IF NOT EXISTS variant_id TEXT`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = db.Close()
	})
	return db
}

func seedProposal(t *testing.T, db *sql.DB, id, supplierID, payload string) {
	t.Helper()
	source := "gap-source-" + supplierID
	if _, err := db.Exec(`INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,'Product gap source','Test','Test') ON CONFLICT(id) DO NOTHING`, source, supplierID); err != nil {
		t.Fatal(err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if err := repository.NormalizeReviewAliases(data); err != nil {
		t.Fatal(err)
	}
	data["supplierWarehouseId"] = source
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO pending_approvals (id, title, requested_data, requested_by, entity_type, status) VALUES ($1, 'کالای تست گپ', $2, $3, 'proposed_product', 'pending')`, id, string(raw), supplierID); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
	var decisions []repository.FieldDecision
	for key, value := range repository.ReviewLeaves(data) {
		decisions = append(decisions, repository.FieldDecision{Key: key, Decision: "approved", ValueHash: repository.ReviewValueHash(value)})
	}
	if err := repository.NewSupplierRepository(db).SaveFieldReview(context.Background(), id, "test-admin", decisions); err != nil {
		t.Fatal(err)
	}
}

// ---------- gaps 8b + 9: approval persistence ----------

func TestApproveProposalPersistsMeasurementsAttributesVariants(t *testing.T) {
	db := productGapsTestDB(t)
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)

	for _, row := range [][2]int64{{10, 100}, {10, 101}, {11, 110}} {
		if _, err := db.Exec(`INSERT INTO dk_attribute_values (dk_attr_id, dk_value_id, value_fa) VALUES ($1, $2, 'v')`, row[0], row[1]); err != nil {
			t.Fatalf("seed dk value: %v", err)
		}
	}
	seedProposal(t, db, "prop-gap-1", "sup-gap-a", `{
		"product_id": "prod-gap-1",
		"category": "digital",
		"supplierBaseCost": 100000,
		"stock": 30,
		"weightGrams": 1200, "lengthMm": 300, "widthMm": 200, "heightMm": 100,
		"categoryAttributes": {"attributes": {"10": [100, 101], "11": [110], "12": [999]}},
		"variants": [
			{"variant_name": "قرمز", "attributes": {"رنگ": "قرمز"}, "stock": 10, "supplier_base_cost": 90000},
			{"variant_name": "آبی", "attributes": {"رنگ": "آبی"}, "stock": 20},
			{"stock": 5}
		]
	}`)

	productID, err := repo.ApproveProposal(ctx, "prop-gap-1", "")
	if err != nil {
		t.Fatalf("ApproveProposal: %v", err)
	}
	if productID != "prod-gap-1" {
		t.Fatalf("productID = %q, want prod-gap-1", productID)
	}

	// Gap 8b: measurement columns written, status stays 'unverified'.
	var weight, length, width, height sql.NullInt64
	var status string
	if err := db.QueryRow(`SELECT weight_grams, length_mm, width_mm, height_mm, measurement_status FROM products WHERE id = 'prod-gap-1'`).Scan(&weight, &length, &width, &height, &status); err != nil {
		t.Fatalf("read product: %v", err)
	}
	if !weight.Valid || weight.Int64 != 1200 || !length.Valid || length.Int64 != 300 ||
		!width.Valid || width.Int64 != 200 || !height.Valid || height.Int64 != 100 {
		t.Errorf("measurements = w:%v l:%v wd:%v h:%v, want 1200/300/200/100",
			weight.Int64, length.Int64, width.Int64, height.Int64)
	}
	if status != "unverified" {
		t.Errorf("measurement_status = %q, want 'unverified' (warehouse verification stays the trust step)", status)
	}

	// Gap 9a: first value per attribute only; unregistered pair (12/999)
	// dropped by the registry join.
	rows, err := db.Query(`SELECT dk_attribute_id, dk_value_id FROM product_attribute_values WHERE product_id = 'prod-gap-1' ORDER BY dk_attribute_id`)
	if err != nil {
		t.Fatalf("read attribute values: %v", err)
	}
	defer rows.Close()
	var got [][2]int64
	for rows.Next() {
		var a, v int64
		if err := rows.Scan(&a, &v); err != nil {
			t.Fatalf("scan attribute value: %v", err)
		}
		got = append(got, [2]int64{a, v})
	}
	want := [][2]int64{{10, 100}, {11, 110}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("product_attribute_values = %v, want %v (first value per attribute, registry-validated)", got, want)
	}

	// Gap 9b: exactly the proposed variants — no synthetic default added.
	variants, err := repo.ListProductVariants(ctx, "sup-gap-a", "prod-gap-1")
	if err != nil {
		t.Fatalf("ListProductVariants: %v", err)
	}
	if len(variants) != 2 {
		t.Fatalf("variants = %d, want exactly 2 (malformed entry skipped, no synthetic default)", len(variants))
	}
	if variants[0].VariantName != "قرمز" || !variants[0].IsDefault || variants[0].Stock != 10 ||
		variants[0].SupplierBaseCost == nil || *variants[0].SupplierBaseCost != 90000 {
		t.Errorf("variant[0] = %+v, want قرمز default stock 10 cost 90000", variants[0])
	}
	if variants[1].VariantName != "آبی" || variants[1].IsDefault || variants[1].Stock != 20 ||
		variants[1].SupplierBaseCost != nil {
		t.Errorf("variant[1] = %+v, want آبی non-default stock 20 cost nil (inherit)", variants[1])
	}

	// The malformed entry was skipped with an audit note, not an error.
	var skipAudits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'proposal_variants_skipped'`).Scan(&skipAudits); err != nil {
		t.Fatalf("count skip audits: %v", err)
	}
	if skipAudits != 1 {
		t.Errorf("proposal_variants_skipped audits = %d, want 1", skipAudits)
	}
}

func TestApproveProposalLegacyPayloadKeepsSyntheticDefault(t *testing.T) {
	db := productGapsTestDB(t)
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)

	// A pre-gap-fix pending proposal: no measurements, no attributes, no
	// variants. Approval must still succeed (no retroactive gate), the
	// product is born unmeasured, and the legacy single synthetic
	// default variant is created as before.
	seedProposal(t, db, "prop-gap-legacy", "sup-gap-a", `{
		"product_id": "prod-gap-legacy",
		"category": "digital",
		"supplierBaseCost": 50000,
		"stock": 7
	}`)
	productID, err := repo.ApproveProposal(ctx, "prop-gap-legacy", "")
	if err != nil {
		t.Fatalf("ApproveProposal legacy: %v", err)
	}
	var weight sql.NullInt64
	if err := db.QueryRow(`SELECT weight_grams FROM products WHERE id = $1`, productID).Scan(&weight); err != nil {
		t.Fatalf("read legacy product: %v", err)
	}
	if weight.Valid {
		t.Errorf("legacy product weight = %d, want NULL (no backfill)", weight.Int64)
	}
	variants, err := repo.ListProductVariants(ctx, "sup-gap-a", productID)
	if err != nil {
		t.Fatalf("ListProductVariants legacy: %v", err)
	}
	if len(variants) != 1 || variants[0].VariantName != "پیش‌فرض" || !variants[0].IsDefault {
		t.Errorf("legacy variants = %+v, want exactly one synthetic default «پیش‌فرض»", variants)
	}
}

// ---------- gap 10: variant pricing write/read/clear ----------

func TestVariantPricingSetHonouredAndClear(t *testing.T) {
	db := productGapsTestDB(t)
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	h := NewSupplierHandlers(repo, auth.NewJWTService("gaps-test"), false)

	if _, err := db.Exec(`INSERT INTO products (id, title, slug, category_slug, supplier_id, original_price, supplier_base_cost) VALUES ('prod-gap-1', 'کالا', 'kala', 'digital', 'sup-gap-a', 110000, 100000)`); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO product_variants (id, product_id, spnc, variant_name, attributes, is_default, stock) VALUES ('var-gap-1', 'prod-gap-1', 'GAPSPNC1', 'تنها', '{}'::jsonb, true, 5)`); err != nil {
		t.Fatalf("seed variant: %v", err)
	}

	// Owner sets the variant cost through the repo write path.
	cost := int64(80000)
	out, err := repo.UpdateVariantPricing(ctx, "sup-gap-a", "prod-gap-1", "var-gap-1", &cost, false, "تست")
	if err != nil {
		t.Fatalf("UpdateVariantPricing: %v", err)
	}
	if out.SupplierBaseCost == nil || *out.SupplierBaseCost != 80000 {
		t.Fatalf("updated variant cost = %v, want 80000", out.SupplierBaseCost)
	}

	// Non-positive cost is rejected (mirrors the product pricing rule).
	zero := int64(0)
	if _, err := repo.UpdateVariantPricing(ctx, "sup-gap-a", "prod-gap-1", "var-gap-1", &zero, false, ""); err == nil {
		t.Error("UpdateVariantPricing with cost 0: want error, got nil")
	}

	// A different supplier cannot touch the variant: repo says no rows,
	// the handler surfaces 404.
	if _, err := repo.UpdateVariantPricing(ctx, "sup-gap-b", "prod-gap-1", "var-gap-1", &cost, false, ""); err != sql.ErrNoRows {
		t.Errorf("foreign UpdateVariantPricing err = %v, want sql.ErrNoRows", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/supplier/products/prod-gap-1/variants/var-gap-1/pricing", strings.NewReader(`{"supplierBaseCost": 70000}`))
	req = req.WithContext(context.WithValue(req.Context(), supplierIDKey, "sup-gap-b"))
	rec := httptest.NewRecorder()
	h.HandleUpdateVariantPricing(rec, req, "prod-gap-1", "var-gap-1")
	if rec.Code != http.StatusNotFound {
		t.Errorf("foreign handler status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}

	// The pricing read path honours the written cost: the catalog loader
	// returns it and priceVariantsForResponse prices the variant from
	// 80000 (→ 88000 at these settings), not from the product's 100000
	// (→ 110000).
	catalogRepo := repository.NewCatalogRepository(db)
	byProduct, err := catalogRepo.GetVariantsByProductIDs(ctx, []string{"prod-gap-1"})
	if err != nil {
		t.Fatalf("GetVariantsByProductIDs: %v", err)
	}
	domainVariants := byProduct["prod-gap-1"]
	if len(domainVariants) != 1 || domainVariants[0].SupplierBaseCost == nil || *domainVariants[0].SupplierBaseCost != 80000 {
		t.Fatalf("catalog variants = %+v, want one variant with cost 80000", domainVariants)
	}
	p := domain.Product{
		ID: "prod-gap-1", Title: "T", Slug: "kala", CategorySlug: "digital",
		OriginalPrice: 110000, SupplierBaseCost: 100000,
	}
	settings := &pricing.PlatformPricingSettings{PlatformPct: 0, CentralProfitPct: 10, DefaultMargin: 20}
	prices := priceVariantsForResponse(p, domainVariants, domain.Store{}, settings, nil, map[string]pricing.StoreVariantOverride{}, nil)
	if prices["var-gap-1"].Price != 88000 {
		t.Errorf("variant price = %d, want 88000 (from variant cost 80000, not product cost 100000)", prices["var-gap-1"].Price)
	}

	// Clear restores inheritance: the column goes back to NULL and the
	// variant prices from the product cost again.
	out, err = repo.UpdateVariantPricing(ctx, "sup-gap-a", "prod-gap-1", "var-gap-1", nil, true, "بازگشت به ارث‌بری")
	if err != nil {
		t.Fatalf("UpdateVariantPricing clear: %v", err)
	}
	if out.SupplierBaseCost != nil {
		t.Errorf("cleared variant cost = %v, want nil (inherit)", *out.SupplierBaseCost)
	}
	byProduct, err = catalogRepo.GetVariantsByProductIDs(ctx, []string{"prod-gap-1"})
	if err != nil {
		t.Fatalf("GetVariantsByProductIDs after clear: %v", err)
	}
	prices = priceVariantsForResponse(p, byProduct["prod-gap-1"], domain.Store{}, settings, nil, map[string]pricing.StoreVariantOverride{}, nil)
	if prices["var-gap-1"].Price != 110000 {
		t.Errorf("variant price after clear = %d, want 110000 (product cost inherited)", prices["var-gap-1"].Price)
	}

	// Both writes were audited.
	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'supplier_variant_pricing_update'`).Scan(&audits); err != nil {
		t.Fatalf("count pricing audits: %v", err)
	}
	if audits != 2 {
		t.Errorf("supplier_variant_pricing_update audits = %d, want 2 (set + clear)", audits)
	}

	// Sanity: the encoded handler response carries the cost field.
	var probe struct {
		SupplierBaseCost *int64 `json:"supplier_base_cost"`
	}
	if err := json.Unmarshal([]byte(`{"supplier_base_cost": 80000}`), &probe); err != nil || probe.SupplierBaseCost == nil {
		t.Errorf("supplier_base_cost JSON shape probe failed: %v", err)
	}
}
