package repository

// Regression test for the inventory_captured capture/restore pair
// (migration 202610250009): MarkPaymentPaid captures exact physical stock
// at payment time, so it must also set
// orders.inventory_captured = TRUE in the same transaction —
// CancelOrder restores stock ONLY under that flag, and before this
// fix no production path ever set it, so every paid-then-cancelled
// order permanently lost its stock.
//
// Uses the disposable, fully migrated schema and explicit purchased origins.

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestMarkPaymentPaidCapturesInventoryAndCancelRestores(t *testing.T) {
	db := openPaymentConcurrencyDB(t)
	defer db.Close()
	ctx := context.Background()

	suffix := uuid.NewString()[:12]
	userID := uuid.NewString()
	plainID := "prod-plain-" + suffix
	varProductID := "prod-var-" + suffix
	variantID := uuid.New()
	siblingID := uuid.New()
	storeID := "store-cap-" + suffix
	orderID := "order-cap-" + suffix
	orderNumber := "ORD-CAP-" + suffix
	token := "TOKEN-CAP-" + suffix

	exec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("fixture %q: %v", query, err)
		}
	}
	exec(`INSERT INTO users(id,mobile,full_name,role,is_active) VALUES($1,$2,'Capture customer','customer',TRUE)`, userID, "098"+suffix[:8])
	exec(`INSERT INTO stores(id,name,slug,owner_type,owner_name,active_plan_id) VALUES($1,$1,$1,'central','Platform','vip')`, storeID)
	exec(`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock) VALUES ($1,'plain',$1,'digital','sup-ali',2000,1000,100,10)`, plainID)
	exec(`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,schema_version,product_type,attribute_schema) VALUES ($1,'variant product',$1,'digital','sup-ali',2000,1000,100,8,2,'variable','{}')`, varProductID)
	exec(`INSERT INTO product_variants (id,product_id,variant_name,spnc,stock,inventory_mode) VALUES ($1,$2,'Purchased SKU',$4,5,'tracked'),($3,$2,'Sibling SKU',$5,3,'tracked')`, variantID, varProductID, siblingID, "A"+suffix[:10], "B"+suffix[:10])
	warehouse := seedCanonicalWarehouseStock(t, db, plainID, "sup-ali", 10)
	for _, lot := range []struct {
		id       uuid.UUID
		quantity int
	}{{variantID, 5}, {siblingID, 3}} {
		exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,warehouse_id) VALUES($1,$2,'sup-ali',$3,'supplier','warehouse',$4,'sellable',$5,$6)`, uuid.NewString(), varProductID, lot.id, "warehouse:"+warehouse, lot.quantity, warehouse)
	}
	insertOrder := func(id, number string, amount int) {
		exec(`INSERT INTO orders(id,customer_user_id,order_number,store_id,store_name,total_amount,payable_amount,payment_method,payment_status,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,status,tracking_code,fulfillment_method) VALUES($1,$2,$3,$4,'Capture store',$5,$5,'cash','pending','Capture customer','09120000000','Tehran','Tehran','Test address','1234567890','processing',$3,'warehouse')`, id, userID, number, storeID, amount)
	}
	insertOrder(orderID, orderNumber, 10000)
	exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price)
		VALUES ($1, $2, $3, 3, 1000, 100, 900, 2000)`, uuid.NewString(), orderID, plainID)
	exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price, variant_id)
		VALUES ($1, $2, $3, 2, 1000, 100, 900, 2000, $4)`, uuid.NewString(), orderID, varProductID, variantID)
	exec(`INSERT INTO payment_transactions (id, order_id, order_number, gateway_token, amount, status)
		VALUES ($1, $2, $3, $4, 10000, 'pending')`, "pay-cap-"+suffix, orderID, orderNumber, token)
	seedCanonicalWarehouseOrderOrigins(t, db, orderID, warehouse)

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
	insertOrder(unpaidOrderID, "ORD-UNPAID-"+suffix, 8000)
	exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price)
		VALUES ($1, $2, $3, 4, 1000, 100, 900, 2000)`, uuid.NewString(), unpaidOrderID, plainID)
	if err := repo.CancelOrder(ctx, unpaidOrderID, userID, false); err != nil {
		t.Fatalf("CancelOrder unpaid: %v", err)
	}
	if got := stockOf(plainID); got != 10 {
		t.Fatalf("plain product stock after unpaid cancel = %d, want 10 (no phantom restore)", got)
	}
	var siblingStock int
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, siblingID).Scan(&siblingStock); err != nil || siblingStock != 3 {
		t.Fatalf("unrelated sibling stock=%d err=%v; want 3", siblingStock, err)
	}
}
