package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestINV08MigrationPreservesHistoricalCapacity(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	// Later migrations depend on placements and preserve legacy exceptions. An
	// earlier upgrade rehearsal must not drop that live fixture evidence.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer conn.ExecContext(ctx, `RESET search_path`)
	schema := "inv08_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = conn.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `SET search_path TO `+schema+`,pg_catalog`); err != nil {
		t.Fatal(err)
	}
	ddl := `
 CREATE TABLE products(id VARCHAR(50) PRIMARY KEY,length_mm INT,width_mm INT,height_mm INT,weight_grams INT,measurement_status TEXT DEFAULT 'unverified'::character varying ,measurement_source TEXT,measurement_version INT DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attribute_schema jsonb,
			barcode character varying(64),
			base_price_override bigint,
			brand text NOT NULL DEFAULT ''::text,
			category_slug character varying(100),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			description text,
			fulfillment_method character varying(20) DEFAULT 'warehouse'::character varying,
			image_url text,
			implicit_agreement boolean NOT NULL DEFAULT false,
			implicit_agreement_until timestamp with time zone,
			is_active boolean NOT NULL DEFAULT true,
			is_featured boolean DEFAULT false,
			is_original boolean NOT NULL DEFAULT true,
			is_special_deal boolean DEFAULT false,
			markup_type character varying(10),
			markup_value numeric(10,2),
			measurement_revision_id text,
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
			supplier_id character varying(50),
			supplier_warranty character varying(100),
			supply_option character varying(20) NOT NULL DEFAULT '1day'::character varying,
			tax_class text NOT NULL DEFAULT 'standard'::text,
			title character varying(255),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying,
			warranty_property_id character varying(60));
 CREATE TABLE product_variants(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,product_id TEXT,length_mm INT,width_mm INT,height_mm INT,weight_grams INT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
			combination_key text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			inventory_mode text NOT NULL DEFAULT 'legacy'::text,
			is_active boolean NOT NULL DEFAULT true,
			is_default boolean NOT NULL DEFAULT false,
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
			spnc character varying(12),
			spnc_number integer,
			stock integer NOT NULL DEFAULT 0,
			supplier_base_cost bigint,
			tax_class text,
			variant_name text NOT NULL DEFAULT 'پیش‌فرض'::text);
 CREATE TABLE putaway_tasks(id TEXT PRIMARY KEY,product_id TEXT,variant_id UUID,warehouse_id TEXT,bin_location TEXT,quantity INT DEFAULT 0 ,status TEXT DEFAULT 'pending'::character varying ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			assigned_to character varying(50),
			completed_at timestamp with time zone,
			created_at timestamp with time zone DEFAULT now(),
			inbound_id character varying(50),
			inventory_source_location_key text);
 CREATE TABLE inbound_putaway_allocations(task_id TEXT,product_id TEXT,variant_id TEXT DEFAULT ''::text ,supplier_id TEXT,warehouse_id TEXT,quantity INT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			disposition_id text,
			receipt_line_id text,
			shipment_id text,
			source_location_key text);
 CREATE TABLE putaway_capacity_reservations(id TEXT PRIMARY KEY,task_id TEXT,bin_code TEXT,volume_cm3 BIGINT,weight_grams BIGINT,status TEXT DEFAULT 'active'::character varying ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			idempotency_key character varying(150),
			measurement_revision_id text,
			released_at timestamp with time zone);
 CREATE TABLE warehouse_bins(bin_code TEXT PRIMARY KEY,quantity INT DEFAULT 0 ,occupied_volume_cm3 BIGINT DEFAULT 0 ,occupied_weight_grams BIGINT DEFAULT 0 ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			aisle character varying(20),
			capacity_percent integer NOT NULL DEFAULT 0,
			capacity_status character varying(20) NOT NULL DEFAULT 'unverified'::character varying,
			capacity_volume_cm3 bigint,
			hall_name character varying(50),
			max_weight_grams bigint,
			position_x double precision,
			position_y double precision,
			position_z double precision,
			product_id character varying(50),
			shelf character varying(20),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_id text,
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying);
 `
	if _, err = conn.ExecContext(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../migrations/202611140010_sku_measurement_revision.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/202611140010_sku_measurement_revision.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	// TEST-02 round 4: the scratch products/product_variants tables
	// already carry the measurement columns (full demanded set), so the
	// migration's bare ADD COLUMNs would fail with "already exists".
	// Drop them first — the migration re-adds them, which is exactly
	// what this test verifies. Same pattern as the checkout-attempt
	// fixture (checkout_attempt_http_test.go).
	if _, err = conn.ExecContext(ctx, `ALTER TABLE products DROP COLUMN IF EXISTS measurement_revision_id; ALTER TABLE product_variants DROP COLUMN IF EXISTS measurement_revision_id, DROP COLUMN IF EXISTS measurement_status, DROP COLUMN IF EXISTS measurement_source, DROP COLUMN IF EXISTS measurement_version; ALTER TABLE putaway_capacity_reservations DROP COLUMN IF EXISTS measurement_revision_id`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	sku := uuid.NewString()
	if _, err = conn.ExecContext(ctx, `INSERT INTO products(id,length_mm,width_mm,height_mm,weight_grams,measurement_status) VALUES('old',10,10,10,10,'verified'); INSERT INTO warehouse_bins VALUES('old-bin',5,123,456); INSERT INTO putaway_tasks(id,product_id,quantity,status,bin_location) VALUES('old-task','old',5,'completed','old-bin')`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `INSERT INTO product_variants(id,product_id,length_mm,width_mm,height_mm,weight_grams) VALUES($1,'old',100,100,100,1000)`, sku); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	var volume, weight, count int
	var parentVerified, skuVerified bool
	if err = conn.QueryRowContext(ctx, `SELECT occupied_volume_cm3,occupied_weight_grams FROM warehouse_bins WHERE bin_code='old-bin'`).Scan(&volume, &weight); err != nil || volume != 123 || weight != 456 {
		t.Fatalf("migration rewrote occupancy=%d/%d err=%v", volume, weight, err)
	}
	if err = conn.QueryRowContext(ctx, `SELECT verified FROM inventory_effective_measurements WHERE product_id='old' AND variant_id=''`).Scan(&parentVerified); err != nil || !parentVerified {
		t.Fatalf("verified parent lost=%t err=%v", parentVerified, err)
	}
	if err = conn.QueryRowContext(ctx, `SELECT verified FROM inventory_effective_measurements WHERE product_id='old' AND variant_id=$1`, sku).Scan(&skuVerified); err != nil || skuVerified {
		t.Fatalf("historical declaration promoted=%t err=%v", skuVerified, err)
	}
	if err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM warehouse_placements`).Scan(&count); err != nil || count != 0 {
		t.Fatal("historical placement invented")
	}
	if err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM warehouse_measurement_reconciliation WHERE task_id='old-task'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("historical unresolved placement hidden")
	}
	if _, err = conn.ExecContext(ctx, string(down)); err == nil {
		t.Fatal("destructive downgrade discarded persisted revision")
	}
}
