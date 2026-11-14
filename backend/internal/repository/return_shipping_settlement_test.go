package repository

// Integration tests for the customer-return shipping settlement
// (migration 202610250202, owner policy Oct 25 2026): QC verdict
// decides who bears the return shipping — defective -> supplier
// (charged at receive, credited to the customer at refund), ok
// (taste) -> customer (debited from the wallet at refund, after the
// product refund, clamped at zero).
//
// Opt-in: skipped unless TEAM04_TEST_DATABASE_URL points at a
// DISPOSABLE Postgres. The test creates and drops its own minimal
// tables and must never run against a live or shared database.
//
// Tariff used below: base 100000 Rial, per-kg 20000 Rial, volumetric
// divisor default 5000, default weight default 500g. A leg fare is
// (base + perKg * ceil(kg) + 5) / 10 Toman, so:
//   500g -> 12000, 1000g -> 12000, 2000g -> 14000,
//   3000g -> 16000, 10000g -> 30000 (Toman).

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func openReturnSettlementDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB integration test")
	}
	db := openPrivateFinanceFixture(t, dsn)
	var err error
	if err != nil {
		t.Fatalf("open disposable database: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("disposable database unavailable: %v", err)
	}
	stmts := []string{
		`DROP TABLE IF EXISTS order_inventory_returns,order_inventory_captures,customer_return_shipping_charges, return_items, customer_returns, customer_wallets, ledger_entries, order_item_origins, order_items, orders, products, payout_requests, supplier_return_financial_events, platform_settings, customer_return_financial_events, inventory_movements, inventory_balances CASCADE`,
		`DROP TABLE IF EXISTS users,suppliers,stores,payment_transactions,referral_bonuses CASCADE`,
		`CREATE TABLE users(id UUID PRIMARY KEY,mobile TEXT,full_name TEXT)`,
		`CREATE TABLE suppliers(id TEXT PRIMARY KEY)`,
		`CREATE TABLE stores(id TEXT PRIMARY KEY)`,
		`CREATE TABLE payment_transactions(id TEXT PRIMARY KEY,order_id TEXT)`,
		`CREATE TABLE referral_bonuses(id TEXT PRIMARY KEY,referrer_store_id TEXT,referred_store_id TEXT)`,
		`DROP SEQUENCE IF EXISTS customer_return_code_seq`,
		`CREATE SEQUENCE customer_return_code_seq`,
		`CREATE TABLE platform_settings (id INT PRIMARY KEY, shipping_base_rate BIGINT, shipping_per_kg BIGINT, volumetric_divisor BIGINT, default_item_weight_grams BIGINT)`,
		`INSERT INTO platform_settings (id, shipping_base_rate, shipping_per_kg, volumetric_divisor, default_item_weight_grams) VALUES (1, 100000, 20000, NULL, NULL)`,
		`CREATE TABLE products (id TEXT PRIMARY KEY, title TEXT, supplier_id TEXT, fulfillment_method TEXT, origin_supplier_warehouse_id TEXT, warehouse_name TEXT, weight_grams INT, length_mm INT, width_mm INT, height_mm INT, stock INT NOT NULL DEFAULT 0)`,
		`CREATE TABLE orders (id TEXT PRIMARY KEY, customer_user_id UUID, order_number TEXT, payment_status TEXT, status TEXT, store_id TEXT,referrer_seller_id TEXT, inventory_captured BOOLEAN NOT NULL DEFAULT FALSE)`,
		`CREATE TABLE order_items (id TEXT PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT,variant_id UUID, wholesale_cost BIGINT, seller_margin BIGINT, final_unit_price BIGINT)`,
		`CREATE TABLE payout_requests (id TEXT PRIMARY KEY, supplier_id TEXT, amount BIGINT, status TEXT)`,
		`CREATE TABLE supplier_return_financial_events (id TEXT PRIMARY KEY, supplier_id TEXT, operation TEXT, amount BIGINT)`,
		`CREATE TABLE ledger_entries (id VARCHAR(80) PRIMARY KEY, date TIMESTAMPTZ NOT NULL DEFAULT NOW(), target_account VARCHAR(150) NOT NULL, target_type VARCHAR(20) NOT NULL, type VARCHAR(10) NOT NULL, amount BIGINT NOT NULL, reason TEXT NOT NULL, operator VARCHAR(100) NOT NULL, related_order_or_dispute VARCHAR(50), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE customer_returns (id UUID PRIMARY KEY, order_id VARCHAR(50) NOT NULL, customer_id UUID NOT NULL, status VARCHAR(20) NOT NULL DEFAULT 'requested', reason TEXT NOT NULL DEFAULT '', admin_note TEXT NOT NULL DEFAULT '', warehouse_id TEXT, prepaid BOOLEAN NOT NULL DEFAULT FALSE, tracking_number TEXT, shipped_at TIMESTAMPTZ, return_code TEXT, cod_refused BOOLEAN NOT NULL DEFAULT FALSE, return_shipping_amount BIGINT, return_shipping_bearer VARCHAR(20), return_shipping_settled_at TIMESTAMPTZ, destination_supplier_warehouse_id TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE return_items (id UUID PRIMARY KEY, return_id UUID NOT NULL, order_item_id VARCHAR(50) NOT NULL, variant_id UUID, qty INT NOT NULL, qc_status VARCHAR(20) NOT NULL DEFAULT 'pending', received_at TIMESTAMPTZ)`,
		`CREATE TABLE customer_wallets (customer_id UUID PRIMARY KEY, balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE customer_return_shipping_charges (id TEXT PRIMARY KEY, return_id UUID NOT NULL, return_item_id UUID NOT NULL, supplier_id VARCHAR(50) NOT NULL, bearer VARCHAR(20) NOT NULL CHECK (bearer IN ('supplier','customer')), amount BIGINT NOT NULL CHECK (amount > 0), ledger_entry_id VARCHAR(80), idempotency_key VARCHAR(150) NOT NULL UNIQUE, reason TEXT NOT NULL, actor_user_id VARCHAR(80) NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE order_item_origins (id TEXT PRIMARY KEY, order_id TEXT, order_item_id TEXT, leg_index INT, origin_type TEXT, supplier_id TEXT, warehouse_id TEXT, supplier_warehouse_id TEXT, origin_label TEXT, quantity INT)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatalf("create disposable schema: %v (stmt %.60s)", err, stmt)
		}
	}
	return db
}

type rsItemSpec struct {
	productID  string
	supplierID string
	weightG    *int // nil -> NULL weight (platform default path)
	qty        int  // ordered quantity
	returnQty  int
	unitPrice  int64
	originWH   string // "" -> no origin row
	originQty  int    // 0 -> defaults to qty
}

func rsSeedReturn(t *testing.T, db *sql.DB, prepaid bool, specs []rsItemSpec) (string, string, []string) {
	t.Helper()
	ctx := context.Background()
	customerID := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id) VALUES($1::uuid)`, customerID); err != nil {
		t.Fatal(err)
	}
	orderID := "ord-rs-" + uuid.NewString()[:8]
	if _, err := db.ExecContext(ctx, `INSERT INTO orders (id, customer_user_id, order_number, payment_status, status) VALUES ($1, $2::uuid, $3, 'paid', 'delivered_confirmed')`, orderID, customerID, "ORD-"+orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	returnID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO customer_returns (id, order_id, customer_id, status, prepaid) VALUES ($1::uuid, $2, $3::uuid, 'approved', $4)`, returnID, orderID, customerID, prepaid); err != nil {
		t.Fatalf("seed return: %v", err)
	}
	returnItemIDs := make([]string, 0, len(specs))
	for _, sp := range specs {
		if sp.supplierID != "" {
			if _, err := db.Exec(`INSERT INTO suppliers(id) VALUES($1) ON CONFLICT DO NOTHING`, sp.supplierID); err != nil {
				t.Fatal(err)
			}
		}

		if _, err := db.ExecContext(ctx, `INSERT INTO products (id, title, supplier_id, fulfillment_method, warehouse_name, weight_grams) VALUES ($1, $2, $3, 'warehouse', 'انبار مرکزی', $4) ON CONFLICT (id) DO NOTHING`, sp.productID, "Product "+sp.productID, sp.supplierID, sp.weightG); err != nil {
			t.Fatalf("seed product: %v", err)
		}
		orderItemID := uuid.NewString()
		if _, err := db.ExecContext(ctx, `INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, seller_margin, final_unit_price) VALUES ($1, $2, $3, $4, 0, 0, $5)`, orderItemID, orderID, sp.productID, sp.qty, sp.unitPrice); err != nil {
			t.Fatalf("seed order item: %v", err)
		}
		{
			originWarehouse := sp.originWH
			if originWarehouse == "" {
				originWarehouse = "fixture-return-warehouse"
			}
			oq := sp.originQty
			if oq == 0 {
				oq = sp.qty
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO order_item_origins (id, order_id, order_item_id, leg_index, origin_type, supplier_id, warehouse_id, origin_label, quantity) VALUES ($1, $2, $3, 0, 'warehouse', $4, $5, 'انبار مرکزی', $6)`, uuid.NewString(), orderID, orderItemID, sp.supplierID, originWarehouse, oq); err != nil {
				t.Fatalf("seed origin: %v", err)
			}
		}
		riID := uuid.NewString()
		if _, err := db.ExecContext(ctx, `INSERT INTO return_items (id, return_id, order_item_id, qty, qc_status) VALUES ($1::uuid, $2::uuid, $3, $4, 'pending')`, riID, returnID, orderItemID, sp.returnQty); err != nil {
			t.Fatalf("seed return item: %v", err)
		}
		returnItemIDs = append(returnItemIDs, riID)
	}
	return returnID, customerID, returnItemIDs
}

func rsWallet(t *testing.T, db *sql.DB, customerID string) int64 {
	t.Helper()
	var balance int64
	err := db.QueryRow(`SELECT balance FROM customer_wallets WHERE customer_id = $1::uuid`, customerID).Scan(&balance)
	if err == sql.ErrNoRows {
		return 0
	}
	if err != nil {
		t.Fatalf("read wallet: %v", err)
	}
	return balance
}

func rsCharges(t *testing.T, db *sql.DB, returnID string) []struct {
	ReturnItemID, SupplierID, Bearer string
	Amount                           int64
	LedgerID                         sql.NullString
	Idem                             string
} {
	t.Helper()
	rows, err := db.Query(`SELECT return_item_id::text, supplier_id, bearer, amount, ledger_entry_id, idempotency_key FROM customer_return_shipping_charges WHERE return_id = $1::uuid ORDER BY return_item_id`, returnID)
	if err != nil {
		t.Fatalf("read charges: %v", err)
	}
	defer rows.Close()
	var out []struct {
		ReturnItemID, SupplierID, Bearer string
		Amount                           int64
		LedgerID                         sql.NullString
		Idem                             string
	}
	for rows.Next() {
		var c struct {
			ReturnItemID, SupplierID, Bearer string
			Amount                           int64
			LedgerID                         sql.NullString
			Idem                             string
		}
		if err := rows.Scan(&c.ReturnItemID, &c.SupplierID, &c.Bearer, &c.Amount, &c.LedgerID, &c.Idem); err != nil {
			t.Fatalf("scan charge: %v", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("charges rows: %v", err)
	}
	return out
}

func intPtr(v int) *int { return &v }

// (1) Defective single item: supplier charged at receive, balance
// drops below zero, refund credits refund + shipping to the wallet.
func TestReturnShippingDefectiveChargedToSupplier(t *testing.T) {
	db := openReturnSettlementDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	supRepo := NewSupplierRepository(db)
	returnID, customerID, itemIDs := rsSeedReturn(t, db, true, []rsItemSpec{
		{productID: "p-def", supplierID: "sup-1", weightG: intPtr(1000), qty: 1, returnQty: 1, unitPrice: 50000, originWH: "wh-1"},
	})

	balanceBefore, err := supRepo.GetSettlementBalance(ctx, "sup-1")
	if err != nil {
		t.Fatalf("supplier balance before: %v", err)
	}
	settlement, err := repo.ReceiveCustomerReturn(ctx, returnID, []CustomerReturnItemQC{{ReturnItemID: itemIDs[0], QCStatus: "defective"}}, "keeper-1")
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if settlement.ReturnShippingAmount != 12000 || settlement.ReturnShippingBearer != "supplier" || settlement.SupplierBorneTotal != 12000 || settlement.CustomerBorneTotal != 0 {
		t.Fatalf("settlement = %+v, want amount 12000 bearer supplier supplierBorne 12000 customerBorne 0", settlement)
	}
	charges := rsCharges(t, db, returnID)
	if len(charges) != 1 || charges[0].Bearer != "supplier" || charges[0].Amount != 12000 || charges[0].SupplierID != "sup-1" || !charges[0].LedgerID.Valid {
		t.Fatalf("charges = %+v, want one supplier row of 12000 with a ledger id", charges)
	}
	if charges[0].Idem != "crsc:"+returnID+":"+itemIDs[0] {
		t.Fatalf("idempotency key = %q", charges[0].Idem)
	}
	var ledgerAmount int64
	var ledgerTarget, ledgerType, ledgerOperator, ledgerRelated string
	if err := db.QueryRow(`SELECT amount, target_account, type, operator, related_order_or_dispute FROM ledger_entries WHERE id = $1`, charges[0].LedgerID.String).Scan(&ledgerAmount, &ledgerTarget, &ledgerType, &ledgerOperator, &ledgerRelated); err != nil {
		t.Fatalf("read ledger entry: %v", err)
	}
	if ledgerAmount != 12000 || ledgerTarget != "sup-1" || ledgerType != "debit" || ledgerOperator != "keeper-1" || ledgerRelated != returnID {
		t.Fatalf("ledger = %d/%s/%s/%s/%s", ledgerAmount, ledgerTarget, ledgerType, ledgerOperator, ledgerRelated)
	}
	// The supplier panel reader lights up on the same rows.
	panelCharges, err := supRepo.ListSupplierReturnShippingCharges(ctx, "sup-1")
	if err != nil || len(panelCharges) != 1 || panelCharges[0].Amount != 12000 {
		t.Fatalf("panel charges = %+v, err %v", panelCharges, err)
	}
	balance, err := supRepo.GetSettlementBalance(ctx, "sup-1")
	if err != nil {
		t.Fatalf("supplier balance: %v", err)
	}
	if delta := balance.ReturnCharges - balanceBefore.ReturnCharges; delta != 12000 {
		t.Fatalf("returnCharges delta = %d, want +12000 (balance %+v)", delta, balance)
	}
	if delta := balance.Available - balanceBefore.Available; delta != -12000 {
		t.Fatalf("available delta = %d, want -12000 (balance %+v)", delta, balance)
	}
	if balance.Available >= 0 {
		t.Fatalf("available = %d, want negative after the charge (policy: may go negative)", balance.Available)
	}
	// A return_code was generated at receipt.
	ret, err := repo.GetCustomerReturn(ctx, returnID)
	if err != nil || ret == nil || ret.ReturnCode == nil || *ret.ReturnCode == "" {
		t.Fatalf("return code not generated: %+v, err %v", ret, err)
	}
	if ret.ReturnShippingAmount == nil || *ret.ReturnShippingAmount != 12000 {
		t.Fatalf("return DTO shipping amount = %v", ret.ReturnShippingAmount)
	}

	amount, refundSettlement, err := repo.RefundCustomerReturn(ctx, returnID, "finance-1")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if amount != 50000 {
		t.Fatalf("product refund = %d, want 50000 (no ok items -> all-items fallback)", amount)
	}
	if refundSettlement == nil || refundSettlement.SupplierBorneTotal != 12000 || refundSettlement.CustomerDebited != 0 {
		t.Fatalf("refund settlement = %+v", refundSettlement)
	}
	if got := rsWallet(t, db, customerID); got != 62000 {
		t.Fatalf("wallet = %d, want 62000 (50000 refund + 12000 supplier-borne shipping)", got)
	}
	var settledAt sql.NullTime
	if err := db.QueryRow(`SELECT return_shipping_settled_at FROM customer_returns WHERE id = $1`, returnID).Scan(&settledAt); err != nil || !settledAt.Valid {
		t.Fatalf("settled_at not set: %v", err)
	}
}

// (2) Taste ('ok') item: nothing moves at receive; at refund the
// wallet nets refund - share. Plus the clamp case: share > refund
// leaves the wallet at exactly 0 and the charge row records only
// what was actually debited.
func TestReturnShippingTasteDebitedAndClamped(t *testing.T) {
	db := openReturnSettlementDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)

	returnID, customerID, itemIDs := rsSeedReturn(t, db, true, []rsItemSpec{
		{productID: "p-ok", supplierID: "sup-1", weightG: intPtr(1000), qty: 1, returnQty: 1, unitPrice: 50000, originWH: "wh-1"},
	})
	settlement, err := repo.ReceiveCustomerReturn(ctx, returnID, []CustomerReturnItemQC{{ReturnItemID: itemIDs[0], QCStatus: "ok"}}, "keeper-1")
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if settlement.ReturnShippingBearer != "customer" || settlement.CustomerBorneTotal != 12000 || settlement.SupplierBorneTotal != 0 {
		t.Fatalf("settlement = %+v, want bearer customer, customerBorne 12000", settlement)
	}
	// Fix #2: no money moves at receive, but the customer's share is
	// now PERSISTED at receive (bearer='customer' row at the
	// receive-time amount, no ledger entry); the refund stage
	// finalizes it to the actually debited amount.
	if charges := rsCharges(t, db, returnID); len(charges) != 1 || charges[0].Bearer != "customer" || charges[0].Amount != 12000 || charges[0].LedgerID.Valid {
		t.Fatalf("charges at receive = %+v, want one persisted customer row of 12000 without ledger id", charges)
	}
	if got := rsWallet(t, db, customerID); got != 0 {
		t.Fatalf("wallet moved at receive: %d", got)
	}
	_, refundSettlement, err := repo.RefundCustomerReturn(ctx, returnID, "finance-1")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if got := rsWallet(t, db, customerID); got != 38000 {
		t.Fatalf("wallet = %d, want 38000 (50000 - 12000)", got)
	}
	if refundSettlement.CustomerDebited != 12000 || refundSettlement.CustomerWrittenOff != 0 {
		t.Fatalf("refund settlement = %+v, want debited 12000 writtenOff 0", refundSettlement)
	}
	charges := rsCharges(t, db, returnID)
	if len(charges) != 1 || charges[0].Bearer != "customer" || charges[0].Amount != 12000 || charges[0].LedgerID.Valid {
		t.Fatalf("charges after refund = %+v, want one customer row of 12000 without ledger id", charges)
	}

	// Clamp: 10000g -> fare 30000 against a 5000 product refund.
	returnID2, customerID2, itemIDs2 := rsSeedReturn(t, db, true, []rsItemSpec{
		{productID: "p-heavy", supplierID: "sup-1", weightG: intPtr(10000), qty: 1, returnQty: 1, unitPrice: 5000, originWH: "wh-1"},
	})
	if _, err := repo.ReceiveCustomerReturn(ctx, returnID2, []CustomerReturnItemQC{{ReturnItemID: itemIDs2[0], QCStatus: "ok"}}, "keeper-1"); err != nil {
		t.Fatalf("receive clamp case: %v", err)
	}
	_, clampSettlement, err := repo.RefundCustomerReturn(ctx, returnID2, "finance-1")
	if err != nil {
		t.Fatalf("refund clamp case: %v", err)
	}
	if got := rsWallet(t, db, customerID2); got != 0 {
		t.Fatalf("clamp wallet = %d, want exactly 0", got)
	}
	if clampSettlement.CustomerBorneTotal != 30000 || clampSettlement.CustomerDebited != 5000 || clampSettlement.CustomerWrittenOff != 25000 {
		t.Fatalf("clamp settlement = %+v, want borne 30000 debited 5000 writtenOff 25000", clampSettlement)
	}
	charges2 := rsCharges(t, db, returnID2)
	if len(charges2) != 1 || charges2[0].Amount != 5000 {
		t.Fatalf("clamp charges = %+v, want one customer row of the actually-debited 5000", charges2)
	}
}

// (3) Mixed return across two suppliers and two origin legs.
func TestReturnShippingMixedSuppliersAndLegs(t *testing.T) {
	db := openReturnSettlementDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	returnID, customerID, itemIDs := rsSeedReturn(t, db, true, []rsItemSpec{
		{productID: "p-a", supplierID: "sup-1", weightG: intPtr(1000), qty: 1, returnQty: 1, unitPrice: 40000, originWH: "wh-1"},
		{productID: "p-b", supplierID: "sup-2", weightG: intPtr(2000), qty: 1, returnQty: 1, unitPrice: 60000, originWH: "wh-2"},
	})
	settlement, err := repo.ReceiveCustomerReturn(ctx, returnID, []CustomerReturnItemQC{
		{ReturnItemID: itemIDs[0], QCStatus: "defective"},
		{ReturnItemID: itemIDs[1], QCStatus: "ok"},
	}, "keeper-1")
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if settlement.ReturnShippingAmount != 26000 || settlement.ReturnShippingBearer != "mixed" ||
		settlement.SupplierBorneTotal != 12000 || settlement.CustomerBorneTotal != 14000 {
		t.Fatalf("settlement = %+v, want amount 26000 mixed supplier 12000 customer 14000 (separate legs)", settlement)
	}
	charges := rsCharges(t, db, returnID)
	// Fix #2: at receive BOTH bearers' rows are persisted — sup-1's
	// supplier row (12000, charged) and sup-2's customer row (14000,
	// recorded at the receive-time amount, collected at refund).
	if len(charges) != 2 {
		t.Fatalf("charges at receive = %+v, want supplier + customer rows", charges)
	}
	byBearer := map[string]int64{}
	for _, c := range charges {
		byBearer[c.Bearer] = c.Amount
	}
	if byBearer["supplier"] != 12000 || byBearer["customer"] != 14000 {
		t.Fatalf("charges at receive = %+v, want supplier 12000 + customer 14000", charges)
	}
	amount, _, err := repo.RefundCustomerReturn(ctx, returnID, "finance-1")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	// Refund basis is ALL received items (owner decision D1, Nov 5
	// 2026 — GAP 6): the defective item refunds in full too, so the
	// product refund is 40000 + 60000, not just the ok item's 60000.
	if amount != 100000 {
		t.Fatalf("product refund = %d, want 100000 (all items, D1 basis)", amount)
	}
	if got := rsWallet(t, db, customerID); got != 98000 {
		t.Fatalf("wallet = %d, want 98000 (100000 + 12000 - 14000)", got)
	}
	if charges := rsCharges(t, db, returnID); len(charges) != 2 {
		t.Fatalf("charges after refund = %+v, want supplier + customer rows", charges)
	}
}

// (4) Partial-quantity return prices on the returned quantity only,
// and two items sharing one leg split that leg's fare pro-rata with
// exact remainder absorption.
func TestReturnShippingPartialQtyAndSharedLeg(t *testing.T) {
	db := openReturnSettlementDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)

	// Ordered 3, returned 2 -> 2000g leg -> 14000, not the full 16000.
	returnID, _, itemIDs := rsSeedReturn(t, db, true, []rsItemSpec{
		{productID: "p-part", supplierID: "sup-1", weightG: intPtr(1000), qty: 3, returnQty: 2, unitPrice: 10000, originWH: "wh-1"},
	})
	settlement, err := repo.ReceiveCustomerReturn(ctx, returnID, []CustomerReturnItemQC{{ReturnItemID: itemIDs[0], QCStatus: "defective"}}, "keeper-1")
	if err != nil {
		t.Fatalf("receive partial: %v", err)
	}
	if settlement.ReturnShippingAmount != 14000 {
		t.Fatalf("partial fare = %d, want 14000 (returned qty only)", settlement.ReturnShippingAmount)
	}

	// Shared leg: 1000g + 2000g from the same warehouse = one 3000g
	// leg, fare 16000, split 5333 + 10667 (order between the two
	// items is id-sorted, so assert the set).
	returnID2, _, itemIDs2 := rsSeedReturn(t, db, true, []rsItemSpec{
		{productID: "p-s1", supplierID: "sup-9", weightG: intPtr(1000), qty: 1, returnQty: 1, unitPrice: 10000, originWH: "wh-1"},
		{productID: "p-s2", supplierID: "sup-9", weightG: intPtr(2000), qty: 1, returnQty: 1, unitPrice: 10000, originWH: "wh-1"},
	})
	settlement2, err := repo.ReceiveCustomerReturn(ctx, returnID2, []CustomerReturnItemQC{
		{ReturnItemID: itemIDs2[0], QCStatus: "defective"},
		{ReturnItemID: itemIDs2[1], QCStatus: "defective"},
	}, "keeper-1")
	if err != nil {
		t.Fatalf("receive shared leg: %v", err)
	}
	if settlement2.ReturnShippingAmount != 16000 {
		t.Fatalf("shared-leg fare = %d, want 16000 (one leg, base charged once)", settlement2.ReturnShippingAmount)
	}
	charges := rsCharges(t, db, returnID2)
	if len(charges) != 2 {
		t.Fatalf("shared-leg charges = %+v, want 2", charges)
	}
	amounts := map[int64]bool{}
	var sum int64
	for _, c := range charges {
		amounts[c.Amount] = true
		sum += c.Amount
	}
	// Pro-rata floor division gives 5333/10666-style shares; which
	// item absorbs the remainder depends on the id-sorted part
	// order, so either complementary split is correct as long as
	// the shares sum EXACTLY to the leg fare.
	splitOK := (amounts[5333] && amounts[10667]) || (amounts[5334] && amounts[10666])
	if sum != 16000 || !splitOK {
		t.Fatalf("shared-leg split = %v (sum %d), want {5333, 10667} or {5334, 10666} summing to the fare", amounts, sum)
	}
}

// (5) Replays: a second receive and a second refund change nothing.
func TestReturnShippingReplaysIdempotent(t *testing.T) {
	db := openReturnSettlementDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	returnID, customerID, itemIDs := rsSeedReturn(t, db, true, []rsItemSpec{
		{productID: "p-rep", supplierID: "sup-1", weightG: intPtr(1000), qty: 1, returnQty: 1, unitPrice: 50000, originWH: "wh-1"},
	})
	qc := []CustomerReturnItemQC{{ReturnItemID: itemIDs[0], QCStatus: "defective"}}
	if _, err := repo.ReceiveCustomerReturn(ctx, returnID, qc, "keeper-1"); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if _, err := repo.ReceiveCustomerReturn(ctx, returnID, qc, "keeper-1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second receive err = %v, want sql.ErrNoRows (status guard)", err)
	}
	if charges := rsCharges(t, db, returnID); len(charges) != 1 {
		t.Fatalf("charges after replayed receive = %d, want 1", len(charges))
	}
	if _, _, err := repo.RefundCustomerReturn(ctx, returnID, "finance-1"); err != nil {
		t.Fatalf("refund: %v", err)
	}
	var settledAt1 string
	if err := db.QueryRow(`SELECT return_shipping_settled_at::text FROM customer_returns WHERE id = $1`, returnID).Scan(&settledAt1); err != nil {
		t.Fatalf("read settled_at: %v", err)
	}
	walletAfter := rsWallet(t, db, customerID)
	if _, _, err := repo.RefundCustomerReturn(ctx, returnID, "finance-1"); err == nil {
		t.Fatalf("second refund succeeded; want the received-status guard to reject it")
	}
	var settledAt2 string
	if err := db.QueryRow(`SELECT return_shipping_settled_at::text FROM customer_returns WHERE id = $1`, returnID).Scan(&settledAt2); err != nil {
		t.Fatalf("read settled_at again: %v", err)
	}
	if settledAt1 != settledAt2 {
		t.Fatalf("settled_at changed on replay: %s -> %s", settledAt1, settledAt2)
	}
	if got := rsWallet(t, db, customerID); got != walletAfter {
		t.Fatalf("wallet changed on replayed refund: %d -> %d", walletAfter, got)
	}
	if charges := rsCharges(t, db, returnID); len(charges) != 1 {
		t.Fatalf("charges after replayed refund = %d, want 1", len(charges))
	}
}

// (6) Post-paid (COD) parcel: receive is refused, cod_refused is
// set, and nothing else moves.
func TestReturnShippingCODRefused(t *testing.T) {
	db := openReturnSettlementDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	returnID, customerID, itemIDs := rsSeedReturn(t, db, false, []rsItemSpec{
		{productID: "p-cod", supplierID: "sup-1", weightG: intPtr(1000), qty: 1, returnQty: 1, unitPrice: 50000, originWH: "wh-1"},
	})
	_, err := repo.ReceiveCustomerReturn(ctx, returnID, []CustomerReturnItemQC{{ReturnItemID: itemIDs[0], QCStatus: "defective"}}, "keeper-1")
	if !errors.Is(err, ErrReturnCODRefused) {
		t.Fatalf("receive err = %v, want ErrReturnCODRefused", err)
	}
	var status string
	var codRefused bool
	var receivedCount int
	if err := db.QueryRow(`SELECT status, cod_refused FROM customer_returns WHERE id = $1`, returnID).Scan(&status, &codRefused); err != nil {
		t.Fatalf("read return: %v", err)
	}
	if status != "approved" || !codRefused {
		t.Fatalf("return after COD refusal: status %q cod_refused %v, want approved/true", status, codRefused)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM return_items WHERE return_id = $1 AND received_at IS NOT NULL`, returnID).Scan(&receivedCount); err != nil || receivedCount != 0 {
		t.Fatalf("received items after COD refusal = %d, err %v; want 0", receivedCount, err)
	}
	if charges := rsCharges(t, db, returnID); len(charges) != 0 {
		t.Fatalf("charges after COD refusal = %+v, want none", charges)
	}
	if got := rsWallet(t, db, customerID); got != 0 {
		t.Fatalf("wallet after COD refusal = %d, want 0", got)
	}
	// Still visible in the warehouse list.
	list, err := repo.ListCustomerReturnsForWarehouse(ctx, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("warehouse list = %d returns, err %v; want the refused return still listed", len(list), err)
	}
}

// (7) NULL product weight falls back to the platform default (500g).
func TestReturnShippingNullWeightDefault(t *testing.T) {
	db := openReturnSettlementDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	returnID, _, itemIDs := rsSeedReturn(t, db, true, []rsItemSpec{
		{productID: "p-noweight", supplierID: "sup-1", weightG: nil, qty: 1, returnQty: 1, unitPrice: 50000, originWH: "wh-1"},
	})
	settlement, err := repo.ReceiveCustomerReturn(ctx, returnID, []CustomerReturnItemQC{{ReturnItemID: itemIDs[0], QCStatus: "defective"}}, "keeper-1")
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if settlement.ReturnShippingAmount != 12000 {
		t.Fatalf("fare with NULL weight = %d, want 12000 (500g default -> 1kg band)", settlement.ReturnShippingAmount)
	}
}

// allocateProRata: exact-sum splitting with remainder absorption.
func TestAllocateProRata(t *testing.T) {
	cases := []struct {
		total   int64
		weights []int64
		want    []int64
	}{
		{16000, []int64{1000, 2000}, []int64{5333, 10667}},
		{12000, []int64{1000}, []int64{12000}},
		{0, []int64{1000, 2000}, []int64{0, 0}},
		{100, []int64{1, 1, 1}, []int64{33, 33, 34}},
		{100, []int64{0, 5}, []int64{0, 100}},
	}
	for _, tc := range cases {
		got := allocateProRata(tc.total, tc.weights)
		if len(got) != len(tc.want) {
			t.Fatalf("allocateProRata(%d, %v) len = %d", tc.total, tc.weights, len(got))
		}
		var sum int64
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("allocateProRata(%d, %v) = %v, want %v", tc.total, tc.weights, got, tc.want)
			}
			sum += got[i]
		}
		if sum != tc.total {
			t.Fatalf("allocateProRata(%d, %v) sums to %d", tc.total, tc.weights, sum)
		}
	}
}
