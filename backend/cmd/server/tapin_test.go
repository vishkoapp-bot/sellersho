package main

// Tapin integration tests (gaps 1-3 + owner quote directive).
//
// The upstream is a mocked Tapin API (httptest) whose fixtures are the
// REAL envelope bodies captured from the owner's shop (332 token not
// found, 338 no shop selected, 30129 Tipax service not activated) and
// the success shapes from Tapin's official Tipax guide PDF (check-price
// p.18 figures, register p.19). DB-gated like the sibling suites:
// TEAM04_TEST_DATABASE_URL points at a disposable Postgres.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"

	_ "github.com/lib/pq"
)

// ---------------------------------------------------------------------------
// Mock Tapin upstream
// ---------------------------------------------------------------------------

type tapinMock struct {
	mu       sync.Mutex
	mode     string // per-path override map below
	modes    map[string]string
	requests map[string][]map[string]any
	server   *httptest.Server
}

const (
	tapinBodyCheckPriceOK = `{"returns":{"status":20,"message":"موفق"},"entries":{"send_price":500000,"send_tax":118636,"total_send_price":618636,"total_product_price":4000000,"total_panel_price":330000,"total_receive_price":618636,"total_weight":1000,"total_discount":0,"courier_discount":0}}`
	tapinBodyRegisterOK   = `{"returns":{"status":20,"message":"موفق"},"entries":{"uuid":"3fa85f64-5717-4562-b3fc-2c963f66afa6","status":0,"contract_code":"ba219299518ir","contract_id":123456}}`
	tapinBodyOptionsOK    = `{"returns":{"status":20,"message":"موفق"},"entries":{}}`
	tapinBody332          = `{"returns":{"status":332,"message":"توکن مورد نظر یافت نشد."},"entries":null}`
	tapinBody338          = `{"returns":{"status":338,"message":"شما هیچ فروشگاه فعالی را انتخاب نکرده اید."},"entries":null}`
	tapinBody30129        = `{"returns":{"status":30129,"message":"فروشگاه شما دسترسی استفاده از این سرویس را ندارد. جهت فعال سازی با پشتیبانی تماس حاصل فرمایید."},"entries":null}`
)

func newTapinMock(t *testing.T) *tapinMock {
	t.Helper()
	m := &tapinMock{modes: map[string]string{}, requests: map[string][]map[string]any{}}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		m.mu.Lock()
		m.requests[r.URL.Path] = append(m.requests[r.URL.Path], parsed)
		mode := m.modes[r.URL.Path]
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case mode == "drop":
			// Simulate an unknown-outcome transport failure: kill the
			// connection without any response.
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, err := hj.Hijack()
				if err == nil {
					conn.Close()
				}
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		case mode == "332":
			fmt.Fprint(w, tapinBody332)
		case mode == "338":
			fmt.Fprint(w, tapinBody338)
		case mode == "30129":
			fmt.Fprint(w, tapinBody30129)
		case strings.HasSuffix(r.URL.Path, "/check-price/"):
			fmt.Fprint(w, tapinBodyCheckPriceOK)
		case strings.HasSuffix(r.URL.Path, "/register/"):
			fmt.Fprint(w, tapinBodyRegisterOK)
		case strings.HasSuffix(r.URL.Path, "/options/"):
			fmt.Fprint(w, tapinBodyOptionsOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(m.server.Close)
	old := tapinAPIBaseURL
	tapinAPIBaseURL = m.server.URL
	t.Cleanup(func() { tapinAPIBaseURL = old })
	resetTapinFareCacheForTest()
	t.Cleanup(resetTapinFareCacheForTest)
	return m
}

func resetTapinFareCacheForTest() {
	tapinFareCache.Lock()
	defer tapinFareCache.Unlock()
	tapinFareCache.entries = map[string]tapinFareCacheEntry{}
}

func (m *tapinMock) setMode(pathSuffix, mode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range []string{
		"/api/v4/tipax/public/user/order/check-price/",
		"/api/v4/tipax/public/user/order/register/",
		"/api/v4/tipax/public/user/order/options/",
	} {
		if strings.HasSuffix(p, pathSuffix) {
			m.modes[p] = mode
		}
	}
}

func (m *tapinMock) calls(pathSuffix string) []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []map[string]any
	for p, reqs := range m.requests {
		if strings.HasSuffix(p, pathSuffix) {
			out = append(out, reqs...)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// DB fixture
// ---------------------------------------------------------------------------

type tapinRepos struct {
	db           *sql.DB
	orderRepo    *repository.OrderRepository
	catalogRepo  *repository.CatalogRepository
	storeRepo    *repository.StoreRepository
	shipmentRepo *repository.SupplierShipmentRepository
	pricingRepo  *repository.PricingRepository
	supplierRepo *repository.SupplierRepository
	tapinRepo    *repository.TapinRepository
}

func tapinTestSetup(t *testing.T) *tapinRepos {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB tapin test")
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
		`DROP TABLE IF EXISTS supplier_shipment_audit_logs, supplier_shipments, order_item_origins, order_items, orders, supplier_shipping_methods, supplier_warehouses, tapin_boxes, tapin_geo_snapshots, suppliers, warehouses, store_products, store_product_overrides, store_niche_overrides, stores, product_images, product_variants, product_niches, products, platform_settings CASCADE`,
		`CREATE TABLE platform_settings (id INT  DEFAULT 1 PRIMARY KEY, return_window_days INT DEFAULT 7 , shipping_base_rate BIGINT DEFAULT 0 , shipping_per_kg BIGINT DEFAULT 0 , volumetric_divisor BIGINT DEFAULT 5000 , default_item_weight_grams BIGINT DEFAULT 500 , tapin_default_box_id TEXT DEFAULT '4'::text ,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			early_withdrawal_fee_pct NUMERIC,
			platform_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
			settlement_day INTEGER,
			supplier_contract_version INTEGER NOT NULL DEFAULT 2,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attribution_model character varying(20) NOT NULL DEFAULT 'last_touch'::character varying,
			base_fee_f bigint NOT NULL DEFAULT 100000,
			central_campaign_budget bigint NOT NULL DEFAULT 0,
			central_profit_pct double precision NOT NULL DEFAULT 10,
			central_store_markup_percent numeric(5,2) NOT NULL DEFAULT 10,
			cookie_days integer NOT NULL DEFAULT 30,
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
			seller_contract_text text NOT NULL DEFAULT ''::text,
			seller_contract_version integer NOT NULL DEFAULT 2,
			storage_fee_enabled boolean NOT NULL DEFAULT false,
			storage_fee_free_days integer NOT NULL DEFAULT 30,
			storage_fee_per_kg_day bigint NOT NULL DEFAULT 2000,
			storage_fee_per_m3_day bigint NOT NULL DEFAULT 8000,
			stuck_order_expiry_enabled boolean NOT NULL DEFAULT false,
			supplier_contract_text text NOT NULL DEFAULT ''::text,
			supplier_fee_overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
			supplier_return_postal_charge bigint,
			tapin_platform_shop_id text,
			tapin_platform_token text,
			tapin_tariff_url text,
			trial_catalog_quota integer NOT NULL DEFAULT 25,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			wallet_min_withdrawal bigint NOT NULL DEFAULT 0,
			wallet_spend_enabled boolean NOT NULL DEFAULT true,
			wallet_withdraw_enabled boolean NOT NULL DEFAULT true,
			warehouse_tariffs jsonb NOT NULL DEFAULT '{}'::jsonb)`,
		// Rates: base 100000 Rial + 20000 Rial/kg, divisor 5000,
		// fallback weight 500g. A 1000 g warehouse leg costs
		// (100000 + 20000 + 5) / 10 = 12000 Toman.
		`INSERT INTO platform_settings (id, return_window_days, shipping_base_rate, shipping_per_kg, volumetric_divisor, default_item_weight_grams, tapin_default_box_id) VALUES (1, 7, 100000, 20000, 5000, 500, '4')`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, slug VARCHAR(255) UNIQUE NOT NULL, category_slug VARCHAR(100), supplier_id VARCHAR(50), original_price BIGINT NOT NULL, supplier_base_cost BIGINT NOT NULL, platform_margin BIGINT NOT NULL DEFAULT 0, is_original BOOLEAN NOT NULL DEFAULT TRUE, stock INT NOT NULL DEFAULT 0, image_url TEXT, specs JSONB DEFAULT '{}'::jsonb, description TEXT, warehouse_name VARCHAR(100) DEFAULT 'انبار مرکزی تهران'::character varying , preparation_days INT DEFAULT 1, supplier_warranty VARCHAR(100), is_featured BOOLEAN DEFAULT FALSE, is_special_deal BOOLEAN DEFAULT FALSE, fulfillment_method VARCHAR(50) DEFAULT 'warehouse'::character varying , supply_option VARCHAR(50) DEFAULT '1day'::character varying , is_active BOOLEAN NOT NULL DEFAULT TRUE, brand TEXT NOT NULL DEFAULT '', spn_number BIGINT, length_mm INT, width_mm INT, height_mm INT, weight_grams INT, origin_supplier_warehouse_id TEXT, shipping_class TEXT NOT NULL DEFAULT 'standard', tax_class TEXT NOT NULL DEFAULT 'standard', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_status VARCHAR(255) NOT NULL DEFAULT 'unverified',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_version INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attribute_schema jsonb,
			barcode character varying(64),
			base_price_override bigint,
			implicit_agreement boolean NOT NULL DEFAULT false,
			implicit_agreement_until timestamp with time zone,
			markup_type character varying(10),
			markup_value numeric(10,2),
			measurement_revision_id text,
			measurement_source character varying(20),
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			product_type text NOT NULL DEFAULT 'legacy'::text,
			rating numeric(3,2) NOT NULL DEFAULT 0,
			reviews_count integer NOT NULL DEFAULT 0,
			schema_version integer NOT NULL DEFAULT 0,
			spn character varying(20),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warranty_property_id character varying(60))`,
		`CREATE TABLE product_niches (product_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, PRIMARY KEY (product_id, niche_id))`,
		`CREATE TABLE product_variants (id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, product_id VARCHAR(50) NOT NULL, spnc VARCHAR(12) NOT NULL UNIQUE, spnc_number INT, variant_name TEXT NOT NULL DEFAULT 'x', attributes JSONB NOT NULL DEFAULT '{}', is_default BOOLEAN NOT NULL DEFAULT FALSE, stock INT NOT NULL DEFAULT 0, supplier_base_cost BIGINT, weight_grams INT, length_mm INT, width_mm INT, height_mm INT, sale_price BIGINT, sale_from TIMESTAMPTZ, sale_to TIMESTAMPTZ, shipping_class TEXT, tax_class TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			combination_key text,
			inventory_mode text NOT NULL DEFAULT 'legacy'::text,
			is_active boolean NOT NULL DEFAULT true,
			max_cart_qty integer,
			measurement_revision_id text,
			measurement_source text,
			measurement_status text NOT NULL DEFAULT 'unverified'::text,
			measurement_version integer NOT NULL DEFAULT 0,
			min_cart_qty integer NOT NULL DEFAULT 1,
			selection jsonb NOT NULL DEFAULT '{}'::jsonb)`,
		`CREATE TABLE product_images (id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, product_id VARCHAR(50) NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE store_products (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, position INT NOT NULL DEFAULT 0, PRIMARY KEY (store_id, product_id))`,
		`CREATE TABLE stores (id VARCHAR(50) PRIMARY KEY, name VARCHAR(255) NOT NULL DEFAULT '', slug VARCHAR(255) NOT NULL DEFAULT '', owner_type VARCHAR(50) NOT NULL DEFAULT 'dropshipper', owner_name VARCHAR(255) NOT NULL DEFAULT '', owner_user_id VARCHAR(50), profit_margin_percent DOUBLE PRECISION NOT NULL DEFAULT 0, custom_store_margin_override DOUBLE PRECISION, is_margin_locked_by_admin BOOLEAN NOT NULL DEFAULT FALSE, active_plan_id VARCHAR(50) NOT NULL DEFAULT '', referral_code VARCHAR(100) NOT NULL DEFAULT '', referrer_seller_id VARCHAR(50), signed_contract_version INT NOT NULL DEFAULT 0, custom_domain VARCHAR(255) NOT NULL DEFAULT '', total_sales BIGINT NOT NULL DEFAULT 0, seller_deals_widget_config JSONB, template_id VARCHAR(100) NOT NULL DEFAULT '', template_config JSONB NOT NULL DEFAULT '{}'::jsonb, logo TEXT NOT NULL DEFAULT '', panel_config JSONB DEFAULT '{}'::jsonb , tagline TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '', status VARCHAR(50) NOT NULL DEFAULT 'active', active_categories TEXT[] NOT NULL DEFAULT '{}', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			addon_section_title character varying(200),
			contract_signature_code text,
			owner_phone text NOT NULL DEFAULT ''::text,
			plan_expires_at timestamp with time zone,
			rejection_reason text,
			related_section_title character varying(200))`,
		`CREATE TABLE store_product_overrides (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, override_value DOUBLE PRECISION NOT NULL DEFAULT 0, mode VARCHAR(20) NOT NULL DEFAULT 'percent')`,
		`CREATE TABLE store_niche_overrides (store_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, override_value DOUBLE PRECISION NOT NULL DEFAULT 0, mode VARCHAR(20) NOT NULL DEFAULT 'percent')`,
		`CREATE TABLE suppliers (id TEXT PRIMARY KEY, owner_user_id TEXT, name TEXT NOT NULL DEFAULT '', logo_url TEXT, rating DECIMAL(3,2) DEFAULT 0, total_products INT DEFAULT 0, delivery_score INT DEFAULT 100, warehouse_city TEXT DEFAULT '', description TEXT, badge TEXT DEFAULT '', supports_direct_shipping BOOLEAN DEFAULT FALSE, default_markup_type VARCHAR(20), default_markup_value DECIMAL(10,2), signed_contract_version INT DEFAULT 1, approval_status TEXT NOT NULL DEFAULT 'approved', approved_by TEXT, approved_at TIMESTAMPTZ, rejection_reason TEXT, tapin_shop_id TEXT, tapin_token TEXT, warehouse_city_confirmed BOOLEAN DEFAULT FALSE, warehouse_city_source VARCHAR(30), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			suspended_at timestamp with time zone,
			suspended_by text,
			suspension_reason text)`,
		`CREATE TABLE supplier_shipping_methods (owner_user_id TEXT NOT NULL, method TEXT NOT NULL, status TEXT  DEFAULT 'disabled'::text NOT NULL, requested_at TIMESTAMPTZ, decided_at TIMESTAMPTZ, decided_by TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE warehouses (id TEXT PRIMARY KEY, name TEXT UNIQUE, province_name TEXT, city_name TEXT, priority INT NOT NULL DEFAULT 100, is_active BOOLEAN NOT NULL DEFAULT TRUE,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			tapin_province_id INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			capacity_m3_total numeric(10,3),
			capacity_units_total integer,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			is_receiving_open boolean NOT NULL DEFAULT true,
			tapin_city_id integer,
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE supplier_warehouses (id TEXT PRIMARY KEY, supplier_id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', province_name TEXT, city_name TEXT, address TEXT, postal_code TEXT, phone TEXT, tapin_province_id INT NOT NULL DEFAULT 0, tapin_city_id INT NOT NULL DEFAULT 0, is_active BOOLEAN NOT NULL DEFAULT TRUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE tapin_geo_snapshots (kind TEXT PRIMARY KEY, payload JSONB NOT NULL, fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE tapin_boxes (id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', length_cm INT NOT NULL, width_cm INT NOT NULL, height_cm INT NOT NULL, max_weight_grams INT, is_active BOOLEAN NOT NULL DEFAULT TRUE, sort INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`INSERT INTO tapin_boxes (id, title, length_cm, width_cm, height_cm, sort) VALUES ('4','سایز ۴',30,20,20,0), ('3','سایز ۳',20,20,15,1)`,
		// Geo snapshot: Tehran province code 1 (cities Tehran 1, Rey 5),
		// Qom province code 26 (city Qom 170).
		`INSERT INTO tapin_geo_snapshots (kind, payload) VALUES ('tree', '[{"code":1,"title":"تهران","cities":[{"code":1,"title":"تهران"},{"code":5,"title":"ری"}]},{"code":26,"title":"قم","cities":[{"code":170,"title":"قم"}]}]'::jsonb)`,
		`CREATE TABLE orders (
			id VARCHAR(50) PRIMARY KEY, customer_user_id UUID, order_number VARCHAR(30), store_id TEXT, store_name TEXT,
			total_amount BIGINT NOT NULL DEFAULT 0, discount_amount BIGINT NOT NULL DEFAULT 0, seller_discount_amount BIGINT NOT NULL DEFAULT 0, discount_code TEXT, coupon_discount_amount BIGINT,
			payable_amount BIGINT NOT NULL DEFAULT 0, payment_method TEXT DEFAULT 'cash'::character varying , payment_status TEXT DEFAULT 'pending'::character varying ,
			customer_full_name TEXT, customer_phone TEXT, customer_province TEXT, customer_city TEXT,
			customer_address TEXT, customer_postal_code TEXT, status TEXT DEFAULT 'processing'::character varying , tracking_code VARCHAR(50),
			shipping_cost BIGINT NOT NULL DEFAULT 0, dropshipper_profit BIGINT NOT NULL DEFAULT 0,
			platform_profit BIGINT NOT NULL DEFAULT 0, referral_commission BIGINT NOT NULL DEFAULT 0,
			referral_commission_released BOOLEAN NOT NULL DEFAULT FALSE, referrer_seller_id TEXT,
			profit_released BOOLEAN NOT NULL DEFAULT FALSE, profit_released_at TIMESTAMPTZ,
			shipped_at TIMESTAMPTZ, confirmed_at TIMESTAMPTZ, disputed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			fulfillment_method TEXT DEFAULT 'warehouse'::character varying , wallet_amount BIGINT NOT NULL DEFAULT 0, gateway_amount BIGINT NOT NULL DEFAULT 0,
			platform_addon_amount BIGINT NOT NULL DEFAULT 0, supplier_addon_amount BIGINT NOT NULL DEFAULT 0,
			inventory_captured BOOLEAN NOT NULL DEFAULT FALSE, packaging_length_mm INT, packaging_width_mm INT,
			packaging_height_mm INT, dispute_reason TEXT, carrier TEXT, tracking_number TEXT,
			shipping_label TEXT, packed_at TIMESTAMPTZ, box_size TEXT, weight_grams INT,
			idempotency_key TEXT, return_window_ends_at TIMESTAMPTZ, shipping_quote_id TEXT
		,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			payment_expires_at timestamp with time zone DEFAULT (now() + '00:15:00'::interval))`,
		`CREATE TABLE order_items (
			id TEXT  DEFAULT gen_random_uuid() PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT DEFAULT 1 ,
			wholesale_cost BIGINT, platform_fee BIGINT, seller_margin BIGINT, final_unit_price BIGINT, discount_amount BIGINT NOT NULL DEFAULT 0,
			variant_id UUID, variant_name TEXT, variant_spnc TEXT
		,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
			product_title_snapshot text)`,
		`CREATE TABLE order_item_origins (id TEXT PRIMARY KEY, order_id TEXT, order_item_id TEXT, leg_index INT, origin_type TEXT, supplier_id TEXT, warehouse_id TEXT, supplier_warehouse_id TEXT, origin_label TEXT, quantity INT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE supplier_shipments (id TEXT PRIMARY KEY, order_id TEXT, supplier_id TEXT, shipping_address JSONB, customer_phone TEXT, customer_name TEXT, status TEXT DEFAULT 'pending'::character varying , tracking_number TEXT, carrier TEXT, supplier_notes TEXT, shipped_at TIMESTAMPTZ, delivered_at TIMESTAMPTZ, packaging_length_mm INT, packaging_width_mm INT, packaging_height_mm INT, tapin_service TEXT, tapin_box_id TEXT, tapin_registered_at TIMESTAMPTZ, tapin_last_status TEXT, tapin_register_attempted_at TIMESTAMPTZ, created_at TIMESTAMPTZ DEFAULT now() , updated_at TIMESTAMPTZ DEFAULT now() ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			send_price bigint)`,
		`CREATE TABLE supplier_shipment_audit_logs (id TEXT PRIMARY KEY, shipment_id TEXT, actor_user_id TEXT, action TEXT, payload JSONB, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), timestamp TIMESTAMPTZ DEFAULT now() )`,
		`INSERT INTO stores (id,name,slug,owner_type,panel_config,seller_deals_widget_config) VALUES ('store-central','مرکزی','central','central','{}'::jsonb,'null'::jsonb)`,
		// sup-tapin: Tapin-connected. sup-plain: not connected.
		`INSERT INTO suppliers (id, owner_user_id, approval_status, tapin_shop_id, tapin_token) VALUES ('sup-tapin','owner-tapin','approved','9b2f6d10-3f6d-4c1a-9c8a-2b7d11111111','test-token-tapin'), ('sup-plain','owner-plain','approved',NULL,NULL)`,
		`INSERT INTO supplier_shipping_methods (owner_user_id, method, status) VALUES ('owner-tapin','direct','enabled'), ('owner-tapin','warehouse','enabled'), ('owner-plain','direct','enabled'), ('owner-plain','warehouse','enabled')`,
		`INSERT INTO warehouses (id, name, province_name, city_name) VALUES ('wh-central','انبار مرکزی تهران','تهران','تهران')`,
		// sup-tapin's default warehouse sits in Tehran (province 1, city 1).
		`INSERT INTO supplier_warehouses (id, supplier_id, name, province_name, city_name, tapin_province_id, tapin_city_id) VALUES ('swh-tapin','sup-tapin','انبار تأمین‌کننده','تهران','تهران',1,1)`,
		// p-tapin: unweighed (500 g fallback), direct, price 2,000,000 Toman.
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number) VALUES ('p-tapin','کالای تاپینی','tapin-good','digital','sup-tapin',2000000,1500000,0,TRUE,10,'img','desc','انبار تأمین‌کننده',1,'گارانتی',FALSE,FALSE,'supplier_direct','1day',TRUE,'برند',50000101)`,
		// p-heavy: 4000 g weighed, direct, Tapin supplier (overweight tests).
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,weight_grams) VALUES ('p-heavy','کالای سنگین','heavy-good','digital','sup-tapin',3000000,2500000,0,TRUE,10,'img','desc','انبار تأمین‌کننده',1,'گارانتی',FALSE,FALSE,'supplier_direct','1day',TRUE,'برند',50000102,4000)`,
		// p-plain: direct product of the UNCONNECTED supplier.
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,weight_grams) VALUES ('p-plain','کالای معمولی','plain-good','digital','sup-plain',1000000,800000,0,TRUE,10,'img','desc','انبار تأمین‌کننده',1,'گارانتی',FALSE,FALSE,'supplier_direct','1day',TRUE,'برند',50000103,1000)`,
		// v-tapin: p-tapin's variant, 2500 g, 150x150x100 mm (P0).
		`INSERT INTO product_variants (id, product_id, spnc, spnc_number, variant_name, is_default, stock, weight_grams, length_mm, width_mm, height_mm) VALUES ('33333333-3333-3333-3333-333333333333','p-tapin','TAPINV1',50000201,'پیش‌فرض',TRUE,10,2500,150,150,100)`,
		// p-central: warehouse product, 1000 g.
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,weight_grams) VALUES ('p-central','کالای انباری','central-good','digital','sup-plain',500000,400000,0,TRUE,10,'img','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'برند',50000104,1000)`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL/seed failed: %v\n%s", err, q)
		}
	}
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name) VALUES('11111111-1111-1111-1111-111111111111','09120000000','checkout test') ON CONFLICT(id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	prepareShippingInventoryFixture(t, db)
	if _, err := db.Exec(`INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name,tapin_province_id,tapin_city_id) VALUES('swh-plain','sup-plain','انبار تأمین‌کننده','تهران','تهران',1,1)`); err != nil {
		t.Fatal(err)
	}
	seedShippingInventoryFixtureLot(t, db, "p-tapin", "sup-tapin", "", "swh-tapin", 10)
	seedShippingInventoryFixtureLot(t, db, "p-heavy", "sup-tapin", "", "swh-tapin", 10)
	seedShippingInventoryFixtureLot(t, db, "p-plain", "sup-plain", "", "swh-plain", 10)
	seedShippingInventoryFixtureLot(t, db, "p-central", "sup-plain", "wh-central", "", 10)
	return &tapinRepos{
		db:           db,
		orderRepo:    repository.NewOrderRepository(db),
		catalogRepo:  repository.NewCatalogRepository(db),
		storeRepo:    repository.NewStoreRepository(db),
		shipmentRepo: repository.NewSupplierShipmentRepository(db),
		pricingRepo:  repository.NewPricingRepository(db),
		supplierRepo: repository.NewSupplierRepository(db),
		tapinRepo:    repository.NewTapinRepository(db),
	}
}

const tapinQuoteDestQom = `{"province":"قم","city":"قم","address":"خیابان تست","postalCode":"3711111111"}`

func tapinQuoteRequest(t *testing.T, rp *tapinRepos, itemsJSON, destinationJSON string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"items":[` + itemsJSON + `],"destination":` + destinationJSON + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shipping/quote", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handleShippingQuote(rec, req, req.Context(), rp.catalogRepo, rp.supplierRepo, rp.db)
	return rec
}

type tapinQuoteResponse struct {
	QuoteID string `json:"quoteId"`
	Total   int64  `json:"totalShipping"`
	Legs    []struct {
		OriginID string `json:"originId"`
		Source   string `json:"source"`
		Fare     int64  `json:"fare"`
	} `json:"legs"`
	Error string `json:"error"`
}

// A connected supplier's leg is priced from the Tapin inquiry (exact
// passthrough of total_send_price 618636 Rial = 61864 Toman), and a
// warehouse leg in the same cart keeps the formula fare.
func TestTapinQuoteConnectedLegPricedFromInquiryMixedCart(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)

	rec := tapinQuoteRequest(t, rp, `{"productId":"p-tapin","quantity":2},{"productId":"p-central","quantity":1}`, tapinQuoteDestQom)
	if rec.Code != http.StatusOK {
		t.Fatalf("quote status = %d, body %s", rec.Code, rec.Body.String())
	}
	var q tapinQuoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatalf("decode quote: %v", err)
	}
	if len(q.Legs) != 2 {
		t.Fatalf("legs = %d, want 2 (%s)", len(q.Legs), rec.Body.String())
	}
	var tapinFare, whFare int64 = -1, -1
	for _, leg := range q.Legs {
		if leg.Source == "tapin" {
			tapinFare = leg.Fare
		} else {
			whFare = leg.Fare
		}
	}
	if tapinFare < 0 {
		t.Fatalf("no tapin-priced leg in %s", rec.Body.String())
	}
	if tapinFare != 61864 {
		t.Fatalf("tapin leg fare = %d, want 61864 (618636 Rial passthrough)", tapinFare)
	}
	if whFare != 12000 {
		t.Fatalf("warehouse leg fare = %d, want formula fare 12000", whFare)
	}
	if q.Total != 61864+12000 {
		t.Fatalf("total = %d, want %d", q.Total, 61864+12000)
	}
	// The inquiry payload follows the captured contract: the supplier's
	// own shop id, destination ids from the geo snapshot, inter-city
	// service (Tehran -> Qom), declared value in Rial, 500 g fallback
	// weight per unweighed item, no sub-1kg padding at exactly 1000 g.
	calls := mock.calls("/check-price/")
	if len(calls) != 1 {
		t.Fatalf("check-price calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c["shop_id"] != "9b2f6d10-3f6d-4c1a-9c8a-2b7d11111111" {
		t.Fatalf("shop_id = %v", c["shop_id"])
	}
	if c["receiver_province_id"] != float64(26) || c["receiver_city_id"] != float64(170) {
		t.Fatalf("receiver geo = %v/%v, want 26/170", c["receiver_province_id"], c["receiver_city_id"])
	}
	if c["service_type"] != float64(2) {
		t.Fatalf("service_type = %v, want 2 (inter-city)", c["service_type"])
	}
	if c["package_weight"] != float64(0) {
		t.Fatalf("package_weight = %v, want 0 (goods exactly 1000 g)", c["package_weight"])
	}
	prods, _ := c["products"].([]any)
	if len(prods) != 1 {
		t.Fatalf("products = %v", c["products"])
	}
	p0, _ := prods[0].(map[string]any)
	if p0["amount_per_count"] != float64(20000000) {
		t.Fatalf("amount_per_count = %v, want 20000000 Rial (2,000,000 Toman)", p0["amount_per_count"])
	}
	if p0["weight_per_count"] != float64(500) {
		t.Fatalf("weight_per_count = %v, want 500 (fallback)", p0["weight_per_count"])
	}
	if p0["count"] != float64(2) {
		t.Fatalf("count = %v, want 2", p0["count"])
	}
}

// P0 (spec §7.4 tests 16 + 22): a variant-weighted Tapin leg feeds
// the inquiry the VARIANT weight (2500 g, not the product's 500 g
// fallback) and the leg's max dims pick the box — 150x150x100 mm
// (15x15x10 cm) fits the smaller box 3 (20x20x15), while the old
// 0,0,0 call always picked the default box 4 (30x20x20).
func TestTapinQuoteVariantWeightAndDims(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)

	rec := tapinQuoteRequest(t, rp, `{"productId":"p-tapin","variantId":"33333333-3333-3333-3333-333333333333","quantity":1}`, tapinQuoteDestQom)
	if rec.Code != http.StatusOK {
		t.Fatalf("quote status = %d, body %s", rec.Code, rec.Body.String())
	}
	calls := mock.calls("/check-price/")
	if len(calls) != 1 {
		t.Fatalf("check-price calls = %d, want 1", len(calls))
	}
	c := calls[0]
	prods, _ := c["products"].([]any)
	if len(prods) != 1 {
		t.Fatalf("products = %v", c["products"])
	}
	p0, _ := prods[0].(map[string]any)
	if p0["weight_per_count"] != float64(2500) {
		t.Fatalf("weight_per_count = %v, want 2500 (variant weight)", p0["weight_per_count"])
	}
	if c["length"] != float64(20) || c["width"] != float64(20) || c["height"] != float64(15) {
		t.Fatalf("box dims = %v/%v/%v, want 20/20/15 (box 3, picked from leg dims)", c["length"], c["width"], c["height"])
	}
}

// Intra-city (origin warehouse city == destination city) uses service
// type 7, and a sub-1kg parcel is padded via package_weight.
func TestTapinQuoteIntraCityAndSubKgPadding(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)

	rec := tapinQuoteRequest(t, rp, `{"productId":"p-tapin","quantity":1}`, `{"province":"تهران","city":"تهران","address":"x","postalCode":"1111111111"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("quote status = %d, body %s", rec.Code, rec.Body.String())
	}
	calls := mock.calls("/check-price/")
	if len(calls) != 1 {
		t.Fatalf("check-price calls = %d, want 1", len(calls))
	}
	if calls[0]["service_type"] != float64(7) {
		t.Fatalf("service_type = %v, want 7 (intra-city)", calls[0]["service_type"])
	}
	if calls[0]["package_weight"] != float64(500) {
		t.Fatalf("package_weight = %v, want 500 (500 g goods padded to 1 kg)", calls[0]["package_weight"])
	}
}

// Fail-closed: an envelope failure on a connected leg fails the whole
// quote with the upstream message — the formula is never substituted.
func TestTapinQuoteEnvelopeFailureFailsClosed(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)
	mock.setMode("/check-price/", "332")

	rec := tapinQuoteRequest(t, rp, `{"productId":"p-tapin","quantity":1}`, tapinQuoteDestQom)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("quote status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "توکن مورد نظر یافت نشد.") {
		t.Fatalf("body must carry the upstream message verbatim: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "quoteId") {
		t.Fatalf("a failed quote must not return a quote: %s", rec.Body.String())
	}
}

// F4 (critic fix): 30129 — credentials valid, Tipax service not yet
// activated for the shop — is the ONE envelope outcome that does NOT
// fail the quote closed: Tapin cannot price the leg at all while the
// activation ticket is pending, so the leg keeps its formula fare
// and the supplier's checkout survives. The resulting quote is the
// pure-formula quote (same id/total), and order creation accepts it
// (the order-time reprice applies the identical fallback).
func TestTapinQuoteServiceNotActivatedFallsBackToFormula(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)
	mock.setMode("/check-price/", "30129")
	ctx := context.Background()

	rec := tapinQuoteRequest(t, rp, `{"productId":"p-tapin","quantity":1}`, tapinQuoteDestQom)
	if rec.Code != http.StatusOK {
		t.Fatalf("quote status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var q tapinQuoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatalf("decode quote: %v", err)
	}
	if len(q.Legs) != 1 || q.Legs[0].Source != "weight_formula" {
		t.Fatalf("legs = %+v, want the single leg on the formula source", q.Legs)
	}
	// The inquiry WAS attempted (the fallback is a verdict, not a skip).
	if calls := mock.calls("/check-price/"); len(calls) != 1 {
		t.Fatalf("check-price calls = %d, want 1", len(calls))
	}
	lines, err := resolveQuoteLines(ctx, rp.catalogRepo, rp.supplierRepo, []shippingQuoteItemReq{{ProductID: "p-tapin", Quantity: 1}})
	if err != nil {
		t.Fatalf("resolve lines: %v", err)
	}
	lines, err = resolveQuoteOrigins(ctx, rp.db, lines)
	if err != nil {
		t.Fatal("resolve exact formula origins", err)
	}
	rates, err := loadShippingRates(ctx, rp.db)
	if err != nil {
		t.Fatalf("load rates: %v", err)
	}
	want := computeShippingQuote(lines, rates, quoteDestinationKey(shippingQuoteDestination{Province: "قم", City: "قم", Address: "خیابان تست", PostalCode: "3711111111"}))
	if q.QuoteID != want.QuoteID || q.Total != want.TotalShipping || q.Legs[0].Fare != want.Legs[0].Fare {
		t.Fatalf("quote = (%s, %d, leg %d), want pure-formula (%s, %d, leg %d)", q.QuoteID, q.Total, q.Legs[0].Fare, want.QuoteID, want.TotalShipping, want.Legs[0].Fare)
	}

	// Parity: an order placed with this quote is accepted — the
	// order-time reprice hits the same 30129 and falls back the
	// same way, so no shipping_quote_changed.
	body := `{"storeId":"central","paymentMethod":"cash","customerInfo":{"fullName":"مشتری تست","phone":"09120000000","province":"قم","city":"قم","address":"خیابان تست","postalCode":"3711111111"},"items":[{"productId":"p-tapin","quantity":1}],"shippingQuoteId":"` + q.QuoteID + `"}`
	orec := tapinCreateOrder(t, rp, body)
	orderID := gap13OrderID(t, orec)
	var shipping int64
	if err := rp.db.QueryRow(`SELECT shipping_cost FROM orders WHERE id = $1`, orderID).Scan(&shipping); err != nil {
		t.Fatalf("read order shipping: %v", err)
	}
	if shipping != q.Total {
		t.Fatalf("order shipping_cost = %d, want quoted total %d", shipping, q.Total)
	}
}

// An unconnected supplier's leg keeps the pure formula quote — same
// quote id as computeShippingQuote alone, and no upstream call at all.
func TestTapinQuoteUnconnectedLegKeepsFormula(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)
	ctx := context.Background()

	rec := tapinQuoteRequest(t, rp, `{"productId":"p-plain","quantity":1}`, tapinQuoteDestQom)
	if rec.Code != http.StatusOK {
		t.Fatalf("quote status = %d, body %s", rec.Code, rec.Body.String())
	}
	var q tapinQuoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatalf("decode quote: %v", err)
	}
	lines, err := resolveQuoteLines(ctx, rp.catalogRepo, rp.supplierRepo, []shippingQuoteItemReq{{ProductID: "p-plain", Quantity: 1}})
	if err != nil {
		t.Fatalf("resolve lines: %v", err)
	}
	lines, err = resolveQuoteOrigins(ctx, rp.db, lines)
	if err != nil {
		t.Fatal("resolve exact formula origins", err)
	}
	rates, err := loadShippingRates(ctx, rp.db)
	if err != nil {
		t.Fatalf("load rates: %v", err)
	}
	want := computeShippingQuote(lines, rates, quoteDestinationKey(shippingQuoteDestination{Province: "قم", City: "قم", Address: "خیابان تست", PostalCode: "3711111111"}))
	if q.QuoteID != want.QuoteID || q.Total != want.TotalShipping {
		t.Fatalf("quote = (%s, %d), want pure-formula (%s, %d)", q.QuoteID, q.Total, want.QuoteID, want.TotalShipping)
	}
	if calls := mock.calls("/check-price/"); len(calls) != 0 {
		t.Fatalf("unconnected leg must not call Tapin, got %d calls", len(calls))
	}
}

// Order creation with a Tapin-priced quote recomputes identically:
// no shipping_quote_changed, and the charged shipping equals the quote.
func TestTapinOrderCreationAcceptsTapinPricedQuote(t *testing.T) {
	rp := tapinTestSetup(t)
	newTapinMock(t)

	rec := tapinQuoteRequest(t, rp, `{"productId":"p-tapin","quantity":2},{"productId":"p-central","quantity":1}`, tapinQuoteDestQom)
	if rec.Code != http.StatusOK {
		t.Fatalf("quote status = %d, body %s", rec.Code, rec.Body.String())
	}
	var q tapinQuoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatalf("decode quote: %v", err)
	}

	body := `{"storeId":"central","paymentMethod":"cash","customerInfo":{"fullName":"مشتری تست","phone":"09120000000","province":"قم","city":"قم","address":"خیابان تست","postalCode":"3711111111"},"items":[{"productId":"p-tapin","quantity":2},{"productId":"p-central","quantity":1}],"shippingQuoteId":"` + q.QuoteID + `"}`
	orec := tapinCreateOrder(t, rp, body)
	orderID := gap13OrderID(t, orec)
	var shipping int64
	if err := rp.db.QueryRow(`SELECT shipping_cost FROM orders WHERE id = $1`, orderID).Scan(&shipping); err != nil {
		t.Fatalf("read order shipping: %v", err)
	}
	if shipping != q.Total {
		t.Fatalf("order shipping_cost = %d, want quoted total %d", shipping, q.Total)
	}

	// Control: a bogus quote id is still rejected.
	bad := tapinCreateOrder(t, rp, strings.Replace(body, q.QuoteID, "deadbeefdeadbeef", 1))
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "shipping_quote_changed") {
		t.Fatalf("bogus quote id: status %d body %s, want shipping_quote_changed", bad.Code, bad.Body.String())
	}
}

func tapinCreateOrder(t *testing.T, rp *tapinRepos, body string) *httptest.ResponseRecorder {
	t.Helper()
	claims := &auth.UserClaims{UserID: gap13UserID, Mobile: "09120000000", Role: "customer", Capabilities: []string{"customer"}}
	ctx := context.WithValue(context.Background(), userClaimsKey, claims)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handleCreateOrder(rec, req, ctx, rp.orderRepo, rp.catalogRepo, rp.storeRepo, rp.shipmentRepo, rp.pricingRepo, rp.supplierRepo, rp.db)
	return rec
}

// ---------------------------------------------------------------------------
// Registration (submitShippingViaTapin)
// ---------------------------------------------------------------------------

func tapinSeedOrder(t *testing.T, rp *tapinRepos, orderID, itemProductID string, qty int, unitPrice int64) {
	t.Helper()
	stmts := []string{
		fmt.Sprintf(`INSERT INTO orders (id, customer_user_id, order_number, store_id, store_name, total_amount, payable_amount, payment_method, payment_status, customer_full_name, customer_phone, customer_province, customer_city, customer_address, customer_postal_code, status, fulfillment_method, tracking_code, carrier, shipping_label, box_size, dispute_reason, idempotency_key) VALUES ('%s', '11111111-1111-1111-1111-111111111111', 'ORD-%s', 'store-central', 'مرکزی', %d, %d, 'cash', 'paid', 'علی رضایی', '09120000000', 'قم', 'قم', 'خیابان تست ۱', '3711111111', 'processing', 'supplier_direct', '', '', '', '', '', '')`, orderID, orderID, unitPrice*int64(qty), unitPrice*int64(qty)),
		fmt.Sprintf(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ('oi-%s', '%s', '%s', %d, 100, 0, 0, %d)`, orderID, orderID, itemProductID, qty, unitPrice),
		fmt.Sprintf(`INSERT INTO supplier_shipments (id, order_id, supplier_id, shipping_address, customer_phone, customer_name, status, packaging_length_mm, packaging_width_mm, packaging_height_mm) VALUES ('ship-%s', '%s', 'sup-tapin', '{}'::jsonb, '09120000000', 'علی رضایی', 'processing', 300, 200, 200)`, orderID, orderID),
	}
	for _, q := range stmts {
		if _, err := rp.db.Exec(q); err != nil {
			t.Fatalf("seed order: %v\n%s", err, q)
		}
	}
	if _, err := rp.db.Exec(`INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,supplier_warehouse_id,origin_label,quantity) VALUES($1,$2,$3,0,'supplier','sup-tapin','swh-tapin','انبار تأمین‌کننده',$4)`, "origin-"+orderID, orderID, "oi-"+orderID, qty); err != nil {
		t.Fatal(err)
	}
}

func tapinShipmentRow(t *testing.T, rp *tapinRepos, orderID string) (status string, carrier, tracking, service, boxID sql.NullString, registered, attempted sql.NullTime) {
	t.Helper()
	err := rp.db.QueryRow(`SELECT status, carrier, tracking_number, tapin_service, tapin_box_id, tapin_registered_at, tapin_register_attempted_at FROM supplier_shipments WHERE order_id = $1`, orderID).
		Scan(&status, &carrier, &tracking, &service, &boxID, &registered, &attempted)
	if err != nil {
		t.Fatalf("read shipment: %v", err)
	}
	return
}

func TestTapinRegistrationSuccess(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)
	tapinSeedOrder(t, rp, "ord-t1", "p-tapin", 2, 2000000)

	out := submitShippingViaTapin(context.Background(), "sup-tapin", "ord-t1", "tipax", "", "actor-1", rp.shipmentRepo, rp.orderRepo, rp.tapinRepo)
	if out.Status != http.StatusOK {
		t.Fatalf("outcome status = %d, body %v", out.Status, out.Body)
	}
	tap, _ := out.Body["tapin"].(map[string]interface{})
	if tap == nil || tap["trackingNumber"] != "ba219299518ir" {
		t.Fatalf("tapin payload = %v, want tracking ba219299518ir", out.Body["tapin"])
	}
	status, carrier, tracking, service, boxID, registered, attempted := tapinShipmentRow(t, rp, "ord-t1")
	if status != "shipped" || carrier.String != "tapin-tipax" || tracking.String != "ba219299518ir" {
		t.Fatalf("shipment = (%s, %s, %s), want shipped/tapin-tipax/ba219299518ir", status, carrier.String, tracking.String)
	}
	if service.String != "tipax" || boxID.String != "4" {
		t.Fatalf("tapin service/box = %s/%s, want tipax/4 (30x20x20 cm parcel)", service.String, boxID.String)
	}
	if !registered.Valid || attempted.Valid {
		t.Fatalf("registered_at set=%v, attempted_at set=%v; want registered set, attempted cleared", registered.Valid, attempted.Valid)
	}
	// Captured-contract payload checks.
	calls := mock.calls("/register/")
	if len(calls) != 1 {
		t.Fatalf("register calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c["register_type"] != float64(2) {
		t.Fatalf("register_type = %v, want 2 (ready-to-send)", c["register_type"])
	}
	if c["manual_id"] != "ORD-ord-t1" {
		t.Fatalf("manual_id = %v, want the order number", c["manual_id"])
	}
	if c["receiver_province_id"] != float64(26) || c["receiver_city_id"] != float64(170) {
		t.Fatalf("receiver geo = %v/%v, want 26/170", c["receiver_province_id"], c["receiver_city_id"])
	}
	if c["receiver_first_name"] != "علی" || c["receiver_last_name"] != "رضایی" {
		t.Fatalf("receiver name = %v %v", c["receiver_first_name"], c["receiver_last_name"])
	}
	if c["package_weight"] != float64(0) {
		t.Fatalf("package_weight = %v, want 0 (goods 1000 g)", c["package_weight"])
	}
	prods, _ := c["products"].([]any)
	if len(prods) != 1 {
		t.Fatalf("products = %v", c["products"])
	}
	p0, _ := prods[0].(map[string]any)
	if p0["amount_per_count"] != float64(20000000) || p0["weight_per_count"] != float64(500) || p0["count"] != float64(2) {
		t.Fatalf("product line = %v, want amount 20000000 Rial / weight 500 / count 2", p0)
	}
	// Idempotency: a second submit is a 409.
	out2 := submitShippingViaTapin(context.Background(), "sup-tapin", "ord-t1", "tipax", "", "actor-1", rp.shipmentRepo, rp.orderRepo, rp.tapinRepo)
	if out2.Status != http.StatusConflict {
		t.Fatalf("second submit status = %d, want 409 (%v)", out2.Status, out2.Body)
	}
}

func TestTapinRegistrationSubKgPadding(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)
	tapinSeedOrder(t, rp, "ord-t2", "p-tapin", 1, 2000000)

	out := submitShippingViaTapin(context.Background(), "sup-tapin", "ord-t2", "tipax", "", "actor-1", rp.shipmentRepo, rp.orderRepo, rp.tapinRepo)
	if out.Status != http.StatusOK {
		t.Fatalf("outcome status = %d, body %v", out.Status, out.Body)
	}
	calls := mock.calls("/register/")
	if len(calls) != 1 || calls[0]["package_weight"] != float64(500) {
		t.Fatalf("package_weight = %v, want 500 (500 g goods padded to 1 kg)", calls)
	}
}

func TestTapinRegistrationEnvelopeFailureChangesNothing(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)
	mock.setMode("/register/", "332")
	tapinSeedOrder(t, rp, "ord-t3", "p-tapin", 1, 2000000)

	out := submitShippingViaTapin(context.Background(), "sup-tapin", "ord-t3", "tipax", "", "actor-1", rp.shipmentRepo, rp.orderRepo, rp.tapinRepo)
	if out.Status != http.StatusBadGateway {
		t.Fatalf("outcome status = %d, want 502 (%v)", out.Status, out.Body)
	}
	if msg, _ := out.Body["error"].(string); !strings.Contains(msg, "توکن مورد نظر یافت نشد.") {
		t.Fatalf("error = %q, want upstream message verbatim", msg)
	}
	status, _, tracking, _, _, _, attempted := tapinShipmentRow(t, rp, "ord-t3")
	if status != "processing" || tracking.Valid || attempted.Valid {
		t.Fatalf("shipment mutated on definite rejection: status=%s tracking=%v attempted=%v", status, tracking, attempted)
	}
}

func TestTapinRegistrationUnknownOutcomeLocksRetry(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)
	mock.setMode("/register/", "drop")
	tapinSeedOrder(t, rp, "ord-t4", "p-tapin", 1, 2000000)

	out := submitShippingViaTapin(context.Background(), "sup-tapin", "ord-t4", "tipax", "", "actor-1", rp.shipmentRepo, rp.orderRepo, rp.tapinRepo)
	if out.Status != http.StatusBadGateway {
		t.Fatalf("outcome status = %d, want 502 (%v)", out.Status, out.Body)
	}
	if out.Body["outcome"] != "unknown" || out.Body["retryable"] != false {
		t.Fatal("uncertain registration must not instruct another send", out.Body)
	}
	_, _, _, _, _, _, attempted := tapinShipmentRow(t, rp, "ord-t4")
	if !attempted.Valid {
		t.Fatalf("unknown outcome must keep tapin_register_attempted_at stamped")
	}
	// Even with a healthy upstream now, the retry is refused until an
	// admin checks the Tapin panel (double-registration guard).
	mock.setMode("/register/", "")
	out2 := submitShippingViaTapin(context.Background(), "sup-tapin", "ord-t4", "tipax", "", "actor-1", rp.shipmentRepo, rp.orderRepo, rp.tapinRepo)
	if out2.Status != http.StatusConflict {
		t.Fatalf("retry status = %d, want 409 (%v)", out2.Status, out2.Body)
	}
	if calls := mock.calls("/register/"); len(calls) != 1 {
		t.Fatalf("register calls = %d, want 1 (no second upstream attempt)", len(calls))
	}
}

func TestTapinRegistrationOverweightRejectedBeforeUpstream(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)
	tapinSeedOrder(t, rp, "ord-t5", "p-heavy", 2, 3000000) // 2 x 4000 g = 8000 g > 6000 g

	out := submitShippingViaTapin(context.Background(), "sup-tapin", "ord-t5", "tipax", "", "actor-1", rp.shipmentRepo, rp.orderRepo, rp.tapinRepo)
	if out.Status != http.StatusUnprocessableEntity {
		t.Fatalf("outcome status = %d, want 422 (%v)", out.Status, out.Body)
	}
	if calls := mock.calls("/register/"); len(calls) != 0 {
		t.Fatalf("overweight parcel must never reach Tapin, got %d calls", len(calls))
	}
}

func TestTapinRegistrationPostStaysStub(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)
	tapinSeedOrder(t, rp, "ord-t6", "p-tapin", 1, 2000000)

	out := submitShippingViaTapin(context.Background(), "sup-tapin", "ord-t6", "post", "", "actor-1", rp.shipmentRepo, rp.orderRepo, rp.tapinRepo)
	if out.Status != http.StatusNotImplemented {
		t.Fatalf("outcome status = %d, want 501 (Post contract not captured) (%v)", out.Status, out.Body)
	}
	if calls := mock.calls("/register/"); len(calls) != 0 {
		t.Fatalf("post must not call upstream, got %d calls", len(calls))
	}
}

func TestTapinRegistrationRequiresConnection(t *testing.T) {
	rp := tapinTestSetup(t)
	newTapinMock(t)
	// ord-t7 belongs to the unconnected supplier.
	if _, err := rp.db.Exec(`INSERT INTO orders (id, customer_user_id, order_number, store_id, store_name, payment_status, customer_full_name, customer_phone, customer_province, customer_city, customer_address, customer_postal_code, status, fulfillment_method) VALUES ('ord-t7', '11111111-1111-1111-1111-111111111111', 'ORD-ord-t7', 'store-central', 'مرکزی', 'paid', 'علی رضایی', '09120000000', 'قم', 'قم', 'x', '3711111111', 'processing', 'supplier_direct')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := rp.db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, final_unit_price) VALUES ('oi-ord-t7', 'ord-t7', 'p-plain', 1, 1000000)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := rp.db.Exec(`INSERT INTO supplier_shipments (id, order_id, supplier_id, status) VALUES ('ship-ord-t7', 'ord-t7', 'sup-plain', 'processing')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	out := submitShippingViaTapin(context.Background(), "sup-plain", "ord-t7", "tipax", "", "actor-1", rp.shipmentRepo, rp.orderRepo, rp.tapinRepo)
	if out.Status != http.StatusBadRequest {
		t.Fatalf("outcome status = %d, want 400 not-configured (%v)", out.Status, out.Body)
	}
}

// ---------------------------------------------------------------------------
// Connection test (HandleTapinTest) — envelope taxonomy
// ---------------------------------------------------------------------------

func tapinTestHandler(t *testing.T, rp *tapinRepos) (*SupplierHandlers, context.Context) {
	t.Helper()
	h := NewSupplierHandlers(rp.supplierRepo, nil, false).WithTapinRepository(rp.tapinRepo)
	ctx := context.WithValue(context.Background(), supplierIDKey, "sup-tapin")
	return h, ctx
}

func TestTapinConnectionTestTaxonomy(t *testing.T) {
	rp := tapinTestSetup(t)
	mock := newTapinMock(t)
	h, ctx := tapinTestHandler(t, rp)

	call := func() (int, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/tapin/test", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		h.HandleTapinTest(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	// Envelope success: connected and the service answers.
	code, out := call()
	if code != http.StatusOK || out["ok"] != true || out["serviceActive"] != true {
		t.Fatalf("success case = %d %v", code, out)
	}

	// 30129: credentials valid, Tipax service inactive — its own state.
	mock.setMode("/options/", "30129")
	code, out = call()
	if code != http.StatusOK || out["ok"] != true || out["serviceActive"] != false {
		t.Fatalf("30129 case = %d %v, want ok:true serviceActive:false", code, out)
	}
	if msg, _ := out["message"].(string); !strings.Contains(msg, "دسترسی استفاده از این سرویس را ندارد") {
		t.Fatalf("30129 message = %q, want upstream verbatim", msg)
	}

	// 332: invalid token.
	mock.setMode("/options/", "332")
	code, out = call()
	if code != http.StatusOK || out["ok"] != false {
		t.Fatalf("332 case = %d %v, want ok:false", code, out)
	}
	if msg, _ := out["message"].(string); !strings.Contains(msg, "توکن مورد نظر یافت نشد.") {
		t.Fatalf("332 message = %q, want upstream verbatim", msg)
	}
}

// ---------------------------------------------------------------------------
// Admin box catalogue roundtrip
// ---------------------------------------------------------------------------

func TestTapinAdminBoxesRoundtrip(t *testing.T) {
	rp := tapinTestSetup(t)

	get := func() (int, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/tapin-boxes", nil)
		rec := httptest.NewRecorder()
		handleT01AdminSettingsTapinBoxes(rec, req, rp.db)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, out := get()
	if code != http.StatusOK || out["defaultBoxId"] != "4" {
		t.Fatalf("GET = %d %v", code, out)
	}
	boxes, _ := out["boxes"].([]any)
	if len(boxes) != 2 {
		t.Fatalf("seeded boxes = %v, want 2", out["boxes"])
	}

	putBody := `{"boxes":[{"id":"4","title":"سایز ۴","lengthCm":30,"widthCm":20,"heightCm":20,"maxWeightGrams":null,"isActive":true,"sort":0},{"id":"3","title":"سایز ۳","lengthCm":20,"widthCm":20,"heightCm":15,"maxWeightGrams":null,"isActive":false,"sort":1},{"id":"5","title":"سایز ۵","lengthCm":40,"widthCm":30,"heightCm":30,"maxWeightGrams":6000,"isActive":true,"sort":2}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/tapin-boxes", strings.NewReader(putBody))
	rec := httptest.NewRecorder()
	handleT01AdminSettingsTapinBoxes(rec, req, rp.db)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", rec.Code, rec.Body.String())
	}
	code, out = get()
	boxes, _ = out["boxes"].([]any)
	if code != http.StatusOK || len(boxes) != 3 {
		t.Fatalf("GET after PUT = %d %v", code, out)
	}
	for _, b := range boxes {
		bm, _ := b.(map[string]any)
		if bm["id"] == "3" && bm["isActive"] != false {
			t.Fatalf("box 3 must be deactivated by the PUT: %v", bm)
		}
	}
	// Supplier-facing list shows only active boxes and the default id.
	sreq := httptest.NewRequest(http.MethodGet, "/api/v1/supplier/tapin/boxes", nil)
	srec := httptest.NewRecorder()
	h := NewSupplierHandlers(rp.supplierRepo, nil, false).WithTapinRepository(rp.tapinRepo)
	ctx := context.WithValue(context.Background(), supplierIDKey, "sup-tapin")
	h.HandleTapinBoxes(srec, sreq.WithContext(ctx))
	var sout map[string]any
	_ = json.Unmarshal(srec.Body.Bytes(), &sout)
	if srec.Code != http.StatusOK || sout["defaultBoxId"] != "4" {
		t.Fatalf("supplier boxes = %d %v", srec.Code, sout)
	}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

func TestTapinPureHelpers(t *testing.T) {
	if got := rialToToman(618636); got != 61864 {
		t.Fatalf("rialToToman(618636) = %d, want 61864", got)
	}
	if got := rialToToman(618635); got != 61864 {
		t.Fatalf("rialToToman(618635) = %d, want 61864 (half-up)", got)
	}
	if got := rialToToman(618634); got != 61863 {
		t.Fatalf("rialToToman(618634) = %d, want 61863", got)
	}
	first, last := splitReceiverName("علی رضایی")
	if first != "علی" || last != "رضایی" {
		t.Fatalf("splitReceiverName = %q/%q", first, last)
	}
	first, last = splitReceiverName("علی")
	if first != "علی" || last != "-" {
		t.Fatalf("single-name split = %q/%q, want علی/-", first, last)
	}
	raw := tapinQuoteErrorBody(&tapinQuoteError{Message: "پیام تست"})
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("error body not JSON: %v", err)
	}
	if body["error"] != "پیام تست" || body["tapinError"] != true {
		t.Fatalf("error body = %v", body)
	}
}
