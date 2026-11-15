package worker

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

// This exact file is copied into both the actual previous committed source and
// the current allowed source. It uses only interfaces present in both versions.
// The barrier is a real slow delivery business SQL read, before per-order work.
func TestLegacySchedulerSlowDeliveryDoesNotStarveExpiry(t *testing.T) {
	dsn := os.Getenv("TEAM09_WORKER_DATABASE_URL")
	if dsn == "" {
		t.Skip("owned disposable database required")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(10)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	prefix := "job01-starve-" + uuid.NewString()[:8]
	delivery, pending := prefix+"-delivery", prefix+"-expiry"
	for _, id := range []string{delivery, pending} {
		exec(`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,payment_expires_at)
  VALUES($1,$1,'store-central','job01 barrier',0,0,'job01','09123456789','x','x','x','1234512345',$1,NOW()+INTERVAL '1 hour')`, id)
	}
	exec(`UPDATE orders SET status='shipped',shipped_at=NOW()-INTERVAL '21 days' WHERE id=$1`, delivery)
	var initialSLA int
	if err = db.QueryRow(`SELECT count(*) FROM operations_job_runs WHERE job='supplier-sla'`).Scan(&initialSLA); err != nil {
		t.Fatal(err)
	}
	barrier, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = barrier.Exec(`LOCK TABLE platform_settings IN ACCESS EXCLUSIVE MODE`); err != nil {
		barrier.Rollback()
		t.Fatal(err)
	}
	w := NewBackgroundWorker(db)
	w.SetOrderRepository(repository.NewOrderRepository(db))
	w.SetFinanceRepository(repository.NewFinanceRepository(db))
	t.Cleanup(func() {
		// Release the owned SQL barrier before Stop so the old implementation's
		// non-cancellable startup cycle can finish before its database is removed.
		_ = barrier.Rollback()
		until := time.Now().Add(5 * time.Second)
		for time.Now().Before(until) {
			var n int
			_ = db.QueryRow(`SELECT count(*) FROM operations_job_runs WHERE job='supplier-sla'`).Scan(&n)
			if n > initialSLA {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		w.Stop()
	})
	w.Start()
	blocked := func() bool {
		var yes bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE l.relation='platform_settings'::regclass AND NOT l.granted AND l.mode='AccessShareLock' AND a.query LIKE '%return_window_days%')`).Scan(&yes); err != nil {
			t.Fatal(err)
		}
		return yes
	}
	barrierDeadline := time.Now().Add(5 * time.Second)
	for !blocked() {
		if time.Now().After(barrierDeadline) {
			t.Fatal("delivery never entered owned SQL barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Log("JOB01_DELIVERY_STAGE_BARRIER_CONFIRMED")
	exec(`UPDATE orders SET payment_expires_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, pending)
	expiryDeadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(expiryDeadline) {
		var status string
		if err = db.QueryRowContext(context.Background(), `SELECT status FROM orders WHERE id=$1`, pending).Scan(&status); err != nil && err != sql.ErrNoRows {
			t.Fatal(err)
		}
		if status == "cancelled" {
			if !blocked() {
				t.Fatal("delivery barrier ended before expiry progressed")
			}
			t.Log("JOB01_EXPIRY_ADVANCED_WHILE_DELIVERY_BLOCKED")
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("JOB01_EXPIRY_STARVED_BEHIND_DELIVERY: eligible order remained pending while delivery SQL was blocked")
}
