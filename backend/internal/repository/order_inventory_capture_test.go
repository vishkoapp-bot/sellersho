package repository

// Regression test for the inventory_captured capture/restore pair
// (migration 202610250009): MarkPaymentPaid debits products.stock /
// product_variants.stock at payment time, so it must also set
// orders.inventory_captured = TRUE in the same transaction —
// CancelOrder restores stock ONLY under that flag, and before this
// fix no production path ever set it, so every paid-then-cancelled
// order permanently lost its stock.
//
// DB-gated exactly like the rest of the team03 suite: skipped unless
// TEAM03_DATABASE_URL points at a DISPOSABLE Postgres database (see
// openTeam03DB). The team03 disposable schema has no product_variants
// table, so this test creates the minimal one it needs (only id and
// stock are touched by the capture/restore statements).

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestMarkPaymentPaidCapturesInventoryAndCancelRestores(t *testing.T) {
	db := openTeam03DB(t)
	defer db.Close()
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS product_variants CASCADE`); err != nil {
		t.Fatalf("drop product_variants: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE product_variants (id UUID PRIMARY KEY, product_id TEXT, stock INT NOT NULL DEFAULT 0)`); err != nil {
		t.Fatalf("create product_variants: %v", err)
	}

	suffix := uuid.NewString()[:12]
	userID := uuid.NewString()
	plainID := "prod-plain-" + suffix
	varProductID := "prod-var-" + suffix
	variantID := uuid.New()
	orderID := "order-cap-" + suffix
	orderNumber := "ORD-CAP-" + suffix
	token := "TOKEN-CAP-" + suffix

	exec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("fixture %q: %v", query, err)
		}
	}
	exec(`INSERT INTO products (id, title, stock) VALUES ($1, 'plain', 10)`, plainID)
	exec(`INSERT INTO products (id, title, stock) VALUES ($1, 'variant product', 8)`, varProductID)
	exec(`INSERT INTO product_variants (id, product_id, stock) VALUES ($1, $2, 5)`, variantID, varProductID)
	exec(`INSERT INTO orders (id, customer_user_id, order_number, total_amount, payable_amount, payment_status, status)
		VALUES ($1, $2, $3, 5000, 5000, 'pending', 'processing')`, orderID, userID, orderNumber)
	exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price)
		VALUES ($1, $2, $3, 3, 1000, 100, 900, 2000)`, "item-plain-"+suffix, orderID, plainID)
	exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price, variant_id)
		VALUES ($1, $2, $3, 2, 1000, 100, 900, 2000, $4)`, "item-var-"+suffix, orderID, varProductID, variantID)
	exec(`INSERT INTO payment_transactions (id, order_id, order_number, gateway_token, amount, status)
		VALUES ($1, $2, $3, $4, 5000, 'pending')`, "pay-cap-"+suffix, orderID, orderNumber, token)

	stockOf := func(productID string) int {
		t.Helper()
		var stock int
		if err := db.QueryRowContext(ctx, `SELECT stock FROM products WHERE id = $1`, productID).Scan(&stock); err != nil {
			t.Fatalf("read product stock: %v", err)
		}
		return stock
	}
	variantStockOf := func() int {
		t.Helper()
		var stock int
		if err := db.QueryRowContext(ctx, `SELECT stock FROM product_variants WHERE id = $1`, variantID).Scan(&stock); err != nil {
			t.Fatalf("read variant stock: %v", err)
		}
		return stock
	}

	repo := NewOrderRepository(db)

	// Pay: stock is debited AND the capture flag is set, atomically.
	payment, err := repo.MarkPaymentPaid(ctx, token, "RRN-CAP-1", "6037-****")
	if err != nil {
		t.Fatalf("MarkPaymentPaid: %v", err)
	}
	if payment == nil || payment.Status != "paid" {
		t.Fatalf("expected paid payment, got %+v", payment)
	}
	if got := stockOf(plainID); got != 7 {
		t.Fatalf("plain product stock after pay = %d, want 7", got)
	}
	if got := stockOf(varProductID); got != 6 {
		t.Fatalf("variant product stock after pay = %d, want 6", got)
	}
	if got := variantStockOf(); got != 3 {
		t.Fatalf("variant stock after pay = %d, want 3", got)
	}
	var captured bool
	var payStatus string
	if err := db.QueryRowContext(ctx, `SELECT inventory_captured, payment_status FROM orders WHERE id = $1`, orderID).Scan(&captured, &payStatus); err != nil {
		t.Fatalf("read order after pay: %v", err)
	}
	if !captured {
		t.Fatalf("inventory_captured is FALSE after MarkPaymentPaid; CancelOrder would never restore this order's stock")
	}
	if payStatus != "paid" {
		t.Fatalf("payment_status after pay = %q, want paid", payStatus)
	}

	// Idempotency: a replayed callback must not debit twice.
	if _, err := repo.MarkPaymentPaid(ctx, token, "RRN-CAP-1", "6037-****"); err != nil {
		t.Fatalf("MarkPaymentPaid replay: %v", err)
	}
	if got := stockOf(plainID); got != 7 {
		t.Fatalf("plain product stock after replay = %d, want 7 (no double debit)", got)
	}
	if got := variantStockOf(); got != 3 {
		t.Fatalf("variant stock after replay = %d, want 3 (no double debit)", got)
	}

	// Cancel the paid order: stock is fully restored on both lines.
	if err := repo.CancelOrder(ctx, orderID, userID, false); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if got := stockOf(plainID); got != 10 {
		t.Fatalf("plain product stock after cancel = %d, want 10 (fully restored)", got)
	}
	if got := stockOf(varProductID); got != 8 {
		t.Fatalf("variant product stock after cancel = %d, want 8 (fully restored)", got)
	}
	if got := variantStockOf(); got != 5 {
		t.Fatalf("variant stock after cancel = %d, want 5 (fully restored)", got)
	}

	// Negative control: cancelling an UNPAID order must restore
	// nothing (the flag is false; restoring would mint phantom stock).
	unpaidOrderID := "order-unpaid-" + suffix
	exec(`INSERT INTO orders (id, customer_user_id, order_number, total_amount, payable_amount, payment_status, status)
		VALUES ($1, $2, $3, 8000, 8000, 'pending', 'processing')`, unpaidOrderID, userID, "ORD-UNPAID-"+suffix)
	exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price)
		VALUES ($1, $2, $3, 4, 1000, 100, 900, 2000)`, "item-unpaid-"+suffix, unpaidOrderID, plainID)
	if err := repo.CancelOrder(ctx, unpaidOrderID, userID, false); err != nil {
		t.Fatalf("CancelOrder unpaid: %v", err)
	}
	if got := stockOf(plainID); got != 10 {
		t.Fatalf("plain product stock after unpaid cancel = %d, want 10 (no phantom restore)", got)
	}
}
