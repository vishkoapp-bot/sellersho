package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSupplierReturnBackActiveRetryAndAtomicTransition(t *testing.T) {
	db := operationsDB(t)
	fixture := opsFixtureNew(t, db)
	orderID, _ := opsOrder(t, db, fixture, true)
	returnID, itemID := uuid.NewString(), uuid.NewString()
	fin005ReturnFixtureExec(t, db, orderID, `
		INSERT INTO customer_returns(id, order_id, customer_id, status, reason, prepaid)
		VALUES ($1, $2, $3, 'received', 'test return', TRUE)
	`, returnID, orderID, fixture.user)
	opsExec(t, db, `
		INSERT INTO return_items(id, return_id, order_item_id, qty, qc_status, received_at)
		SELECT $1, $2, id::text, 1, 'defective', NOW() FROM order_items WHERE order_id = $3
	`, itemID, returnID, orderID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := NewSupplierRepository(db)
	createKey := uuid.NewString()
	const reason = "return the checked item"
	first, err := repo.CreateSupplierReturnBack(ctx, fixture.supplier, itemID, 1, reason, createKey)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.CreateSupplierReturnBack(ctx, fixture.supplier, itemID, 1, reason, createKey)
	if err != nil || replayed.ID != first.ID || replayed.Status != "requested" {
		t.Fatalf("active request retry = %+v, %v, want original request %s", replayed, err, first.ID)
	}
	if _, err = repo.CreateSupplierReturnBack(ctx, fixture.supplier, itemID, 1, "changed reason", createKey); !errors.Is(err, ErrSupplierReturnBackBadState) {
		t.Fatalf("reuse with changed reason must conflict, got %v", err)
	}
	if _, err = repo.CreateSupplierReturnBack(ctx, fixture.supplier, itemID, 1, reason, uuid.NewString()); !errors.Is(err, ErrSupplierReturnBackActive) {
		t.Fatalf("second active request must conflict, got %v", err)
	}
	if _, err = repo.CancelSupplierReturnBack(ctx, fixture.supplier, first.ID, "withdraw request", createKey); !errors.Is(err, ErrSupplierReturnBackBadState) {
		t.Fatalf("key used to create cannot cancel, got %v", err)
	}
	current, err := repo.GetSupplierReturnBack(ctx, fixture.supplier, first.ID)
	if err != nil || current.Status != "requested" {
		t.Fatalf("failed transition changed request: %+v, %v", current, err)
	}

	cancelKey := uuid.NewString()
	cancelled, err := repo.CancelSupplierReturnBack(ctx, fixture.supplier, first.ID, "withdraw request", cancelKey)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("cancel request = %+v, %v", cancelled, err)
	}
	cancelled, err = repo.CancelSupplierReturnBack(ctx, fixture.supplier, first.ID, "withdraw request", cancelKey)
	if err != nil || cancelled.ID != first.ID || cancelled.Status != "cancelled" {
		t.Fatalf("cancel retry = %+v, %v", cancelled, err)
	}
	replayed, err = repo.CreateSupplierReturnBack(ctx, fixture.supplier, itemID, 1, reason, createKey)
	if err != nil || replayed.ID != first.ID || replayed.Status != "cancelled" {
		t.Fatalf("retry after transition must return original request, got %+v, %v", replayed, err)
	}
	var requests, events int
	err = db.QueryRow(`
		SELECT (SELECT COUNT(*) FROM supplier_return_back_requests WHERE return_item_id = $1),
			(SELECT COUNT(*) FROM supplier_return_back_events WHERE back_request_id = $2)
	`, itemID, first.ID).Scan(&requests, &events)
	if err != nil || requests != 1 || events != 2 {
		t.Fatalf("request/event counts = %d/%d, %v, want 1/2", requests, events, err)
	}

	// Another legitimate request may follow a cancellation. Simultaneous
	// identical submissions must resolve to a single new request and event.
	type submission struct {
		back *SupplierCustomerReturnBackRequest
		err  error
	}
	key := uuid.NewString()
	start := make(chan struct{})
	results := make(chan submission, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			back, err := repo.CreateSupplierReturnBack(ctx, fixture.supplier, itemID, 1, reason, key)
			results <- submission{back: back, err: err}
		}()
	}
	close(start)
	one, two := <-results, <-results
	if one.err != nil || two.err != nil || one.back.ID == first.ID || one.back.ID != two.back.ID {
		t.Fatalf("concurrent retries disagree: %+v/%v and %+v/%v", one.back, one.err, two.back, two.err)
	}
	if err = db.QueryRow(`
		SELECT (SELECT COUNT(*) FROM supplier_return_back_requests WHERE return_item_id = $1),
			(SELECT COUNT(*) FROM supplier_return_back_events WHERE back_request_id = $2)
	`, itemID, one.back.ID).Scan(&requests, &events); err != nil || requests != 2 || events != 1 {
		t.Fatalf("concurrent request/event counts = %d/%d, %v, want 2/1", requests, events, err)
	}

	opsExec(t, db, `UPDATE supplier_return_back_requests SET status = 'shipped_back', shipped_at = NOW() WHERE id = $1`, one.back.ID)
	completed, err := repo.CompleteSupplierReturnBack(ctx, fixture.supplier, one.back.ID, "")
	if err != nil || completed.Status != "completed" || completed.CompletedAt == nil {
		t.Fatalf("confirm receipt = %+v, %v", completed, err)
	}
	completedAgain, err := repo.CompleteSupplierReturnBack(ctx, fixture.supplier, one.back.ID, "")
	if err != nil || completedAgain.CompletedAt == nil || !completedAgain.CompletedAt.Equal(*completed.CompletedAt) {
		t.Fatalf("confirm receipt retry = %+v, %v", completedAgain, err)
	}
	if _, err = repo.CreateSupplierReturnBack(ctx, fixture.supplier, itemID, 1, reason, uuid.NewString()); !errors.Is(err, ErrSupplierReturnBackBadQuantity) {
		t.Fatalf("completed item must reduce remaining quantity, got %v", err)
	}
}
