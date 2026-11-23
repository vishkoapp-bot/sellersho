package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestQUAL01MixedRoleCustomerReturnScope(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `
		SET search_path = pg_temp;
		CREATE TEMP TABLE users (id text PRIMARY KEY, warehouse_city text, is_active bool, warehouse_city_confirmed bool);
		CREATE TEMP TABLE warehouses (id text PRIMARY KEY, name text, city_name text);
		CREATE TEMP TABLE customer_returns (
			id uuid PRIMARY KEY, warehouse_id text, destination_supplier_warehouse_id text,
			order_id text DEFAULT 'order', customer_id text DEFAULT 'customer', status text DEFAULT 'approved',
			reason text DEFAULT '', admin_note text DEFAULT '', prepaid bool, tracking_number text,
			shipped_at timestamptz, return_code text DEFAULT '', return_shipping_amount bigint DEFAULT 0,
			return_shipping_bearer text DEFAULT '', created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now());
		CREATE TEMP TABLE return_items (id uuid, return_id uuid, order_item_id text DEFAULT 'item', variant_id text, qty int, qc_status text, received_at timestamptz);
		CREATE TEMP TABLE orders(id text PRIMARY KEY);
		CREATE TEMP TABLE suppliers(id text PRIMARY KEY);
		CREATE TEMP TABLE products(id text PRIMARY KEY, supplier_id text);
		CREATE TEMP TABLE order_items(id text PRIMARY KEY, product_id text);
		CREATE TEMP TABLE order_inventory_captures(order_item_id text, variant_id uuid);
		CREATE TEMP TABLE product_units (LIKE public.product_units INCLUDING ALL);
		CREATE TEMP TABLE inventory_movements (LIKE public.inventory_movements INCLUDING ALL);
		CREATE TEMP TABLE inventory_balances(id text PRIMARY KEY, product_id text, supplier_id text,
			variant_id uuid, location_key text, status text, location_type text, owner_type text,
			quantity int, unit_tracked bool DEFAULT false);
		INSERT INTO orders VALUES('order');
		INSERT INTO suppliers VALUES('supplier');
		INSERT INTO products VALUES('product','supplier');
		INSERT INTO order_items VALUES('item','product');
		INSERT INTO inventory_balances VALUES('source','product','supplier',NULL,'return:scope','sellable','warehouse','supplier',1,false),
			('target','product','supplier',NULL,'bin:bin','sellable','bin','supplier',1,false);
		CREATE TEMP TABLE warehouse_customer_return_bin_sources (return_id uuid, return_item_id uuid,
			product_id text, variant_id text, product_title text, variant_name text, variant_spnc text,
			warehouse_id text, qc_status text, qty int, source_proven bool, available_quantity int);
		CREATE TEMP TABLE inventory_effective_measurements (product_id text, variant_id text,
			length_mm int, width_mm int, height_mm int, weight_grams int, verified bool, revision text, source text);
		CREATE TEMP TABLE warehouse_return_bin_entries (id text, return_id uuid, return_item_id uuid, bin_code text,
			quantity int, reason text, actor_user_id text, idempotency_key text,
			volume_cm3 bigint DEFAULT 10, weight_grams bigint DEFAULT 20, measurement_revision_id text DEFAULT 'revision',
			source_balance_id text DEFAULT 'source', variant_id text DEFAULT '',
			received_at timestamptz DEFAULT now(), UNIQUE(actor_user_id,idempotency_key));
		INSERT INTO users VALUES ('staff',' Tehran ',true,true),('blank',' ',true,true),
			('unconfirmed','Tehran',true,false),('inactive','Tehran',false,true);
		INSERT INTO warehouses VALUES ('local','Local','Tehran'),('remote','Remote','Shiraz'),('empty','Empty','');`)
	if err != nil {
		t.Fatal(err)
	}
	orders := &OrderRepository{db: db}
	warehouse := &WarehouseRepository{db: db}
	staff := WithWarehouseStaffID(ctx, "staff")
	for _, tc := range []struct {
		name     string
		site     any
		supplier any
		scoped   context.Context
		want     bool
	}{
		{"same city", "local", nil, staff, true},
		{"different city", "remote", nil, staff, false},
		{"supplier destination", "local", "supplier", staff, false},
		{"missing destination", nil, nil, staff, false},
		{"unknown destination", "unknown", nil, staff, false},
		{"blank staff city", "empty", nil, WithWarehouseStaffID(ctx, "blank"), false},
		{"unconfirmed staff city", "local", nil, WithWarehouseStaffID(ctx, "unconfirmed"), false},
		{"inactive confirmed staff", "local", nil, WithWarehouseStaffID(ctx, "inactive"), false},
		{"unknown staff", "local", nil, WithWarehouseStaffID(ctx, "unknown"), false},
		{"nonstaff", "remote", nil, ctx, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, item, key := uuid.NewString(), uuid.NewString(), uuid.NewString()
			if _, err := db.ExecContext(ctx, `INSERT INTO customer_returns(id,warehouse_id,destination_supplier_warehouse_id) VALUES ($1,$2,$3)`, id, tc.site, tc.supplier); err != nil {
				t.Fatal(err)
			}
			// No money tables exist in this fixture. Out-of-scope requests must
			// stop at authorization before inventory discovery or finance locking.
			ret, err := orders.GetCustomerReturn(tc.scoped, id)
			if err != nil || (ret != nil) != tc.want {
				t.Fatalf("detail found=%t, err=%v, want found=%t", ret != nil, err, tc.want)
			}
			_, err = warehouse.ListCustomerReturnBinSources(tc.scoped, id)
			if (tc.want && err != nil) || (!tc.want && !errors.Is(err, sql.ErrNoRows)) {
				t.Fatalf("bin sources: %v", err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO return_items(id,return_id) VALUES ($1,$2)`, item, id); err != nil {
				t.Fatal(err)
			}
			place := func(scope context.Context) (ReturnBinPlacementResult, error) {
				return warehouse.PlaceCustomerReturnItemInBin(scope, id, item, "bin", 1, "reason", "staff", key)
			}
			if !tc.want {
				if _, err := db.ExecContext(ctx, `ALTER TABLE order_items RENAME TO hidden_order_items`); err != nil {
					t.Fatal(err)
				}
			}
			first, err := place(tc.scoped)
			if !tc.want {
				if _, restoreErr := db.ExecContext(ctx, `ALTER TABLE hidden_order_items RENAME TO order_items`); restoreErr != nil {
					t.Fatal(restoreErr)
				}
			}
			if tc.want {
				second, replayErr := place(tc.scoped)
				if err != nil || replayErr != nil || first.ID == "" || first.ID != second.ID {
					t.Fatalf("putaway or replay: %v, %v", err, replayErr)
				}
			} else {
				if !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("out-of-scope putaway: %v", err)
				}
				_, err = orders.ReceiveCustomerReturn(tc.scoped, id, []CustomerReturnItemQC{{ReturnItemID: item, QCStatus: "ok"}}, "staff")
				if !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("out-of-scope receive: %v", err)
				}
				// An already-recorded request must also remain invisible.
				if _, err := place(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err := place(tc.scoped); !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("out-of-scope replay: %v", err)
				}
			}
		})
	}
}
