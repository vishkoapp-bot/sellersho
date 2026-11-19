package repository

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestQUAL01WarehouseReadyQueuesCityScope(t *testing.T) {
	db := gap13QueueSetup(t) // Requires an explicitly named disposable database.
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`ALTER TABLE order_items ALTER COLUMN id TYPE varchar(255) USING id::text;
		CREATE TABLE users (id text PRIMARY KEY, warehouse_city text, is_active boolean, warehouse_city_confirmed boolean);
		CREATE TABLE warehouses (id text PRIMARY KEY, city_name text);
		CREATE TABLE inventory_balances (id text PRIMARY KEY, variant_id uuid, location_key text, location_type text);
		INSERT INTO users VALUES ('staff-a', ' City A ', TRUE, TRUE), ('staff-b', 'City B', TRUE, TRUE), ('blank', ' ', TRUE, TRUE),
			('inactive', 'City A', FALSE, TRUE), ('unconfirmed', 'City A', TRUE, FALSE);
		INSERT INTO warehouses VALUES ('a1', 'City A'), ('a2', ' City A '), ('b1', 'City B'), ('blank-site', ' ');
		INSERT INTO products (id, title, fulfillment_method) VALUES ('p', 'fixture', 'warehouse');`)

	type leg struct{ kind, site string }
	a1, a2, b1 := leg{"warehouse", "a1"}, leg{"warehouse", "a2"}, leg{"warehouse", "b1"}
	type line struct {
		quantity int
		legs     []leg
	}
	cases := []struct {
		name  string
		lines []line
	}{
		{"same-city", []line{{3, []leg{a1, a2, {"supplier", "supplier-site"}}}}},
		{"other-city", []line{{1, []leg{b1}}}},
		{"mixed-city", []line{{1, []leg{a1}}, {1, []leg{b1}}}},
		{"unknown-line", []line{{1, []leg{a1}}, {1, nil}}},
		{"unknown-site", []line{{2, []leg{a1, {"warehouse", "missing"}}}}},
		{"blank-site", []line{{2, []leg{a1, {"warehouse", "blank-site"}}}}},
		{"partial", []line{{2, []leg{a1}}}},
		{"legacy", []line{{2, []leg{a1, {"legacy", "a1"}}}}},
		{"no-work", []line{{1, []leg{{"supplier", "supplier-site"}}}}},
	}
	r := NewWarehouseRepository(db)
	base := context.Background()
	for _, queue := range []struct {
		status string
		read   func(context.Context) ([]ReadyToPickOrder, error)
	}{{"processing", r.GetOrdersReadyToPick}, {"picked", r.GetOrdersReadyToPack}, {"packed", r.GetOrdersReadyToShip}} {
		t.Run(queue.status, func(t *testing.T) {
			all := map[string]bool{}
			if queue.status == "processing" {
				// gap13QueueSetup also seeds three unscoped pick orders.
				all["ord-a"], all["ord-b"], all["ord-c"] = true, true, true
			}
			for _, tc := range cases {
				id := queue.status + "-" + tc.name
				all[id] = true
				exec(`INSERT INTO orders (id, order_number, store_name, customer_full_name, status, payment_status, fulfillment_method, customer_city)
					VALUES ($1,$1,'fixture','fixture',$2,'paid','mixed','City A')`, id, queue.status)
				for _, item := range tc.lines {
					itemID := uuid.NewString()
					exec(`INSERT INTO order_items (id,order_id,product_id,quantity) VALUES ($1,$2,'p',$3)`, itemID, id, item.quantity)
					for i, origin := range item.legs {
						exec(`INSERT INTO order_item_origins (id,order_id,order_item_id,leg_index,origin_type,warehouse_id,supplier_warehouse_id,quantity)
							VALUES ($1,$2,$3,$4,$5,CASE WHEN $5='warehouse' THEN $6 END,CASE WHEN $5='supplier' THEN $6 END,1)`,
							uuid.NewString(), id, itemID, i, origin.kind, origin.site)
					}
				}
			}
			for _, check := range []struct {
				staff string
				want  map[string]bool
			}{
				{"staff-a", map[string]bool{queue.status + "-same-city": true}},
				{"staff-b", map[string]bool{queue.status + "-other-city": true}},
				{"blank", map[string]bool{}},
				{"inactive", map[string]bool{}},
				{"unconfirmed", map[string]bool{}},
				{"missing", map[string]bool{}},
				{"", all},
			} {
				ctx := base
				if check.staff != "" {
					ctx = WithWarehouseStaffID(ctx, check.staff)
				}
				rows, err := queue.read(ctx)
				if err != nil {
					t.Fatal(err)
				}
				got := map[string]bool{}
				for _, row := range rows {
					got[row.ID] = true
				}
				if !reflect.DeepEqual(got, check.want) || len(rows) != len(check.want) {
					t.Errorf("staff %q: got %v, want %v", check.staff, got, check.want)
				}
			}
		})
	}
}
