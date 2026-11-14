package repository

// Storage-position lifecycle tests (gap 7 follow-up). DB-gated like
// the sibling suites: they run ONLY when TEAM04_TEST_DATABASE_URL
// points at a DISPOSABLE Postgres; otherwise they skip. The DDL is a
// minimal subset of the real migrations, sufficient for the flows
// under test: inbound QC pass (RecordQCPass / CompleteQC), order
// ShipOrder, supplier stock-return HandoverReturn, and the fee
// engine's RunStorageFeeAssessment.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func storagePositionSeed(t *testing.T, db *sql.DB) {
	t.Helper()
	ddl := []string{
		`DROP TABLE IF EXISTS warehouse_storage_fee_charges, warehouse_storage_positions, supplier_return_events, supplier_return_parcels, supplier_return_requests, inventory_reservations, notification_outbox, supplier_shipments, order_item_origins, order_items, orders, putaway_tasks, qc_results, inbound_shipments, inventory_movements, inventory_balances, products, warehouses, suppliers, platform_settings CASCADE`,
		`CREATE TABLE platform_settings (id INT PRIMARY KEY, storage_fee_enabled BOOLEAN NOT NULL DEFAULT false, storage_fee_free_days INT NOT NULL DEFAULT 30, storage_fee_per_kg_day BIGINT NOT NULL DEFAULT 2000, storage_fee_per_m3_day BIGINT NOT NULL DEFAULT 8000)`,
		`INSERT INTO platform_settings (id) VALUES (1)`,
		`CREATE TABLE suppliers (id VARCHAR(50) PRIMARY KEY)`,
		`INSERT INTO suppliers (id) VALUES ('sup-1')`,
		`CREATE TABLE warehouses (id TEXT PRIMARY KEY)`,
		`INSERT INTO warehouses (id) VALUES ('wh-1')`,
		// prod-measured: 1500 g, 500x400x300 mm -> unit day 3480 Toman
		// at the default tariffs (see storage_fee_repo_test.go).
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, supplier_id VARCHAR(50), fulfillment_method TEXT, stock INT NOT NULL DEFAULT 0, length_mm INT, width_mm INT, height_mm INT, weight_grams INT, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`INSERT INTO products (id, supplier_id, fulfillment_method, length_mm, width_mm, height_mm, weight_grams) VALUES ('prod-measured', 'sup-1', 'warehouse', 500, 400, 300, 1500)`,
		`INSERT INTO products (id, supplier_id, fulfillment_method, length_mm, width_mm, height_mm, weight_grams) VALUES ('prod-b', 'sup-1', 'warehouse', 500, 400, 300, 1500)`,
		`INSERT INTO products (id, supplier_id, fulfillment_method) VALUES ('prod-legacy', 'sup-1', 'warehouse')`,
		`CREATE TABLE inbound_shipments (id TEXT PRIMARY KEY, supplier_id VARCHAR(50), items_count INT NOT NULL DEFAULT 0, status TEXT NOT NULL, warehouse_id TEXT)`,
		`CREATE TABLE qc_results (id TEXT PRIMARY KEY, product_id VARCHAR(50), inbound_id TEXT, inspector_user_id TEXT, status TEXT, defect_reason TEXT, quantity_passed INT, quantity_failed INT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE inventory_movements (id TEXT PRIMARY KEY, product_id VARCHAR(50), supplier_id VARCHAR(50), quantity INT, from_status TEXT, to_status TEXT, from_location_key TEXT, to_location_key TEXT, reference_type TEXT, reference_id TEXT, idempotency_key TEXT UNIQUE, actor_user_id TEXT, metadata JSONB DEFAULT '{}'::jsonb)`,
		`CREATE TABLE inventory_balances (id TEXT PRIMARY KEY, product_id VARCHAR(50), supplier_id VARCHAR(50), owner_type TEXT, location_type TEXT, location_key TEXT, status TEXT, quantity INT NOT NULL DEFAULT 0, reconciled BOOLEAN NOT NULL DEFAULT FALSE, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE (product_id, owner_type, location_type, location_key, status))`,
		`CREATE TABLE putaway_tasks (id TEXT PRIMARY KEY, product_id VARCHAR(50), inbound_id TEXT, bin_location TEXT, quantity INT, status TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE orders (id TEXT PRIMARY KEY, order_number TEXT, store_name TEXT, customer_full_name TEXT, customer_user_id UUID, store_id TEXT, total_amount BIGINT NOT NULL DEFAULT 0, status TEXT, fulfillment_method TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), discount_amount BIGINT NOT NULL DEFAULT 0, shipping_cost BIGINT NOT NULL DEFAULT 0, payable_amount BIGINT NOT NULL DEFAULT 0, payment_status TEXT, customer_phone TEXT, customer_province TEXT, customer_city TEXT, customer_address TEXT, customer_postal_code TEXT, tracking_code TEXT, tracking_number TEXT, carrier TEXT, shipping_label TEXT, packed_at TIMESTAMPTZ, shipped_at TIMESTAMPTZ, box_size TEXT, weight_grams INT)`,
		`CREATE TABLE order_items (id UUID PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT, final_unit_price BIGINT)`,
		`CREATE TABLE order_item_origins (id TEXT PRIMARY KEY, order_id TEXT, order_item_id UUID, leg_index INT, origin_type TEXT, supplier_id TEXT, warehouse_id TEXT, supplier_warehouse_id TEXT, origin_label TEXT, quantity INT)`,
		`CREATE TABLE supplier_shipments (id TEXT PRIMARY KEY, order_id TEXT, status TEXT)`,
		`CREATE TABLE notification_outbox (recipient_user_id TEXT, kind TEXT, title TEXT, body TEXT, payload JSONB)`,
		`CREATE TABLE supplier_return_requests (id TEXT PRIMARY KEY, supplier_id VARCHAR(50), product_id VARCHAR(50), requested_quantity INT, status TEXT, handover_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE supplier_return_parcels (id TEXT PRIMARY KEY, return_request_id TEXT, status TEXT, handed_over_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE inventory_reservations (id TEXT PRIMARY KEY, product_id VARCHAR(50), supplier_id VARCHAR(50), location_key TEXT, quantity INT, reference_type TEXT, reference_id TEXT, status TEXT, idempotency_key TEXT)`,
		`CREATE TABLE supplier_return_events (id TEXT PRIMARY KEY, return_request_id TEXT, from_status TEXT, to_status TEXT, actor_user_id TEXT, idempotency_key TEXT UNIQUE)`,
		`CREATE TABLE warehouse_storage_positions (id TEXT PRIMARY KEY, product_id VARCHAR(50) NOT NULL, supplier_id VARCHAR(50) NOT NULL, warehouse_id TEXT NOT NULL, inbound_id TEXT, goods_class VARCHAR(20) NOT NULL DEFAULT 'normal', qty INT NOT NULL, stocked_at TIMESTAMPTZ NOT NULL, original_stocked_at TIMESTAMPTZ NOT NULL, last_fee_date DATE, last_assessed_date DATE, total_fee_charged_toman BIGINT NOT NULL DEFAULT 0, closed_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE warehouse_storage_fee_charges (id TEXT PRIMARY KEY, position_id TEXT NOT NULL, supplier_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, warehouse_id TEXT NOT NULL, fee_date DATE NOT NULL, qty INT NOT NULL, fee_unit_day_toman BIGINT NOT NULL, amount_toman BIGINT NOT NULL CHECK (amount_toman > 0), goods_class VARCHAR(20) NOT NULL, estimated BOOLEAN NOT NULL DEFAULT FALSE, journal_debit_id TEXT, journal_credit_id TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE (position_id, fee_date))`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("storage-position seed DDL failed: %v", err)
		}
	}
	for _, q := range []string{
		`ALTER TABLE products ADD COLUMN schema_version INT NOT NULL DEFAULT 0`,
		`ALTER TABLE inventory_balances ADD COLUMN variant_id UUID`,
		`CREATE TABLE IF NOT EXISTS product_variants(id UUID PRIMARY KEY,product_id TEXT,inventory_mode TEXT,is_active BOOLEAN DEFAULT TRUE)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

// seedInboundForQC inserts a 'received' inbound plus the physical
// balance RecordQCPass/CompleteQC will move into sellable.
func seedInboundForQC(t *testing.T, db *sql.DB, inboundID, productID, warehouseID string, physicalQty int) {
	t.Helper()
	wh := "NULL"
	args := []interface{}{inboundID, physicalQty}
	if warehouseID != "" {
		wh = "$3"
		args = append(args, warehouseID)
	}
	if _, err := db.Exec(`INSERT INTO inbound_shipments (id, supplier_id, items_count, status, warehouse_id) VALUES ($1, 'sup-1', $2, 'received', `+wh+`)`, args...); err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO inventory_balances (id, product_id, supplier_id, owner_type, location_type, location_key, status, quantity, reconciled)
		VALUES ('BAL-PHYS-' || $1, $2, 'sup-1', 'supplier', 'inbound', 'inbound:' || $1, 'physical', $3, TRUE)`, inboundID, productID, physicalQty); err != nil {
		t.Fatalf("seed physical balance: %v", err)
	}
}

type positionState struct {
	id                string
	qty               int
	closed            bool
	inboundID         string
	originalStockedAt time.Time
}

func openPositions(t *testing.T, db *sql.DB, productID string) []positionState {
	t.Helper()
	rows, err := db.Query(`SELECT id, qty, closed_at IS NOT NULL, COALESCE(inbound_id, ''), original_stocked_at
		FROM warehouse_storage_positions WHERE product_id = $1 ORDER BY stocked_at ASC, id ASC`, productID)
	if err != nil {
		t.Fatalf("read positions: %v", err)
	}
	defer rows.Close()
	out := []positionState{}
	for rows.Next() {
		var p positionState
		if err := rows.Scan(&p.id, &p.qty, &p.closed, &p.inboundID, &p.originalStockedAt); err != nil {
			t.Fatalf("scan position: %v", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("positions rows: %v", err)
	}
	return out
}

func TestStoragePositionCreatedByQCPassAndToppedUp(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	warehouse := "w-" + uuid.NewString()
	ctx := context.Background()
	repo := NewWarehouseRepository(db)

	first := inv09ReceiveLegacy(t, db, f, warehouse, 10)
	if err := repo.RecordQCPass(ctx, f.product, first, f.user, 6); err != nil {
		t.Fatalf("RecordQCPass #1: %v", err)
	}
	if positions := openPositions(t, db, f.product); len(positions) != 0 {
		t.Fatalf("QC alone created storage position: %+v", positions)
	}
	inv09PlacePending(t, db, f, first, warehouse)
	positions := openPositions(t, db, f.product)
	if len(positions) != 1 {
		t.Fatalf("positions after first QC pass = %d, want 1", len(positions))
	}
	p := positions[0]
	if p.qty != 6 || p.closed || p.inboundID != first {
		t.Fatalf("position = %+v, want qty 6, open, inbound INB-1", p)
	}
	firstOriginal := p.originalStockedAt

	// A second inbound for the same triple tops the SAME position up;
	// the free-window clock (original_stocked_at) must not move.
	second := inv09ReceiveLegacy(t, db, f, warehouse, 5)
	if err := repo.RecordQCPass(ctx, f.product, second, f.user, 5); err != nil {
		t.Fatalf("RecordQCPass #2: %v", err)
	}
	inv09PlacePending(t, db, f, second, warehouse)
	positions = openPositions(t, db, f.product)
	if len(positions) != 1 {
		t.Fatalf("positions after top-up = %d, want 1 (merged)", len(positions))
	}
	if positions[0].qty != 11 {
		t.Fatalf("topped-up qty = %d, want 11", positions[0].qty)
	}
	if !positions[0].originalStockedAt.Equal(firstOriginal) {
		t.Fatalf("original_stocked_at moved on top-up: %v -> %v", firstOriginal, positions[0].originalStockedAt)
	}
	if positions[0].inboundID != first {
		t.Fatalf("inbound_id after top-up = %q, want INB-1 (the opener)", positions[0].inboundID)
	}

	// Replay of the same QC pass must not double-count the position.
	if err := repo.RecordQCPass(ctx, f.product, second, f.user, 5); err != nil {
		t.Fatalf("RecordQCPass replay: %v", err)
	}
	positions = openPositions(t, db, f.product)
	if len(positions) != 1 || positions[0].qty != 11 {
		t.Fatalf("after replay positions = %+v, want one position of 11", positions)
	}
}

func TestStoragePositionCreatedByCompleteQC(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	warehouse := "w-" + uuid.NewString()
	ctx := context.Background()
	repo := NewWarehouseRepository(db)

	inbound := inv09ReceiveLegacy(t, db, f, warehouse, 4)
	if err := repo.CompleteQC(ctx, f.product, inbound, f.user, 4, 0, 0, "ok"); err != nil {
		t.Fatalf("CompleteQC: %v", err)
	}
	if positions := openPositions(t, db, f.product); len(positions) != 0 {
		t.Fatalf("QC alone created storage position: %+v", positions)
	}
	inv09PlacePending(t, db, f, inbound, warehouse)
	positions := openPositions(t, db, f.product)
	if len(positions) != 1 || positions[0].qty != 4 || positions[0].closed {
		t.Fatalf("positions = %+v, want one open position of 4", positions)
	}
	// Replay: CompleteQC's own completed-guard makes it a no-op.
	if err := repo.CompleteQC(ctx, f.product, inbound, f.user, 4, 0, 0, "ok"); err != nil {
		t.Fatalf("CompleteQC replay: %v", err)
	}
	positions = openPositions(t, db, f.product)
	if len(positions) != 1 || positions[0].qty != 4 {
		t.Fatalf("after replay positions = %+v, want one position of 4", positions)
	}
}

func TestStoragePositionQCWithoutWarehouseFailsClosed(t *testing.T) {
	db := team04TestDB(t)
	storagePositionSeed(t, db)
	ctx := context.Background()
	repo := NewWarehouseRepository(db)

	seedInboundForQC(t, db, "INB-4", "prod-b", "", 3)
	if err := repo.RecordQCPass(ctx, "prod-b", "INB-4", "inspector-1", 3); !errors.Is(err, ErrInboundProvenance) {
		t.Fatalf("RecordQCPass (no warehouse): %v", err)
	}
	if positions := openPositions(t, db, "prod-b"); len(positions) != 0 {
		t.Fatalf("positions = %+v, want none (inbound has no warehouse to attribute)", positions)
	}
	// Missing immutable origin requires reconciliation and no credit.
	var sellable int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity), 0) FROM inventory_balances WHERE product_id = 'prod-b' AND status = 'sellable'`).Scan(&sellable); err != nil || sellable != 0 {
		t.Fatalf("sellable balance = %d (err %v), want 0", sellable, err)
	}
}

func seedPackedOrder(t *testing.T, db *sql.DB, orderID, itemID, productID string, qty int, originType, originWarehouse string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO orders (id, order_number, customer_user_id, status, payment_status, fulfillment_method)
		VALUES ($1, $1, '11111111-1111-1111-1111-111111111111', 'packed', 'paid', 'warehouse')`, orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, final_unit_price) VALUES ($1, $2, $3, $4, 1000)`, itemID, orderID, productID, qty); err != nil {
		t.Fatalf("seed order item: %v", err)
	}
	wh := "NULL"
	args := []interface{}{"ORG-" + orderID, orderID, itemID, originType, qty}
	if originWarehouse != "" {
		wh = "$6"
		args = append(args, originWarehouse)
	}
	if _, err := db.Exec(`INSERT INTO order_item_origins (id, order_id, order_item_id, leg_index, origin_type, supplier_id, warehouse_id, origin_label, quantity)
		VALUES ($1, $2, $3, 0, $4, 'sup-1', `+wh+`, 'test', $5)`, args...); err != nil {
		t.Fatalf("seed origin: %v", err)
	}
}

func TestStoragePositionShipOrderFIFOSplit(t *testing.T) {
	db := team04TestDB(t)
	storagePositionSeed(t, db)
	ctx := context.Background()
	repo := NewWarehouseRepository(db)

	// Two open positions for prod-measured: oldest 3, newer 4.
	old := storageStockedAt(10)
	newer := storageStockedAt(5)
	if _, err := db.Exec(`INSERT INTO warehouse_storage_positions (id, product_id, supplier_id, warehouse_id, qty, stocked_at, original_stocked_at)
		VALUES ('WSP-OLD', 'prod-measured', 'sup-1', 'wh-1', 3, $1, $1), ('WSP-NEW', 'prod-measured', 'sup-1', 'wh-1', 4, $2, $2)`, old, newer); err != nil {
		t.Fatalf("seed positions: %v", err)
	}
	// A supplier-direct product with a position that must NOT move.
	if _, err := db.Exec(`INSERT INTO warehouse_storage_positions (id, product_id, supplier_id, warehouse_id, qty, stocked_at, original_stocked_at)
		VALUES ('WSP-DIRECT', 'prod-b', 'sup-1', 'wh-1', 7, $1, $1)`, newer); err != nil {
		t.Fatalf("seed direct position: %v", err)
	}

	seedPackedOrder(t, db, "ORD-1", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "prod-measured", 5, "warehouse", "wh-1")
	if err := repo.ShipOrder(ctx, "ORD-1", "post", "TRK-1"); err != nil {
		t.Fatalf("ShipOrder: %v", err)
	}
	positions := openPositions(t, db, "prod-measured")
	if len(positions) != 2 {
		t.Fatalf("positions = %+v, want both rows retained", positions)
	}
	if positions[0].id != "WSP-OLD" || positions[0].qty != 0 || !positions[0].closed {
		t.Fatalf("oldest position = %+v, want qty 0 and closed (FIFO drains it first)", positions[0])
	}
	if positions[1].id != "WSP-NEW" || positions[1].qty != 2 || positions[1].closed {
		t.Fatalf("newer position = %+v, want qty 2 and open (split remainder)", positions[1])
	}

	// Supplier-direct item ships without touching its position.
	seedPackedOrder(t, db, "ORD-2", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "prod-b", 2, "supplier", "")
	if err := repo.ShipOrder(ctx, "ORD-2", "post", "TRK-2"); err != nil {
		t.Fatalf("ShipOrder direct: %v", err)
	}
	direct := openPositions(t, db, "prod-b")
	if len(direct) != 1 || direct[0].qty != 7 || direct[0].closed {
		t.Fatalf("supplier-direct position = %+v, want untouched qty 7 open", direct)
	}
}

func TestStoragePositionShipLegacyStockWithoutPositionsFailsClosed(t *testing.T) {
	db := team04TestDB(t)
	storagePositionSeed(t, db)
	ctx := context.Background()
	repo := NewWarehouseRepository(db)

	seedPackedOrder(t, db, "ORD-3", "cccccccc-cccc-cccc-cccc-cccccccccccc", "prod-legacy", 2, "warehouse", "wh-1")
	if err := repo.ShipOrder(ctx, "ORD-3", "post", "TRK-3"); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("ShipOrder with unresolved named-origin coverage: %v", err)
	}
	if positions := openPositions(t, db, "prod-legacy"); len(positions) != 0 {
		t.Fatalf("positions = %+v, want none (legacy stock has no positions to decrement)", positions)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM orders WHERE id = 'ORD-3'`).Scan(&status); err != nil || status != "packed" {
		t.Fatalf("order status = %q (err %v), want unchanged packed", status, err)
	}
}

func TestStoragePositionHandoverReturnDecrement(t *testing.T) {
	db := team04TestDB(t)
	storagePositionSeed(t, db)
	ctx := context.Background()
	repo := NewSupplierRepository(db)

	if _, err := db.Exec(`INSERT INTO warehouse_storage_positions (id, product_id, supplier_id, warehouse_id, qty, stocked_at, original_stocked_at)
		VALUES ('WSP-RET', 'prod-measured', 'sup-1', 'wh-1', 5, NOW(), NOW())`); err != nil {
		t.Fatalf("seed position: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO supplier_return_requests (id, supplier_id, product_id, requested_quantity, status) VALUES ('RET-1', 'sup-1', 'prod-measured', 3, 'packed')`); err != nil {
		t.Fatalf("seed return: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO supplier_return_parcels (id, return_request_id, status) VALUES ('PAR-1', 'RET-1', 'confirmed')`); err != nil {
		t.Fatalf("seed parcel: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO inventory_balances (id, product_id, supplier_id, owner_type, location_type, location_key, status, quantity, reconciled)
		VALUES ('BAL-RR', 'prod-measured', 'sup-1', 'supplier', 'warehouse', 'warehouse:INB-9', 'return_requested', 3, TRUE)`); err != nil {
		t.Fatalf("seed reserved balance: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO inventory_reservations (id, product_id, supplier_id, location_key, quantity, reference_type, reference_id, status, idempotency_key)
		VALUES ('RES-1', 'prod-measured', 'sup-1', 'warehouse:INB-9', 3, 'supplier_return', 'RET-1', 'active', 'res-key-1')`); err != nil {
		t.Fatalf("seed reservation: %v", err)
	}

	if err := repo.HandoverReturn(ctx, "RET-1", "warehouse-1", "handover-key-1"); err != nil {
		t.Fatalf("HandoverReturn: %v", err)
	}
	positions := openPositions(t, db, "prod-measured")
	if len(positions) != 1 || positions[0].qty != 2 || positions[0].closed {
		t.Fatalf("positions after handover = %+v, want one open position of 2", positions)
	}
	// Replay: the in_transit state guard makes handover a no-op.
	if err := repo.HandoverReturn(ctx, "RET-1", "warehouse-1", "handover-key-1"); err != nil {
		t.Fatalf("HandoverReturn replay: %v", err)
	}
	positions = openPositions(t, db, "prod-measured")
	if len(positions) != 1 || positions[0].qty != 2 {
		t.Fatalf("positions after replay = %+v, want unchanged qty 2", positions)
	}
}

func TestStoragePositionPutawayAgedPositionIsChargedByEngine(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	warehouse := "w-" + uuid.NewString()
	if err := NewSupplierRepository(db).RecordProductMeasurement(context.Background(), f.product, f.user, 500, 400, 300, 1500, "measured storage fixture"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	warehouseRepo := NewWarehouseRepository(db)
	supplierRepo := NewSupplierRepository(db)

	// Stock arrives through the real receipt/QC/putaway path, creating the
	// position; then the position is aged to 31 days (seeded
	// timestamps) so the engine's free window has exactly one
	// chargeable day.
	inbound := inv09ReceiveLegacy(t, db, f, warehouse, 2)
	if err := warehouseRepo.RecordQCPass(ctx, f.product, inbound, f.user, 2); err != nil {
		t.Fatalf("RecordQCPass: %v", err)
	}
	inv09PlacePending(t, db, f, inbound, warehouse)
	aged := storageStockedAt(31)
	if _, err := db.Exec(`UPDATE warehouse_storage_positions SET stocked_at = $1, original_stocked_at = $1 WHERE product_id = $2`, aged, f.product); err != nil {
		t.Fatalf("age position: %v", err)
	}
	if _, err := db.Exec(`UPDATE platform_settings SET storage_fee_enabled = true WHERE id = 1`); err != nil {
		t.Fatalf("enable fees: %v", err)
	}
	summary, err := supplierRepo.RunStorageFeeAssessment(ctx)
	if err != nil {
		t.Fatalf("RunStorageFeeAssessment: %v", err)
	}
	// unit day = 3480 (1.5 kg x 2000 + 0.06 m3 x 8000); qty 2 -> 6960
	// for the single chargeable day beyond the 30 free days.
	if summary.ChargesCreated != 1 || summary.TotalCharged != 6960 || summary.PositionsCharged != 1 {
		t.Fatalf("summary = %+v, want 1 charge of 6960 across 1 position", summary)
	}
	if n := storageChargeCount(t, db); n != 1 {
		t.Fatalf("charge rows = %d, want 1", n)
	}
}
