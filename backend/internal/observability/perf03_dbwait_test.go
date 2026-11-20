package observability

import (
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// PERF-03 G5: the pool-wait histogram folds cumulative sql.DBStats
// counters into per-interval mean-wait samples and exposes them in
// /metrics with p95-computable buckets.
func perf03DBWaitMetrics(t *testing.T, m *Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}

func TestPERF03DBWaitHistogramSamplesIntervalMeans(t *testing.T) {
	m := New()
	// Baseline snapshot establishes the previous counters; a tick
	// with no new waits adds nothing.
	m.sampleDBWaitLocked(sql.DBStats{WaitCount: 100, WaitDuration: 2 * time.Second})
	m.sampleDBWaitLocked(sql.DBStats{WaitCount: 100, WaitDuration: 2 * time.Second})
	if out := perf03DBWaitMetrics(t, m); strings.Contains(out, "sellersho_db_wait_seconds_count") {
		t.Fatalf("no-wait intervals must not emit a histogram, got:\n%s", out)
	}
	// 10 new waits totalling 500 ms -> one 50 ms mean sample.
	m.sampleDBWaitLocked(sql.DBStats{WaitCount: 110, WaitDuration: 2500 * time.Millisecond})
	out := perf03DBWaitMetrics(t, m)
	if !strings.Contains(out, "sellersho_db_wait_seconds_count 1") {
		t.Fatalf("want histogram count 1, got:\n%s", out)
	}
	if !strings.Contains(out, `sellersho_db_wait_seconds_bucket{le="0.05"} 1`) {
		t.Fatalf("want the 50 ms sample in the le=0.05 bucket, got:\n%s", out)
	}
	if !strings.Contains(out, `sellersho_db_wait_seconds_bucket{le="0.025"} 0`) {
		t.Fatalf("want the 50 ms sample above the le=0.025 bucket, got:\n%s", out)
	}
	if !strings.Contains(out, "sellersho_db_wait_last_mean_seconds 0.05") {
		t.Fatalf("want last-mean gauge 0.05, got:\n%s", out)
	}
	if !strings.Contains(out, "sellersho_db_wait_max_mean_seconds 0.05") {
		t.Fatalf("want max-mean gauge 0.05, got:\n%s", out)
	}
	// 10 more waits totalling 100 ms -> a second 10 ms sample; the
	// max-mean gauge keeps the 50 ms bound.
	m.sampleDBWaitLocked(sql.DBStats{WaitCount: 120, WaitDuration: 2600 * time.Millisecond})
	out = perf03DBWaitMetrics(t, m)
	if !strings.Contains(out, "sellersho_db_wait_seconds_count 2") {
		t.Fatalf("want histogram count 2, got:\n%s", out)
	}
	if !strings.Contains(out, `sellersho_db_wait_seconds_bucket{le="0.01"} 1`) {
		t.Fatalf("want one sample in the le=0.01 bucket, got:\n%s", out)
	}
	if !strings.Contains(out, "sellersho_db_wait_last_mean_seconds 0.01") {
		t.Fatalf("want last-mean gauge 0.01, got:\n%s", out)
	}
	if !strings.Contains(out, "sellersho_db_wait_max_mean_seconds 0.05") {
		t.Fatalf("want max-mean gauge to keep 0.05, got:\n%s", out)
	}
}

// PERF-03 G5: StartDBWaitSampling with a nil DB or a non-positive
// interval is a no-op and never panics; starting twice is a no-op.
func TestPERF03DBWaitSamplingGuards(t *testing.T) {
	m := New()
	m.StartDBWaitSampling(nil, 100*time.Millisecond)
	db, err := sql.Open("postgres", "postgres://perf03:perf03@localhost:5433/perf03_nonexistent?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m.StartDBWaitSampling(db, 0)
	m.StartDBWaitSampling(db, 10*time.Millisecond)
	m.StartDBWaitSampling(db, 10*time.Millisecond)
	m.StopDBWaitSampling()
	m.StopDBWaitSampling()
}
