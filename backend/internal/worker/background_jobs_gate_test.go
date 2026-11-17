package worker

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"github.com/drup/platform/internal/jobrunner"
	"github.com/lib/pq"
	"testing"
)

func TestExpiryBudgetFailureRejectsUnrelatedJoinedErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		evidence bool
		budget   bool
		want     bool
	}{
		{"observed business cancellation", errors.Join(driver.ErrBadConn, context.DeadlineExceeded, context.DeadlineExceeded), true, true, true},
		{"bad connection alone", driver.ErrBadConn, true, true, false},
		{"no budget evidence", errors.Join(driver.ErrBadConn, context.DeadlineExceeded), false, false, false},
		{"automatic retry without elapsed budget", errors.Join(driver.ErrBadConn, context.DeadlineExceeded), true, false, false},
		{"metadata failure", errors.Join(jobrunner.ErrMetadata, context.DeadlineExceeded, driver.ErrBadConn), true, true, false},
		{"unknown joined failure", errors.Join(errors.New("unexpected business failure"), context.DeadlineExceeded, driver.ErrBadConn), true, true, false},
		{"transaction done with evidence", sql.ErrTxDone, true, true, true},
		{"transaction done without evidence", sql.ErrTxDone, false, false, false},
		{"postgres cancellation", &pq.Error{Code: "57014"}, false, false, true},
		{"postgres unrelated", &pq.Error{Code: "23505"}, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := expectedExpiryBudgetFailure(tc.err, tc.evidence, tc.budget); got != tc.want {
				t.Fatalf("got %v want %v: %v", got, tc.want, tc.err)
			}
		})
	}
}

func TestCrashBarrierCandidatesExcludeDeferredOrders(t *testing.T) {
	db := jobDB(t)
	f := seedJobFixture(t, db)
	seedJobOrders(t, db, f, 1, 4)
	jobExec(t, db, `INSERT INTO background_job_candidate_retry(job,candidate,next_retry_at,reason) VALUES('unpaid-order-expiry',$1,clock_timestamp()+INTERVAL '5 seconds','transient')`, f.prefix+"1")
	jobExec(t, db, `INSERT INTO order_expiry_review_state(order_id,next_retry_at,reason) VALUES($1,clock_timestamp()+INTERVAL '5 minutes','candidate_rejected')`, f.prefix+"2")
	ids, err := jobCancellationCandidates(db, f.store)
	if err != nil || len(ids) != 2 || ids[0] != f.prefix+"3" || ids[1] != f.prefix+"4" {
		t.Fatalf("eligible barrier pair=%v err=%v; want orders3/4", ids, err)
	}
	var deferred int
	if err = db.QueryRow(`SELECT count(*) FROM background_job_candidate_retry WHERE candidate=$1`, f.prefix+"1").Scan(&deferred); err != nil || deferred != 1 {
		t.Fatal("selector removed retry policy evidence", deferred, err)
	}
}
