package repository

import (
	"context"
	"os"
	"testing"

	"github.com/drup/platform/internal/testdb"
	"github.com/drup/platform/internal/warehouseopt"
)

func TestQUAL01WarehouseOverviewAndRoute(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	exec := func(sql string) {
		t.Helper()
		if _, err := db.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`SET search_path=pg_temp;
		CREATE TEMP TABLE users(id text PRIMARY KEY,warehouse_city text,is_active bool,warehouse_city_confirmed bool);
		CREATE TEMP TABLE warehouses(id text PRIMARY KEY,name text,city_name text);
		CREATE TEMP TABLE warehouse_bins(bin_code text PRIMARY KEY,warehouse_id text,warehouse_name text,capacity_volume_cm3 bigint,occupied_volume_cm3 bigint,product_id text,quantity int,position_x float8,position_y float8,position_z float8,updated_at timestamptz DEFAULT now());
		CREATE TEMP TABLE putaway_capacity_reservations(bin_code text,status text,volume_cm3 bigint);
		CREATE TEMP TABLE inbound_shipments(id text,warehouse_id text,status text);
		CREATE TEMP TABLE inbound_inventory_lines(id text,warehouse_id text,product_id text);
		CREATE TEMP TABLE inbound_putaway_allocations(task_id text,warehouse_id text,product_id text,source_location_key text);
		CREATE TEMP TABLE inventory_movements(reference_type text,reference_id text,product_id text,from_location_key text,to_status text);
		CREATE TEMP TABLE orders(id text PRIMARY KEY,order_number text,store_name text DEFAULT '',customer_full_name text DEFAULT '',total_amount bigint DEFAULT 0,status text,fulfillment_method text,created_at timestamptz DEFAULT now(),discount_amount bigint DEFAULT 0,shipping_cost bigint DEFAULT 0,payable_amount bigint DEFAULT 0,payment_status text,customer_phone text,customer_province text,customer_city text,customer_address text,customer_postal_code text,tracking_code text);
		CREATE TEMP TABLE order_items(id text PRIMARY KEY,order_id text,product_id text,variant_id uuid,variant_name text,variant_spnc text,quantity int,final_unit_price bigint);
		CREATE TEMP TABLE products(id text,title text,fulfillment_method text);
		CREATE TEMP TABLE product_variants(id uuid,inventory_mode text);
		CREATE TEMP TABLE order_item_origins(order_id text,order_item_id text,origin_type text,warehouse_id text,supplier_warehouse_id text,quantity int);
		CREATE TEMP TABLE inventory_balances(id text,product_id text,variant_id uuid,warehouse_id text,supplier_warehouse_id text,location_type text,location_key text,bin_code text,status text,quantity int);
		CREATE TEMP TABLE order_inventory_captures(order_item_id text,product_id text,allocations jsonb);
		INSERT INTO users VALUES ('active',' City A ',true,true),('inactive','City A',false,true),('unconfirmed','City A',true,false);
		INSERT INTO warehouses VALUES ('a','Site A','City A'),('a2','Site A2','City A'),('b','Site B','City B');
		INSERT INTO warehouse_bins VALUES ('a-bin','a','stale',100,20,'p',1,5,0,0),('a2-bin','a2','Site A',50,10,'p',4,2,0,0),('b-bin','b','Site A',1000,500,'p',9,1,0,0),('unknown',NULL,'Site A',1000,500,'p',9,1,0,0);
		INSERT INTO putaway_capacity_reservations VALUES ('a-bin','active',10),('b-bin','active',99),('a-bin','released',99);
		INSERT INTO inbound_shipments VALUES ('ia','a','pending'),('ib','b','receiving'),('ix',NULL,'pending');
		INSERT INTO inbound_inventory_lines VALUES ('la','a','p'),('lb','b','p');
		INSERT INTO inbound_putaway_allocations VALUES ('pa','a','p','qc:la');
		INSERT INTO inventory_movements VALUES ('inbound_transfer','la','p','inbound-line:la','quarantine'),('inbound_transfer','lb','p','inbound-line:lb','quarantine'),('inbound_qc_disposition','qc','p','inbound-line:la','qc_accepted'),('putaway_confirmed','pa','p','qc:la','sellable'),('qc_pass','unattributed','p','legacy','qc_accepted');
		INSERT INTO orders(id,order_number,status,fulfillment_method,payment_status) VALUES ('order-a','A','processing','warehouse','paid'),('order-b','B','processing','warehouse','paid');
		INSERT INTO products VALUES ('p','fixture','warehouse');
		INSERT INTO order_items(id,order_id,product_id,quantity) VALUES ('item-a','order-a','p',2),('item-b','order-b','p',1);
		INSERT INTO order_item_origins VALUES ('order-a','item-a','warehouse','a',NULL,1),('order-a','item-a','warehouse','a2',NULL,1),('order-b','item-b','warehouse','b',NULL,1);
		INSERT INTO inventory_balances VALUES ('balance-a','p',NULL,'a',NULL,'bin','bin:a-bin',NULL,'sellable',1),('balance-a2','p',NULL,'a2',NULL,'bin','bin:a2-bin',NULL,'sellable',4),('balance-b','p',NULL,'b',NULL,'bin','bin:b-bin',NULL,'sellable',9);
		INSERT INTO order_inventory_captures VALUES ('item-a','p','[{"balanceId":"balance-a","quantity":1},{"balanceId":"balance-a2","quantity":1},{"balanceId":"balance-b","quantity":1}]');`)

	ctx := func(id string) context.Context { return WithWarehouseStaffID(context.Background(), id) }
	wms := NewWMSRepository(db)
	assertOverview := func(id string, want WarehouseOverview) {
		t.Helper()
		got, err := wms.GetOverview(ctx(id))
		if err != nil || *got != want {
			t.Fatalf("overview(%q) = %+v, %v, want %+v", id, got, err, want)
		}
	}
	assertOverview("active", WarehouseOverview{WarehouseName: "City A", CapacityVolumeCm3: 150, OccupiedVolumeCm3: 30, ReservedVolumeCm3: 10, CapacityPercent: 26, InboundPending: 1, VerifiedMovements: 3})
	repo := NewWarehouseRepository(db)
	for _, id := range []string{"inactive", "unconfirmed", "unknown"} {
		assertOverview(id, WarehouseOverview{})
		if _, err := repo.OptimizePicking(ctx(id), nil, "Site A", warehouseopt.Point{}); err == nil {
			t.Fatalf("unauthorized actor %q received a route", id)
		}
	}
	assertOwnRoute := func() {
		t.Helper()
		got, err := repo.OptimizePicking(ctx("active"), nil, "Site A", warehouseopt.Point{})
		if err != nil || len(got.Stops) != 1 || got.Stops[0].ID != "a-bin" || len(got.MissingBins) != 0 {
			t.Fatalf("route = %+v, %v, want a-bin only", got, err)
		}
	}
	assertOwnRoute()
	exec(`DELETE FROM order_inventory_captures`)
	assertOwnRoute()
	for _, request := range []struct {
		site string
		ids  []string
	}{{"Site B", nil}, {"Site A", []string{"order-b"}}} {
		if _, err := repo.OptimizePicking(ctx("active"), request.ids, request.site, warehouseopt.Point{}); err == nil {
			t.Fatalf("foreign request %+v received a route", request)
		}
	}
	exec(`INSERT INTO warehouses VALUES ('duplicate','Site A','City B')`)
	if _, err := repo.OptimizePicking(ctx("active"), nil, "Site A", warehouseopt.Point{}); err == nil {
		t.Fatal("duplicate exact warehouse name did not fail closed")
	}
}
