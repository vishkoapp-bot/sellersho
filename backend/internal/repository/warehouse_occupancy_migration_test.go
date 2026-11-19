package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The canonical migrated fixture already contains retained legacy exceptions;
// never delete their evidence merely to downgrade a test. Rehearse the upgrade
// in an owned schema with the exact pre-upgrade columns used by this migration.
func TestINV06MigrationPreservesAndFlagsHistoricalCounters(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer conn.ExecContext(ctx, `RESET search_path`)
	var originalExceptions int
	if err = db.QueryRow(`SELECT COUNT(*) FROM warehouse_occupancy_exceptions`).Scan(&originalExceptions); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../migrations/202611150001_warehouse_occupancy.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/202611150001_warehouse_occupancy.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	ddl := `
 CREATE TABLE warehouse_bins(bin_code TEXT PRIMARY KEY,warehouse_id TEXT,capacity_status TEXT DEFAULT 'unverified'::character varying ,quantity INT DEFAULT 0 ,occupied_volume_cm3 BIGINT DEFAULT 0 ,occupied_weight_grams BIGINT DEFAULT 0 ,updated_at TIMESTAMPTZ DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			aisle character varying(20),
			capacity_percent integer NOT NULL DEFAULT 0,
			capacity_volume_cm3 bigint,
			hall_name character varying(50),
			max_weight_grams bigint,
			position_x double precision,
			position_y double precision,
			position_z double precision,
			product_id character varying(50),
			shelf character varying(20),
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying);
 CREATE TABLE inventory_measurement_revisions(id TEXT PRIMARY KEY,length_mm INT,width_mm INT,height_mm INT,weight_grams INT,verified BOOLEAN,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			actor_user_id text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			product_id text,
			reason text,
			scope text,
			variant_id text NOT NULL DEFAULT ''::text);
 CREATE TABLE warehouse_placements(id TEXT PRIMARY KEY,task_id TEXT,product_id TEXT,variant_id TEXT DEFAULT ''::text ,supplier_id TEXT,warehouse_id TEXT,bin_code TEXT,quantity INT,volume_cm3 BIGINT,weight_grams BIGINT,measurement_revision_id TEXT,created_at TIMESTAMPTZ DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			return_entry_id text,
			source_kind text NOT NULL DEFAULT 'inbound_putaway'::text);
 CREATE TABLE inventory_balances(id TEXT PRIMARY KEY,product_id TEXT,variant_id UUID,supplier_id TEXT,owner_type TEXT,warehouse_id TEXT,location_type TEXT,location_key TEXT,status TEXT,quantity INT DEFAULT 0 ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			bin_code character varying(30),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			reconciled boolean NOT NULL DEFAULT false,
			supplier_warehouse_id text,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_name character varying(100));
 CREATE TABLE orders(id VARCHAR(50) PRIMARY KEY,status TEXT DEFAULT 'processing'::character varying ,payment_status TEXT DEFAULT 'pending'::character varying ,shipped_at TIMESTAMPTZ,
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
			created_at timestamp with time zone NOT NULL DEFAULT now(),
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
			shipping_cost bigint NOT NULL DEFAULT 0,
			shipping_label text,
			shipping_quote_id text,
			store_id character varying(50),
			store_name character varying(100),
			supplier_addon_amount bigint NOT NULL DEFAULT 0,
			total_amount bigint,
			tracking_code character varying(50),
			tracking_number character varying(100),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			wallet_amount bigint NOT NULL DEFAULT 0,
			weight_grams integer);
 CREATE TABLE order_inventory_captures(order_id TEXT,order_item_id TEXT,product_id TEXT,variant_id UUID,quantity INT,restored_quantity INT DEFAULT 0,inventory_mode TEXT DEFAULT 'tracked',allocations JSONB DEFAULT '[]'::jsonb ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			variant_quantity integer NOT NULL DEFAULT 0);
 CREATE TABLE inventory_reservations(id TEXT,product_id TEXT,variant_id UUID,supplier_id TEXT,location_key TEXT,quantity INT,reference_type TEXT,reference_id TEXT,status TEXT DEFAULT 'active'::character varying ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			idempotency_key character varying(150),
			released_at timestamp with time zone);
 CREATE TABLE supplier_return_requests(id TEXT,status TEXT DEFAULT 'awaiting_return_test'::character varying ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			accepted_quantity integer NOT NULL DEFAULT 0,
			cancelled_by character varying(80),
			collection_method character varying(30),
			completed_at timestamp with time zone,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			failed_quantity integer NOT NULL DEFAULT 0,
			handover_at timestamp with time zone,
			idempotency_key character varying(150),
			inspected_at timestamp with time zone,
			inspection_status character varying(20),
			inventory_reference_id character varying(80),
			postal_charge bigint NOT NULL DEFAULT 0,
			product_id character varying(50),
			reason text,
			received_at timestamp with time zone,
			rejection_reason text,
			requested_quantity integer,
			reviewed_at timestamp with time zone,
			reviewed_by character varying(80),
			supplier_id character varying(50),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			variant_id uuid,
			warehouse_bin_code character varying(30));
 `
	for _, seed := range []bool{false, true} {
		schema := "inv06_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, err = conn.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, `SET search_path TO `+schema+`,pg_catalog`); err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
		if seed {
			fixture := `
   INSERT INTO inventory_measurement_revisions VALUES('m1',100,100,100,100,TRUE);
   INSERT INTO warehouse_bins(bin_code,warehouse_id,capacity_status,quantity,occupied_volume_cm3,occupied_weight_grams) VALUES('departed','wh','verified',1,1000,100),('unknown','wh','verified',4,123,456),('paid-held','wh','verified',2,2000,200);
   INSERT INTO warehouse_placements(id,task_id,product_id,variant_id,supplier_id,warehouse_id,bin_code,quantity,volume_cm3,weight_grams,measurement_revision_id) VALUES('p1','t1','prod1','10000000-0000-0000-0000-000000000001','sup','wh','departed',1,1000,100,'m1'),('p2','t2','prod2','10000000-0000-0000-0000-000000000002','sup','wh','paid-held',2,2000,200,'m1');
   INSERT INTO inventory_balances VALUES('b1','prod1','10000000-0000-0000-0000-000000000001','sup','supplier','wh','bin','bin:departed','sellable',0),('b2','prod2','10000000-0000-0000-0000-000000000002','sup','supplier','wh','bin','bin:paid-held','sellable',1);
   INSERT INTO orders VALUES('o1','shipped','paid',NOW()),('o2','pending','paid',NULL);
   INSERT INTO order_inventory_captures(order_id,order_item_id,product_id,variant_id,quantity,allocations) VALUES('o1','oi1','prod1','10000000-0000-0000-0000-000000000001',1,'[{"balanceId":"b1","quantity":1}]'),('o2','oi2','prod2','10000000-0000-0000-0000-000000000002',1,'[{"balanceId":"b2","quantity":1}]');
   `
			if _, err = conn.ExecContext(ctx, fixture); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = conn.ExecContext(ctx, string(up)); err != nil {
			t.Fatal(err)
		}
		if !seed {
			if _, err = conn.ExecContext(ctx, string(down)); err != nil {
				t.Fatal(err)
			}
			if _, err = conn.ExecContext(ctx, string(up)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		for _, row := range []struct {
			bin, status string
			q, v, w     int
		}{{"departed", "disputed", 1, 1000, 100}, {"unknown", "disputed", 4, 123, 456}, {"paid-held", "verified", 2, 2000, 200}} {
			var status string
			var q, v, w int
			if err = conn.QueryRowContext(ctx, `SELECT capacity_status,quantity,occupied_volume_cm3,occupied_weight_grams FROM warehouse_bins WHERE bin_code=$1`, row.bin).Scan(&status, &q, &v, &w); err != nil || status != row.status || q != row.q || v != row.v || w != row.w {
				t.Errorf("history bin=%s status=%s qty/volume/weight=%d/%d/%d err=%v", row.bin, status, q, v, w, err)
			}
		}
		var events int
		if err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM warehouse_departure_events`).Scan(&events); err != nil || events != 0 {
			t.Fatal("migration invented historical departures")
		}
		if _, err = conn.ExecContext(ctx, `UPDATE warehouse_bins SET capacity_status='verified' WHERE bin_code='unknown'`); err == nil {
			t.Fatal("capacity verification silently cleared unknown occupancy")
		}
		if _, err = conn.ExecContext(ctx, string(down)); err == nil {
			t.Fatal("downgrade discarded retained exceptions")
		}
	}
	var after int
	if err = db.QueryRow(`SELECT COUNT(*) FROM public.warehouse_occupancy_exceptions`).Scan(&after); err != nil || after != originalExceptions {
		t.Fatalf("upgrade rehearsal changed original fixture evidence=%d/%d err=%v", originalExceptions, after, err)
	}
}
