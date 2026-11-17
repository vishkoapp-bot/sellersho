package main

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"testing"
	"time"

	"github.com/drup/platform/internal/repository"
)

//go:embed financial-history.sql
var financialHistorySQL string

func seedAndVerifyFinancialHistory(t *testing.T, db *sql.DB) {
	t.Helper()
	seedAndVerifyAgedHistory(t, db)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, financialHistorySQL); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertZero := func(label, query string) {
		t.Helper()
		var n int64
		if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatal(label, err)
		}
		if n != 0 {
			t.Fatalf("%s: %d violations", label, n)
		}
	}
	assertZero("immutable origins", `SELECT count(*) FROM (SELECT oi.id FROM order_items oi LEFT JOIN order_item_origins src ON src.order_item_id=oi.id WHERE oi.order_id LIKE 'cap-order-%' GROUP BY oi.id HAVING sum(src.quantity)<>max(oi.quantity) OR count(DISTINCT src.supplier_id)<>1 OR count(src.id)=0 OR bool_or(src.order_id<>oi.order_id)) bad`)
	assertZero("snapshots", `SELECT count(*) FROM orders o JOIN (SELECT order_id,sum(quantity*final_unit_price) total,sum(quantity*seller_margin) seller FROM order_items GROUP BY order_id) s ON s.order_id=o.id WHERE o.id LIKE 'cap-order-%' AND (o.payable_amount<>s.total OR o.dropshipper_profit<>s.seller)`)
	assertZero("source conservation", `SELECT abs((SELECT sum(quantity) FROM inventory_movements WHERE reference_type='capacity_synthetic_receipt')-(SELECT sum(quantity) FROM inventory_balances WHERE product_id LIKE 'cap-product-%' AND status='sellable')-(SELECT sum(quantity-restored_quantity) FROM order_inventory_captures WHERE product_id LIKE 'cap-product-%'))`)
	orders := repository.NewOrderRepository(db)
	released, err := orders.ReleaseClosedReturnWindows(ctx)
	if err != nil || released != 100 {
		t.Fatalf("real release cohort: count=%d err=%v", released, err)
	}
	if n, err := orders.ReleaseClosedReturnWindows(ctx); err != nil || n != 0 {
		t.Fatalf("release replay: count=%d err=%v", n, err)
	}
	assertZero("paired posted release", `SELECT count(*) FROM (SELECT related_order_or_dispute FROM ledger_entries WHERE related_order_or_dispute LIKE 'cap-order-%' GROUP BY related_order_or_dispute HAVING sum(CASE WHEN type='credit' THEN amount ELSE -amount END)<>0 OR count(*)<>2) bad`)
	finance := repository.NewFinanceRepository(db)
	suppliers := repository.NewSupplierRepository(db)
	for _, i := range []int{1, 20, 40, 60, 100} {
		id := fmt.Sprintf("cap-supplier-%d", i)
		var earned, fees int64
		if err := db.QueryRowContext(ctx, `SELECT
 (SELECT COALESCE(sum(oi.quantity*oi.wholesale_cost),0) FROM order_items oi JOIN orders o ON o.id=oi.order_id JOIN order_item_origins src ON src.order_item_id=oi.id WHERE src.supplier_id=$1 AND o.payment_status='paid' AND o.status='delivered_confirmed'),
 (SELECT COALESCE(sum(amount_toman),0) FROM warehouse_storage_fee_charges WHERE supplier_id=$1)`, id).Scan(&earned, &fees); err != nil {
			t.Fatal(err)
		}
		readCtx, cancelRead := context.WithTimeout(ctx, 15*time.Second)
		readStarted := time.Now()
		balance, err := suppliers.GetSettlementBalance(readCtx, id)
		cancelRead()
		t.Logf("CAPACITY_METRIC supplier_balance sample=%d duration_ms=%d", i, time.Since(readStarted).Milliseconds())
		if err != nil || balance.Earned != earned || balance.StorageFees != fees || balance.Available != earned-fees {
			t.Fatalf("supplier immutable earnings/posted fee %s: %+v earned=%d fees=%d err=%v", id, balance, earned, fees, err)
		}
	}
	for i := 1; i <= 100; i++ {
		store := fmt.Sprintf("cap-store-%d", i)
		var owner string
		if err := db.QueryRowContext(ctx, `SELECT owner_user_id::text FROM stores WHERE id=$1`, store).Scan(&owner); err != nil {
			t.Fatal(err)
		}
		before, err := finance.GetStoreBalance(ctx, store)
		if err != nil || before.AvailableAmount < 100000 {
			t.Fatalf("earned balance %s: %v %v", store, before, err)
		}
		p, err := finance.RequestOwnedStorePayout(ctx, owner, store, "SYNTHETIC-NO-TRANSFER", "Synthetic bank", 100000, "cap-payout-"+store)
		if err != nil {
			t.Fatal(err)
		}
		replay, err := finance.RequestOwnedStorePayout(ctx, owner, store, "SYNTHETIC-NO-TRANSFER", "Synthetic bank", 100000, "cap-payout-"+store)
		if err != nil || replay.ID != p.ID {
			t.Fatalf("payout replay %s: %v", store, err)
		}
		after, err := finance.GetStoreBalance(ctx, store)
		if err != nil || after.AvailableAmount != before.AvailableAmount-100000 {
			t.Fatalf("reservation delta %s: %v", store, err)
		}
		for _, month := range []string{time.Now().UTC().Format("2006-01"), time.Now().UTC().AddDate(0, -1, 0).Format("2006-01")} {
			statement, err := finance.GetStoreStatement(ctx, store, month)
			if err != nil {
				t.Fatal(store, month, err)
			}
			if statement.ClosingAvailable-statement.OpeningAvailable != statement.TotalCredit-statement.TotalDebit-(statement.ClosingReserved-statement.OpeningReserved) || statement.CurrentAvailable != after.AvailableAmount {
				t.Fatalf("canonical posted/reserved reconciliation %s %s: %+v", store, month, statement)
			}
		}
	}
	t.Log("synthetic history:100000 orders,140000 lines,80000 paid,60000 delivered;100 real releases and100 idempotent payout reservations; origin/stock/posted/reserved assertions passed")
}
