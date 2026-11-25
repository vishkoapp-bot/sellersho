package repository

import (
	"context"
	"testing"
	"time"
)

func TestSupplierStatementMatchesAvailableAndScopesFinancialRows(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	owner := opsFixtureNew(t, db)
	foreign := opsFixtureNew(t, db)
	for _, f := range []opsFixture{owner, foreign} {
		orderID, _ := opsOrder(t, db, f, true)
		warehouse := seedCanonicalWarehouseStock(t, db, f.product, f.supplier, 100)
		seedCanonicalWarehouseOrderOrigins(t, db, orderID, warehouse)
		opsExec(t, db, `UPDATE orders SET status='delivered_confirmed', confirmed_at=now() WHERE id=$1`, orderID)
	}
	r := NewSupplierRepository(db)
	payout, err := r.RequestSettlement(ctx, owner.supplier, 300, "IR000000000000000000000000", "", "Sample")
	if err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	month := time.Now().In(loc).Format("2006-01")
	page, err := r.GetSupplierStatement(ctx, owner.supplier, month, StatementPageOptions{Limit: 1})
	if err != nil || page.TotalEntries != 2 || page.Credits != 1000 || page.Debits != 300 || page.Balance.Available != 700 || len(page.Entries) != 1 || page.Entries[0].ID != payout || page.NextCursor == "" {
		t.Fatalf("statement = %+v, %v", page, err)
	}
	second, err := r.GetSupplierStatement(ctx, owner.supplier, month, StatementPageOptions{Limit: 1, Cursor: page.NextCursor})
	if err != nil || len(second.Entries) != 1 || second.Entries[0].Kind != "order" || second.NextCursor != "" {
		t.Fatalf("second page = %+v, %v", second, err)
	}
	opsExec(t, db, `UPDATE payout_requests SET processed_at=now()+interval '2 months' WHERE id=$1`, payout)
	sameMonth, err := r.GetSupplierStatement(ctx, owner.supplier, month, StatementPageOptions{})
	if err != nil || sameMonth.TotalEntries != 2 || sameMonth.Debits != 300 {
		t.Fatalf("processing date moved the original reservation to another month: %+v, %v", sameMonth, err)
	}
}
