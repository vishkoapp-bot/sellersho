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
	"os"
	"testing"

	_ "github.com/lib/pq"
)

func gap13QueueSetup(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GAP13_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GAP13_TEST_DATABASE_URL not set; skipping disposable-DB gap13 queue test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ddl := []string{
		`DROP TABLE IF EXISTS order_item_origins, order_items, orders, products, warehouse_bins, supplier_shipping_methods, suppliers CASCADE`,
		`CREATE TABLE products (id TEXT PRIMARY KEY, title TEXT, supplier_id TEXT, fulfillment_method TEXT)`,
		`CREATE TABLE orders (id TEXT PRIMARY KEY, order_number TEXT, store_name TEXT, customer_full_name TEXT, total_amount BIGINT NOT NULL DEFAULT 0, status TEXT, fulfillment_method TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), discount_amount BIGINT NOT NULL DEFAULT 0, shipping_cost BIGINT NOT NULL DEFAULT 0, payable_amount BIGINT NOT NULL DEFAULT 0, payment_status TEXT, customer_phone TEXT, customer_province TEXT, customer_city TEXT, customer_address TEXT, customer_postal_code TEXT, tracking_code TEXT)`,
		`CREATE TABLE order_items (id UUID PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT, final_unit_price BIGINT)`,
		`CREATE TABLE order_item_origins (id TEXT PRIMARY KEY, order_id TEXT, order_item_id UUID, leg_index INT, origin_type TEXT, supplier_id TEXT, warehouse_id TEXT, supplier_warehouse_id TEXT, origin_label TEXT, quantity INT)`,
		`CREATE TABLE warehouse_bins (bin_code TEXT, product_id TEXT, updated_at TIMESTAMPTZ)`,
		`CREATE TABLE suppliers (id TEXT PRIMARY KEY, owner_user_id TEXT)`,
		`CREATE TABLE supplier_shipping_methods (owner_user_id TEXT NOT NULL, method TEXT NOT NULL, status TEXT NOT NULL)`,
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
