package repository

// GAP 6 + GAP 15 money-integrity tests (Nov 5, 2026):
//   GAP 6 — refunding a customer return restocks at receive (ok ->
//   sellable via the canonical ledger, defective -> quarantine),
//   reverses the supplier's product cost and the dropshipper
//   seller's margin at refund through customer_return_financial_events,
//   refunds ALL received items (owner decision D1), and filing a
//   return can no longer over-claim an order item's quantity.
//   GAP 15 — finance adjustment documents (ledger_entries payable
//   legs) move supplier/store Available, and the treasury's owed-to
//   figures are the canonical balances (SumAllSuppliersAvailable ==
//   Σ per-supplier Available, signed).
//
// Opt-in integration tests in the Team 03 pattern: skipped unless
// TEAM03_DATABASE_URL points at a DISPOSABLE Postgres database. The
// setup creates and drops its own tables and must never run against
// a live or shared database.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func openGapMoneyDB(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEAM03_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEAM03_DATABASE_URL is not set")
	}
	db := openPrivateFinanceFixture(t, databaseURL)
	var err error
	if err != nil {
		t.Fatalf("open disposable database: %v", err)
	}
	ctx := context.Background()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users(id UUID PRIMARY KEY,mobile TEXT,full_name TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Skipf("disposable database unavailable: %v", err)
	}
	stmts := []string{
		`DROP TABLE IF EXISTS order_inventory_returns, order_inventory_captures, seller_early_withdrawals, customer_return_financial_events, customer_return_shipping_charges, supplier_return_financial_events, inventory_movements, inventory_balances, return_items, customer_returns, customer_wallets, payment_transactions, disputes, ledger_entries, referral_bonuses, order_item_origins, order_items, orders, product_variants, products, supplier_warehouses, warehouses, payout_requests, seller_service_purchases, suppliers, stores, platform_settings CASCADE`,
		`DROP SEQUENCE IF EXISTS customer_return_code_seq`,
		`CREATE SEQUENCE customer_return_code_seq`,
		`CREATE TABLE finance_command_requests(principal_type TEXT,principal_id TEXT,operation TEXT,client_key TEXT,fingerprint TEXT,result JSONB,PRIMARY KEY(principal_type,principal_id,operation,client_key))`,
		`CREATE TABLE platform_settings (id INT PRIMARY KEY, return_window_days INT, shipping_base_rate BIGINT, shipping_per_kg BIGINT, volumetric_divisor BIGINT, default_item_weight_grams BIGINT)`,
		`INSERT INTO platform_settings (id, return_window_days, shipping_base_rate, shipping_per_kg, volumetric_divisor, default_item_weight_grams) VALUES (1, 7, 100000, 20000, NULL, NULL)`,
		`CREATE TABLE products (id TEXT PRIMARY KEY, title TEXT, stock INT NOT NULL DEFAULT 0, supplier_id TEXT, fulfillment_method TEXT, origin_supplier_warehouse_id TEXT, warehouse_name TEXT, weight_grams INT, length_mm INT, width_mm INT, height_mm INT, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_variants (id UUID PRIMARY KEY, product_id TEXT, stock INT NOT NULL DEFAULT 0)`,
		`CREATE TABLE suppliers (id VARCHAR(50) PRIMARY KEY, approval_status TEXT NOT NULL DEFAULT 'approved')`,
		`CREATE TABLE stores (id TEXT PRIMARY KEY, owner_type TEXT, referrer_seller_id TEXT)`,
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
			id TEXT PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT,
			wholesale_cost BIGINT, platform_fee BIGINT, seller_margin BIGINT, final_unit_price BIGINT, discount_amount BIGINT NOT NULL DEFAULT 0,
			variant_id UUID, variant_name TEXT, variant_spnc TEXT
		)`,
		`CREATE TABLE ledger_entries (
			id VARCHAR(80) PRIMARY KEY, date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			target_account VARCHAR(150) NOT NULL, target_type VARCHAR(20) NOT NULL,
			type VARCHAR(10) NOT NULL, amount BIGINT NOT NULL, reason TEXT NOT NULL,
			operator VARCHAR(100) NOT NULL, related_order_or_dispute VARCHAR(50),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE referral_bonuses (id TEXT PRIMARY KEY, referrer_store_id TEXT, referred_store_id TEXT, status TEXT, released_at TIMESTAMPTZ, amount BIGINT)`,
		`CREATE TABLE warehouses (id TEXT PRIMARY KEY, name TEXT, province_name TEXT, city_name TEXT, priority INT NOT NULL DEFAULT 100, is_active BOOLEAN NOT NULL DEFAULT TRUE)`,
		`CREATE TABLE supplier_warehouses (id TEXT PRIMARY KEY, supplier_id TEXT, name TEXT, province_name TEXT, city_name TEXT, address TEXT, is_active BOOLEAN NOT NULL DEFAULT TRUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE order_item_origins (id TEXT PRIMARY KEY, order_id TEXT, order_item_id TEXT, leg_index INT, origin_type TEXT, supplier_id TEXT, warehouse_id TEXT, supplier_warehouse_id TEXT, origin_label TEXT, quantity INT)`,
		`CREATE TABLE customer_returns (
			id UUID PRIMARY KEY, order_id VARCHAR(50) NOT NULL, customer_id UUID NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'requested', reason TEXT NOT NULL DEFAULT '',
			admin_note TEXT NOT NULL DEFAULT '', warehouse_id TEXT, prepaid BOOLEAN NOT NULL DEFAULT FALSE,
			tracking_number TEXT, shipped_at TIMESTAMPTZ, return_code TEXT,
			cod_refused BOOLEAN NOT NULL DEFAULT FALSE,
			return_shipping_amount BIGINT, return_shipping_bearer VARCHAR(20),
			return_shipping_settled_at TIMESTAMPTZ,
			destination_supplier_warehouse_id TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE return_items (
			id UUID PRIMARY KEY, return_id UUID NOT NULL, order_item_id VARCHAR(50) NOT NULL,
			variant_id UUID, qty INT NOT NULL, qc_status VARCHAR(20) NOT NULL DEFAULT 'pending', received_at TIMESTAMPTZ
		)`,
		`CREATE TABLE customer_return_shipping_charges (
			id TEXT PRIMARY KEY, return_id UUID NOT NULL, return_item_id UUID NOT NULL,
			supplier_id VARCHAR(50) NOT NULL, bearer VARCHAR(20) NOT NULL,
			amount BIGINT NOT NULL, ledger_entry_id VARCHAR(80),
			idempotency_key VARCHAR(150) NOT NULL UNIQUE, reason TEXT NOT NULL,
			actor_user_id VARCHAR(80) NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
		`CREATE TABLE payout_requests (
			id TEXT PRIMARY KEY, store_id TEXT, supplier_id TEXT, recipient_type TEXT, recipient_name TEXT,
			shaba TEXT, bank_name TEXT, amount BIGINT, status TEXT,
			available_balance BIGINT, rejection_reason TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), processed_at TIMESTAMPTZ,
			transfer_reference TEXT, transfer_confirmed_by TEXT, transfer_confirmed_at TIMESTAMPTZ
		)`,
		`CREATE TABLE seller_service_purchases (id TEXT PRIMARY KEY, store_id TEXT, amount BIGINT)`,
		`CREATE TABLE seller_early_withdrawals (id TEXT PRIMARY KEY, store_id TEXT, fee_amount BIGINT, gross_amount BIGINT, status TEXT)`,
		`INSERT INTO stores (id, owner_type) VALUES ('central', 'central')`,
		`CREATE TABLE supplier_return_financial_events (id TEXT PRIMARY KEY, supplier_id TEXT, operation TEXT, amount BIGINT)`,
		`CREATE TABLE inventory_movements (
			id TEXT PRIMARY KEY, product_id TEXT, supplier_id TEXT, quantity INT,
			from_status TEXT, to_status TEXT, from_location_key TEXT, to_location_key TEXT,
			reference_type TEXT, reference_id TEXT, idempotency_key TEXT UNIQUE,
			actor_user_id TEXT, metadata JSONB, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE inventory_balances (
			id TEXT PRIMARY KEY, product_id TEXT, supplier_id TEXT, owner_type TEXT,
			location_type TEXT, location_key TEXT, status TEXT, variant_id UUID, quantity INT NOT NULL DEFAULT 0,
			reconciled BOOLEAN NOT NULL DEFAULT FALSE, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (product_id, owner_type, location_type, location_key, status)
		)`,
		// The events table exactly as migration 202611050006 creates it.
		`CREATE TABLE customer_return_financial_events (
			id VARCHAR(80) PRIMARY KEY,
			return_id UUID NOT NULL REFERENCES customer_returns(id) ON DELETE CASCADE,
			return_item_id UUID NOT NULL REFERENCES return_items(id) ON DELETE CASCADE,
			order_id TEXT NOT NULL,
			supplier_id VARCHAR(50) REFERENCES suppliers(id) ON DELETE RESTRICT,
			store_id TEXT,
			component VARCHAR(20) NOT NULL CHECK (component IN ('product_cost','seller_profit')),
			operation VARCHAR(20) NOT NULL CHECK (operation IN ('debit','reversal','refund')),
			amount BIGINT NOT NULL CHECK (amount > 0),
			ledger_entry_id VARCHAR(80),
			idempotency_key VARCHAR(150) NOT NULL UNIQUE,
			reason TEXT NOT NULL,
			actor_user_id VARCHAR(80) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`INSERT INTO warehouses (id, name, province_name, city_name, priority) VALUES ('wh-1', 'انبار مرکزی', 'قم', 'قم', 10)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			db.Close()
			t.Fatalf("create disposable schema: %v (stmt %.60s)", err, stmt)
		}
	}
	installRefundFixtureSchema(t, db)
	return db
}

func gmSupplier(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO suppliers (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, id); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
}

func gmProduct(t *testing.T, db *sql.DB, id, supplierID string, stock int) {
	t.Helper()
	gmSupplier(t, db, supplierID)
	if _, err := db.Exec(`INSERT INTO products (id, title, stock, supplier_id, fulfillment_method, warehouse_name, weight_grams) VALUES ($1, $2, $3, NULLIF($4, ''), 'warehouse', 'انبار مرکزی', 1000)`,
		id, "Product "+id, stock, supplierID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
}

// gmBalance seeds one canonical sellable balance row (marks the
// product as canonical-covered).
func gmBalance(t *testing.T, db *sql.DB, productID, supplierID, locationKey string, qty int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO inventory_balances (id, product_id, supplier_id, owner_type, location_type, location_key, status, quantity, reconciled)
		VALUES ($1, $2, $3, 'supplier', 'warehouse', $4, 'sellable', $5, TRUE)`,
		"INV-"+uuid.NewString(), productID, supplierID, locationKey, qty); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
}

func gmOrder(t *testing.T, db *sql.DB, id, customerID, storeID string, captured, profitReleased bool, dropshipperProfit int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name) VALUES($1::uuid,$2,'Finance fixture') ON CONFLICT(id) DO NOTHING`, customerID, "f"+customerID[:12]); err != nil {
		t.Fatal(err)
	}
	if storeID == "" {
		storeID = "central"
	}
	if _, err := db.Exec(`INSERT INTO orders (id, customer_user_id, order_number, store_id, store_name, payable_amount, payment_method, payment_status, customer_full_name, customer_phone, customer_province, customer_city, customer_address, customer_postal_code, status, tracking_code, dropshipper_profit, profit_released, inventory_captured)
		VALUES ($1, $2::uuid, $3, $4, 'Store', 100000, 'cash', 'paid', 'Test Customer', '09120000000', 'Tehran', 'Tehran', 'Test St 1', '1234567890', 'delivered_confirmed', 'TRK-1', $5, $6, $7)`,
		id, customerID, "ORD-"+id, storeID, dropshipperProfit, profitReleased, captured); err != nil {
		t.Fatalf("seed order: %v", err)
	}
}

func gmItem(t *testing.T, db *sql.DB, orderID, productID string, qty int, wholesale, margin, price int64, variantID string) string {
	t.Helper()
	itemID := uuid.NewString()
	var variantArg interface{}
	if variantID != "" {
		variantArg = variantID
	}
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price, variant_id)
		VALUES ($1, $2, $3, $4, $5, 0, $6, $7, $8::uuid)`,
		itemID, orderID, productID, qty, wholesale, margin, price, variantArg); err != nil {
		t.Fatalf("seed order item: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,warehouse_id,origin_label,quantity) SELECT $1,$2,$3,0,'warehouse',supplier_id,'wh-1','انبار مرکزی',$4 FROM products WHERE id=$5`, "fixture-origin-"+itemID, orderID, itemID, qty, productID); err != nil {
		t.Fatal(err)
	}
	return itemID
}

// gmFileReturn runs the real create -> approve flow (prepaid) and
// returns the return id plus its return-item ids keyed by order item.
func gmFileReturn(t *testing.T, repo *OrderRepository, customerID, orderID string, items []CustomerReturnItemRequest) (string, map[string]string) {
	t.Helper()
	ctx := context.Background()
	seedVerifiedRefundFixturePayment(t, repo.db, orderID)
	ret, err := repo.CreateCustomerReturn(ctx, customerID, orderID, "gap test", items, CustomerReturnOptions{Prepaid: true})
	if err != nil {
		t.Fatalf("create return: %v", err)
	}
	if err := repo.ApproveCustomerReturn(ctx, ret.ID, ""); err != nil {
		t.Fatalf("approve return: %v", err)
	}
	return ret.ID, gmReturnItems(t, repo.db, ret.ID)
}

func gmReturnItems(t *testing.T, db *sql.DB, returnID string) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT order_item_id, id::text FROM return_items WHERE return_id = $1`, returnID)
	if err != nil {
		t.Fatalf("load return items: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var orderItemID, returnItemID string
		if err := rows.Scan(&orderItemID, &returnItemID); err != nil {
			t.Fatalf("scan return item: %v", err)
		}
		out[orderItemID] = returnItemID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("return items: %v", err)
	}
	return out
}

func gmReceive(t *testing.T, repo *OrderRepository, returnID string, qc map[string]string) {
	t.Helper()
	list := make([]CustomerReturnItemQC, 0, len(qc))
	for returnItemID, verdict := range qc {
		list = append(list, CustomerReturnItemQC{ReturnItemID: returnItemID, QCStatus: verdict})
	}
	if _, err := repo.ReceiveCustomerReturn(context.Background(), returnID, list, "keeper-gap"); err != nil {
		t.Fatalf("receive return: %v", err)
	}
}

func gmWallet(t *testing.T, db *sql.DB, customerID string) int64 {
	t.Helper()
	var bal int64
	if err := db.QueryRow(`SELECT COALESCE(balance, 0) FROM customer_wallets WHERE customer_id = $1::uuid`, customerID).Scan(&bal); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0
		}
		t.Fatalf("read wallet: %v", err)
	}
	return bal
}

func gmProductStock(t *testing.T, db *sql.DB, productID string) int {
	t.Helper()
	var stock int
	if err := db.QueryRow(`SELECT stock FROM products WHERE id = $1`, productID).Scan(&stock); err != nil {
		t.Fatalf("read product stock: %v", err)
	}
	return stock
}

func gmBalanceQty(t *testing.T, db *sql.DB, productID, locationKey, status string) int {
	t.Helper()
	var qty int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity), 0) FROM inventory_balances WHERE product_id = $1 AND location_key = $2 AND status = $3`, productID, locationKey, status).Scan(&qty); err != nil {
		t.Fatalf("read balance qty: %v", err)
	}
	return qty
}

// G6-T1: ok item on a canonical-covered product restocks sellable at
// receive (movement + holding-lot credit + products.stock + variant),
// and the refund claws the cost back.
func TestGap6RestockOkCoveredProduct(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	supplierRepo := NewSupplierRepository(db)
	customerID := uuid.NewString()

	gmProduct(t, db, "p-t1", "sup-t1", 5)
	gmBalance(t, db, "p-t1", "sup-t1", "warehouse:wh-1", 5)
	variantID := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO product_variants (id, product_id, stock) VALUES ($1::uuid, 'p-t1', 3)`, variantID); err != nil {
		t.Fatalf("seed variant: %v", err)
	}
	gmOrder(t, db, "ord-t1", customerID, "", true, false, 0)
	itemID := gmItem(t, db, "ord-t1", "p-t1", 2, 10000, 0, 20000, variantID)

	returnID, ri := gmFileReturn(t, repo, customerID, "ord-t1", []CustomerReturnItemRequest{{OrderItemID: itemID, Qty: 2}})
	gmReceive(t, repo, returnID, map[string]string{ri[itemID]: "ok"})

	if got := gmBalanceQty(t, db, "p-t1", "custreturn:"+returnID, "sellable"); got != 2 {
		t.Fatalf("holding-lot sellable = %d, want 2", got)
	}
	var movements int
	if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE idempotency_key = $1`, "crstk:"+returnID+":"+ri[itemID]+":sellable").Scan(&movements); err != nil || movements != 1 {
		t.Fatalf("restock movements = %d (err %v), want 1", movements, err)
	}
	if got := gmProductStock(t, db, "p-t1"); got != 7 {
		t.Fatalf("products.stock = %d, want 7 (5 + 2 restocked)", got)
	}
	var variantStock int
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id = $1::uuid`, variantID).Scan(&variantStock); err != nil || variantStock != 5 {
		t.Fatalf("variant stock = %d (err %v), want 5 (3 + 2)", variantStock, err)
	}
	// Replay: the status guard rejects a second receive.
	if _, err := repo.ReceiveCustomerReturn(ctx, returnID, []CustomerReturnItemQC{{ReturnItemID: ri[itemID], QCStatus: "ok"}}, "keeper-gap"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second receive err = %v, want sql.ErrNoRows", err)
	}
	if got := gmProductStock(t, db, "p-t1"); got != 7 {
		t.Fatalf("products.stock after replay = %d, want 7", got)
	}

	amount, _, err := repo.RefundCustomerReturn(ctx, returnID, "finance-gap")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if amount != 40000 {
		t.Fatalf("refund amount = %d, want 40000", amount)
	}
	bal, err := supplierRepo.GetSettlementBalance(ctx, "sup-t1")
	if err != nil {
		t.Fatalf("supplier balance: %v", err)
	}
	if bal.Earned != 20000 || bal.ReturnClawbacks != 20000 || bal.Available != 0 {
		t.Fatalf("balance = %+v, want earned 20000 clawbacks 20000 available 0", bal)
	}
}

// G6-T2: a defective item never becomes sellable (quarantine only),
// refunds in full under D1, and its cost claws back.
func TestGap6DefectiveQuarantineAndClawback(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	supplierRepo := NewSupplierRepository(db)
	customerID := uuid.NewString()

	gmProduct(t, db, "p-t2", "sup-t2", 4)
	gmBalance(t, db, "p-t2", "sup-t2", "warehouse:wh-1", 4)
	gmOrder(t, db, "ord-t2", customerID, "", true, false, 0)
	itemID := gmItem(t, db, "ord-t2", "p-t2", 1, 8000, 0, 15000, "")

	returnID, ri := gmFileReturn(t, repo, customerID, "ord-t2", []CustomerReturnItemRequest{{OrderItemID: itemID, Qty: 1}})
	gmReceive(t, repo, returnID, map[string]string{ri[itemID]: "defective"})

	if got := gmProductStock(t, db, "p-t2"); got != 4 {
		t.Fatalf("products.stock = %d, want 4 (defective never sellable)", got)
	}
	if got := gmBalanceQty(t, db, "p-t2", "custreturn:"+returnID, "quarantine"); got != 1 {
		t.Fatalf("quarantine credit = %d, want 1", got)
	}
	if got := gmBalanceQty(t, db, "p-t2", "custreturn:"+returnID, "sellable"); got != 0 {
		t.Fatalf("sellable credit = %d, want 0", got)
	}

	amount, _, err := repo.RefundCustomerReturn(ctx, returnID, "finance-gap")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if amount != 15000 {
		t.Fatalf("refund amount = %d, want the full 15000 (D1)", amount)
	}
	var evAmount int64
	var component string
	if err := db.QueryRow(`SELECT amount, component FROM customer_return_financial_events WHERE return_id = $1::uuid`, returnID).Scan(&evAmount, &component); err != nil {
		t.Fatalf("clawback event: %v", err)
	}
	if evAmount != 8000 || component != "product_cost" {
		t.Fatalf("clawback event = %d/%s, want 8000/product_cost", evAmount, component)
	}
	bal, err := supplierRepo.GetSettlementBalance(ctx, "sup-t2")
	if err != nil {
		t.Fatalf("supplier balance: %v", err)
	}
	// The supplier bears both deductions: the 8000 cost clawback and
	// the 12000 defective-item return fare (1000g at the seeded
	// tariff), so Available is 8000 - 12000 - 8000 = -12000.
	if bal.ReturnClawbacks != 8000 || bal.ReturnCharges != 12000 || bal.Available != -12000 {
		t.Fatalf("balance = %+v, want clawbacks 8000 charges 12000 available -12000", bal)
	}
}

// G6-T3: mixed return at partial quantity — the refund covers both
// items at the returned quantities only, and each item claws back at
// its own returned quantity.
func TestGap6MixedPartialReturn(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	gmProduct(t, db, "p-t3a", "sup-t3", 10)
	gmBalance(t, db, "p-t3a", "sup-t3", "warehouse:wh-1", 10)
	gmProduct(t, db, "p-t3b", "sup-t3", 10)
	gmBalance(t, db, "p-t3b", "sup-t3", "warehouse:wh-1", 10)
	gmOrder(t, db, "ord-t3", customerID, "", true, false, 0)
	itemA := gmItem(t, db, "ord-t3", "p-t3a", 2, 10000, 0, 20000, "")
	itemB := gmItem(t, db, "ord-t3", "p-t3b", 1, 5000, 0, 30000, "")

	returnID, ri := gmFileReturn(t, repo, customerID, "ord-t3", []CustomerReturnItemRequest{
		{OrderItemID: itemA, Qty: 1},
		{OrderItemID: itemB, Qty: 1},
	})
	gmReceive(t, repo, returnID, map[string]string{ri[itemA]: "ok", ri[itemB]: "defective"})

	amount, _, err := repo.RefundCustomerReturn(ctx, returnID, "finance-gap")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if amount != 50000 {
		t.Fatalf("refund = %d, want 50000 (1x20000 ok + 1x30000 defective, D1)", amount)
	}
	events := map[string]int64{}
	rows, err := db.Query(`SELECT return_item_id::text, amount FROM customer_return_financial_events WHERE return_id = $1::uuid`, returnID)
	if err != nil {
		t.Fatalf("clawback events: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rid string
		var amt int64
		if err := rows.Scan(&rid, &amt); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		events[rid] = amt
	}
	if events[ri[itemA]] != 10000 || events[ri[itemB]] != 5000 || len(events) != 2 {
		t.Fatalf("clawback events = %v, want A 10000 + B 5000 at returned qty only", events)
	}
}

// G6-T4: a dropshipper order's refund reverses the seller margin out
// of the store balance, while the order row itself stays untouched
// (delivered_confirmed, profit released).
func TestGap6SellerProfitReversal(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	financeRepo := NewFinanceRepository(db)
	customerID := uuid.NewString()

	if _, err := db.Exec(`INSERT INTO stores (id, owner_type) VALUES ('store-t4', 'dropshipper')`); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	gmProduct(t, db, "p-t4", "sup-t4", 3)
	gmBalance(t, db, "p-t4", "sup-t4", "warehouse:wh-1", 3)
	gmOrder(t, db, "ord-t4", customerID, "store-t4", true, true, 7000)
	itemID := gmItem(t, db, "ord-t4", "p-t4", 1, 10000, 7000, 20000, "")

	before, err := financeRepo.GetStoreBalance(ctx, "store-t4")
	if err != nil {
		t.Fatalf("store balance before: %v", err)
	}
	if before.AvailableAmount != 7000 {
		t.Fatalf("store available before = %d, want 7000 (released profit)", before.AvailableAmount)
	}

	returnID, ri := gmFileReturn(t, repo, customerID, "ord-t4", []CustomerReturnItemRequest{{OrderItemID: itemID, Qty: 1}})
	gmReceive(t, repo, returnID, map[string]string{ri[itemID]: "ok"})
	if _, _, err := repo.RefundCustomerReturn(ctx, returnID, "finance-gap"); err != nil {
		t.Fatalf("refund: %v", err)
	}

	after, err := financeRepo.GetStoreBalance(ctx, "store-t4")
	if err != nil {
		t.Fatalf("store balance after: %v", err)
	}
	if after.SellerReturnReversals != 7000 || after.AvailableAmount != 0 {
		t.Fatalf("store balance after = %+v, want reversals 7000 available 0", after)
	}
	var status string
	var released bool
	if err := db.QueryRow(`SELECT status, profit_released FROM orders WHERE id = 'ord-t4'`).Scan(&status, &released); err != nil {
		t.Fatalf("read order: %v", err)
	}
	if status != "delivered_confirmed" || !released {
		t.Fatalf("order mutated by refund: status %q released %v, want delivered_confirmed/true", status, released)
	}
}

// G6-T5: a refund replay is rejected by the status guard; the wallet
// and the event set stay exactly as the first refund left them.
func TestGap6RefundReplaySingleCredit(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	gmProduct(t, db, "p-t5", "sup-t5", 2)
	gmBalance(t, db, "p-t5", "sup-t5", "warehouse:wh-1", 2)
	gmOrder(t, db, "ord-t5", customerID, "", true, false, 0)
	itemID := gmItem(t, db, "ord-t5", "p-t5", 1, 10000, 0, 20000, "")

	returnID, ri := gmFileReturn(t, repo, customerID, "ord-t5", []CustomerReturnItemRequest{{OrderItemID: itemID, Qty: 1}})
	gmReceive(t, repo, returnID, map[string]string{ri[itemID]: "ok"})
	if _, _, err := repo.RefundCustomerReturn(ctx, returnID, "finance-gap"); err != nil {
		t.Fatalf("refund: %v", err)
	}
	walletAfter := gmWallet(t, db, customerID)
	var eventsAfter int
	if err := db.QueryRow(`SELECT COUNT(*) FROM customer_return_financial_events WHERE return_id = $1::uuid`, returnID).Scan(&eventsAfter); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if _, _, err := repo.RefundCustomerReturn(ctx, returnID, "finance-gap"); err == nil {
		t.Fatalf("second refund succeeded; want the received-status guard to reject it")
	}
	if got := gmWallet(t, db, customerID); got != walletAfter {
		t.Fatalf("wallet changed on replay: %d -> %d", walletAfter, got)
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM customer_return_financial_events WHERE return_id = $1::uuid`, returnID).Scan(&events); err != nil {
		t.Fatalf("count events after replay: %v", err)
	}
	if events != eventsAfter || events != 1 {
		t.Fatalf("events after replay = %d, want exactly the first refund's 1", events)
	}
}

// G6-T6: a supplier who already withdrew everything goes negative
// when a return claws back — the refund is never blocked on the
// supplier's balance.
func TestGap6ClawbackDrivesNegativeAvailable(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	supplierRepo := NewSupplierRepository(db)
	customerID := uuid.NewString()

	gmProduct(t, db, "p-t6", "sup-t6", 10)
	gmBalance(t, db, "p-t6", "sup-t6", "warehouse:wh-1", 10)
	// Order 1 earns 10000; order 2 earns (and later claws back) 6000.
	gmOrder(t, db, "ord-t6a", customerID, "", true, false, 0)
	gmItem(t, db, "ord-t6a", "p-t6", 1, 10000, 0, 20000, "")
	gmOrder(t, db, "ord-t6b", customerID, "", true, false, 0)
	itemB := gmItem(t, db, "ord-t6b", "p-t6", 1, 6000, 0, 12000, "")

	if _, err := supplierRepo.RequestSettlement(ctx, "sup-t6", 16000, "IR1", "Bank", "Holder"); err != nil {
		t.Fatalf("request full payout: %v", err)
	}
	if _, err := db.Exec(`UPDATE payout_requests SET status = 'paid' WHERE supplier_id = 'sup-t6'`); err != nil {
		t.Fatalf("mark payout paid: %v", err)
	}
	bal, err := supplierRepo.GetSettlementBalance(ctx, "sup-t6")
	if err != nil {
		t.Fatalf("balance after payout: %v", err)
	}
	if bal.Available != 0 {
		t.Fatalf("available after full payout = %d, want 0", bal.Available)
	}

	returnID, ri := gmFileReturn(t, repo, customerID, "ord-t6b", []CustomerReturnItemRequest{{OrderItemID: itemB, Qty: 1}})
	gmReceive(t, repo, returnID, map[string]string{ri[itemB]: "ok"})
	if _, _, err := repo.RefundCustomerReturn(ctx, returnID, "finance-gap"); err != nil {
		t.Fatalf("refund: %v", err)
	}
	bal, err = supplierRepo.GetSettlementBalance(ctx, "sup-t6")
	if err != nil {
		t.Fatalf("balance after clawback: %v", err)
	}
	if bal.Available != -6000 {
		t.Fatalf("available after clawback = %d, want -6000 (negative is tolerated)", bal.Available)
	}
}

// G6-T7: the over-return guard — two filings cannot claim more than
// the ordered quantity between them, and neither can one filing
// with a duplicated item.
func TestGap6OverReturnGuard(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	gmProduct(t, db, "p-t7", "sup-t7", 5)
	gmOrder(t, db, "ord-t7", customerID, "", true, false, 0)
	itemID := gmItem(t, db, "ord-t7", "p-t7", 2, 10000, 0, 20000, "")

	if _, err := repo.CreateCustomerReturn(ctx, customerID, "ord-t7", "first", []CustomerReturnItemRequest{{OrderItemID: itemID, Qty: 2}}, CustomerReturnOptions{Prepaid: true}); err != nil {
		t.Fatalf("first filing: %v", err)
	}
	if _, err := repo.CreateCustomerReturn(ctx, customerID, "ord-t7", "second", []CustomerReturnItemRequest{{OrderItemID: itemID, Qty: 1}}, CustomerReturnOptions{Prepaid: true}); err == nil {
		t.Fatalf("second filing over the ordered quantity succeeded; want rejection")
	}
	if _, err := repo.CreateCustomerReturn(ctx, customerID, "ord-t7", "duplicated", []CustomerReturnItemRequest{
		{OrderItemID: itemID, Qty: 2},
		{OrderItemID: itemID, Qty: 1},
	}, CustomerReturnOptions{Prepaid: true}); err == nil {
		t.Fatalf("duplicated-item filing over the ordered quantity succeeded; want rejection")
	}
}

// G6-T8: a legacy product (no canonical rows) is coverage-seeded on
// its first return receipt, so the legacy stock survives the
// projection and the returned unit lands on top: 10 -> 11.
func TestGap6LegacyCoverageSeeding(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	gmProduct(t, db, "p-t8", "sup-t8", 10)
	gmOrder(t, db, "ord-t8", customerID, "", true, false, 0)
	itemID := gmItem(t, db, "ord-t8", "p-t8", 1, 10000, 0, 20000, "")

	returnID, ri := gmFileReturn(t, repo, customerID, "ord-t8", []CustomerReturnItemRequest{{OrderItemID: itemID, Qty: 1}})
	gmReceive(t, repo, returnID, map[string]string{ri[itemID]: "ok"})

	if got := gmProductStock(t, db, "p-t8"); got != 11 {
		t.Fatalf("products.stock = %d, want 11 (legacy 10 seeded + 1 returned)", got)
	}
	var sellable int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity), 0) FROM inventory_balances WHERE product_id = 'p-t8' AND status = 'sellable'`).Scan(&sellable); err != nil {
		t.Fatalf("sellable sum: %v", err)
	}
	if sellable != 11 {
		t.Fatalf("sellable balances sum = %d, want 11", sellable)
	}
}

// G6-T9: dispute-cancelling the order AFTER a clawback must not
// double-reverse the cost: Earned drops the whole order and the
// clawback term excludes the cancelled order's events.
func TestGap6DisputeCannotRefundAlreadyReturnedPrincipal(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	supplierRepo := NewSupplierRepository(db)
	customerID := uuid.NewString()

	gmProduct(t, db, "p-t9", "sup-t9", 10)
	gmBalance(t, db, "p-t9", "sup-t9", "warehouse:wh-1", 10)
	gmOrder(t, db, "ord-t9a", customerID, "", true, false, 0)
	itemA := gmItem(t, db, "ord-t9a", "p-t9", 1, 10000, 0, 20000, "")
	gmOrder(t, db, "ord-t9b", customerID, "", true, false, 0)
	gmItem(t, db, "ord-t9b", "p-t9", 1, 30000, 0, 60000, "")

	returnID, ri := gmFileReturn(t, repo, customerID, "ord-t9a", []CustomerReturnItemRequest{{OrderItemID: itemA, Qty: 1}})
	gmReceive(t, repo, returnID, map[string]string{ri[itemA]: "ok"})
	if _, _, err := repo.RefundCustomerReturn(ctx, returnID, "finance-gap"); err != nil {
		t.Fatalf("refund: %v", err)
	}
	bal, err := supplierRepo.GetSettlementBalance(ctx, "sup-t9")
	if err != nil {
		t.Fatalf("balance before dispute: %v", err)
	}
	if bal.Earned != 40000 || bal.ReturnClawbacks != 10000 || bal.Available != 30000 {
		t.Fatalf("balance before dispute = %+v, want earned 40000 clawbacks 10000 available 30000", bal)
	}

	// A customer return already refunded this entire captured source. A later
	// dispute cannot issue it again or erase the existing supplier reversal.
	if _, err := db.Exec(`INSERT INTO disputes(id,order_id,resolution_action,status,claim_amount) VALUES('dsp-t9','ord-t9a',NULL,'pending',20000)`); err != nil {
		t.Fatal(err)
	}
	if err := NewFinanceRepository(db).ResolveDispute(ctx, "dsp-t9", "refund_supplier", "supplier", "finance-gap", 20000); !errors.Is(err, ErrRefundPrincipalUnresolved) {
		t.Fatalf("dispute double-spent returned principal %v", err)
	}
	bal, err = supplierRepo.GetSettlementBalance(ctx, "sup-t9")
	if err != nil {
		t.Fatalf("balance after dispute: %v", err)
	}
	if bal.Earned != 40000 || bal.ReturnClawbacks != 10000 || bal.Available != 30000 {
		t.Fatalf("balance after dispute = %+v, want earned 40000 clawbacks 10000 available 30000 (existing return preserved)", bal)
	}
}

// G15-T1: adjustment documents move supplier Available by their
// signed net (debit -, credit +); the platform leg and
// dispute-shaped ledger rows never move it.
func TestGap15AdjustmentsMoveSupplierAvailable(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	supplierRepo := NewSupplierRepository(db)
	customerID := uuid.NewString()

	gmProduct(t, db, "p-g15", "sup-g15", 10)
	gmOrder(t, db, "ord-g15", customerID, "", true, false, 0)
	gmItem(t, db, "ord-g15", "p-g15", 1, 100000, 0, 200000, "")

	insertLeg := func(id, account, typ, kind string, amount int64, reason string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO ledger_entries (id, target_account, target_type, type, amount, reason, operator) VALUES ($1, $2, $3, $4, $5, $6, 'finance-gap')`,
			id, account, typ, kind, amount, reason); err != nil {
			t.Fatalf("insert ledger leg %s: %v", id, err)
		}
	}
	// A debit document (بدهکار کردن — decrease), with its platform leg.
	insertLeg("led-adj1-pay", "supplier_payable:sup-g15", "supplier", "debit", 50000, "سند تعدیل جریمه تأخیر")
	insertLeg("led-adj1-plt", "platform", "platform", "credit", 50000, "سند تعدیل جریمه تأخیر")
	bal, err := supplierRepo.GetSettlementBalance(ctx, "sup-g15")
	if err != nil {
		t.Fatalf("balance after debit doc: %v", err)
	}
	if bal.Adjustments != -50000 || bal.Available != 50000 {
		t.Fatalf("after debit doc = %+v, want adjustments -50000 available 50000", bal)
	}
	// A credit document (بستانکار کردن — increase).
	insertLeg("led-adj2-pay", "supplier_payable:sup-g15", "supplier", "credit", 20000, "سند تعدیل جبران کسری")
	bal, err = supplierRepo.GetSettlementBalance(ctx, "sup-g15")
	if err != nil {
		t.Fatalf("balance after credit doc: %v", err)
	}
	if bal.Adjustments != -30000 || bal.Available != 70000 {
		t.Fatalf("after credit doc = %+v, want adjustments -30000 available 70000", bal)
	}
	// A dispute-resolution debit (raw party account, arbitration
	// reason) must NOT move the balance.
	insertLeg("led-dsp1", "sup-g15", "supplier", "debit", 40000, "صدور حکم داوری پرونده dsp-g15")
	bal, err = supplierRepo.GetSettlementBalance(ctx, "sup-g15")
	if err != nil {
		t.Fatalf("balance after dispute row: %v", err)
	}
	if bal.Adjustments != -30000 || bal.Available != 70000 {
		t.Fatalf("after dispute row = %+v, want unchanged adjustments -30000 available 70000", bal)
	}
}

// G15-T1b: the seller mirror — a store adjustment document moves
// GetStoreBalance's Available the same way.
func TestGap15SellerAdjustmentMirror(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	financeRepo := NewFinanceRepository(db)
	customerID := uuid.NewString()

	if _, err := db.Exec(`INSERT INTO stores (id, owner_type) VALUES ('store-g15', 'dropshipper')`); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	gmProduct(t, db, "p-g15s", "sup-g15s", 5)
	gmOrder(t, db, "ord-g15s", customerID, "store-g15", true, true, 100000)
	gmItem(t, db, "ord-g15s", "p-g15s", 1, 50000, 100000, 200000, "")

	if _, err := db.Exec(`INSERT INTO ledger_entries (id, target_account, target_type, type, amount, reason, operator) VALUES ('led-sadj1', 'store_payable:store-g15', 'seller', 'debit', 40000, 'سند تعدیل اصلاح کارمزد', 'finance-gap')`); err != nil {
		t.Fatalf("insert seller adjustment: %v", err)
	}
	bal, err := financeRepo.GetStoreBalance(ctx, "store-g15")
	if err != nil {
		t.Fatalf("store balance: %v", err)
	}
	if bal.Adjustments != -40000 || bal.AvailableAmount != 60000 {
		t.Fatalf("store balance = %+v, want adjustments -40000 available 60000", bal)
	}
}

// G15-T2: the treasury identity — SumAllSuppliersAvailable equals
// the signed sum of the per-supplier panel balances exactly,
// negative balances included.
func TestGap15TreasuryIdentity(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	supplierRepo := NewSupplierRepository(db)
	customerID := uuid.NewString()

	// sup-a: earned 100000, 30000 already paid out -> 70000.
	gmProduct(t, db, "p-g15a", "sup-g15a", 10)
	gmOrder(t, db, "ord-g15a", customerID, "", true, false, 0)
	gmItem(t, db, "ord-g15a", "p-g15a", 1, 100000, 0, 200000, "")
	if _, err := db.Exec(`INSERT INTO payout_requests (id, supplier_id, amount, status) VALUES ('pay-g15a', 'sup-g15a', 30000, 'paid')`); err != nil {
		t.Fatalf("seed payout: %v", err)
	}
	// sup-b: earned 50000, an 80000 clawback event on a delivered
	// order, a 10000 credit adjustment -> -20000.
	gmProduct(t, db, "p-g15b", "sup-g15b", 10)
	gmOrder(t, db, "ord-g15b", customerID, "", true, false, 0)
	itemB := gmItem(t, db, "ord-g15b", "p-g15b", 1, 50000, 0, 100000, "")
	repo := NewOrderRepository(db)
	returnID, ri := gmFileReturn(t, repo, customerID, "ord-g15b", []CustomerReturnItemRequest{{OrderItemID: itemB, Qty: 1}})
	if _, err := db.Exec(`INSERT INTO customer_return_financial_events (id, return_id, return_item_id, order_id, supplier_id, component, operation, amount, idempotency_key, reason, actor_user_id)
		VALUES ('CRF-g15b', $1::uuid, $2::uuid, 'ord-g15b', 'sup-g15b', 'product_cost', 'debit', 80000, 'crcb:manual:g15b', 'seeded clawback', 'test')`, returnID, ri[itemB]); err != nil {
		t.Fatalf("seed clawback event: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO ledger_entries (id, target_account, target_type, type, amount, reason, operator) VALUES ('led-g15b', 'supplier_payable:sup-g15b', 'supplier', 'credit', 10000, 'سند تعدیل جبران', 'finance-gap')`); err != nil {
		t.Fatalf("seed adjustment: %v", err)
	}

	balA, err := supplierRepo.GetSettlementBalance(ctx, "sup-g15a")
	if err != nil {
		t.Fatalf("balance A: %v", err)
	}
	balB, err := supplierRepo.GetSettlementBalance(ctx, "sup-g15b")
	if err != nil {
		t.Fatalf("balance B: %v", err)
	}
	if balA.Available != 70000 || balB.Available != -20000 {
		t.Fatalf("panel balances = A %d / B %d, want 70000 / -20000", balA.Available, balB.Available)
	}
	total, err := supplierRepo.SumAllSuppliersAvailable(ctx)
	if err != nil {
		t.Fatalf("treasury sum: %v", err)
	}
	if total != balA.Available+balB.Available || total != 50000 {
		t.Fatalf("treasury sum = %d, want exactly Σ panel Available = 50000", total)
	}
}

// G15-T3: the payout gate sees the adjustments term — after a debit
// adjustment, a request above the new Available is refused while a
// request at the new Available still goes through.
func TestGap15PayoutGateSeesAdjustment(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	supplierRepo := NewSupplierRepository(db)
	customerID := uuid.NewString()

	gmProduct(t, db, "p-g15t3", "sup-g15t3", 10)
	gmOrder(t, db, "ord-g15t3", customerID, "", true, false, 0)
	gmItem(t, db, "ord-g15t3", "p-g15t3", 1, 100000, 0, 200000, "")

	if _, err := db.Exec(`INSERT INTO ledger_entries (id, target_account, target_type, type, amount, reason, operator) VALUES ('led-g15t3', 'supplier_payable:sup-g15t3', 'supplier', 'debit', 50000, 'سند تعدیل کسر خسارت', 'finance-gap')`); err != nil {
		t.Fatalf("seed adjustment: %v", err)
	}
	if _, err := supplierRepo.RequestSettlement(ctx, "sup-g15t3", 80000, "IR1", "Bank", "Holder"); err == nil {
		t.Fatalf("payout of 80000 above the adjusted Available (50000) succeeded; want refusal")
	}
	if _, err := supplierRepo.RequestSettlement(ctx, "sup-g15t3", 50000, "IR1", "Bank", "Holder"); err != nil {
		t.Fatalf("payout at the adjusted Available refused: %v", err)
	}
}
