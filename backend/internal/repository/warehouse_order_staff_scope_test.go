package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestWarehouseOrderMutationsRequireCanonicalOriginCity(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	warehouse := seedCanonicalWarehouseStock(t, db, f.product, f.supplier, 1)
	order, _ := opsOrder(t, db, f, true)
	number := "qual01-" + uuid.NewString()[:12]
	opsExec(t, db, `UPDATE orders SET order_number=$2,status='processing',fulfillment_method='warehouse' WHERE id=$1`, order, number)
	opsExec(t, db, `UPDATE users SET warehouse_city='Test city', is_active=true, warehouse_city_confirmed=true WHERE id=$1`, f.user)
	scoped := WithWarehouseStaffID(context.Background(), f.user)
	repo := NewWarehouseRepository(db)
	assertDenied := func(t *testing.T, operation string, err error) {
		t.Helper()
		if !errors.Is(err, ErrWarehouseOrderNotFound) {
			t.Fatalf("%s: got %v, want warehouse order not found", operation, err)
		}
	}
	assertStatus := func(t *testing.T, want string) {
		t.Helper()
		var got string
		if err := db.QueryRow(`SELECT status FROM orders WHERE id=$1`, order).Scan(&got); err != nil || got != want {
			t.Fatalf("order status: got %q, err %v, want %q", got, err, want)
		}
	}

	assertDenied(t, "start without origins", repo.StartPickingOrder(scoped, number, f.user))
	assertStatus(t, "processing")
	seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
	for _, flags := range []struct {
		name              string
		active, confirmed bool
	}{
		{"inactive", false, true},
		{"unconfirmed", true, false},
	} {
		t.Run(flags.name, func(t *testing.T) {
			opsExec(t, db, `UPDATE users SET is_active=$2, warehouse_city_confirmed=$3 WHERE id=$1`, f.user, flags.active, flags.confirmed)
			assertDenied(t, flags.name+" start", repo.StartPickingOrder(scoped, number, f.user))
			assertDenied(t, flags.name+" pick", repo.PickItem(scoped, number, nil))
			assertDenied(t, flags.name+" complete", repo.CompletePickingOrder(scoped, number))
			_, _, err := repo.PackOrder(scoped, number, 100, "box")
			assertDenied(t, flags.name+" pack", err)
			assertDenied(t, flags.name+" ship", repo.ShipOrder(scoped, number, "post", "tracking"))
			assertStatus(t, "processing")
		})
	}
	opsExec(t, db, `UPDATE users SET warehouse_city='Other city', is_active=true, warehouse_city_confirmed=true WHERE id=$1`, f.user)
	assertDenied(t, "cross-city start", repo.StartPickingOrder(scoped, number, f.user))
	assertStatus(t, "processing")

	opsExec(t, db, `UPDATE users SET warehouse_city='Test city' WHERE id=$1`, f.user)
	if err := repo.StartPickingOrder(scoped, number, f.user); err != nil {
		t.Fatalf("same-city start by order number: %v", err)
	}
	opsExec(t, db, `UPDATE users SET warehouse_city='Other city' WHERE id=$1`, f.user)
	assertDenied(t, "cross-city pick item", repo.PickItem(scoped, number, map[string]interface{}{"qual01": "denied"}))
	assertDenied(t, "cross-city complete picking", repo.CompletePickingOrder(scoped, order))
	assertStatus(t, "picking")

	opsExec(t, db, `UPDATE users SET warehouse_city='Test city' WHERE id=$1`, f.user)
	if err := repo.PickItem(scoped, number, map[string]interface{}{"qual01": "allowed"}); err != nil {
		t.Fatalf("same-city pick item by order number: %v", err)
	}
	var picked string
	if err := db.QueryRow(`SELECT picked_items::text FROM pick_tasks WHERE order_id=$1`, order).Scan(&picked); err != nil || picked != `[{"qual01": "allowed"}]` {
		t.Fatalf("picked items: got %s, err %v, want only allowed item", picked, err)
	}
	if err := repo.CompletePickingOrder(scoped, number); err != nil {
		t.Fatalf("same-city complete picking: %v", err)
	}
	opsExec(t, db, `UPDATE users SET warehouse_city='Other city' WHERE id=$1`, f.user)
	_, _, err := repo.PackOrder(scoped, number, 0, "")
	assertDenied(t, "cross-city pack without weight", err)
	_, _, err = repo.PackOrder(scoped, number, 100, "box")
	assertDenied(t, "cross-city pack with weight", err)
	assertStatus(t, "picked")

	opsExec(t, db, `UPDATE users SET warehouse_city='Test city' WHERE id=$1`, f.user)
	if _, _, err := repo.PackOrder(scoped, number, 100, "box"); err != nil {
		t.Fatalf("same-city pack: %v", err)
	}
	opsExec(t, db, `UPDATE users SET warehouse_city='Other city' WHERE id=$1`, f.user)
	assertDenied(t, "cross-city ship", repo.ShipOrderWithActor(scoped, number, "post", "tracking", f.user))
	assertStatus(t, "packed")
}
