package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/drup/platform/internal/jobrunner"
	"github.com/drup/platform/internal/repository"
)

func TestWorkerPreStartCancellationAndCompatibilityAdmission(t *testing.T) {
	db := jobDB(t)
	w := NewBackgroundWorker(db)
	w.SetFinanceRepository(repository.NewFinanceRepository(db))
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	before := db.Stats().WaitCount
	done := make(chan error, 1)
	go func() { done <- w.RunDeliveryJob(context.Background()) }()
	until := time.Now().Add(time.Second)
	for db.Stats().WaitCount == before && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if db.Stats().WaitCount == before {
		t.Fatal("manual run did not reach actual pool wait")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err = w.StopContext(ctx); err != nil {
		t.Fatal("worker failed to cancel pre-Start admission", err)
	}
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("manual cancellation", err)
	}
	before = db.Stats().WaitCount
	late := make(chan struct{})
	go func() { defer close(late); w.ReleaseExpiredDeliveryLocks(); w.RunMonthlySettlementIfDue() }()
	select {
	case <-late:
	case <-time.After(150 * time.Millisecond):
		t.Fatal("exported compatibility methods bypass stopped Runner")
	}
	if db.Stats().WaitCount != before {
		t.Fatal("late compatibility admission reached SQL")
	}
	if _, err = w.runner().Run(context.Background(), "monthly-settlement"); !errors.Is(err, jobrunner.ErrStopped) {
		t.Fatal("late monthly admission", err)
	}
	w.Start()
	if err = w.StopContext(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibilityEntrypointsRespectDatabaseLease(t *testing.T) {
	db := jobDB(t)
	w := NewBackgroundWorker(db)
	w.SetFinanceRepository(repository.NewFinanceRepository(db))
	store := repository.JobStore{DB: db}
	for _, id := range []string{"delivery-lock-releaser", "monthly-settlement"} {
		ok, err := store.Acquire(context.Background(), id, "ops01-held", time.Minute)
		if err != nil || !ok {
			t.Fatal("fixture lease", id, ok, err)
		}
		defer store.Release(context.Background(), id, "ops01-held")
	}
	var before, after int
	if err := db.QueryRow(`SELECT count(*) FROM background_job_outcomes`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	w.ReleaseExpiredDeliveryLocks()
	w.RunMonthlySettlementIfDue()
	if err := db.QueryRow(`SELECT count(*) FROM background_job_outcomes`).Scan(&after); err != nil || before != after {
		t.Fatal("compatibility caller bypassed lease", before, after, err)
	}
	var legacy int
	if err := db.QueryRow(`SELECT count(*) FROM job_runs`).Scan(&legacy); err != nil || legacy != 0 {
		t.Fatal("busy compatibility delivery wrote direct-cycle history", legacy, err)
	}
}

func TestDeliveryConfirmationKeepsGuardedConfiguredWindow(t *testing.T) {
	db := jobDB(t)
	f := seedJobFixture(t, db)
	seedJobOrders(t, db, f, 1, 3)
	jobExec(t, db, `UPDATE platform_settings SET return_window_days=13`)
	jobExec(t, db, `UPDATE orders SET status='shipped',shipped_at=NOW()-INTERVAL '21 days' WHERE id LIKE $1`, f.prefix+"%")
	jobExec(t, db, `UPDATE orders SET shipped_at=NOW()-INTERVAL '19 days' WHERE id=$1`, f.prefix+"2")
	jobExec(t, db, `UPDATE orders SET profit_released=TRUE WHERE id=$1`, f.prefix+"3")
	w := NewBackgroundWorker(db)
	for i, id := range []string{f.prefix + "1", f.prefix + "2", f.prefix + "3"} {
		ok, err := w.confirmDelivery(context.Background(), id)
		if err != nil || ok != (i == 0) {
			t.Fatal("20-day/profit guard changed", id, ok, err)
		}
	}
	var days int
	var released bool
	if err := db.QueryRow(`SELECT EXTRACT(DAY FROM return_window_ends_at-confirmed_at)::int,profit_released FROM orders WHERE id=$1`, f.prefix+"1").Scan(&days, &released); err != nil || days != 13 || released {
		t.Fatal("configured return window or staged release changed", days, released, err)
	}
}
