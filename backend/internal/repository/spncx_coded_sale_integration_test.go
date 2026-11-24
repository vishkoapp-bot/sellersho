package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestSpncxCodedStockCapsAdmissionAndCapture(t *testing.T) {
	db, schema, _, variant := skuInventoryFixture(t)
	ctx := context.Background()
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE inventory_balances SET quantity=5 WHERE variant_id=$1 AND status='sellable'`, variant); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	scoped := skuScopedDB(t, schema)
	if _, err := ResolveInventoryOrigins(ctx, scoped, "p", variant, "supplier_direct", 4); !errors.Is(err, ErrInsufficientInventory) {
		t.Errorf("uncoded stock was quoted: %v", err)
	}
	tx = skuTestTx(t, db, schema)
	defer tx.Rollback()
	for _, x := range []struct {
		id  string
		qty int
	}{{"uncoded", 4}, {"coded", 3}} {
		if _, err := tx.Exec(`INSERT INTO orders(id) VALUES($1)`, x.id); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,$2,'p',$3,$4)`, uuid.NewString(), x.id, variant, x.qty); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "uncoded"); !errors.Is(err, ErrInsufficientInventory) {
		t.Errorf("uncoded stock was captured: %v", err)
	}
	var captured int
	if err := tx.QueryRow(`SELECT count(*) FROM order_inventory_captures WHERE order_id='uncoded'`).Scan(&captured); err != nil || captured != 0 {
		t.Fatalf("uncoded capture records=%d err=%v", captured, err)
	}
	for i := 0; i < 2; i++ {
		if ok, err := captureOrderInventoryTx(ctx, tx, "coded"); err != nil || !ok {
			t.Fatalf("coded capture %t %v", ok, err)
		}
	}
	var codes, physical int
	if err := tx.QueryRow(`SELECT (SELECT count(DISTINCT unit_id) FROM order_item_units WHERE order_id='coded'),(SELECT sum(quantity) FROM inventory_balances WHERE variant_id=$1 AND status='sellable')`, variant).Scan(&codes, &physical); err != nil || codes != 3 || physical != 2 {
		t.Fatalf("codes=%d physical=%d err=%v", codes, physical, err)
	}
}

func TestSpncxPublicAvailabilityUsesEligibleOriginAndUnit(t *testing.T) {
	db, schema, _, sku := skuInventoryFixture(t)
	ctx := context.Background()
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE supplier_shipping_methods (LIKE public.supplier_shipping_methods INCLUDING ALL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE suppliers SET owner_user_id='d628062b-7a00-43f8-ac8d-cb03df9d8430' WHERE id='s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE inventory_balances SET quantity=5 WHERE variant_id=$1`, sku); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE product_units SET status='quarantined' WHERE variant_id=$1 AND sequence=3`, sku); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	scoped := skuScopedDB(t, schema)
	repo := NewCatalogRepository(scoped)
	stocks, err := repo.CodedStockByVariant(ctx, []string{"p"})
	if err != nil || stocks[sku] != 0 {
		t.Fatalf("warehouse routing cannot use supplier codes: stock=%d err=%v", stocks[sku], err)
	}
	if _, err := scoped.Exec(`INSERT INTO supplier_shipping_methods(owner_user_id,method,status)
		VALUES('d628062b-7a00-43f8-ac8d-cb03df9d8430','direct','enabled')`); err != nil {
		t.Fatal(err)
	}
	stocks, err = repo.CodedStockByVariant(ctx, []string{"p"})
	if err != nil || stocks[sku] != 2 {
		t.Fatalf("direct routing uses only live coded units: stock=%d err=%v", stocks[sku], err)
	}
	if _, err := scoped.Exec(`UPDATE supplier_warehouses SET is_active=FALSE WHERE id='sw1'`); err != nil {
		t.Fatal(err)
	}
	stocks, err = repo.CodedStockByVariant(ctx, []string{"p"})
	if err != nil || stocks[sku] != 0 {
		t.Fatalf("inactive origin cannot sell: stock=%d err=%v", stocks[sku], err)
	}
}

func TestSpncxHoldRecoveryDoesNotBorrowAnotherOriginsCodes(t *testing.T) {
	db, schema, sku, _ := skuInventoryFixture(t)
	ctx := context.Background()
	redis := testRedisClient(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	if err := creditSKUInventoryTx(ctx, tx, "p", "s", sku, 1, "sellable", "supplier", "supplier-warehouse:sw2", "sku_stock_adjustment", sku, "test-second-declaration:"+sku, ""); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ order, warehouse string }{{"a-admit", "sw1"}, {"b-reject", "sw1"}, {"c-admit", "sw2"}} {
		if _, err := tx.Exec(`INSERT INTO orders(id) VALUES($1)`, x.order); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,$2,'p',$3,1)`, uuid.NewString(), x.order, sku); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`UPDATE order_item_origins SET supplier_warehouse_id=$2 WHERE order_id=$1`, x.order, x.warehouse); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	repo := NewOrderRepository(skuScopedDB(t, schema))
	for i := 0; i < 2; i++ {
		if _, err := repo.RebuildInventoryHolds(ctx, redis); !errors.Is(err, ErrInsufficientInventory) {
			t.Errorf("only the oversubscribed origin should fail: %v", err)
		}
		for id, want := range map[string]bool{"a-admit": true, "b-reject": false, "c-admit": true} {
			held, err := redis.HasInventoryHold(ctx, id, "p", sku, 1)
			if err != nil || held != want {
				t.Errorf("order %s held=%t want=%t err=%v", id, held, want, err)
			}
		}
	}
}

func TestSpncxCaptureRejectsDeactivatedProduct(t *testing.T) {
	db, schema, sku, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	if _, err := tx.Exec(`INSERT INTO orders(id) VALUES('deactivated')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,'deactivated','p',$2,1)`, uuid.NewString(), sku); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE products SET is_active=FALSE WHERE id='p'`); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "deactivated"); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("deactivated product captured: %v", err)
	}
}
