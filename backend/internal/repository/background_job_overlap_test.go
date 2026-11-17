package repository

import (
	"context"
	"testing"
)

func TestFinancialReviewExcludesOverlappingAutomaticRetry(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	id, _ := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE orders SET payment_expires_at=NOW()-INTERVAL '2 minutes' WHERE id=$1`, id)
	opsExec(t, db, `INSERT INTO order_expiry_review_state(order_id,reason,next_retry_at) VALUES($1,'candidate_rejected',NOW()+INTERVAL '5 minutes')`, id)
	opsExec(t, db, `INSERT INTO background_job_candidate_retry(job,candidate,reason,next_retry_at) VALUES('unpaid-order-expiry',$1,'transient',NOW()+INTERVAL '1 minute')`, id)
	h := JobHealth{Job: "unpaid-order-expiry"}
	if err := jobBacklog(context.Background(), db, &h); err != nil {
		t.Fatal(err)
	}
	if h.Ready != 0 || h.Review != 1 || h.AutomaticRetry != 0 {
		t.Fatalf("financial review must exclude automatic retry: Ready=%d Review=%d AutomaticRetry=%d", h.Ready, h.Review, h.AutomaticRetry)
	}
}
