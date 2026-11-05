package repository

// Return-destination derivation tests (owner rule, Oct 28 2026): a
// customer return goes back to where the order shipped FROM.
//
// Opt-in integration tests in the Team 03 pattern: skipped unless
// TEAM03_DATABASE_URL points at a DISPOSABLE Postgres database. The
// setup creates and drops its own minimal tables and must never run
// against a live or shared database.
//
// Covered:
//   - supplier-warehouse origin -> destination_supplier_warehouse_id;
//   - central-warehouse origin -> warehouse_id;
//   - mixed origins -> central consolidation (top-priority active
//     central warehouse), with NO ambiguity admin_note;
//   - legacy orders without origin rows -> product fallback set
//     (origin_supplier_warehouse_id / warehouse_name);
//   - supplier-direct with no recorded warehouse -> the supplier's
//     single active warehouse; several -> consolidation + admin_note;
//   - a client-sent warehouse id (bogus or real-but-wrong) is ignored;
//   - DeriveReturnDestination (the quote read path) for a subset;
//   - the supplier panel list reports destinationType 'supplier';
//   - the warehouse queue destination filter.

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func openReturnDestDB(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEAM03_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEAM03_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open disposable database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Skipf("disposable database unavailable: %v", err)
	}
	stmts := []string{
		`DROP TABLE IF EXISTS supplier_return_back_requests, return_items, customer_returns, customer_wallets, payment_transactions, disputes, ledger_entries, referral_bonuses, order_item_origins, order_items, orders, product_variants, products, supplier_warehouses, warehouses, platform_settings CASCADE`,
		`CREATE TABLE platform_settings (id INT PRIMARY KEY, return_window_days INT)`,
		`INSERT INTO platform_settings (id, return_window_days) VALUES (1, 7)`,
		`CREATE TABLE products (id TEXT PRIMARY KEY, title TEXT, stock INT NOT NULL DEFAULT 0, supplier_id TEXT, fulfillment_method TEXT, origin_supplier_warehouse_id TEXT, warehouse_name TEXT)`,
		`CREATE TABLE product_variants (id UUID PRIMARY KEY, variant_name TEXT)`,
		`CREATE TABLE orders (
			id TEXT PRIMARY KEY, customer_user_id UUID, order_number TEXT, store_id TEXT, store_name TEXT,
			total_amount BIGINT NOT NULL DEFAULT 0, discount_amount BIGINT NOT NULL DEFAULT 0, seller_discount_amount BIGINT NOT NULL DEFAULT 0, discount_code TEXT,
			payable_amount BIGINT NOT NULL DEFAULT 0, payment_method TEXT, payment_status TEXT,
			customer_full_name TEXT, customer_phone TEXT, customer_province TEXT, customer_city TEXT,
			customer_address TEXT, customer_postal_code TEXT, status TEXT, tracking_code TEXT,
			shipping_cost BIGINT NOT NULL DEFAULT 0, dropshipper_profit BIGINT NOT NULL DEFAULT 0,
			platform_profit BIGINT NOT NULL DEFAULT 0, referral_commission BIGINT NOT NULL DEFAULT 0,
			referral_commission_released BOOLEAN NOT NULL DEFAULT FALSE, referrer_seller_id TEXT,
			profit_released BOOLEAN NOT NULL DEFAULT FALSE, profit_released_at TIMESTAMPTZ,
			shipped_at TIMESTAMPTZ, confirmed_at TIMESTAMPTZ, disputed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			fulfillment_method TEXT, wallet_amount BIGINT NOT NULL DEFAULT 0, gateway_amount BIGINT NOT NULL DEFAULT 0,
			platform_addon_amount BIGINT NOT NULL DEFAULT 0, supplier_addon_amount BIGINT NOT NULL DEFAULT 0,
			inventory_captured BOOLEAN NOT NULL DEFAULT FALSE, packaging_length_mm INT, packaging_width_mm INT,
			packaging_height_mm INT, dispute_reason TEXT, carrier TEXT, tracking_number TEXT,
			shipping_label TEXT, packed_at TIMESTAMPTZ, box_size TEXT, weight_grams INT,
			idempotency_key TEXT, return_window_ends_at TIMESTAMPTZ, shipping_quote_id TEXT
		)`,
		`CREATE TABLE order_items (
			id UUID PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT,
			wholesale_cost BIGINT, platform_fee BIGINT, seller_margin BIGINT, final_unit_price BIGINT, discount_amount BIGINT NOT NULL DEFAULT 0,
			variant_id UUID, variant_name TEXT, variant_spnc TEXT
		)`,
		`CREATE TABLE warehouses (id TEXT PRIMARY KEY, name TEXT, province_name TEXT, city_name TEXT, priority INT NOT NULL DEFAULT 100, is_active BOOLEAN NOT NULL DEFAULT TRUE)`,
		`CREATE TABLE supplier_warehouses (id TEXT PRIMARY KEY, supplier_id TEXT, name TEXT, province_name TEXT, city_name TEXT, address TEXT, is_active BOOLEAN NOT NULL DEFAULT TRUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE order_item_origins (
			id TEXT PRIMARY KEY, order_id TEXT, order_item_id UUID, leg_index INT, origin_type TEXT,
			supplier_id TEXT, warehouse_id TEXT, supplier_warehouse_id TEXT, origin_label TEXT, quantity INT
		)`,
		`CREATE TABLE ledger_entries (
			id VARCHAR(50) PRIMARY KEY, date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			target_account VARCHAR(150) NOT NULL, target_type VARCHAR(20) NOT NULL,
			type VARCHAR(10) NOT NULL, amount BIGINT NOT NULL, reason TEXT NOT NULL,
			operator VARCHAR(100) NOT NULL, related_order_or_dispute VARCHAR(50),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE referral_bonuses (id TEXT PRIMARY KEY, referred_store_id TEXT, status TEXT, released_at TIMESTAMPTZ, amount BIGINT)`,
		`CREATE TABLE customer_returns (
			id UUID PRIMARY KEY, order_id VARCHAR(50) NOT NULL, customer_id UUID NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'requested', reason TEXT NOT NULL DEFAULT '',
			admin_note TEXT NOT NULL DEFAULT '', warehouse_id TEXT, prepaid BOOLEAN NOT NULL DEFAULT FALSE,
			tracking_number TEXT, shipped_at TIMESTAMPTZ, return_code TEXT,
			return_shipping_amount BIGINT, return_shipping_bearer VARCHAR(20),
			destination_supplier_warehouse_id TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE return_items (
			id UUID PRIMARY KEY, return_id UUID NOT NULL, order_item_id VARCHAR(50) NOT NULL,
			variant_id UUID, qty INT NOT NULL, qc_status VARCHAR(20) NOT NULL DEFAULT 'pending', received_at TIMESTAMPTZ
		)`,
		`CREATE TABLE supplier_return_back_requests (
			id VARCHAR(60) PRIMARY KEY, return_item_id UUID NOT NULL, quantity INT NOT NULL,
			status VARCHAR(30) NOT NULL DEFAULT 'requested', reason TEXT NOT NULL DEFAULT '',
			review_reason TEXT NOT NULL DEFAULT '', reviewed_at TIMESTAMPTZ, shipped_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ, carrier_code VARCHAR(40), tracking_number VARCHAR(100),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE customer_wallets (customer_id UUID PRIMARY KEY, balance BIGINT NOT NULL DEFAULT 0, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE disputes (
			id TEXT PRIMARY KEY, order_id TEXT, order_number TEXT, type TEXT, customer_name TEXT,
			product_title TEXT, claim_amount BIGINT, reason TEXT, store_id TEXT, supplier_name TEXT,
			status TEXT, responsible_party TEXT, resolution_action TEXT, resolution_date TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE payment_transactions (
			id TEXT PRIMARY KEY, order_id TEXT, order_number TEXT, gateway_token TEXT,
			rrn TEXT, card_mask TEXT, amount BIGINT, status TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			db.Close()
			t.Fatalf("create disposable schema: %v (stmt %.60s)", err, stmt)
		}
	}
	return db
}

func rdWarehouse(t *testing.T, db *sql.DB, id, name, city string, priority int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO warehouses (id, name, province_name, city_name, priority) VALUES ($1, $2, 'البرز', $3, $4)`, id, name, city, priority); err != nil {
		t.Fatalf("seed warehouse: %v", err)
	}
}

func rdSupplierWarehouse(t *testing.T, db *sql.DB, id, supplierID, name, city, address string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO supplier_warehouses (id, supplier_id, name, province_name, city_name, address) VALUES ($1, $2, $3, 'البرز', $4, $5)`, id, supplierID, name, city, address); err != nil {
		t.Fatalf("seed supplier warehouse: %v", err)
	}
}

func rdProduct(t *testing.T, db *sql.DB, id, supplierID, fulfillment, originSwh, warehouseName string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO products (id, title, supplier_id, fulfillment_method, origin_supplier_warehouse_id, warehouse_name) VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''))`, id, "Product "+id, supplierID, fulfillment, originSwh, warehouseName); err != nil {
		t.Fatalf("seed product: %v", err)
	}
}

func rdOrderWithItem(t *testing.T, db *sql.DB, orderID, customerID, productID string, qty int) string {
	t.Helper()
	team03InsertOrder(t, db, orderID, customerID, "delivered_confirmed", 0)
	itemID := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1::uuid, $2, $3, $4, 10000, 0, 0, 20000)`, itemID, orderID, productID, qty); err != nil {
		t.Fatalf("seed order item: %v", err)
	}
	return itemID
}

func rdOrigin(t *testing.T, db *sql.DB, orderID, itemID, originType, supplierID, warehouseID, supplierWarehouseID string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO order_item_origins (id, order_id, order_item_id, leg_index, origin_type, supplier_id, warehouse_id, supplier_warehouse_id, origin_label, quantity) VALUES ($1, $2, $3::uuid, 0, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), '', 1)`,
		uuid.NewString(), orderID, itemID, originType, supplierID, warehouseID, supplierWarehouseID); err != nil {
		t.Fatalf("seed origin: %v", err)
	}
}

func rdCreate(t *testing.T, repo *OrderRepository, customerID, orderID string, itemIDs []string, opts CustomerReturnOptions) *CustomerReturn {
	t.Helper()
	items := make([]CustomerReturnItemRequest, 0, len(itemIDs))
	for _, id := range itemIDs {
		items = append(items, CustomerReturnItemRequest{OrderItemID: id, Qty: 1})
	}
	ret, err := repo.CreateCustomerReturn(context.Background(), customerID, orderID, "test reason", items, opts)
	if err != nil {
		t.Fatalf("create return: %v", err)
	}
	return ret
}

func TestReturnDestinationFromSupplierOrigin(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdSupplierWarehouse(t, db, "swh-1", "sup-1", "کارخانه کرج", "کرج", "جاده مخصوص، پلاک ۱۲")
	rdProduct(t, db, "p-sup", "sup-1", "supplier_direct", "", "")
	itemID := rdOrderWithItem(t, db, "ord-rd-sup", customerID, "p-sup", 1)
	rdOrigin(t, db, "ord-rd-sup", itemID, "supplier", "sup-1", "", "swh-1")

	ret := rdCreate(t, repo, customerID, "ord-rd-sup", []string{itemID}, CustomerReturnOptions{})
	if ret.DestinationSupplierWarehouseID != "swh-1" {
		t.Fatalf("destination supplier warehouse = %q, want swh-1", ret.DestinationSupplierWarehouseID)
	}
	if ret.WarehouseID != "" {
		t.Fatalf("warehouse id = %q, want empty for a supplier destination", ret.WarehouseID)
	}
	if ret.DestinationType != "supplier" {
		t.Fatalf("destination type = %q, want supplier", ret.DestinationType)
	}
	if ret.DestinationName != "کارخانه کرج" {
		t.Fatalf("destination name = %q, want the supplier warehouse name", ret.DestinationName)
	}
}

func TestReturnDestinationFromCentralOrigin(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdProduct(t, db, "p-wh", "", "warehouse", "", "")
	itemID := rdOrderWithItem(t, db, "ord-rd-central", customerID, "p-wh", 1)
	rdOrigin(t, db, "ord-rd-central", itemID, "warehouse", "", "wh-1", "")

	ret := rdCreate(t, repo, customerID, "ord-rd-central", []string{itemID}, CustomerReturnOptions{})
	if ret.WarehouseID != "wh-1" {
		t.Fatalf("warehouse id = %q, want wh-1", ret.WarehouseID)
	}
	if ret.DestinationSupplierWarehouseID != "" {
		t.Fatalf("destination supplier warehouse = %q, want empty", ret.DestinationSupplierWarehouseID)
	}
	if ret.DestinationType != "central" || ret.DestinationName != "انبار مرکزی قم" {
		t.Fatalf("destination = %q/%q, want central/انبار مرکزی قم", ret.DestinationType, ret.DestinationName)
	}
}

func TestReturnDestinationMixedOriginsConsolidateToCentral(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdWarehouse(t, db, "wh-2", "انبار مرکزی تهران", "تهران", 90)
	rdSupplierWarehouse(t, db, "swh-1", "sup-1", "کارخانه کرج", "کرج", "")
	rdProduct(t, db, "p-sup", "sup-1", "supplier_direct", "", "")
	rdProduct(t, db, "p-wh", "", "warehouse", "", "")

	team03InsertOrder(t, db, "ord-rd-mixed", customerID, "delivered_confirmed", 0)
	itemA := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1::uuid, 'ord-rd-mixed', 'p-sup', 1, 10000, 0, 0, 20000)`, itemA); err != nil {
		t.Fatalf("seed item A: %v", err)
	}
	itemB := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1::uuid, 'ord-rd-mixed', 'p-wh', 1, 10000, 0, 0, 20000)`, itemB); err != nil {
		t.Fatalf("seed item B: %v", err)
	}
	rdOrigin(t, db, "ord-rd-mixed", itemA, "supplier", "sup-1", "", "swh-1")
	rdOrigin(t, db, "ord-rd-mixed", itemB, "warehouse", "", "wh-2", "")

	ret := rdCreate(t, repo, customerID, "ord-rd-mixed", []string{itemA, itemB}, CustomerReturnOptions{})
	if ret.DestinationSupplierWarehouseID != "" {
		t.Fatalf("mixed return kept a supplier destination %q, want consolidation", ret.DestinationSupplierWarehouseID)
	}
	if ret.WarehouseID != "wh-1" {
		t.Fatalf("mixed return warehouse = %q, want wh-1 (top-priority consolidation)", ret.WarehouseID)
	}
	if ret.AdminNote != "" {
		t.Fatalf("mixed return admin note = %q, want empty (mixed is not the ambiguous case)", ret.AdminNote)
	}
}

func TestReturnDestinationLegacyProductFallback(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdSupplierWarehouse(t, db, "swh-9", "sup-9", "انبار تأمین‌کننده قم", "قم", "")

	// Supplier-direct product with a recorded origin warehouse.
	rdProduct(t, db, "p-legacy-sup", "sup-9", "supplier_direct", "swh-9", "")
	itemA := rdOrderWithItem(t, db, "ord-rd-legacy-sup", customerID, "p-legacy-sup", 1)
	retA := rdCreate(t, repo, customerID, "ord-rd-legacy-sup", []string{itemA}, CustomerReturnOptions{})
	if retA.DestinationSupplierWarehouseID != "swh-9" {
		t.Fatalf("legacy supplier fallback = %q, want swh-9", retA.DestinationSupplierWarehouseID)
	}

	// Warehouse product whose warehouse_name names a central warehouse.
	rdProduct(t, db, "p-legacy-wh", "", "warehouse", "", "انبار مرکزی قم")
	itemB := rdOrderWithItem(t, db, "ord-rd-legacy-wh", customerID, "p-legacy-wh", 1)
	retB := rdCreate(t, repo, customerID, "ord-rd-legacy-wh", []string{itemB}, CustomerReturnOptions{})
	if retB.WarehouseID != "wh-1" || retB.DestinationType != "central" {
		t.Fatalf("legacy warehouse fallback = %q/%q, want wh-1/central", retB.WarehouseID, retB.DestinationType)
	}
}

func TestReturnDestinationSupplierWarehouseResolution(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)

	// Exactly one active warehouse -> that one.
	rdSupplierWarehouse(t, db, "swh-2", "sup-2", "انبار یگانه", "قم", "")
	rdProduct(t, db, "p-one", "sup-2", "supplier_direct", "", "")
	itemA := rdOrderWithItem(t, db, "ord-rd-one", customerID, "p-one", 1)
	retA := rdCreate(t, repo, customerID, "ord-rd-one", []string{itemA}, CustomerReturnOptions{})
	if retA.DestinationSupplierWarehouseID != "swh-2" {
		t.Fatalf("single-warehouse supplier fallback = %q, want swh-2", retA.DestinationSupplierWarehouseID)
	}

	// Several active warehouses, none recorded -> ambiguous:
	// consolidation + admin_note.
	rdSupplierWarehouse(t, db, "swh-3a", "sup-3", "انبار الف", "قم", "")
	rdSupplierWarehouse(t, db, "swh-3b", "sup-3", "انبار ب", "قم", "")
	rdProduct(t, db, "p-many", "sup-3", "supplier_direct", "", "")
	itemB := rdOrderWithItem(t, db, "ord-rd-many", customerID, "p-many", 1)
	retB := rdCreate(t, repo, customerID, "ord-rd-many", []string{itemB}, CustomerReturnOptions{})
	if retB.DestinationSupplierWarehouseID != "" || retB.WarehouseID != "wh-1" {
		t.Fatalf("ambiguous supplier destination = swh %q / wh %q, want consolidation wh-1", retB.DestinationSupplierWarehouseID, retB.WarehouseID)
	}
	if retB.AdminNote == "" {
		t.Fatalf("ambiguous supplier destination must record the ambiguity in admin_note")
	}
}

func TestReturnDestinationClientWarehouseIgnored(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdSupplierWarehouse(t, db, "swh-1", "sup-1", "کارخانه کرج", "کرج", "")
	rdProduct(t, db, "p-sup", "sup-1", "supplier_direct", "", "")

	// A bogus free-text id: the old code rejected the filing with
	// "warehouse not found"; the derived destination must win now.
	itemA := rdOrderWithItem(t, db, "ord-rd-bogus", customerID, "p-sup", 1)
	rdOrigin(t, db, "ord-rd-bogus", itemA, "supplier", "sup-1", "", "swh-1")
	retA := rdCreate(t, repo, customerID, "ord-rd-bogus", []string{itemA}, CustomerReturnOptions{WarehouseID: "انبار دم دریا"})
	if retA.DestinationSupplierWarehouseID != "swh-1" {
		t.Fatalf("bogus client warehouse: destination = %q, want derived swh-1", retA.DestinationSupplierWarehouseID)
	}

	// A real but wrong central warehouse id: derived still wins.
	itemB := rdOrderWithItem(t, db, "ord-rd-wrong", customerID, "p-sup", 1)
	rdOrigin(t, db, "ord-rd-wrong", itemB, "supplier", "sup-1", "", "swh-1")
	retB := rdCreate(t, repo, customerID, "ord-rd-wrong", []string{itemB}, CustomerReturnOptions{WarehouseID: "wh-1"})
	if retB.DestinationSupplierWarehouseID != "swh-1" || retB.WarehouseID != "" {
		t.Fatalf("wrong client warehouse: destination = swh %q / wh %q, want derived swh-1 only", retB.DestinationSupplierWarehouseID, retB.WarehouseID)
	}
}

func TestDeriveReturnDestinationForQuoteSubset(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	ctx := context.Background()
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdSupplierWarehouse(t, db, "swh-1", "sup-1", "کارخانه کرج", "کرج", "جاده مخصوص، پلاک ۱۲")
	rdProduct(t, db, "p-sup", "sup-1", "supplier_direct", "", "")
	rdProduct(t, db, "p-wh", "", "warehouse", "", "")

	team03InsertOrder(t, db, "ord-rd-quote", customerID, "delivered_confirmed", 0)
	itemA := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1::uuid, 'ord-rd-quote', 'p-sup', 1, 10000, 0, 0, 20000)`, itemA); err != nil {
		t.Fatalf("seed item A: %v", err)
	}
	itemB := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1::uuid, 'ord-rd-quote', 'p-wh', 1, 10000, 0, 0, 20000)`, itemB); err != nil {
		t.Fatalf("seed item B: %v", err)
	}
	rdOrigin(t, db, "ord-rd-quote", itemA, "supplier", "sup-1", "", "swh-1")
	rdOrigin(t, db, "ord-rd-quote", itemB, "warehouse", "", "wh-1", "")

	only, err := repo.DeriveReturnDestination(ctx, "ord-rd-quote", []string{itemA})
	if err != nil {
		t.Fatalf("derive subset: %v", err)
	}
	if only.Type != "supplier" || only.SupplierWarehouseID != "swh-1" || only.City != "کرج" || only.Address == "" {
		t.Fatalf("subset destination = %+v, want supplier swh-1 in کرج with an address", only)
	}
	both, err := repo.DeriveReturnDestination(ctx, "ord-rd-quote", []string{itemA, itemB})
	if err != nil {
		t.Fatalf("derive both: %v", err)
	}
	if both.Type != "central" || both.WarehouseID != "wh-1" {
		t.Fatalf("mixed subset destination = %+v, want central consolidation wh-1", both)
	}
}

func TestSupplierCustomerReturnsShowSupplierDestination(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	supplierRepo := NewSupplierRepository(db)
	ctx := context.Background()
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdSupplierWarehouse(t, db, "swh-1", "sup-1", "کارخانه کرج", "کرج", "")
	rdProduct(t, db, "p-sup", "sup-1", "supplier_direct", "", "")
	itemID := rdOrderWithItem(t, db, "ord-rd-panel", customerID, "p-sup", 1)
	rdOrigin(t, db, "ord-rd-panel", itemID, "supplier", "sup-1", "", "swh-1")
	ret := rdCreate(t, repo, customerID, "ord-rd-panel", []string{itemID}, CustomerReturnOptions{})

	list, err := supplierRepo.ListSupplierCustomerReturns(ctx, "sup-1")
	if err != nil {
		t.Fatalf("supplier returns list: %v", err)
	}
	var found *SupplierCustomerReturn
	for i := range list {
		if list[i].ReturnID == ret.ID {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatalf("created return not in the supplier list (%d rows)", len(list))
	}
	if found.DestinationType == nil || *found.DestinationType != "supplier" {
		t.Fatalf("supplier list destinationType = %v, want supplier", found.DestinationType)
	}
	if found.DestinationName == nil || *found.DestinationName != "کارخانه کرج" {
		t.Fatalf("supplier list destinationName = %v, want کارخانه کرج", found.DestinationName)
	}
	if found.WarehouseCity == nil || *found.WarehouseCity != "کرج" {
		t.Fatalf("supplier list warehouseCity = %v, want کرج", found.WarehouseCity)
	}
}

func TestWarehouseQueueDestinationFilter(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	ctx := context.Background()
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdSupplierWarehouse(t, db, "swh-1", "sup-1", "کارخانه کرج", "کرج", "")
	rdProduct(t, db, "p-sup", "sup-1", "supplier_direct", "", "")
	rdProduct(t, db, "p-wh", "", "warehouse", "", "")

	itemA := rdOrderWithItem(t, db, "ord-rd-queue-sup", customerID, "p-sup", 1)
	rdOrigin(t, db, "ord-rd-queue-sup", itemA, "supplier", "sup-1", "", "swh-1")
	retSup := rdCreate(t, repo, customerID, "ord-rd-queue-sup", []string{itemA}, CustomerReturnOptions{})

	itemB := rdOrderWithItem(t, db, "ord-rd-queue-wh", customerID, "p-wh", 1)
	rdOrigin(t, db, "ord-rd-queue-wh", itemB, "warehouse", "", "wh-1", "")
	retWh := rdCreate(t, repo, customerID, "ord-rd-queue-wh", []string{itemB}, CustomerReturnOptions{})

	for _, ret := range []*CustomerReturn{retSup, retWh} {
		if err := repo.ApproveCustomerReturn(ctx, ret.ID, ""); err != nil {
			t.Fatalf("approve %s: %v", ret.ID, err)
		}
	}

	all, err := repo.ListCustomerReturnsForWarehouse(ctx, "")
	if err != nil {
		t.Fatalf("unfiltered queue: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered queue = %d returns, want 2", len(all))
	}
	supplierQueue, err := repo.ListCustomerReturnsForWarehouse(ctx, "swh-1")
	if err != nil {
		t.Fatalf("supplier queue: %v", err)
	}
	if len(supplierQueue) != 1 || supplierQueue[0].ID != retSup.ID {
		t.Fatalf("supplier queue = %+v, want only the supplier-destined return", supplierQueue)
	}
	centralQueue, err := repo.ListCustomerReturnsForWarehouse(ctx, "wh-1")
	if err != nil {
		t.Fatalf("central queue: %v", err)
	}
	if len(centralQueue) != 1 || centralQueue[0].ID != retWh.ID {
		t.Fatalf("central queue = %+v, want only the central-destined return", centralQueue)
	}
}
