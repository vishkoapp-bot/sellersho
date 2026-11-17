package repository

import (
	"errors"
	"reflect"
	"testing"
)

func TestTransactionLockOrderNormalizesWholePlan(t *testing.T) {
	in := []TransactionRowLock{
		{Table: "warehouse_storage_positions", ID: "z"},
		{Table: "suppliers", ID: "a"},
		{Table: "products", ID: "b"},
		{Table: "orders", ID: "b"},
		{Table: "stores", ID: "z"},
		{Table: "orders", ID: "a"},
		{Table: "product_variants", ID: "0"},
		{Table: "products", ID: "a"},
		{Table: "orders", ID: "a"},
	}
	want := []TransactionRowLock{
		{Table: "orders", ID: "a"}, {Table: "orders", ID: "b"},
		{Table: "stores", ID: "z"}, {Table: "suppliers", ID: "a"},
		{Table: "products", ID: "a"}, {Table: "products", ID: "b"},
		{Table: "product_variants", ID: "0"},
		{Table: "warehouse_storage_positions", ID: "z"},
	}
	got, err := normalizeTransactionLocks(in)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical plan = %#v, %v; want %#v", got, err, want)
	}
	if len(in) != 9 || in[0].Table != "warehouse_storage_positions" {
		t.Fatal("normalization mutated the caller's discovery plan")
	}
}

func TestTransactionLockOrderRejectsInvalidIdentifiers(t *testing.T) {
	for _, lock := range []TransactionRowLock{{Table: "orders; DROP TABLE orders", ID: "a"}, {Table: "orders", ID: ""}, {Table: "inventory_placements", ID: "a"}} {
		if _, err := normalizeTransactionLocks([]TransactionRowLock{lock}); !errors.Is(err, ErrTransactionLockOrder) {
			t.Fatalf("invalid lock %#v accepted: %v", lock, err)
		}
	}
}
