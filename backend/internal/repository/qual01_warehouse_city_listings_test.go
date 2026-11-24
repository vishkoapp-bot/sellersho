package repository

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
)

func TestQUAL01WarehouseCityListings(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `
		SET search_path = pg_temp;
		CREATE TEMP TABLE users (id text PRIMARY KEY, warehouse_city text DEFAULT 'Tehran',
			is_active bool DEFAULT true, warehouse_city_confirmed bool DEFAULT false);
		CREATE TEMP TABLE warehouses (id text PRIMARY KEY, name text, city_name text);
		CREATE TEMP TABLE supplier_warehouses (LIKE warehouses INCLUDING ALL);
		CREATE TEMP TABLE products (id text PRIMARY KEY, title text);
		CREATE TEMP TABLE warehouse_bins (
			bin_code text PRIMARY KEY, warehouse_id text, warehouse_name text,
			hall_name text DEFAULT '', aisle text DEFAULT '', shelf text DEFAULT '', capacity_percent int DEFAULT 0,
			product_id text, quantity int DEFAULT 0, updated_at timestamptz DEFAULT now(),
			capacity_volume_cm3 bigint, occupied_volume_cm3 bigint, max_weight_grams bigint,
			occupied_weight_grams bigint, capacity_status text DEFAULT 'unverified',
			position_x float8, position_y float8, position_z float8);
		CREATE TEMP TABLE putaway_capacity_reservations (bin_code text, status text, volume_cm3 bigint);
		CREATE TEMP TABLE customer_returns (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(), warehouse_id text, destination_supplier_warehouse_id text,
			order_id text DEFAULT 'order', customer_id uuid DEFAULT '00000000-0000-0000-0000-000000000001',
			status text DEFAULT 'approved',
			reason text DEFAULT '', admin_note text DEFAULT '', prepaid bool, tracking_number text,
			shipped_at timestamptz, return_code text DEFAULT '', return_shipping_amount bigint DEFAULT 0,
			return_shipping_bearer text DEFAULT '', created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now());
		CREATE TEMP TABLE return_items (id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			return_id uuid NOT NULL REFERENCES customer_returns(id), order_item_id text,
			variant_id uuid, qty int, qc_status text, received_at timestamptz);
		CREATE TEMP TABLE warehouse_customer_return_bin_sources (return_id uuid, source_proven bool, available_quantity int);
		INSERT INTO users VALUES ('staff', ' Tehran ', true, true), ('blank', '   ', true, true),
			('defaulted', DEFAULT, true, DEFAULT), ('inactive', 'Tehran', false, true);
		INSERT INTO warehouses VALUES ('a', 'Site A', 'Tehran'), ('b', 'Site B', ' Tehran '),
			('c', 'Site C', 'Shiraz'), ('d', 'Site D', NULL), ('e', 'Site E', 'tehran'), ('f', 'Site F', '   ');
		INSERT INTO supplier_warehouses VALUES ('s1', 'Supplier A', 'Tehran'),
			('s2', 'Supplier B', 'Shiraz'), ('s3', 'Supplier C', '   ');
		INSERT INTO warehouse_bins (bin_code, warehouse_id, warehouse_name) VALUES
			('a','a','Site A'), ('b','b','Site B'), ('foreign','c','Site A'), ('null-city','d','Site A'),
			('case','e','Site A'), ('unknown','missing','Site A'), ('legacy',NULL,'Site A');
	`)
	if err != nil {
		t.Fatal(err)
	}
	// The paged repository batches return items by UUID as in the live
	// schema. Keep readable case names separate from persisted identifiers.
	returnIDs := map[string]string{}
	for _, fixture := range []struct{ name, warehouse, supplierWarehouse, status string }{
		{"a", "a", "", "approved"},
		{"b", "b", "", "received"},
		{"foreign", "c", "", "approved"},
		{"blank-city", "f", "", "approved"},
		{"case", "e", "", "approved"},
		{"legacy", "", "", "approved"},
		{"supplier-own", "", "s1", "approved"},
		{"supplier-foreign", "a", "s2", "approved"},
		{"supplier-unknown", "a", "missing", "approved"},
		{"supplier-blank", "a", "s3", "approved"},
		{"refunded", "a", "", "refunded"},
		{"refunded-empty", "a", "", "refunded"},
		{"pending", "a", "", "pending"},
	} {
		var id string
		err := db.QueryRowContext(ctx, `INSERT INTO customer_returns
			(warehouse_id, destination_supplier_warehouse_id, status)
			VALUES (NULLIF($1, ''), NULLIF($2, ''), $3) RETURNING id`,
			fixture.warehouse, fixture.supplierWarehouse, fixture.status).Scan(&id)
		if err != nil {
			t.Fatalf("seed return %s: %v", fixture.name, err)
		}
		returnIDs[fixture.name] = id
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO warehouse_customer_return_bin_sources VALUES ($1, true, 1)`, returnIDs["refunded"]); err != nil {
		t.Fatal(err)
	}
	var returnItemID string
	if err := db.QueryRowContext(ctx, `INSERT INTO return_items (return_id, order_item_id, qty, qc_status)
		VALUES ($1, 'order-line', 2, 'pending') RETURNING id`, returnIDs["a"]).Scan(&returnItemID); err != nil {
		t.Fatal(err)
	}
	bins := &WMSRepository{db: db}
	orders := &OrderRepository{db: db}
	checkBins := func(t *testing.T, ctx context.Context, want ...string) {
		t.Helper()
		rows, err := bins.GetBins(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, row := range rows {
			got = append(got, row.BinCode)
		}
		if !slices.Equal(got, want) {
			t.Errorf("bins = %v, want %v", got, want)
		}
	}
	checkReturns := func(t *testing.T, ctx context.Context, dest string, want ...string) {
		t.Helper()
		rows, err := orders.ListCustomerReturnsForWarehouse(ctx, dest)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, row := range rows {
			got = append(got, row.ID)
			if row.ID == returnIDs["a"] && (len(row.Items) != 1 || row.Items[0].ID != returnItemID || row.Items[0].Qty != 2) {
				t.Errorf("return a items = %+v, want its persisted UUID item with quantity 2", row.Items)
			}
		}
		var expected []string
		for _, name := range want {
			id, ok := returnIDs[name]
			if !ok {
				t.Fatalf("unknown return fixture %q", name)
			}
			expected = append(expected, id)
		}
		slices.Sort(got)
		slices.Sort(expected)
		if !slices.Equal(got, expected) {
			t.Errorf("returns for %q = %v, want cases %v with IDs %v", dest, got, want, expected)
		}
	}
	staff := WithWarehouseStaffID(ctx, "staff")
	checkBins(t, staff, "a", "b")
	checkReturns(t, staff, "", "a", "b", "refunded")
	checkReturns(t, staff, "a", "a", "refunded")
	checkReturns(t, staff, "s1")
	checkReturns(t, staff, "c")
	for _, id := range []string{"blank", "missing", "defaulted", "inactive"} {
		t.Run(id, func(t *testing.T) {
			staff := WithWarehouseStaffID(ctx, id)
			checkBins(t, staff)
			checkReturns(t, staff, "")
		})
	}
	checkBins(t, ctx, "a", "b", "case", "foreign", "legacy", "null-city", "unknown")
	checkReturns(t, ctx, "", "a", "b", "blank-city", "case", "foreign", "legacy", "refunded", "supplier-blank", "supplier-foreign", "supplier-own", "supplier-unknown")
	checkReturns(t, ctx, "a", "a", "refunded", "supplier-blank", "supplier-foreign", "supplier-unknown")
}
