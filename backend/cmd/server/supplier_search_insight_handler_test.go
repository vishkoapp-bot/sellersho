package main

// Supplier search-insight endpoint permission tests (D12). They run
// ONLY when TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local
// Postgres (never the live database); otherwise they skip. The
// endpoint stays behind RequireSupplierAccess with the default
// supplier.manage permission: the owner and a supplier.manage member
// get 200, a products.manage-only member gets 403, and the 501 stub
// is gone (the response always carries the three section keys).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

func TestSupplierSearchInsightPermissions(t *testing.T) {
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping DB integration test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()
	ddl := []string{
		`DROP TABLE IF EXISTS order_items, orders, search_queries, products, categories, supplier_members, suppliers, users CASCADE`,
		`CREATE TABLE users (id VARCHAR(50)  DEFAULT gen_random_uuid() PRIMARY KEY, is_active BOOLEAN NOT NULL DEFAULT TRUE,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			email VARCHAR(255),
			mobile VARCHAR(50) NOT NULL DEFAULT '',
			username VARCHAR(255),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			failed_login_attempts integer NOT NULL DEFAULT 0,
			full_name character varying(100),
			locked_until timestamp with time zone,
			password_hash text,
			role character varying(30) NOT NULL DEFAULT 'customer'::character varying,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_city character varying(50) NOT NULL DEFAULT 'تهران'::character varying)`,
		`CREATE TABLE suppliers (id VARCHAR(50) PRIMARY KEY, owner_user_id VARCHAR(50), approval_status VARCHAR(30) NOT NULL DEFAULT 'approved',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			name VARCHAR(200) NOT NULL DEFAULT '',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			warehouse_city VARCHAR(255) NOT NULL DEFAULT 'تهران',
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			approved_at timestamp with time zone,
			approved_by character varying(50),
			badge character varying(50),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			default_markup_type character varying(10),
			default_markup_value numeric(10,2),
			delivery_score integer DEFAULT 100,
			description text,
			logo_url text,
			rating numeric(3,2) DEFAULT 5.00,
			rejection_reason text,
			signed_contract_version integer NOT NULL DEFAULT 1,
			supports_direct_shipping boolean DEFAULT false,
			suspended_at timestamp with time zone,
			suspended_by text,
			suspension_reason text,
			tapin_shop_id text,
			tapin_token text,
			total_products integer DEFAULT 0,
			warehouse_city_confirmed boolean NOT NULL DEFAULT false,
			warehouse_city_source character varying(20))`,
		`CREATE TABLE supplier_members (supplier_id VARCHAR(50) NOT NULL, user_id VARCHAR(50) NOT NULL, role_key VARCHAR(50) NOT NULL DEFAULT '', permissions JSONB NOT NULL DEFAULT '[]', is_active BOOLEAN NOT NULL DEFAULT TRUE,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			id uuid NOT NULL DEFAULT gen_random_uuid(),
			invited_by uuid,
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE categories (id VARCHAR(50) PRIMARY KEY, title TEXT NOT NULL, slug VARCHAR(100) NOT NULL,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			depth smallint NOT NULL DEFAULT 0,
			gradient character varying(100),
			icon_name character varying(50),
			image_url text,
			is_active boolean NOT NULL DEFAULT true,
			parent_id character varying(50),
			sort_order integer NOT NULL DEFAULT 0)`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, slug VARCHAR(255) UNIQUE NOT NULL, category_slug VARCHAR(100), supplier_id VARCHAR(50), image_url TEXT, brand TEXT NOT NULL DEFAULT '', is_active BOOLEAN NOT NULL DEFAULT TRUE,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_status VARCHAR(255) NOT NULL DEFAULT 'unverified',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_version INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attribute_schema jsonb,
			barcode character varying(64),
			base_price_override bigint,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			description text,
			fulfillment_method character varying(20) DEFAULT 'warehouse'::character varying,
			height_mm integer,
			implicit_agreement boolean NOT NULL DEFAULT false,
			implicit_agreement_until timestamp with time zone,
			is_featured boolean DEFAULT false,
			is_original boolean NOT NULL DEFAULT true,
			is_special_deal boolean DEFAULT false,
			length_mm integer,
			markup_type character varying(10),
			markup_value numeric(10,2),
			measurement_revision_id text,
			measurement_source character varying(20),
			origin_supplier_warehouse_id text,
			original_price bigint,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			platform_margin bigint,
			preparation_days integer DEFAULT 1,
			product_type text NOT NULL DEFAULT 'legacy'::text,
			rating numeric(3,2) NOT NULL DEFAULT 0,
			reviews_count integer NOT NULL DEFAULT 0,
			schema_version integer NOT NULL DEFAULT 0,
			shipping_class text NOT NULL DEFAULT 'standard'::text,
			specs jsonb DEFAULT '{}'::jsonb,
			spn character varying(20),
			spn_number bigint,
			stock integer NOT NULL DEFAULT 0,
			supplier_base_cost bigint,
			supplier_warranty character varying(100),
			supply_option character varying(20) NOT NULL DEFAULT '1day'::character varying,
			tax_class text NOT NULL DEFAULT 'standard'::text,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying,
			warranty_property_id character varying(60),
			weight_grams integer,
			width_mm integer)`,
		`CREATE TABLE search_queries (id BIGSERIAL PRIMARY KEY, term_normalized TEXT NOT NULL, store_slug TEXT NOT NULL, result_count INTEGER NOT NULL, catalog_result_count INTEGER, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE orders (id VARCHAR(50) PRIMARY KEY, payment_status VARCHAR(20) NOT NULL DEFAULT 'pending', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			order_number VARCHAR(50) NOT NULL DEFAULT '',
			payable_amount BIGINT NOT NULL DEFAULT 0,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			customer_user_id UUID,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			box_size character varying(80),
			carrier character varying(100),
			confirmed_at timestamp with time zone,
			coupon_discount_amount bigint,
			customer_address text,
			customer_city character varying(50),
			customer_full_name character varying(100),
			customer_phone character varying(20),
			customer_postal_code character varying(20),
			customer_province character varying(50),
			discount_amount bigint NOT NULL DEFAULT 0,
			discount_code text,
			dispute_reason text,
			disputed_at timestamp with time zone,
			dropshipper_profit bigint NOT NULL DEFAULT 0,
			fulfillment_method character varying(20) DEFAULT 'warehouse'::character varying,
			gateway_amount bigint NOT NULL DEFAULT 0,
			idempotency_key text,
			inventory_captured boolean NOT NULL DEFAULT false,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			packed_at timestamp with time zone,
			payment_expires_at timestamp with time zone DEFAULT (now() + '00:15:00'::interval),
			payment_method character varying(20) NOT NULL DEFAULT 'cash'::character varying,
			platform_addon_amount bigint NOT NULL DEFAULT 0,
			platform_profit bigint NOT NULL DEFAULT 0,
			profit_released boolean DEFAULT false,
			profit_released_at timestamp with time zone,
			referral_commission bigint DEFAULT 0,
			referral_commission_released boolean DEFAULT false,
			referrer_seller_id character varying(50),
			return_window_ends_at timestamp with time zone,
			seller_discount_amount bigint NOT NULL DEFAULT 0,
			shipped_at timestamp with time zone,
			shipping_cost bigint NOT NULL DEFAULT 0,
			shipping_label text,
			shipping_quote_id text,
			status character varying(30) NOT NULL DEFAULT 'processing'::character varying,
			store_id character varying(50),
			store_name character varying(100),
			supplier_addon_amount bigint NOT NULL DEFAULT 0,
			total_amount bigint,
			tracking_code character varying(50),
			tracking_number character varying(100),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			wallet_amount bigint NOT NULL DEFAULT 0,
			weight_grams integer)`,
		`CREATE TABLE order_items (id VARCHAR(50)  DEFAULT gen_random_uuid() PRIMARY KEY, order_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, quantity INT NOT NULL DEFAULT 0, wholesale_cost BIGINT NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			discount_amount bigint NOT NULL DEFAULT 0,
			final_unit_price bigint,
			platform_fee bigint,
			pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
			product_title_snapshot text,
			seller_margin bigint,
			variant_id uuid,
			variant_name text,
			variant_spnc character varying(12))`,
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
