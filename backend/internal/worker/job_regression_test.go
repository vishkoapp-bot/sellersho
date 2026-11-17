package worker

import (
	"context"
	"github.com/drup/platform/internal/testdb"
	"os"
	"testing"
	"time"
)

func TestBackgroundWorkerStopIdempotent(t *testing.T) {
	w := NewBackgroundWorker(nil)
	w.Stop()
	w.Stop()
	w.Start() // stop-before-start must remain stopped
}

func TestMonthlyMissingRepositoryIsFailureNotDisabledSetting(t *testing.T) {
	result, err := NewBackgroundWorker(nil).monthlySettlement(context.Background())
	if err == nil || result.Disabled {
		t.Fatalf("missing dependency disguised as configured pause: %+v %v", result, err)
	}
}

func TestBackgroundOutcomeSurvivesExpiredBusinessContext(t *testing.T) {
	dsn := os.Getenv("TEAM09_WORKER_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable database required")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now().Add(-time.Second)
	NewBackgroundWorker(db).recordJobRun(ctx, started, 7, 3, context.DeadlineExceeded)
	var released int64
	if err := db.QueryRow(`SELECT released FROM job_runs WHERE started_at=$1`, started).Scan(&released); err != nil {
		t.Fatalf("outcome lost after cancellation: %v", err)
	}
	if released != 7 {
		t.Fatalf("committed count lost: %d", released)
	}
}
