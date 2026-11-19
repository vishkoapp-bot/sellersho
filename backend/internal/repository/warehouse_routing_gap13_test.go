package repository

// Gap 13 warehouse-queue tests: the queue reads the PERSISTED
// per-item origin (order_item_origins, written once at order
// creation by the single routing resolver) instead of re-deriving
// routing from the supplier_shipping_methods toggles. Skipped
// unless GAP13_TEST_DATABASE_URL points at a DISPOSABLE Postgres.

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/google/uuid"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/testdb"

	_ "github.com/lib/pq"
)

func gap13QueueSetup(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GAP13_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GAP13_TEST_DATABASE_URL not set; skipping disposable-DB gap13 queue test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	base := db
	schema := "gap13_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := base.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); base.Close() })
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema+",public")
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " search_path=" + schema + ",public"
	}
	db, err = testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ddl := []string{
		`CREATE TABLE product_variants(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,inventory_mode TEXT DEFAULT 'legacy'::text ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
			combination_key text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			height_mm integer,
			is_active boolean NOT NULL DEFAULT true,
			is_default boolean NOT NULL DEFAULT false,
			length_mm integer,
			max_cart_qty integer,
			measurement_revision_id text,
			measurement_source text,
			measurement_status text NOT NULL DEFAULT 'unverified'::text,
			measurement_version integer NOT NULL DEFAULT 0,
			min_cart_qty integer NOT NULL DEFAULT 1,
			product_id character varying(50),
			sale_from timestamp with time zone,
			sale_price bigint,
			sale_to timestamp with time zone,
			selection jsonb NOT NULL DEFAULT '{}'::jsonb,
			shipping_class text,
			spnc character varying(12),
			spnc_number integer,
			stock integer NOT NULL DEFAULT 0,
			supplier_base_cost bigint,
			tax_class text,
			variant_name text NOT NULL DEFAULT 'پیش‌فرض'::text,
			weight_grams integer,
			width_mm integer)`,
		`CREATE TABLE order_inventory_captures(order_item_id TEXT PRIMARY KEY,allocations JSONB DEFAULT '[]'::jsonb ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			inventory_mode text,
			order_id character varying(50),
			product_id character varying(50),
			quantity integer,
			restored_quantity integer NOT NULL DEFAULT 0,
			variant_id uuid,
			variant_quantity integer NOT NULL DEFAULT 0)`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title TEXT, supplier_id TEXT, fulfillment_method TEXT DEFAULT 'warehouse'::character varying ,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_status VARCHAR(255) NOT NULL DEFAULT 'unverified',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_version INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attribute_schema jsonb,
			barcode character varying(64),
			base_price_override bigint,
			brand text NOT NULL DEFAULT ''::text,
			category_slug character varying(100),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			description text,
			height_mm integer,
			image_url text,
			implicit_agreement boolean NOT NULL DEFAULT false,
			implicit_agreement_until timestamp with time zone,
			is_active boolean NOT NULL DEFAULT true,
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
			slug character varying(255),
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
		`CREATE TABLE orders (id VARCHAR(50) PRIMARY KEY, order_number VARCHAR(30), store_name TEXT, customer_full_name TEXT, total_amount BIGINT NOT NULL DEFAULT 0, status TEXT DEFAULT 'processing'::character varying , fulfillment_method TEXT DEFAULT 'warehouse'::character varying , created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), discount_amount BIGINT NOT NULL DEFAULT 0, shipping_cost BIGINT NOT NULL DEFAULT 0, payable_amount BIGINT NOT NULL DEFAULT 0, payment_status TEXT DEFAULT 'pending'::character varying , customer_phone TEXT, customer_province TEXT, customer_city TEXT, customer_address TEXT, customer_postal_code TEXT, tracking_code VARCHAR(50),
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			customer_user_id UUID,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			box_size character varying(80),
			carrier character varying(100),
			confirmed_at timestamp with time zone,
			coupon_discount_amount bigint,
			discount_code text,
			dispute_reason text,
			disputed_at timestamp with time zone,
			dropshipper_profit bigint NOT NULL DEFAULT 0,
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
			shipping_label text,
			shipping_quote_id text,
			store_id character varying(50),
			supplier_addon_amount bigint NOT NULL DEFAULT 0,
			tracking_number character varying(100),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			wallet_amount bigint NOT NULL DEFAULT 0,
			weight_grams integer)`,
		`CREATE TABLE order_items (id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT DEFAULT 1 , final_unit_price BIGINT,variant_id UUID,variant_name TEXT,variant_spnc TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			discount_amount bigint NOT NULL DEFAULT 0,
			platform_fee bigint,
			pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
			product_title_snapshot text,
			seller_margin bigint,
			wholesale_cost bigint)`,
		`CREATE TABLE order_item_origins (id TEXT PRIMARY KEY, order_id TEXT, order_item_id UUID, leg_index INT, origin_type TEXT, supplier_id TEXT, warehouse_id TEXT, supplier_warehouse_id TEXT, origin_label TEXT, quantity INT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE warehouse_bins (bin_code TEXT, product_id TEXT, updated_at TIMESTAMPTZ DEFAULT now() ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			aisle character varying(20),
			capacity_percent integer NOT NULL DEFAULT 0,
			capacity_status character varying(20) NOT NULL DEFAULT 'unverified'::character varying,
			capacity_volume_cm3 bigint,
			hall_name character varying(50),
			max_weight_grams bigint,
			occupied_volume_cm3 bigint NOT NULL DEFAULT 0,
			occupied_weight_grams bigint NOT NULL DEFAULT 0,
			position_x double precision,
			position_y double precision,
			position_z double precision,
			quantity integer NOT NULL DEFAULT 0,
			shelf character varying(20),
			warehouse_id text,
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying)`,
		`CREATE TABLE suppliers (id TEXT PRIMARY KEY, owner_user_id TEXT,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			approval_status VARCHAR(20) NOT NULL DEFAULT 'approved',
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
		`CREATE TABLE supplier_shipping_methods (owner_user_id TEXT NOT NULL, method TEXT NOT NULL, status TEXT  DEFAULT 'disabled'::text NOT NULL,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			decided_at timestamp with time zone,
			decided_by text,
			requested_at timestamp with time zone)`,
		`INSERT INTO products (id, title, supplier_id, fulfillment_method) VALUES
			('p-a1','A1','sup-x','warehouse'),
			('p-a2','A2','sup-x','supplier_direct'),
			('p-b1','B1','sup-x','supplier_direct'),
			('p-c1','C1','sup-direct-only','warehouse')`,
		`INSERT INTO suppliers (id, owner_user_id) VALUES ('sup-direct-only','owner-direct'), ('sup-x','owner-x')`,
		// Toggle bait: sup-direct-only is direct-only TODAY. Under
		// the old queue derivation this would pull p-c1's item OUT
		// of the queue; the origins contract must ignore it.
		`INSERT INTO supplier_shipping_methods (owner_user_id, method, status) VALUES ('owner-direct','direct','enabled'), ('owner-direct','warehouse','disabled')`,
		`INSERT INTO orders (id, order_number, store_name, customer_full_name, status, fulfillment_method, payment_status) VALUES
			('ord-a','ORD-A','store','cust','processing','mixed','paid'),
			('ord-b','ORD-B','store','cust','processing','warehouse','paid'),
			('ord-c','ORD-C','store','cust','processing','warehouse','paid')`,
		`INSERT INTO order_items (id, order_id, product_id, quantity, final_unit_price) VALUES
			('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1','ord-a','p-a1',1,1000),
			('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2','ord-a','p-a2',2,1000),
			('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb1','ord-b','p-b1',1,1000),
			('cccccccc-cccc-cccc-cccc-ccccccccccc1','ord-c','p-c1',1,1000)`,
		// ord-a: the warehouse-flagged item shipped from the
		// SUPPLIER (origin 'supplier' wins over the flag), and the
		// direct-flagged item sits in the warehouse (origin
		// 'warehouse' wins over the flag).
		`INSERT INTO order_item_origins (id, order_id, order_item_id, leg_index, origin_type, quantity) VALUES
			('org-a1','ord-a','aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1',0,'supplier',1),
			('org-a2','ord-a','aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2',1,'warehouse',2)`,
		// ord-b and ord-c items deliberately have NO origin rows
		// (legacy): they follow the product flag only.
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL/seed failed: %v\n%s", err, q)
		}
	}
	return db
}

// The pick list must contain EXACTLY the warehouse-routed items:
// p-a2 (origin warehouse, despite its direct flag) and p-c1
// (origin-less legacy, warehouse flag — unaffected by the
// supplier's later toggle flip). p-a1 (origin supplier, despite its
// warehouse flag) and p-b1 (origin-less, direct flag) stay out.
func TestGap13WarehouseQueueReadsPersistedOrigins(t *testing.T) {
	db := gap13QueueSetup(t)
	repo := NewWarehouseRepository(db)
	orders, err := repo.GetOrdersReadyToPick(context.Background())
	if err != nil {
		t.Fatalf("GetOrdersReadyToPick: %v", err)
	}
	got := map[string]int{}
	for _, o := range orders {
		var items []struct {
			ProductID string `json:"productId"`
			Quantity  int    `json:"quantity"`
		}
		if len(o.Items) > 0 {
			if err := json.Unmarshal(o.Items, &items); err != nil {
				t.Fatalf("decode items of %s: %v", o.ID, err)
			}
		}
		for _, it := range items {
			got[it.ProductID] = it.Quantity
		}
	}
	want := map[string]int{"p-a2": 2, "p-c1": 1}
	if len(got) != len(want) {
		t.Fatalf("pick list items = %v, want exactly %v", got, want)
	}
	for id, qty := range want {
		if got[id] != qty {
			t.Fatalf("pick list items = %v, want exactly %v", got, want)
		}
	}
}
