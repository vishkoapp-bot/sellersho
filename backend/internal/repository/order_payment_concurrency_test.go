package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	"github.com/google/uuid"
)

func openPaymentConcurrencyDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Skipf("Postgres integration test unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Skipf("Postgres integration test unavailable: %v", err)
	}
	return db
}

func TestConcurrentPaymentCallbacksConsumeInventoryOnce(t *testing.T) {
	db := openPaymentConcurrencyDB(t)
	defer db.Close()
	ctx := context.Background()
	suffix := uuid.NewString()[:12]
	userID := uuid.NewString()
	storeID := "store-pay-" + suffix
	productID := "product-pay-" + suffix
	orderID := "order-pay-" + suffix
	orderNumber := "ORD-PAY-" + suffix
	paymentID := "pay-" + suffix
	tracking := "TRK-PAY-" + suffix
	mobile := "097" + suffix[:8]

	_, err := db.ExecContext(ctx, `INSERT INTO users(id,mobile,full_name,role,is_active) VALUES($1,$2,'Concurrency Customer','customer',TRUE)`, userID, mobile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO stores(id,name,slug,owner_type,owner_name,active_plan_id) VALUES($1,'Payment concurrency store',$2,'central','Platform','vip')`, storeID, storeID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO products(id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock) VALUES($1,'Payment concurrency product',$1,'digital','sup-ali',2000,1000,100,1)`, productID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO orders(id,customer_user_id,order_number,store_id,store_name,total_amount,payable_amount,payment_method,payment_status,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,status,tracking_code,fulfillment_method) VALUES($1,$2,$3,$4,'Payment concurrency store',2000,2000,'cash','pending','Concurrency Customer',$5,'Tehran','Tehran','Test Address','1234567890','processing',$6,'warehouse')`, orderID, userID, orderNumber, storeID, mobile, tracking)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO order_items(order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price) VALUES($1,$2,1,1000,100,900,2000)`, orderID, productID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES($1,$2,$3,$4,2000,'pending')`, paymentID, orderID, orderNumber, "TOKEN-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM payment_transactions WHERE id=$1`, paymentID)
		_, _ = db.Exec(`DELETE FROM orders WHERE id=$1`, orderID)
		_, _ = db.Exec(`DELETE FROM products WHERE id=$1`, productID)
		_, _ = db.Exec(`DELETE FROM stores WHERE id=$1`, storeID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	repo := NewOrderRepository(db)
	warehouse := seedCanonicalWarehouseStock(t, db, productID, "sup-ali", 1)
	seedCanonicalWarehouseOrderOrigins(t, db, orderID, warehouse)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := repo.MarkPaymentPaid(ctx, "TOKEN-"+suffix, "RRN-"+suffix, "6037-****")
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	successes, failures := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrInsufficientInventory) {
			failures++
		} else {
			t.Fatalf("unexpected callback result: %v", err)
		}
	}
	if successes != 2 || failures != 0 {
		t.Fatalf("idempotent callbacks successes=%d failures=%d; expected both callbacks to return paid", successes, failures)
	}

	var stock int
	var paymentStatus, orderPaymentStatus string
	var profitReleased bool
	if err := db.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=$1`, productID).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT pt.status,o.payment_status,o.profit_released FROM payment_transactions pt JOIN orders o ON o.id=pt.order_id WHERE pt.id=$1`, paymentID).Scan(&paymentStatus, &orderPaymentStatus, &profitReleased); err != nil {
		t.Fatal(err)
	}
	if stock != 0 || paymentStatus != "paid" || orderPaymentStatus != "paid" || profitReleased {
		t.Fatalf("bad callback persistence: stock=%d tx=%s order=%s profitReleased=%v", stock, paymentStatus, orderPaymentStatus, profitReleased)
	}
	var paidTransactions, ledgerEffects int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_transactions WHERE id=$1 AND status='paid'`, paymentID).Scan(&paidTransactions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1`, orderID).Scan(&ledgerEffects); err != nil {
		t.Fatal(err)
	}
	if paidTransactions != 1 || ledgerEffects != 0 {
		t.Fatalf("duplicate callback effects: paid transaction rows=%d ledger entries=%d", paidTransactions, ledgerEffects)
	}
}
