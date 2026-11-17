package worker

import (
	"context"
	"testing"
	"time"
)

func TestFIN005ConfirmationRechecksRetainedFailureAfterParentWait(t *testing.T) {
	db := jobDB(t)
	f := seedJobFixture(t, db)
	seedJobOrders(t, db, f, 1, 1)
	id := f.prefix + "1"
	jobExec(t, db, `UPDATE orders SET status='shipped',payment_status='paid',shipped_at=NOW()-INTERVAL '21 days',payment_expires_at=NULL WHERE id=$1`, id)
	jobExec(t, db, `INSERT INTO supplier_shipments(id,order_id,supplier_id,status,shipping_address,customer_phone,customer_name) VALUES($1,$1,$2,'shipped','{}','fixture','fixture')`, id, f.supplier)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	accepted, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Rollback()
	if _, err = accepted.ExecContext(ctx, `SELECT id FROM orders WHERE id=$1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	// Retained old-writer source may commit without propagating aggregate status.
	if _, err = accepted.ExecContext(ctx, `UPDATE supplier_shipments SET status='failed' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		confirmed, e := NewBackgroundWorker(db).confirmDelivery(ctx, id)
		if confirmed && e == nil {
			e = errFIN005UnexpectedConfirmation{}
		}
		result <- e
	}()
	for {
		var waiting bool
		if err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT id FROM orders WHERE id=$1 FOR UPDATE%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
	if err = accepted.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	var clean bool
	if err = db.QueryRow(`SELECT status='shipped' AND confirmed_at IS NULL AND return_window_ends_at IS NULL AND NOT profit_released FROM orders WHERE id=$1`, id).Scan(&clean); err != nil || !clean {
		t.Fatalf("retained failure gained synthetic confirmation/window/release %v %v", clean, err)
	}
}

type errFIN005UnexpectedConfirmation struct{}

func (errFIN005UnexpectedConfirmation) Error() string {
	return "confirmation admitted retained failed carrier source after parent wait"
}
