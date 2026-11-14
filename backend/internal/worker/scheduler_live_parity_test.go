package worker

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/drup/platform/internal/testdb"

	_ "github.com/lib/pq"
)

// TestDeliveryLockReleaserStagedCycle is an opt-in integration test
// for the live staged profit release (MASTER-AUDIT D34). It is
// skipped during normal unit test runs unless
// TEAM09_WORKER_DATABASE_URL points at a DISPOSABLE database: it
// uses the canonical migrated orders/job_runs schema in its guarded database.
//
// Expected staged behaviour under test:
//   - shipped/delivered_unconfirmed older than 20 days -> confirmed
//     (status delivered_confirmed) but profit NOT released while the
//     return window is open or unrecorded;
//   - delivered_confirmed with a closed return window -> released;
//   - delivered_confirmed with an open window, a NULL window, or a
//     recent shipped_at -> untouched. The NULL-window case is the
//     documented TODO(U19/U26) gap: the worker deliberately does not
//     invent a fallback window.
func TestDeliveryLockReleaserStagedCycle(t *testing.T) {
	databaseURL := os.Getenv("TEAM09_WORKER_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEAM09_WORKER_DATABASE_URL is not set")
	}
	db, err := testdb.Open(t, databaseURL)
	if err != nil {
		t.Fatalf("open disposable database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("disposable database unavailable: %v", err)
	}

	var initialJobCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM job_runs`).Scan(&initialJobCount); err != nil {
		t.Fatal(err)
	}
	customer := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role,is_active) VALUES($1,$2,'Worker fixture customer','customer',TRUE)`, customer, "09"+time.Now().Format("150405000")); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	seed := []struct {
		name   string
		status string
		ship   *time.Time
		window *time.Time
	}{
		{"confirm-only", "shipped", timePtr(now.AddDate(0, 0, -21)), nil},
		{"release-closed-window", "delivered_confirmed", timePtr(now.AddDate(0, 0, -30)), timePtr(now.Add(-time.Hour))},
		{"open-window", "delivered_confirmed", timePtr(now.AddDate(0, 0, -30)), timePtr(now.Add(24 * time.Hour))},
		{"null-window", "delivered_confirmed", timePtr(now.AddDate(0, 0, -30)), nil},
		{"recent-shipment", "shipped", timePtr(now.Add(-24 * time.Hour)), nil},
	}
	ids := map[string]string{}
	for _, s := range seed {
		id := "worker-" + uuid.NewString()[:20]
		_, err := db.Exec(
			`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,status,shipped_at,return_window_ends_at)
			VALUES($1,$1,'store-central','Worker fixture',0,0,'Worker customer','09123456789','Test','Test','Test','1234512345',$1,$2,$3,$4,$5)`,
			id, customer, s.status, s.ship, s.window,
		)
		if err != nil {
			t.Fatalf("seed %s: %v", s.name, err)
		}
		ids[s.name] = id
	}

	w := NewBackgroundWorker(db)
	released, confirmed, err := w.runDeliveryLockReleaserCycle()
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if confirmed != 1 {
		t.Errorf("confirmed = %d, want 1 (only confirm-only)", confirmed)
	}
	if released != 1 {
		t.Errorf("released = %d, want 1 (only release-closed-window)", released)
	}

	check := func(name, wantStatus string, wantReleased bool) {
		t.Helper()
		var status string
		var profitReleased bool
		err := db.QueryRow(`SELECT status, profit_released FROM orders WHERE id = $1`, ids[name]).Scan(&status, &profitReleased)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if status != wantStatus || profitReleased != wantReleased {
			t.Errorf("%s: status=%s profit_released=%v, want status=%s profit_released=%v",
				name, status, profitReleased, wantStatus, wantReleased)
		}
	}
	check("confirm-only", "delivered_confirmed", false)
	check("release-closed-window", "delivered_confirmed", true)
	check("open-window", "delivered_confirmed", false)
	check("null-window", "delivered_confirmed", false)
	check("recent-shipment", "shipped", false)

	var jobID string
	var jobReleased, jobConfirmed int64
	var jobErr string
	err = db.QueryRow(`SELECT job_id, released, confirmed, error FROM job_runs ORDER BY id DESC LIMIT 1`).
		Scan(&jobID, &jobReleased, &jobConfirmed, &jobErr)
	if err != nil {
		t.Fatalf("read job_runs: %v", err)
	}
	if jobID != "delivery-lock-releaser" || jobReleased != 1 || jobConfirmed != 1 || jobErr != "" {
		t.Errorf("job_runs row: job_id=%q released=%d confirmed=%d error=%q, want delivery-lock-releaser/1/1/empty",
			jobID, jobReleased, jobConfirmed, jobErr)
	}

	// The hourly search rollup (TODO U73) must write nothing.
	w.runSearchRollup()
	var jobCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM job_runs`).Scan(&jobCount); err != nil || jobCount != initialJobCount+1 {
		t.Errorf("job_runs count after rollup = %d (err %v), want %d", jobCount, err, initialJobCount+1)
	}
}

func timePtr(t time.Time) *time.Time { return &t }
