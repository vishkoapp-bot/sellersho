package main

// Critic fix 1 (Phase 2): end-to-end carrier-webhook coverage for the
// supplier-direct 'failed' path — through handleWebhookTrackingUpdate
// (not just the repo function). Runs ONLY when
// SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL points at a DISPOSABLE
// local Postgres; otherwise skips, like the other DB-gated suites in
// this package.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

const carrierWebhookNotifyDDL = `
DROP TABLE IF EXISTS notification_outbox, supplier_shipment_audit_logs, supplier_shipments, order_items, orders, products, stores, suppliers, users CASCADE;
CREATE TABLE users (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	mobile VARCHAR(15) UNIQUE NOT NULL,
	full_name VARCHAR(100) NOT NULL DEFAULT '',
	role VARCHAR(20) NOT NULL DEFAULT 'customer',
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE suppliers (
	id VARCHAR(50) PRIMARY KEY,
	owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
	name VARCHAR(200) NOT NULL
);
CREATE TABLE stores (
	id VARCHAR(50) PRIMARY KEY,
	name VARCHAR(200) NOT NULL DEFAULT '',
	slug VARCHAR(200) NOT NULL DEFAULT '',
	owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL
);
CREATE TABLE products (
	id VARCHAR(50) PRIMARY KEY,
	supplier_id VARCHAR(50) REFERENCES suppliers(id),
	title VARCHAR(200) NOT NULL DEFAULT '',
	stock INT NOT NULL DEFAULT 0
);
CREATE TABLE orders (
	id VARCHAR(50) PRIMARY KEY,
	customer_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
	order_number VARCHAR(30) NOT NULL DEFAULT '',
	store_id VARCHAR(50) REFERENCES stores(id),
	status VARCHAR(30) NOT NULL DEFAULT 'pending',
	payment_status VARCHAR(30) NOT NULL DEFAULT 'pending',
	payable_amount BIGINT NOT NULL DEFAULT 0,
	fulfillment_method VARCHAR(30) DEFAULT 'supplier_direct',
	shipped_at TIMESTAMPTZ,
	disputed_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE order_items (
	id VARCHAR(50) PRIMARY KEY,
	order_id VARCHAR(50) REFERENCES orders(id) ON DELETE CASCADE,
	product_id VARCHAR(50) REFERENCES products(id),
	quantity INT NOT NULL DEFAULT 1,
	variant_id VARCHAR(50)
);
CREATE TABLE supplier_shipments (
	id VARCHAR(50) PRIMARY KEY,
	order_id VARCHAR(50) REFERENCES orders(id) ON DELETE CASCADE,
	supplier_id VARCHAR(50) REFERENCES suppliers(id),
	status VARCHAR(30) NOT NULL DEFAULT 'pending',
	carrier VARCHAR(100) DEFAULT '',
	tracking_number VARCHAR(100) DEFAULT '',
	shipped_at TIMESTAMPTZ,
	delivered_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE supplier_shipment_audit_logs (
	id VARCHAR(50) PRIMARY KEY,
	shipment_id VARCHAR(50) NOT NULL,
	actor_user_id VARCHAR(50) NOT NULL DEFAULT '',
	action VARCHAR(50) NOT NULL DEFAULT '',
	timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE notification_outbox (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	recipient_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind VARCHAR(50) NOT NULL,
	title VARCHAR(200) NOT NULL,
	body TEXT NOT NULL,
	payload JSONB NOT NULL DEFAULT '{}'::jsonb,
	status VARCHAR(20) NOT NULL DEFAULT 'pending'
		CHECK (status IN ('pending', 'sent', 'read')),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	sent_at TIMESTAMPTZ,
	read_at TIMESTAMPTZ
);
`

func carrierWebhookNotifyDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL not set; skipping disposable-DB integration test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	if _, err := db.Exec(carrierWebhookNotifyDDL); err != nil {
		t.Fatalf("seed DDL: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCarrierWebhookFailedNotifiesCustomerEndToEnd(t *testing.T) {
	db := carrierWebhookNotifyDB(t)
	ctx := context.Background()
	var customerID string
	if err := db.QueryRow(`INSERT INTO users (mobile, full_name, role) VALUES ('09120000020', 'Customer', 'customer') RETURNING id`).Scan(&customerID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	var supplierOwnerID string
	if err := db.QueryRow(`INSERT INTO users (mobile, full_name, role) VALUES ('09120000021', 'Supplier', 'supplier') RETURNING id`).Scan(&supplierOwnerID); err != nil {
		t.Fatalf("seed supplier owner: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO suppliers (id, owner_user_id, name) VALUES ('sup-1', $1, 'Test Supplier')`, supplierOwnerID); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO stores (id, name, slug, owner_user_id) VALUES ('store-1', 'Test Store', 'test-store', $1)`, supplierOwnerID); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO products (id, supplier_id, title, stock) VALUES ('prod-1', 'sup-1', 'تیشرت', 10)`); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	seed := func(orderID, orderNumber, tracking string) {
		if _, err := db.Exec(`INSERT INTO orders (id, customer_user_id, order_number, store_id, status, payment_status, payable_amount, fulfillment_method) VALUES ($1, $2, $3, 'store-1', 'shipped', 'paid', 500000, 'supplier_direct')`, orderID, customerID, orderNumber); err != nil {
			t.Fatalf("seed order %s: %v", orderID, err)
		}
		if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity) VALUES ($1, $2, 'prod-1', 1)`, "item-"+orderID, orderID); err != nil {
			t.Fatalf("seed item %s: %v", orderID, err)
		}
		if _, err := db.Exec(`INSERT INTO supplier_shipments (id, order_id, supplier_id, status, tracking_number) VALUES ($1, $2, 'sup-1', 'shipped', $3)`, "ship-"+orderID, orderID, tracking); err != nil {
			t.Fatalf("seed shipment %s: %v", orderID, err)
		}
	}
	seed("ord-1", "ORD-1", "TRK-FAIL1")
	seed("ord-2", "ORD-2", "TRK-OK2")

	shipmentRepo := repository.NewSupplierShipmentRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	callWebhook := func(tracking, status string) int {
		t.Helper()
		payload, _ := json.Marshal(map[string]string{"trackingNumber": tracking, "status": status})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/tracking/update", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Secret", "test-secret")
		w := httptest.NewRecorder()
		handleWebhookTrackingUpdate(w, req, ctx, shipmentRepo, orderRepo, "test-secret")
		return w.Code
	}
	countKind := func(kind string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM notification_outbox WHERE kind = $1`, kind).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", kind, err)
		}
		return n
	}

	// 'failed' on an in-flight direct shipment: order flips to
	// delivery_disputed and the customer is told — through the
	// handler, not just the repo function.
	if code := callWebhook("TRK-FAIL1", "failed"); code != http.StatusOK {
		t.Fatalf("failed webhook: status %d, want 200", code)
	}
	var orderStatus, shipStatus string
	if err := db.QueryRow(`SELECT status FROM orders WHERE id = 'ord-1'`).Scan(&orderStatus); err != nil || orderStatus != "delivery_disputed" {
		t.Fatalf("ord-1 status = %q, want delivery_disputed", orderStatus)
	}
	if err := db.QueryRow(`SELECT status FROM supplier_shipments WHERE tracking_number = 'TRK-FAIL1'`).Scan(&shipStatus); err != nil || shipStatus != "failed" {
		t.Fatalf("shipment status = %q, want failed", shipStatus)
	}
	if n := countKind("carrier_failed"); n != 1 {
		t.Fatalf("carrier_failed rows = %d, want 1", n)
	}
	var recipient string
	if err := db.QueryRow(`SELECT recipient_user_id::text FROM notification_outbox WHERE kind = 'carrier_failed'`).Scan(&recipient); err != nil || recipient != customerID {
		t.Fatalf("carrier_failed recipient = %q, want customer %s", recipient, customerID)
	}

	// Repeat 'failed' webhook: still 200, still exactly one row.
	if code := callWebhook("TRK-FAIL1", "failed"); code != http.StatusOK {
		t.Fatalf("repeated failed webhook: status %d, want 200", code)
	}
	if n := countKind("carrier_failed"); n != 1 {
		t.Fatalf("carrier_failed rows after repeat = %d, want still 1", n)
	}

	// 'delivered' still works through the same path and notifies
	// nobody about a failure.
	if code := callWebhook("TRK-OK2", "delivered"); code != http.StatusOK {
		t.Fatalf("delivered webhook: status %d, want 200", code)
	}
	if err := db.QueryRow(`SELECT status FROM orders WHERE id = 'ord-2'`).Scan(&orderStatus); err != nil || orderStatus != "delivered" {
		t.Fatalf("ord-2 status = %q, want delivered", orderStatus)
	}
	if n := countKind("carrier_failed"); n != 1 {
		t.Fatalf("carrier_failed rows after delivered = %d, want still 1", n)
	}
}
