package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func openCancellationDB(t *testing.T) *sql.DB {
	t.Helper()
	raw := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	base, err := testdb.Open(t, raw)
	if err != nil {
		t.Fatal(err)
	}
	schema := "inv01_" + uuid.NewString()[:8]
	if _, err = base.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	db, err := testdb.Open(t, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); base.Close() })
	for _, ddl := range []string{
		`CREATE TABLE orders(id text PRIMARY KEY,order_number text,customer_user_id uuid,status text,payment_status text,inventory_captured bool,shipped_at timestamptz,updated_at timestamptz,fulfillment_method text,tracking_code text,tracking_number text,carrier text,store_id text)`,
		`CREATE TABLE products(id text PRIMARY KEY,stock int)`,
		`CREATE TABLE order_items(id text PRIMARY KEY,order_id text,product_id text,quantity int,variant_id uuid)`,
		`CREATE TABLE order_inventory_captures(order_item_id text PRIMARY KEY,order_id text,product_id text,variant_id uuid,quantity int,variant_quantity int,restored_quantity int,inventory_mode text,allocations jsonb)`,
		`CREATE TABLE supplier_shipments(id text PRIMARY KEY,order_id text,supplier_id text,status text,shipped_at timestamptz,delivered_at timestamptz,carrier text,tracking_number text,supplier_notes text,created_at timestamptz DEFAULT now(),updated_at timestamptz)`,
		`CREATE TABLE supplier_shipment_audit_logs(id text,shipment_id text,actor_user_id text,action text,timestamp timestamptz)`,
		`CREATE TABLE notification_outbox(recipient_user_id text,kind text,title text,body text,payload jsonb)`,
		`ALTER TABLE orders ADD COLUMN referrer_seller_id text`,
		`ALTER TABLE products ADD COLUMN supplier_id text DEFAULT '', ADD COLUMN fulfillment_method text DEFAULT 'warehouse'`,
		`CREATE TABLE order_item_origins(id text,order_item_id text,supplier_id text,origin_type text,warehouse_id text,leg_index int,quantity int)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func seedCancellation(t *testing.T, db *sql.DB, id, method string) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO orders(id,order_number,customer_user_id,status,payment_status,inventory_captured,shipped_at,updated_at,fulfillment_method,tracking_code,tracking_number,carrier,store_id) VALUES($1,$1,'00000000-0000-0000-0000-000000000001','packed','paid',true,NULL,now(),'` + method + `',NULL,NULL,NULL,NULL)`,
		`INSERT INTO products(id,stock) VALUES($1,7)`,
		`INSERT INTO order_items VALUES($1,$1,$1,3,NULL)`,
		`INSERT INTO order_inventory_captures VALUES($1,$1,$1,NULL,3,0,0,'legacy','[]')`,
	} {
		if _, err := db.Exec(q, id); err != nil {
			t.Fatal(err)
		}
	}
}
func TestAuditOrderPartiallyShippedCannotRestoreShippedStock(t *testing.T) {
	db := openCancellationDB(t)
	r := NewOrderRepository(db)
	for _, admin := range []bool{false, true} {
		for _, method := range []string{"supplier_direct", "mixed", "warehouse"} {
			id := fmt.Sprintf("%v-%s", admin, method)
			seedCancellation(t, db, id, method)
			if method == "warehouse" {
				_, _ = db.Exec(`UPDATE orders SET shipped_at=now() WHERE id=$1`, id)
			} else {
				_, err := db.Exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,status,shipped_at) VALUES($1,$1,'s1','failed',now()),($1||'-2',$1,'s2','pending',NULL)`, id)
				if err != nil {
					t.Fatal(err)
				}
			}
			err := r.CancelOrder(context.Background(), id, "00000000-0000-0000-0000-000000000001", admin)
			if err == nil {
				t.Errorf("%s: partially dispatched cancellation succeeded", id)
			}
			var stock int
			db.QueryRow(`SELECT stock FROM products WHERE id=$1`, id).Scan(&stock)
			if stock != 7 {
				t.Errorf("%s stock=%d want7", id, stock)
			}
		}
	}
}

func TestCancellationDispatchRaces(t *testing.T) {
	db := openCancellationDB(t)
	if _, err := db.Exec(`INSERT INTO suppliers(id,name) VALUES('s1','Cancellation fixture supplier') ON CONFLICT(id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	// Warehouse storage rows are deliberately present; dispatch exercises the
	// actual storage decrement as well as the shared order guard.
	for _, ddl := range []string{
		`CREATE TABLE warehouse_storage_positions(id text PRIMARY KEY,product_id text,supplier_id text,warehouse_id text,qty int,stocked_at timestamptz,closed_at timestamptz,updated_at timestamptz)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	for _, method := range []string{"mixed", "warehouse"} {
		t.Run(method, func(t *testing.T) {
			seedPosition := func(id string) {
				t.Helper()
				for _, query := range []string{
					`INSERT INTO order_item_origins VALUES($1,$1,'s1','warehouse','wh',0,3)`,
					`INSERT INTO warehouse_storage_positions VALUES($1,$1,'s1','wh',3,now(),NULL,now())`,
				} {
					if _, err := db.Exec(query, id); err != nil {
						t.Fatal(err)
					}
				}
			}
			// Prove dispatch can win in this fixture before testing contention.
			// Otherwise a broken dispatch fixture could let cancellation win
			// every iteration and falsely satisfy the exactly-one-winner check.
			control := "dispatch-control-" + method
			seedCancellation(t, db, control, method)
			if method == "warehouse" {
				seedPosition(control)
				if err := NewWarehouseRepository(db).ShipOrder(context.Background(), control, "post", control); err != nil {
					t.Fatal("warehouse dispatch control", err)
				}
			} else {
				if _, err := db.Exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,status) VALUES($1,$1,'s1','pending')`, control); err != nil {
					t.Fatal(err)
				}
				if err := NewSupplierShipmentRepository(db).SubmitShipping(context.Background(), "s1", control, "post", control, "actor"); err != nil {
					t.Fatal("supplier dispatch control", err)
				}
			}
			if err := NewOrderRepository(db).CancelOrder(context.Background(), control, "", true); !errors.Is(err, ErrOrderPartiallyDispatched) {
				t.Fatalf("dispatched control cancellation=%v", err)
			}
			for i := 0; i < 100; i++ {
				id := fmt.Sprintf("race-%s-%d", method, i)
				seedCancellation(t, db, id, method)
				if method == "warehouse" {
					seedPosition(id)
				}
				if _, err := db.Exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,status) VALUES($1,$1,'s1','pending')`, id); err != nil {
					t.Fatal(err)
				}
				if method == "warehouse" {
					if _, err := db.Exec(`UPDATE supplier_shipments SET status='shipped',shipped_at=now() WHERE id=$1`, id); err != nil {
						t.Fatal(err)
					} /* warehouse-only has no direct leg */
					if _, err := db.Exec(`DELETE FROM supplier_shipments WHERE id=$1`, id); err != nil {
						t.Fatal(err)
					}
				}
				start := make(chan struct{})
				results := make(chan error, 2)
				go func() {
					<-start
					results <- NewOrderRepository(db).CancelOrder(context.Background(), id, "00000000-0000-0000-0000-000000000001", i%2 == 0)
				}()
				go func() {
					<-start
					if method == "mixed" {
						results <- NewSupplierShipmentRepository(db).SubmitShipping(context.Background(), "s1", id, "post", id, "actor")
					} else {
						results <- NewWarehouseRepository(db).ShipOrder(context.Background(), id, "post", id)
					}
				}()
				close(start)
				a, b := <-results, <-results
				for _, err := range []error{a, b} {
					if err != nil && !errors.Is(err, ErrShipmentState) && !errors.Is(err, ErrOrderPartiallyDispatched) {
						t.Fatalf("unexpected race error: %v", err)
					}
				}
				if (a == nil) == (b == nil) {
					t.Fatalf("iteration %d expected exactly one winner: %v / %v", i, a, b)
				}
				var status string
				var stock int
				if err := db.QueryRow(`SELECT o.status,p.stock FROM orders o JOIN products p ON p.id=o.id WHERE o.id=$1`, id).Scan(&status, &stock); err != nil {
					t.Fatal(err)
				}
				if status == "cancelled" && stock != 10 {
					t.Fatalf("cancelled stock=%d", stock)
				}
				if method == "warehouse" {
					var qty int
					if err := db.QueryRow(`SELECT qty FROM warehouse_storage_positions WHERE id=$1`, id).Scan(&qty); err != nil {
						t.Fatal(err)
					}
					want := 0
					if status == "cancelled" {
						want = 3
					}
					if qty != want {
						t.Fatalf("position qty=%d want%d", qty, want)
					}
				}
				if status != "cancelled" && stock != 7 {
					t.Fatalf("dispatched status=%s stock=%d", status, stock)
				}
			}
		})
	}
}
func TestCancellationRestoreReplayAndPartialReturn(t *testing.T) {
	db := openCancellationDB(t)
	r := NewOrderRepository(db)
	seedCancellation(t, db, "replay", "warehouse")
	for i := 0; i < 2; i++ {
		if err := r.CancelOrder(context.Background(), "replay", "", true); err != nil {
			t.Fatal(err)
		}
	}
	var stock int
	if err := db.QueryRow(`SELECT stock FROM products WHERE id='replay'`).Scan(&stock); err != nil || stock != 10 {
		t.Fatalf("replay stock=%d err=%v", stock, err)
	}
	seedCancellation(t, db, "fully-returned", "warehouse")
	if _, err := db.Exec(`UPDATE order_inventory_captures SET restored_quantity=quantity WHERE order_id='fully-returned'; UPDATE products SET stock=10 WHERE id='fully-returned'`); err != nil {
		t.Fatal(err)
	}
	if err := r.CancelOrder(context.Background(), "fully-returned", "", true); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock FROM products WHERE id='fully-returned'`).Scan(&stock); err != nil || stock != 10 {
		t.Fatalf("fully returned double restore: %d %v", stock, err)
	}
	seedCancellation(t, db, "returned", "warehouse")
	if _, err := db.Exec(`UPDATE order_inventory_captures SET restored_quantity=1 WHERE order_id='returned'`); err != nil {
		t.Fatal(err)
	}
	if err := r.CancelOrder(context.Background(), "returned", "", true); err == nil {
		t.Fatal("partial returned cancellation should fail closed")
	}
	var status string
	if err := db.QueryRow(`SELECT o.status,p.stock FROM orders o JOIN products p ON p.id=o.id WHERE o.id='returned'`).Scan(&status, &stock); err != nil || status != "packed" || stock != 7 {
		t.Fatalf("partial return mutated status=%s stock=%d err=%v", status, stock, err)
	}
}

func TestCancelledOrderRejectsAlternateDispatchWriters(t *testing.T) {
	db := openCancellationDB(t)
	seedCancellation(t, db, "alternates", "supplier_direct")
	if _, err := db.Exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,status,tracking_number) VALUES('leg','alternates','s','pending','track')`); err != nil {
		t.Fatal(err)
	}
	if err := NewOrderRepository(db).CancelOrder(context.Background(), "alternates", "", true); err != nil {
		t.Fatal(err)
	}
	shipments := NewSupplierShipmentRepository(db)
	for _, attempt := range []func() error{
		func() error { return shipments.MarkShipped(context.Background(), "leg", "post", "track", nil) },
		func() error {
			return shipments.MarkShippedForSupplier(context.Background(), "s", "leg", "post", "track", nil)
		},
		func() error {
			_, err := shipments.UpdateShipmentStatus(context.Background(), "track", "shipped")
			return err
		},
		func() error {
			return NewOrderRepository(db).UpdateOrderFulfillment(context.Background(), "alternates", "shipped", "track")
		},
	} {
		if err := attempt(); !errors.Is(err, ErrShipmentState) {
			t.Fatalf("alternate dispatch after cancellation: %v", err)
		}
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM supplier_shipments WHERE id='leg'`).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("shipment changed: %s %v", status, err)
	}
}

func TestCarrierFailurePreservesLegacyDispatchEvidence(t *testing.T) {
	db := openCancellationDB(t)
	seedCancellation(t, db, "legacy-failure", "mixed")
	if _, err := db.Exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,status,tracking_number) VALUES('leg','legacy-failure','s','shipped','track')`); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSupplierShipmentRepository(db).UpdateShipmentStatus(context.Background(), "track", "failed"); err != nil {
		t.Fatal(err)
	}
	if err := NewOrderRepository(db).CancelOrder(context.Background(), "legacy-failure", "", true); !errors.Is(err, ErrOrderPartiallyDispatched) {
		t.Fatalf("failure erased custody evidence: %v", err)
	}
}
