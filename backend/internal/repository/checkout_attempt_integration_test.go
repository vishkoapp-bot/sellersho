package repository

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
)

func checkoutOrderFixture(t *testing.T) (*OrderRepository, opsFixture, domain.Order) {
	t.Helper()
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	old, _ := opsOrder(t, db, f, false)
	repo := NewOrderRepository(db)
	order, err := repo.GetOrderByID(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	order.ID = "checkout-" + uuid.NewString()[:12]
	order.OrderNumber = order.ID
	order.TrackingCode = order.ID
	key := uuid.NewString()
	order.IdempotencyKey = &key
	return repo, f, *order
}
func TestCheckoutAttemptAtomicCommitOrAbandon(t *testing.T) {
	repo, f, template := checkoutOrderFixture(t)
	ctx := context.Background()
	hash := strings.Repeat("a", 64)
	for i := 0; i < 20; i++ {
		order := template
		order.ID = "checkout-" + uuid.NewString()[:12]
		order.OrderNumber = order.ID
		order.TrackingCode = order.ID
		key := uuid.NewString()
		order.IdempotencyKey = &key
		if _, err := repo.BeginCheckoutAttempt(ctx, f.user, key, hash); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var createErr, abandonErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _, createErr = repo.CreateCheckoutOrderWithShipments(ctx, order, nil, hash)
		}()
		go func() { defer wg.Done(); _, abandonErr = repo.AbandonCheckoutAttempt(ctx, f.user, key) }()
		wg.Wait()
		if abandonErr != nil {
			t.Fatal(abandonErr)
		}
		if createErr != nil && !errors.Is(createErr, ErrCheckoutAbandoned) {
			t.Fatal(createErr)
		}
		a, err := repo.GetCheckoutAttempt(ctx, f.user, key)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err := repo.db.QueryRow(`SELECT count(*) FROM orders WHERE customer_user_id=$1 AND idempotency_key=$2`, f.user, key).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if a.State == "completed" {
			if count != 1 || a.OrderID != order.ID || createErr != nil {
				t.Fatalf("completed race lost order: %+v count %d err %v", a, count, createErr)
			}
		} else if a.State != "abandoned" || count != 0 || !errors.Is(createErr, ErrCheckoutAbandoned) {
			t.Fatalf("abandoned race created an order %+v count %d err %v", a, count, createErr)
		}
	}
}
func TestCheckoutAttemptSQLGuardsPreventMutationAndWrongOrder(t *testing.T) {
	repo, f, order := checkoutOrderFixture(t)
	ctx := context.Background()
	key := *order.IdempotencyKey
	hash := strings.Repeat("a", 64)
	if _, err := repo.BeginCheckoutAttempt(ctx, f.user, key, hash); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE checkout_attempts SET payload_hash=repeat('b',64) WHERE attempt_id=$1`, `UPDATE checkout_attempts SET attempt_id='changed' WHERE attempt_id=$1`, `DELETE FROM checkout_attempts WHERE attempt_id=$1`, `UPDATE checkout_attempts SET state='completed',order_id=(SELECT id FROM orders ORDER BY id LIMIT 1) WHERE attempt_id=$1`} {
		if _, err := repo.db.Exec(q, key); err == nil {
			t.Fatalf("checkout invariant mutable: %s", q)
		}
	}
	if _, _, err := repo.CreateCheckoutOrderWithShipments(ctx, order, nil, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`UPDATE checkout_attempts SET state='abandoned',order_id=NULL WHERE attempt_id=$1`, key); err == nil {
		t.Fatal("completed order could be hidden")
	}
}
func TestCheckoutAttemptReplayCannotRecaptureInventoryOrCreatePayment(t *testing.T) {
	repo, f, order := checkoutOrderFixture(t)
	ctx := context.Background()
	key := *order.IdempotencyKey
	hash := strings.Repeat("a", 64)
	wh := seedCanonicalWarehouseStock(t, repo.db, f.product, f.supplier, 100)
	if _, err := repo.BeginCheckoutAttempt(ctx, f.user, key, hash); err != nil {
		t.Fatal(err)
	}
	id, created, err := repo.CreateCheckoutOrderWithShipments(ctx, order, nil, hash)
	if err != nil || !created {
		t.Fatalf("first create %v %v", created, err)
	}
	seedCanonicalWarehouseOrderOrigins(t, repo.db, id, wh)
	token := uuid.NewString()
	if err := repo.CreatePaymentTransaction(ctx, PaymentTransaction{ID: uuid.NewString(), OrderID: id, OrderNumber: order.OrderNumber, GatewayToken: token, Amount: 2000}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MarkPaymentPaid(ctx, token, "local-recorded-payment", "mask"); err != nil {
		t.Fatal(err)
	}
	var before string
	query := `SELECT json_build_array((SELECT stock FROM products WHERE id=$1),(SELECT COALESCE(sum(quantity),0) FROM inventory_balances WHERE product_id=$1),(SELECT count(*) FROM inventory_movements WHERE product_id=$1),(SELECT count(*) FROM ledger_entries),(SELECT count(*) FROM payment_transactions WHERE order_id=$2),(SELECT count(*) FROM order_refund_obligations WHERE order_id=$2))::text`
	if err := repo.db.QueryRow(query, f.product, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			retry := order
			retry.ID = "unexpected-" + uuid.NewString()[:12]
			got, created, err := repo.CreateCheckoutOrderWithShipments(ctx, retry, nil, hash)
			if err == nil && (got != id || created) {
				err = errors.New("replay created a second order")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var after string
	if err := repo.db.QueryRow(query, f.product, id).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("replay changed stock/ledger/payment facts: %s -> %s", before, after)
	}
}
