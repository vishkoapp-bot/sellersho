package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/drup/platform/internal/testdb"

	"github.com/google/uuid"
)

// These tests NEVER use DATABASE_URL or any host application credentials.
// The release coordinator supplies a disposable migrated PostgreSQL database.
// Each test uses its own schema so concurrency tests exercise actual row locks.
func skuInventoryFixture(t *testing.T) (*sql.DB, string, string, string) {
	t.Helper()
	dsn := os.Getenv("SKU_COMMERCE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SKU_COMMERCE_DATABASE_URL to a disposable migrated database")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "sku_test_" + uuid.NewString()[:8]
	if _, err := db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); db.Close() })
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SET LOCAL search_path TO ` + schema); err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE suppliers(id TEXT PRIMARY KEY,
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
			owner_user_id uuid,
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
		`INSERT INTO suppliers VALUES('s')`,
		`CREATE TABLE products(id VARCHAR(50) PRIMARY KEY,supplier_id VARCHAR(50),stock INT  DEFAULT 0 NOT NULL,schema_version INT NOT NULL DEFAULT 0,product_type TEXT NOT NULL DEFAULT 'legacy',origin_supplier_warehouse_id TEXT DEFAULT 'sw1',updated_at TIMESTAMPTZ DEFAULT NOW(),title TEXT DEFAULT 'product',weight_grams INT DEFAULT 100,fulfillment_method TEXT DEFAULT 'warehouse',
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
			original_price bigint,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			platform_margin bigint,
			preparation_days integer DEFAULT 1,
			rating numeric(3,2) NOT NULL DEFAULT 0,
			reviews_count integer NOT NULL DEFAULT 0,
			shipping_class text NOT NULL DEFAULT 'standard'::text,
			slug character varying(255),
			specs jsonb DEFAULT '{}'::jsonb,
			spn character varying(20),
			spn_number bigint,
			supplier_base_cost bigint,
			supplier_warranty character varying(100),
			supply_option character varying(20) NOT NULL DEFAULT '1day'::character varying,
			tax_class text NOT NULL DEFAULT 'standard'::text,
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying,
			warranty_property_id character varying(60),
			width_mm integer)`,
		`CREATE TABLE product_variants(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,product_id TEXT,stock INT  DEFAULT 0 NOT NULL,inventory_mode TEXT NOT NULL DEFAULT 'legacy',is_active BOOL NOT NULL DEFAULT TRUE,min_cart_qty INT DEFAULT 1,max_cart_qty INT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
			combination_key text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			height_mm integer,
			is_default boolean NOT NULL DEFAULT false,
			length_mm integer,
			measurement_revision_id text,
			measurement_source text,
			measurement_status text NOT NULL DEFAULT 'unverified'::text,
			measurement_version integer NOT NULL DEFAULT 0,
			sale_from timestamp with time zone,
			sale_price bigint,
			sale_to timestamp with time zone,
			selection jsonb NOT NULL DEFAULT '{}'::jsonb,
			shipping_class text,
			spnc character varying(12),
			spnc_number integer,
			supplier_base_cost bigint,
			tax_class text,
			variant_name text NOT NULL DEFAULT 'پیش‌فرض'::text,
			weight_grams integer,
			width_mm integer)`,
		`CREATE TABLE inventory_balances(id VARCHAR(80) PRIMARY KEY,product_id VARCHAR(50),supplier_id VARCHAR(50),variant_id UUID,owner_type VARCHAR(20),location_type VARCHAR(20),location_key VARCHAR(150),status VARCHAR(30),warehouse_id TEXT,supplier_warehouse_id TEXT,quantity INT  DEFAULT 0 NOT NULL CHECK(quantity>=0),reconciled BOOL DEFAULT false ,updated_at TIMESTAMPTZ DEFAULT NOW(),bin_code VARCHAR(30),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_name character varying(100),
			CONSTRAINT inventory_balances_status_check CHECK (status IN ('physical','sellable','sales_reserved','return_requested','quarantine','in_transit','damaged','unreconciled','inbound_reserved','qc_accepted')))`,
		`CREATE TRIGGER inventory_assign_origin BEFORE INSERT ON inventory_balances FOR EACH ROW EXECUTE FUNCTION public.inventory_assign_origin()`,
		`CREATE UNIQUE INDEX legacy_balance_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL`,
		`CREATE UNIQUE INDEX sku_balance_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL`,
		`CREATE TABLE warehouses(id TEXT PRIMARY KEY,is_active BOOL DEFAULT TRUE,name TEXT DEFAULT 'warehouse',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			tapin_province_id INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			capacity_m3_total numeric(10,3),
			capacity_units_total integer,
			city_name text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			is_receiving_open boolean NOT NULL DEFAULT true,
			priority integer NOT NULL DEFAULT 100,
			province_name text,
			tapin_city_id integer,
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE supplier_warehouses(id TEXT PRIMARY KEY,supplier_id TEXT,is_active BOOL DEFAULT TRUE,name TEXT DEFAULT 'supplier warehouse',
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			address text,
			city_name text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			phone character varying(20),
			postal_code character varying(10),
			province_name text,
			tapin_city_id integer,
			tapin_province_id integer,
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE warehouse_storage_receipt_lots (LIKE public.warehouse_storage_receipt_lots INCLUDING ALL)`,
		`CREATE TABLE warehouse_storage_positions(id TEXT PRIMARY KEY,product_id TEXT,supplier_id TEXT,warehouse_id TEXT,inbound_id TEXT,goods_class TEXT DEFAULT 'normal'::character varying ,qty INT,stocked_at TIMESTAMPTZ DEFAULT NOW(),original_stocked_at TIMESTAMPTZ,closed_at TIMESTAMPTZ,updated_at TIMESTAMPTZ DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			last_assessed_date date,
			last_fee_date date,
			total_fee_charged_toman bigint NOT NULL DEFAULT 0)`,
		`CREATE TABLE warehouse_bins(bin_code TEXT PRIMARY KEY,warehouse_id TEXT,
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
			product_id character varying(50),
			quantity integer NOT NULL DEFAULT 0,
			shelf character varying(20),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying)`,
		`INSERT INTO warehouse_bins VALUES('A-1','w1')`,
		`CREATE TABLE inbound_shipments(id TEXT PRIMARY KEY,status TEXT DEFAULT 'in_transit'::character varying ,warehouse_id TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			carrier character varying(100),
			cartons_count integer,
			completed_at timestamp with time zone,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			dispatch_at timestamp with time zone,
			dispatched_at timestamp with time zone,
			expected_arrival timestamp with time zone,
			items jsonb DEFAULT '[]'::jsonb,
			items_count integer,
			notes text,
			received_at timestamp with time zone,
			scanned_items jsonb DEFAULT '[]'::jsonb,
			started_receiving_at timestamp with time zone,
			supplier_id character varying(50),
			supplier_name character varying(100),
			supplier_warehouse_id text,
			truck_plate character varying(50),
			warehouse_name character varying(100) NOT NULL DEFAULT 'انبار مرکزی تهران'::character varying)`,
		`CREATE TABLE shipments(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,received_at TIMESTAMPTZ,warehouse_id TEXT,supplier_warehouse_id TEXT DEFAULT 'sw1',shipment_code TEXT UNIQUE,supplier_id TEXT DEFAULT 's',status TEXT DEFAULT 'draft'::character varying ,created_at TIMESTAMPTZ DEFAULT NOW(),creation_key TEXT,request_fingerprint TEXT)`,
		`CREATE UNIQUE INDEX carton_creation_key_unique ON shipments(supplier_id,creation_key)`,
		`CREATE TABLE order_item_origins(id TEXT PRIMARY KEY,order_id TEXT,order_item_id UUID,leg_index INT,origin_type TEXT,supplier_id TEXT,warehouse_id TEXT,supplier_warehouse_id TEXT,quantity INT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			origin_label text)`,
		`INSERT INTO warehouses VALUES('w1',TRUE),('w2',TRUE)`,
		`INSERT INTO supplier_warehouses VALUES('sw1','s',TRUE),('sw2','s',TRUE)`,
		`CREATE FUNCTION fixture_origin() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO order_item_origins VALUES(NEW.id::text,NEW.order_id,NEW.id,0,'supplier','s',NULL,'sw1',NEW.quantity); RETURN NEW; END $$`,
		`CREATE TABLE inventory_movements(id VARCHAR(80),product_id VARCHAR(50),supplier_id VARCHAR(50),variant_id UUID,quantity INT,from_status VARCHAR(30),to_status VARCHAR(30),from_location_key VARCHAR(150),to_location_key VARCHAR(150),reference_type VARCHAR(40),reference_id VARCHAR(80),idempotency_key VARCHAR(150) UNIQUE,actor_user_id VARCHAR(80),metadata JSONB DEFAULT '{}'::jsonb ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE order_items(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,order_id TEXT,product_id TEXT,variant_id UUID,quantity INT DEFAULT 1 ,final_unit_price BIGINT DEFAULT 100,variant_name TEXT,variant_spnc TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			discount_amount bigint NOT NULL DEFAULT 0,
			platform_fee bigint,
			pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
			product_title_snapshot text,
			seller_margin bigint,
			wholesale_cost bigint)`,
		`CREATE TRIGGER fixture_origin AFTER INSERT ON order_items FOR EACH ROW EXECUTE FUNCTION fixture_origin()`,
		`CREATE TABLE orders(id VARCHAR(50) PRIMARY KEY,order_number VARCHAR(30),status TEXT DEFAULT 'processing'::character varying ,payment_status TEXT DEFAULT 'pending'::character varying ,payment_expires_at TIMESTAMPTZ DEFAULT (now() + '00:15:00'::interval) ,customer_user_id UUID,shipped_at TIMESTAMPTZ,inventory_captured BOOL DEFAULT FALSE,updated_at TIMESTAMPTZ DEFAULT NOW(),store_id TEXT,referrer_seller_id TEXT,discount_code TEXT NOT NULL DEFAULT '',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			payable_amount BIGINT NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			box_size character varying(80),
			carrier character varying(100),
			confirmed_at timestamp with time zone,
			coupon_discount_amount bigint,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			customer_address text,
			customer_city character varying(50),
			customer_full_name character varying(100),
			customer_phone character varying(20),
			customer_postal_code character varying(20),
			customer_province character varying(50),
			discount_amount bigint NOT NULL DEFAULT 0,
			dispute_reason text,
			disputed_at timestamp with time zone,
			dropshipper_profit bigint NOT NULL DEFAULT 0,
			fulfillment_method character varying(20) DEFAULT 'warehouse'::character varying,
			gateway_amount bigint NOT NULL DEFAULT 0,
			idempotency_key text,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			packed_at timestamp with time zone,
			payment_method character varying(20) NOT NULL DEFAULT 'cash'::character varying,
			platform_addon_amount bigint NOT NULL DEFAULT 0,
			platform_profit bigint NOT NULL DEFAULT 0,
			profit_released boolean DEFAULT false,
			profit_released_at timestamp with time zone,
			referral_commission bigint DEFAULT 0,
			referral_commission_released boolean DEFAULT false,
			return_window_ends_at timestamp with time zone,
			seller_discount_amount bigint NOT NULL DEFAULT 0,
			shipping_cost bigint NOT NULL DEFAULT 0,
			shipping_label text,
			shipping_quote_id text,
			store_name character varying(100),
			supplier_addon_amount bigint NOT NULL DEFAULT 0,
			total_amount bigint,
			tracking_code character varying(50),
			tracking_number character varying(100),
			wallet_amount bigint NOT NULL DEFAULT 0,
			weight_grams integer)`,
		`CREATE TABLE supplier_shipments(id TEXT PRIMARY KEY,order_id TEXT,shipped_at TIMESTAMPTZ,delivered_at TIMESTAMPTZ,status TEXT DEFAULT 'pending'::character varying ,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			tracking_number VARCHAR(255),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			carrier character varying(100),
			created_at timestamp with time zone DEFAULT now(),
			customer_name character varying(255),
			customer_phone character varying(20),
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			send_price bigint,
			shipping_address jsonb,
			supplier_id character varying(255),
			supplier_notes text,
			tapin_box_id text,
			tapin_last_status text,
			tapin_register_attempted_at timestamp with time zone,
			tapin_registered_at timestamp with time zone,
			tapin_service text,
			updated_at timestamp with time zone DEFAULT now())`,
		`CREATE TABLE referral_bonuses(referrer_store_id TEXT,referred_store_id TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			amount bigint NOT NULL DEFAULT 100000,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			id character varying(50),
			released_at timestamp with time zone,
			status character varying(20) NOT NULL DEFAULT 'locked'::character varying)`,
		`CREATE TABLE payment_transactions(id TEXT PRIMARY KEY,order_id TEXT,order_number TEXT,gateway_token TEXT UNIQUE,rrn TEXT,card_mask TEXT,amount BIGINT,status TEXT DEFAULT 'pending'::character varying ,created_at TIMESTAMPTZ DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			compensation_capture boolean NOT NULL DEFAULT false)`,
		`CREATE UNIQUE INDEX payment_transactions_one_active_per_order ON payment_transactions(order_id) WHERE status IN ('pending','paid')`,
		`CREATE TABLE users(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			email VARCHAR(255),
			mobile VARCHAR(50) NOT NULL DEFAULT '',
			username VARCHAR(255),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			failed_login_attempts integer NOT NULL DEFAULT 0,
			full_name character varying(100),
			is_active boolean NOT NULL DEFAULT true,
			locked_until timestamp with time zone,
			password_hash text,
			role character varying(30) NOT NULL DEFAULT 'customer'::character varying,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_city character varying(50) NOT NULL DEFAULT 'تهران'::character varying)`,
		`CREATE TABLE customer_wallets(customer_id UUID PRIMARY KEY,balance BIGINT NOT NULL DEFAULT 0,updated_at TIMESTAMPTZ DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			locked_balance bigint NOT NULL DEFAULT 0)`,
		`CREATE TABLE ledger_entries(id TEXT PRIMARY KEY,target_account TEXT,target_type TEXT,type TEXT,amount BIGINT,reason TEXT,operator TEXT,related_order_or_dispute TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			date timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE customer_returns(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,order_id TEXT,customer_id UUID,status TEXT DEFAULT 'requested'::character varying ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			admin_note text NOT NULL DEFAULT ''::text,
			cod_refused boolean NOT NULL DEFAULT false,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			destination_supplier_warehouse_id text,
			prepaid boolean NOT NULL DEFAULT false,
			reason text NOT NULL DEFAULT ''::text,
			return_code text,
			return_shipping_amount bigint,
			return_shipping_bearer character varying(20),
			return_shipping_quote_at timestamp with time zone,
			return_shipping_quote_groups jsonb,
			return_shipping_quote_source text,
			return_shipping_quote_toman bigint,
			return_shipping_settled_at timestamp with time zone,
			shipped_at timestamp with time zone,
			tracking_number text NOT NULL DEFAULT ''::text,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_id text)`,
		`CREATE TABLE return_items(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,return_id UUID,order_item_id UUID,qty INT,received_at TIMESTAMPTZ,qc_status TEXT DEFAULT 'pending'::character varying ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			variant_id uuid)`,
		`CREATE TABLE disputes(id TEXT PRIMARY KEY,order_id TEXT,status TEXT DEFAULT 'pending'::character varying ,resolution_action TEXT,claim_amount BIGINT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			customer_name character varying(100),
			financial_decision text,
			order_number character varying(30),
			product_title character varying(255),
			reason text,
			resolution_date timestamp with time zone,
			responsible_party character varying(100),
			store_id character varying(50),
			supplier_name character varying(100),
			type character varying(30))`,
		`CREATE TABLE order_inventory_captures(order_item_id TEXT PRIMARY KEY,order_id TEXT,product_id TEXT,variant_id UUID,quantity INT,variant_quantity INT DEFAULT 0 ,restored_quantity INT NOT NULL DEFAULT 0,inventory_mode TEXT,allocations JSONB DEFAULT '[]'::jsonb ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE order_inventory_returns(return_item_id TEXT PRIMARY KEY,order_item_id UUID,quantity INT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE shipment_items(id UUID PRIMARY KEY DEFAULT gen_random_uuid(),shipment_id UUID,variant_id UUID,expected_qty INT,received_qty INT DEFAULT 0,inventory_received_qty INT DEFAULT 0,qc_status TEXT DEFAULT 'pending')`,
		`CREATE FUNCTION fixture_shipment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO shipments(id,warehouse_id) VALUES(NEW.shipment_id,'w1') ON CONFLICT DO NOTHING; RETURN NEW; END $$`,
		`CREATE TRIGGER fixture_shipment BEFORE INSERT ON shipment_items FOR EACH ROW EXECUTE FUNCTION fixture_shipment()`,
		`CREATE TABLE putaway_tasks(id TEXT PRIMARY KEY,product_id TEXT,variant_id UUID,inventory_source_location_key TEXT,quantity INT DEFAULT 0 ,status TEXT DEFAULT 'pending'::character varying ,created_at TIMESTAMPTZ DEFAULT now() ,warehouse_id TEXT,inbound_id TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			assigned_to character varying(50),
			bin_location character varying(50),
			completed_at timestamp with time zone)`,
		`CREATE TRIGGER sync_fixture_stock AFTER INSERT OR UPDATE OR DELETE ON inventory_balances FOR EACH ROW EXECUTE FUNCTION public.sync_product_stock_projection()`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	// Install the actual principal migration in this private schema. Cancellation
	// must exercise current financial guards as well as exact SKU restoration.
	// TEST-02 round 4: the fixture's own DDL above already carries
	// payment_transactions.compensation_capture (full demanded set), so
	// the principal migration's bare ADD COLUMN would fail with "already
	// exists". Drop it first — the migration re-adds it, which is exactly
	// what this fixture verifies. The migration itself drops and recreates
	// the payment_transactions_one_active_per_order index (lines 4-5), so
	// the index must be PRESENT here, not dropped.
	// Same pattern as the checkout-attempt fixture.
	if _, err := tx.Exec(`ALTER TABLE payment_transactions DROP COLUMN IF EXISTS compensation_capture`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"202611140006_inventory_transfer_provenance.up.sql", "202610250007_refunds_table.up.sql", "202611150004_order_refund_principal.up.sql", "202611230002_spncx_product_units.up.sql"} {
		migration, err := os.ReadFile("../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	red, blue := uuid.NewString(), uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO products VALUES('p','s',0,2,'variable','sw1',NOW())`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO product_variants(id,product_id,stock,inventory_mode,spnc) VALUES($1,'p',1,'tracked','A0C00001'),($2,'p',3,'tracked','A0C00002')`, red, blue); err != nil {
		t.Fatal(err)
	}
	if err := SeedTrackedSKUInventoryTx(context.Background(), tx, "p", "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE inventory_balances SET supplier_warehouse_id='sw1'`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db, schema, red, blue
}

func skuScopedDB(t *testing.T, schema string) *sql.DB {
	return skuScopedDBSearchPath(t, schema, schema)
}

// skuScopedDBSearchPath opens a handle to a scratch schema with an explicit
// search_path. TEST-02: migration runs need the schema,public variant — the
// shared disposable DB owns pg_trgm in public, so the trigram opclass is
// invisible under a schema-only search_path and migration 202611190002 fails.
// Tables still land in the scratch schema (first in the path); test bodies
// keep the strict schema-only handle.
func skuScopedDBSearchPath(t *testing.T, schema, searchPath string) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SKU_COMMERCE_DATABASE_URL")
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("invalid disposable database URL")
		}
		q := u.Query()
		q.Set("search_path", searchPath)
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " search_path=" + searchPath
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal("open disposable schema database")
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSKUInventoryPaymentCallbackIsAtomicAndIdempotent(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	for _, order := range []string{"pay-one", "pay-two"} {
		if _, err := tx.Exec(`INSERT INTO orders(id,status,payment_status) VALUES($1,'processing','pending')`, order); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,$2,'p',$3,1)`, uuid.NewString(), order, red); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES($1,$1,$1,$1,1000,'pending')`, order); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	scoped := skuScopedDB(t, schema)
	repo := NewOrderRepository(scoped)
	type result struct {
		order string
		err   error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, order := range []string{"pay-one", "pay-two"} {
		wg.Add(1)
		go func(order string) {
			defer wg.Done()
			_, err := repo.MarkPaymentPaid(context.Background(), order, "rrn", "masked")
			results <- result{order, err}
		}(order)
	}
	wg.Wait()
	close(results)
	winner, loser := "", ""
	for r := range results {
		if r.err == nil {
			if winner != "" {
				t.Fatal("both callbacks captured the last SKU unit")
			}
			winner = r.order
		} else if errors.Is(r.err, ErrInsufficientInventory) {
			loser = r.order
		} else {
			t.Fatalf("unexpected payment error: %v", r.err)
		}
	}
	if winner == "" || loser == "" {
		t.Fatal("expected one paid and one insufficient inventory callback")
	}
	if _, err := repo.MarkPaymentPaid(context.Background(), winner, "rrn", "masked"); err != nil {
		t.Fatal(err)
	}
	var stock, captured int
	var state string
	if err := scoped.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT COUNT(*) FROM order_inventory_captures`).Scan(&captured); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT status FROM payment_transactions WHERE gateway_token=$1`, loser).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if stock != 0 || captured != 1 || state != "pending" {
		t.Fatalf("non-atomic/replayed payment stock=%d captures=%d loser=%s", stock, captured, state)
	}
	if err := repo.CancelOrder(context.Background(), winner, "fixture:sku-cancel", true); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock != 1 {
		t.Fatalf("paid cancellation restored %d units, want 1", stock)
	}
	var preserved bool
	if err := scoped.QueryRow(`SELECT p.status='paid' AND (SELECT COUNT(*) FROM order_refund_obligations WHERE source_payment_id=p.id AND amount_toman=1000 AND status='unknown' AND destination='original_payment')=1 FROM payment_transactions p WHERE p.id=$1`, winner).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("SKU restoration lost captured source/unknown liability %v %v", preserved, err)
	}
}

func skuTestTx(t *testing.T, db *sql.DB, schema string) *sql.Tx {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`SET LOCAL search_path TO ` + schema); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	return tx
}

func TestSKUInventoryExactCaptureCancelAndLegacyIsolation(t *testing.T) {
	db, schema, red, blue := skuInventoryFixture(t)
	ctx := context.Background()
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	id := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES('unallocated','p','s','supplier','supplier','legacy','sellable',7,TRUE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO orders(id) VALUES('o')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'o','p',$2,1)`, id, red); err != nil {
		t.Fatal(err)
	}
	if handled, err := captureOrderInventoryTx(ctx, tx, "o"); err != nil || !handled {
		t.Fatalf("capture %v %v", handled, err)
	}
	var r, b, unallocated int
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&r); err != nil {
		t.Fatal(err)
	}
	tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, blue).Scan(&b)
	tx.QueryRow(`SELECT quantity FROM inventory_balances WHERE id='unallocated'`).Scan(&unallocated)
	if r != 0 || b != 3 || unallocated != 7 {
		t.Fatalf("capture changed wrong pool red=%d blue=%d unallocated=%d", r, b, unallocated)
	}
	for i := 0; i < 2; i++ {
		if handled, err := restoreOrderInventoryTx(ctx, tx, "o"); err != nil || !handled {
			t.Fatalf("restore %v %v", handled, err)
		}
	}
	tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&r)
	if r != 1 {
		t.Fatalf("replayed restore stock=%d", r)
	}
	var code, status string
	var restored bool
	if err := tx.QueryRow(`SELECT oiu.code,u.status,oiu.restored_at IS NOT NULL
		FROM order_item_units oiu JOIN product_units u ON u.id=oiu.unit_id
		WHERE oiu.order_item_id=$1`, id).Scan(&code, &status, &restored); err != nil || code != "A0C00001-X0001" || status != "in_stock" || !restored {
		t.Fatalf("cancel did not restore the captured unit: code=%s status=%s restored=%t err=%v", code, status, restored, err)
	}
}

func TestSKUInventoryLegacyUntrackedRejectsUncodedCapture(t *testing.T) {
	db, schema, _, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	sku, item := uuid.NewString(), uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO products VALUES('legacy','s',5,0,'legacy','sw1',NOW())`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO product_variants(id,product_id,stock,inventory_mode) VALUES($1,'legacy',0,'legacy')`, sku); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'legacy-order','legacy',$2,1)`, item, sku); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(context.Background(), tx, "legacy-order"); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("uncoded legacy capture: %v", err)
	}
	var units int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM product_units WHERE product_id='legacy'`).Scan(&units); err != nil || units != 0 {
		t.Fatalf("legacy product units=%d, want 0, err=%v", units, err)
	}
	var physical, captures int
	if err := tx.QueryRow(`SELECT stock, (SELECT COUNT(*) FROM order_inventory_captures WHERE order_id='legacy-order')
		FROM products WHERE id='legacy'`).Scan(&physical, &captures); err != nil || physical != 5 || captures != 0 {
		t.Fatalf("failed capture physical=%d captures=%d err=%v", physical, captures, err)
	}
}

func TestSKUInventoryStockAdjustmentProtectsOtherLocationsAndSKUs(t *testing.T) {
	db, schema, red, blue := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	if err := moveSKUInventoryTx(ctx, tx, "p", "s", red, 1, "sellable", "sellable", "supplier-warehouse:sw1", "warehouse", "warehouse:w", "test", "receipt", "stock-test-transfer", ""); err != nil {
		t.Fatal(err)
	}
	if err := SetTrackedSKUStockTx(ctx, tx, "p", "s", red, 0, "reduce supplier declaration", "s"); err == nil {
		t.Fatal("aggregate stock edit removed warehouse-held units")
	}
	for i := 0; i < 2; i++ {
		if err := SetTrackedSKUStockTx(ctx, tx, "p", "s", red, 3, "replenish supplier declaration", "s"); err != nil {
			t.Fatal(err)
		}
	}
	var r, b, held int
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&r); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, blue).Scan(&b); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT quantity FROM inventory_balances WHERE variant_id=$1 AND location_key='warehouse:w'`, red).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if r != 3 || b != 3 || held != 1 {
		t.Fatalf("wrong SKU/location adjusted red=%d blue=%d held=%d", r, b, held)
	}
	if err := SetTrackedSKUStockTx(ctx, tx, "p", "foreign", red, 5, "foreign supplier attempt", "s"); err == nil {
		t.Fatal("foreign supplier adjusted stock")
	}
}

func TestSKUInventoryMissingForeignAndZeroSKUReject(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'missing','p',NULL,1)`, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "missing"); err == nil {
		t.Fatal("V2 purchase omitted SKU")
	}
	foreign := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO product_variants(id,product_id,stock,inventory_mode) VALUES($1,'foreign-product',5,'tracked')`, foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'foreign','p',$2,1)`, uuid.NewString(), foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "foreign"); err == nil {
		t.Fatal("foreign product SKU accepted")
	}
	if err := SetTrackedSKUStockTx(ctx, tx, "p", "s", red, 0, "sold out fixture", "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'zero','p',$2,1)`, uuid.NewString(), red); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "zero"); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("zero tracked stock accepted: %v", err)
	}
}

func TestSKUInventoryFailedQCRemainsQuarantined(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	shipment := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty) VALUES($1,$2,1)`, shipment, red); err != nil {
		t.Fatal(err)
	}
	bookFixtureInbound(t, ctx, tx, shipment, red, 1, "sw1")
	for i := 0; i < 2; i++ {
		if _, err := ReceiveTrackedSKUShipmentItemTx(ctx, tx, shipment, red, 1, "failed"); err != nil {
			t.Fatal(err)
		}
	}
	var sellable, quarantine, tasks int
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&sellable); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE variant_id=$1 AND status='quarantine'`, red).Scan(&quarantine); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM putaway_tasks`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if sellable != 0 || quarantine != 1 || tasks != 0 {
		t.Fatalf("failed QC became sellable stock=%d quarantine=%d tasks=%d", sellable, quarantine, tasks)
	}
}

func TestSKUInventoryConcurrentLastUnitOnlyOneCapture(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	for _, order := range []string{"one", "two"} {
		if _, err := tx.Exec(`INSERT INTO orders(id) VALUES($1)`, order); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,$2,'p',$3,1)`, uuid.NewString(), order, red); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, order := range []string{"one", "two"} {
		wg.Add(1)
		go func(order string) {
			defer wg.Done()
			tx, err := db.Begin()
			if err != nil {
				results <- err
				return
			}
			defer tx.Rollback()
			if _, err = tx.Exec(`SET LOCAL search_path TO ` + schema); err == nil {
				_, err = captureOrderInventoryTx(context.Background(), tx, order)
			}
			if err == nil {
				err = tx.Commit()
			}
			results <- err
		}(order)
	}
	wg.Wait()
	close(results)
	success, rejected := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrInsufficientInventory) {
			rejected++
		} else {
			t.Fatalf("unexpected capture error %v", err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}
}

func TestSKUInventoryDuplicateLinesAndInactiveReject(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO orders(id) VALUES('duplicate'),('inactive'); SAVEPOINT duplicate_capture`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'duplicate','p',$2,1)`, uuid.NewString(), red); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := captureOrderInventoryTx(context.Background(), tx, "duplicate"); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("duplicate lines bypassed stock: %v", err)
	}
	// A failed payment rolls back its transaction; isolate the next scenario
	// from any provisional lot/unit writes made by the rejected capture.
	if _, err := tx.Exec(`ROLLBACK TO SAVEPOINT duplicate_capture`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DELETE FROM order_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE product_variants SET is_active=FALSE WHERE id=$1`, red); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($2,'inactive','p',$1,1)`, red, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(context.Background(), tx, "inactive"); err == nil {
		t.Fatal("captured inactive SKU")
	}
}

func TestSKUInventoryReceiptAndReturnRemainExactAndIdempotent(t *testing.T) {
	db, f, warehouse, shipment, red, blue := inv09ReceiveTracked(t, 1)
	ctx := context.Background()
	var code string
	if err := db.QueryRow(`SELECT shipment_code FROM shipments WHERE id=$1`, shipment).Scan(&code); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := NewSupplierRepository(db).ReceiveWarehouseShipment(ctx, code, []WarehouseReceiveItem{{red, 1, "pass"}}); err != nil {
			t.Fatal(err)
		}
	}
	var r, b, tasks int
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&r); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, blue).Scan(&b); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM putaway_tasks WHERE inbound_id=$1`, shipment).Scan(&tasks); err != nil || r != 0 || b != 3 || tasks != 0 {
		t.Fatalf("receipt mixed stock red=%d blue=%d tasks=%d err=%v", r, b, tasks, err)
	}
	wh := NewWarehouseRepository(db)
	for i := 0; i < 2; i++ {
		if err := wh.RecordQCPass(ctx, f.product, shipment, f.user, 1); err != nil {
			t.Fatal(err)
		}
	}
	inv09PlacePending(t, db, f, shipment, warehouse)
	order, token := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE order_items SET variant_id=$2 WHERE order_id=$1`, order, red)
	seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
	if _, err := NewOrderRepository(db).MarkPaymentPaid(ctx, token, "R"+red[:20], "mask"); err != nil {
		t.Fatal(err)
	}
	var orderItem string
	if err := db.QueryRow(`SELECT id::text FROM order_items WHERE order_id=$1`, order).Scan(&orderItem); err != nil {
		t.Fatal(err)
	}
	returnID, returnItem, secondReturnItem := uuid.NewString(), uuid.NewString(), uuid.NewString()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Model a historical duplicate return claim to exercise the restock
	// backstop. Scope the protected fixture writes to this disposable,
	// rolled-back transaction and use the normal FIN005 writer fence.
	if _, err := tx.ExecContext(ctx, `SELECT id FROM orders WHERE id=$1 FOR UPDATE`, order); err != nil {
		t.Fatal(err)
	}
	if err := markDisputeWriterTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO customer_returns(id,order_id,customer_id) VALUES($1,$2,$3)`, returnID, order, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO return_items(id,return_id,order_item_id,variant_id,qty)
		VALUES($1,$3,$4,$5,1),($2,$3,$4,$5,1)`, returnItem, secondReturnItem, returnID, orderItem, red); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if handled, err := restoreCapturedReturnTx(ctx, tx, returnItem, orderItem, f.product, f.supplier, 1, "ok", "warehouse:"+warehouse, f.user); err != nil || !handled {
			t.Fatalf("return handled=%v err=%v", handled, err)
		}
	}
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&r); err != nil || r != 1 {
		t.Fatalf("return replay stock=%d err=%v", r, err)
	}
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, blue).Scan(&b); err != nil || b != 3 {
		t.Fatalf("return touched sibling=%d err=%v", b, err)
	}
	if _, err := restoreCapturedReturnTx(ctx, tx, secondReturnItem, orderItem, f.product, f.supplier, 1, "ok", "warehouse:"+warehouse, f.user); err == nil {
		t.Fatal("returned more than captured")
	}
	var raw []byte
	if err := tx.QueryRow(`SELECT allocations FROM order_inventory_captures WHERE order_item_id=$1`, orderItem).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var allocations []skuAllocation
	if err := json.Unmarshal(raw, &allocations); err != nil || len(allocations) != 1 || allocations[0].Quantity != 1 || allocations[0].OriginID == "" {
		t.Fatalf("missing exact capture allocation %s err=%v", raw, err)
	}
}

func TestSKUInventoryReceiptCannotSellBeforeExactPutaway(t *testing.T) {
	db, f, warehouse, shipment, red, _ := inv09ReceiveTracked(t, 2)
	ctx := context.Background()
	wh := NewWarehouseRepository(db)
	if err := wh.RecordQCPass(ctx, f.product, shipment, f.user, 2); err != nil {
		t.Fatal(err)
	}
	order, token := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE order_items SET variant_id=$2 WHERE order_id=$1`, order, red)
	seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
	orders := NewOrderRepository(db)
	if _, err := orders.MarkPaymentPaid(ctx, token, "before-placement", "mask"); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("staging purchased: %v", err)
	}
	inv09PlacePending(t, db, f, shipment, warehouse)
	if _, err := orders.MarkPaymentPaid(ctx, token, "before-placement", "mask"); !errors.Is(err, ErrCapturePendingManualResolution) {
		t.Fatalf("failed charged capture replayed after stock became available: %v", err)
	}
	order, token = opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE order_items SET variant_id=$2 WHERE order_id=$1`, order, red)
	seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
	if _, err := orders.MarkPaymentPaid(ctx, token, "R"+red[:20], "mask"); err != nil {
		t.Fatal(err)
	}
	var staged, sellable int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE variant_id=$1 AND status IN ('physical','qc_accepted','quarantine')`, red).Scan(&staged); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&sellable); err != nil || staged != 0 || sellable != 1 {
		t.Fatalf("stranded staged=%d sellable=%d err=%v", staged, sellable, err)
	}
	var origin, location string
	if err := db.QueryRow(`SELECT ib.warehouse_id,ib.location_type FROM order_inventory_captures ic CROSS JOIN LATERAL jsonb_array_elements(ic.allocations) a JOIN inventory_balances ib ON ib.id=a->>'balanceId' WHERE ic.order_id=$1`, order).Scan(&origin, &location); err != nil || origin != warehouse || location != "bin" {
		t.Fatalf("capture origin=%s location=%s err=%v", origin, location, err)
	}
}

func bookFixtureInbound(t *testing.T, ctx context.Context, tx *sql.Tx, shipment, variant string, quantity int, source string) {
	t.Helper()
	if err := lockInventoryParentsTx(ctx, tx, []inventoryCaptureItem{{ProductID: "p", VariantID: variant}}); err != nil {
		t.Fatal(err)
	}
	if err := reserveInboundTransferTx(ctx, tx, shipment, "s", "w1", []InboundTransferLine{{ProductID: "p", VariantID: variant, Quantity: quantity, SourceKind: "transfer", Source: InventoryOrigin{Type: "supplier", SupplierID: "s", SupplierWarehouseID: source}}}); err != nil {
		t.Fatal(err)
	}
	if err := dispatchInboundTransferTx(ctx, tx, shipment); err != nil {
		t.Fatal(err)
	}
}
