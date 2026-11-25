package repository

import (
	"context"
	"testing"
	"time"

	"github.com/drup/platform/internal/domain"
)

func TestPanelWorkCountsMatchScopedCursorPages(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	owner := opsFixtureNew(t, db)
	foreign := opsFixtureNew(t, db)
	repo := NewSupplierShipmentRepository(db)
	for _, entry := range []struct {
		fixture opsFixture
		paid    bool
		name    string
	}{
		{owner, true, "old"}, {owner, true, "new"}, {owner, false, "unpaid"}, {foreign, true, "foreign"},
	} {
		id, _ := opsOrder(t, db, entry.fixture, entry.paid)
		if err := repo.CreateShipment(ctx, domain.SupplierShipment{ID: "work-" + id, OrderID: id, SupplierID: entry.fixture.supplier, CustomerName: "Sample", CustomerPhone: "09123456789"}); err != nil {
			t.Fatal(err)
		}
		if entry.name == "old" {
			opsExec(t, db, `UPDATE supplier_shipments SET dispatch_due_at=now()-interval '1 day' WHERE order_id=$1`, id)
			fin005ReturnFixtureExec(t, db, id, `UPDATE orders SET status='delivery_disputed' WHERE id=$1`, id)
		}
	}
	opsExec(t, db, `UPDATE products SET preparation_days=10 WHERE id=$1`, owner.product)
	counts, err := NewSupplierRepository(db).GetSupplierWorkCounts(ctx, owner.supplier)
	if err != nil || counts.Ready != 2 || counts.Overdue != 1 {
		t.Fatalf("supplier counts = %+v, %v", counts, err)
	}
	first, err := repo.GetSupplierOrderWorkPage(ctx, owner.supplier, "ready", ListPageOptions{Limit: 1})
	if err != nil || len(first.Items) != 1 || !first.HasMore {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second, err := repo.GetSupplierOrderWorkPage(ctx, owner.supplier, "ready", ListPageOptions{Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.HasMore || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second = %+v, %v", second, err)
	}
	if due := first.Items[0].DispatchDueAt; due == nil || due.Sub(first.Items[0].CreatedAt) > 2*24*time.Hour {
		t.Fatalf("product edit moved saved dispatch commitment: %v", due)
	}
	page, err := repo.GetSupplierOrderWorkPage(ctx, owner.supplier, "overdue", ListPageOptions{})
	if err != nil || len(page.Items) != counts.Overdue || page.Items[0].DispatchDueAt == nil || !page.Items[0].DispatchDueAt.Before(time.Now()) {
		t.Fatalf("overdue = %+v, %v", page, err)
	}
	orders := NewOrderRepository(db)
	followUp, err := orders.CountStoreOrderFollowUp(ctx, owner.store)
	if err != nil || followUp != 1 {
		t.Fatalf("seller count = %d, %v", followUp, err)
	}
	sellerPage, err := orders.GetOrdersByStoreIDPage(ctx, owner.store, ListPageOptions{}, "follow-up")
	if err != nil || len(sellerPage.Items) != followUp {
		t.Fatalf("seller list = %+v, %v", sellerPage, err)
	}
}
