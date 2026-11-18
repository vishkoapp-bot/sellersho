package observability

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
	"github.com/drup/platform/internal/testredis"
)

// OBS-01 D-04: every job publishes last-success / backlog / oldest-age.
func TestWorkerGaugesForAllJobs(t *testing.T) {
	db, e := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	m := New()
	m.SetDB(db)
	ctx := context.Background()
	// No runs on record: every job is stale.
	if stale := m.pollWorkerGauges(ctx, db); !stale {
		t.Fatal("jobs with no successful run must be stale")
	}
	// One successful run per job: five in operations_job_runs,
	// delivery-lock-releaser in job_runs (where JobStore records it).
	for _, job := range []string{"unpaid-order-expiry", "ad-cleanup", "qc-response-deadline", "supplier-sla", "monthly-settlement"} {
		if _, e := db.Exec(`INSERT INTO operations_job_runs(job,affected,error,started_at,finished_at) VALUES($1,0,'',now(),now())`, job); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := db.Exec(`INSERT INTO job_runs(job_id,started_at,released,confirmed,duration_ms,error) VALUES('delivery-lock-releaser',now(),0,0,1,'')`); e != nil {
		t.Fatal(e)
	}
	if stale := m.pollWorkerGauges(ctx, db); stale {
		t.Fatal("jobs with a fresh successful run must not be stale")
	}
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	s := w.Body.String()
	for _, prefix := range []string{
		"worker_unpaid_order_expiry", "worker_ad_cleanup", "worker_qc_response_deadline",
		"worker_supplier_sla", "worker_delivery_lock_releaser", "worker_monthly_settlement",
	} {
		for _, suffix := range []string{"_last_success_timestamp_seconds", "_backlog", "_oldest_age_seconds"} {
			if !strings.Contains(s, "sellersho_"+prefix+suffix) {
				t.Fatalf("missing gauge sellersho_%s%s", prefix, suffix)
			}
		}
	}
	// Legacy expiry aliases keep publishing (dashboard + gate G-02).
	for _, name := range []string{"sellersho_worker_expiry_last_success_timestamp_seconds", "sellersho_worker_expiry_backlog", "sellersho_worker_expiry_oldest_age_seconds"} {
		if !strings.Contains(s, name) {
			t.Fatalf("missing legacy gauge %s", name)
		}
	}
}

// OBS-01 D-12: read vs mutation classes are distinguishable in the HTTP metrics.
func TestHTTPOpclassLabels(t *testing.T) {
	m := New()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orders/", func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); w.WriteHeader(200) })
	h := m.HTTP(mux, mux)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/orders/1", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/v1/orders", strings.NewReader("{}")))
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	s := w.Body.String()
	if !strings.Contains(s, `opclass="read"`) || !strings.Contains(s, `opclass="mutation"`) {
		t.Fatalf("opclass labels missing in metrics output:\n%s", s)
	}
}

// OBS-01 D-06: reject counter by reason + admission in-flight gauge.
func TestRejectCounterAndInflight(t *testing.T) {
	m := New()
	m.RecordReject("request_overloaded")
	m.RecordReject("request_overloaded")
	m.RecordReject("service_draining")
	m.RecordReject("request_body_too_large")
	m.RecordReject("bogus_reason_must_not_create_a_series")
	m.SetAdmissionInflight("api", 3)
	m.SetAdmissionInflight("upload", 1)
	m.SetAdmissionInflight("bogus_lane", 9)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	s := w.Body.String()
	for _, want := range []string{
		`sellersho_http_rejects_total{reason="request_overloaded"} 2`,
		`sellersho_http_rejects_total{reason="service_draining"} 1`,
		`sellersho_http_rejects_total{reason="request_body_too_large"} 1`,
		`sellersho_admission_inflight{lane="api"} 3`,
		`sellersho_admission_inflight{lane="upload"} 1`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "bogus") {
		t.Fatalf("unbounded reject/inflight value leaked a series:\n%s", s)
	}
}

// OBS-01 D-05: no job may be recorded under "reconciliation" (or the old
// "expiry"/"settlement" labels); the six job IDs are the operations.
func TestWorkerOperationsAreJobIDs(t *testing.T) {
	m := New()
	for _, job := range []string{"unpaid-order-expiry", "ad-cleanup", "qc-response-deadline", "supplier-sla", "delivery-lock-releaser", "monthly-settlement"} {
		m.Observe("worker", job, "success", time.Millisecond)
	}
	m.Observe("worker", "reconciliation", "success", time.Millisecond)
	m.Observe("worker", "settlement", "success", time.Millisecond)
	m.Observe("worker", "expiry", "success", time.Millisecond)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	s := w.Body.String()
	for _, job := range []string{"unpaid-order-expiry", "ad-cleanup", "qc-response-deadline", "supplier-sla", "delivery-lock-releaser", "monthly-settlement"} {
		if !strings.Contains(s, `operation="`+job+`"`) {
			t.Fatalf("missing operation %q", job)
		}
	}
	if strings.Contains(s, `operation="reconciliation"`) || strings.Contains(s, `operation="settlement"`) || strings.Contains(s, `operation="expiry"`) {
		t.Fatalf("retired shared operation label still emitted:\n%s", s)
	}
}

// OBS-01 D-13: poller end-to-end — real HTTP traffic through
// PollDependencies delivers an api_errors alert to the admin inbox
// (not a hand-built AlertSnapshot), and the verified backup signal
// appears as a gauge after the publisher row lands.
func TestPollDependenciesDeliversAlertsEndToEnd(t *testing.T) {
	db, e := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	// F-1 (OBS-01 supervisor NO-GO): the alert-state table is shared by
	// every test in this package's disposable DB.
	// TestAdminAlertDeliveryDeduplicatedAndRearmed leaves all codes active
	// with notified_at=now(), so without a reset the INSERT ... ON CONFLICT
	// transition in DeliverAlerts never re-arms delivery_pending for
	// api_errors (active stays true, so pending stays false) and the
	// cooldown UPDATE matches nothing — the poller would deliver nothing
	// even though the traffic is real. Resetting notified_at alone is NOT
	// enough for that reason. Deleting the rows restores the fresh-DB state
	// this test assumes (a fresh row defaults to notified_at='-infinity'
	// with delivery_pending=true on first active delivery); the 15-minute
	// production cooldown behavior is untouched.
	if _, e := db.Exec(`DELETE FROM observability_alert_state`); e != nil {
		t.Fatal(e)
	}
	redisClient := testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))
	var admin, customer string
	if e = db.QueryRow(`INSERT INTO users(mobile,role,is_active,full_name) VALUES('09999000011','admin',true,'OBS E2E admin') RETURNING id`).Scan(&admin); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`INSERT INTO users(mobile,role,is_active,full_name) VALUES('09999000012','customer',true,'OBS E2E customer') RETURNING id`).Scan(&customer); e != nil {
		t.Fatal(e)
	}
	m := New()
	m.SetDB(db)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orders/ok", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/api/v1/orders/fail", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	h := m.HTTP(mux, mux)
	// 10 requests, 1 error = 10% error ratio, far above the 0.1% threshold.
	for i := 0; i < 9; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/orders/ok", nil))
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/orders/fail", nil))

	pollCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { m.PollDependencies(pollCtx, redisClient); close(done) }()
	defer func() { cancel(); <-done }()

	// The first poller tick runs immediately; wait for the outbox row.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var count int
		if e := db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert' AND payload->>'alertCode'='api_errors'`, admin).Scan(&count); e != nil {
			t.Fatal(e)
		}
		if count >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("poller did not deliver api_errors to the admin inbox from real traffic")
		}
		time.Sleep(100 * time.Millisecond)
	}
	var customerCount int
	if e := db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert'`, customer).Scan(&customerCount); e != nil || customerCount != 0 {
		t.Fatalf("customer received %d alerts: %v", customerCount, e)
	}
	// No dependency-loss alert: both dependencies are genuinely up.
	// Scoped to this test's admin: the outbox is shared by every test in
	// this package's disposable DB, and
	// TestAdminAlertDeliveryDeduplicatedAndRearmed legitimately delivers a
	// dependency_loss row to its own admin (plus any seeded admins) before
	// this test runs — an unscoped count would count those leftovers as
	// spurious rows from this test's poller. This test's admin was created
	// fresh above, so any dependency_loss row addressed to them can only
	// have come from this test's poller.
	var depCount int
	if e := db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert' AND payload->>'alertCode'='dependency_loss'`, admin).Scan(&depCount); e != nil || depCount != 0 {
		t.Fatalf("spurious dependency_loss rows: %d: %v", depCount, e)
	}
	// Per-job gauges are present after the first tick (D-04).
	scrape := func() string {
		w := httptest.NewRecorder()
		m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
		return w.Body.String()
	}
	for _, name := range []string{
		"sellersho_worker_unpaid_order_expiry_last_success_timestamp_seconds",
		"sellersho_worker_monthly_settlement_backlog",
	} {
		if !strings.Contains(scrape(), name) {
			t.Fatalf("missing per-job gauge %s after poller tick", name)
		}
	}
	// Backup gauge is absent before any publisher row (D-02: unknown,
	// never fabricated).
	if strings.Contains(scrape(), "sellersho_backup_last_success_timestamp_seconds") {
		t.Fatal("backup gauge present with no publisher row (fabricated signal)")
	}
	// The publisher writes a verified row; the next 15 s tick publishes it.
	if _, e := db.Exec(`INSERT INTO backup_signals(kind,last_success_at,verified_artifacts) VALUES('nightly',now(),'db.sql.gz,uploads.tar.gz,config.tar.gz') ON CONFLICT (kind) DO UPDATE SET last_success_at=EXCLUDED.last_success_at`); e != nil {
		t.Fatal(e)
	}
	deadline = time.Now().Add(20 * time.Second)
	for {
		if strings.Contains(scrape(), "sellersho_backup_last_success_timestamp_seconds") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("backup gauge did not appear after the publisher row landed")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// OBS-01 C-01: the poller-side quantile math over the HTTP histogram
// series. 100 samples: 94 at or below 0.3 s, 98 at or below 0.75 s, all
// 100 at or below 1 s — cumulative counts [94, 98, 100, 100], so p95 is
// the 95th sample (0.75 bucket) and p99 the 99th (1 s bucket).
func TestHistogramQuantile(t *testing.T) {
	bounds := []float64{0.3, 0.75, 1, 2}
	counts := []int64{94, 98, 100, 100}
	if q := histogramQuantile(bounds, counts, 0, 0.95); q != 0.75 {
		t.Fatalf("p95 = %v, want 0.75", q)
	}
	if q := histogramQuantile(bounds, counts, 0, 0.99); q != 1 {
		t.Fatalf("p99 = %v, want 1", q)
	}
	if q := histogramQuantile(bounds, []int64{0, 0, 0, 0}, 0, 0.95); q != 0 {
		t.Fatalf("empty histogram quantile = %v, want 0", q)
	}
	if q := histogramQuantile(bounds, []int64{0, 0, 0, 0}, 10, 0.95); !math.IsInf(q, 1) {
		t.Fatalf("all-samples-past-last-bucket quantile = %v, want +Inf", q)
	}
}

// sampleWithDurations builds one cumulative histogram series the way
// Registry.add does: Count plus per-bucket cumulative counts.
func sampleWithDurations(durs ...float64) *sample {
	s := &sample{Buckets: make([]int64, len(buckets))}
	for _, d := range durs {
		s.Count++
		for i, b := range buckets {
			if d <= b {
				s.Buckets[i]++
			}
		}
	}
	return s
}

func slowReads(n int, seconds float64) *sample {
	durs := make([]float64, n)
	for i := range durs {
		durs[i] = seconds
	}
	return sampleWithDurations(durs...)
}

// OBS-01 C-01: sloFromHTTPDelta diffs cumulative histogram series into
// a poll-window p95/p99 per class. The first tick only baselines; a
// window with fewer than sloMinSamplesPerClass requests per class is
// unknown; non-API classes never feed the SLO.
func TestSLOFromHTTPDelta(t *testing.T) {
	readKey := `class="api",method="GET",route="/api/v1/orders/*",status="200",outcome="completed",opclass="read"`
	mutKey := `class="api",method="POST",route="/api/v1/orders",status="200",outcome="completed",opclass="mutation"`
	assetKey := `class="asset",method="GET",route="/static/*",status="200",outcome="completed",opclass="read"`
	// First tick baselines: no window, nothing known.
	cur := map[string]*sample{readKey: slowReads(10, 0.01)}
	slo, prev := sloFromHTTPDelta(nil, cur)
	if slo.known {
		t.Fatal("first tick must baseline, not report a window")
	}
	// Window 1: 20 more reads at 0.4 s (p95/p99 = 0.5 s bucket) plus a
	// slow asset series that must be ignored. Cumulative: 10 fast +
	// 20 slow reads.
	durs := make([]float64, 30)
	for i := 0; i < 10; i++ {
		durs[i] = 0.01
	}
	for i := 10; i < 30; i++ {
		durs[i] = 0.4
	}
	cur[readKey] = sampleWithDurations(durs...)
	cur[assetKey] = slowReads(20, 5)
	slo, prev = sloFromHTTPDelta(prev, cur)
	if !slo.known || slo.readP95 != 0.5 || slo.readP99 != 0.5 {
		t.Fatalf("read window quantiles = known=%v p95=%v p99=%v, want known=true p95=0.5 p99=0.5", slo.known, slo.readP95, slo.readP99)
	}
	if slo.mutationP95 != 0 || slo.mutationP99 != 0 {
		t.Fatalf("mutation quantiles without mutation traffic = %v/%v, want 0/0", slo.mutationP95, slo.mutationP99)
	}
	// Window 2: only 5 slow mutations — below the 10-sample floor, so
	// the mutation class stays unknown and the read class (no new
	// traffic) is unknown too.
	cur[mutKey] = slowReads(5, 1.0)
	slo, prev = sloFromHTTPDelta(prev, cur)
	if slo.known {
		t.Fatalf("below-floor window must be unknown, got %+v", slo)
	}
	// Window 3: 10 more mutations at 1.0 s — mutation p95 = 1 s bucket.
	cur[mutKey] = slowReads(15, 1.0)
	slo, _ = sloFromHTTPDelta(prev, cur)
	if !slo.known || slo.mutationP95 != 1 || slo.mutationP99 != 1 {
		t.Fatalf("mutation window quantiles = known=%v p95=%v p99=%v, want known=true p95=1 p99=1", slo.known, slo.mutationP95, slo.mutationP99)
	}
}

// OBS-01 C-01: the poller delivers a slo_breach alert from real slow
// traffic (not a hand-built snapshot). Ten slow reads inside one 15 s
// poll window trip the read p95 arm (all ~400 ms > 300 ms). The first
// tick baselines the histogram, so the requests are driven after the
// poller starts; the breach is evaluated on the next tick.
func TestPollDependenciesDeliversSLOBreach(t *testing.T) {
	db, e := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	// Fresh alert state (F-1 pattern): the synthetic SLO test runs
	// before this test in the package and leaves slo_breach active.
	if _, e := db.Exec(`DELETE FROM observability_alert_state`); e != nil {
		t.Fatal(e)
	}
	redisClient := testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))
	var admin, customer string
	if e = db.QueryRow(`INSERT INTO users(mobile,role,is_active,full_name) VALUES('09999000041','admin',true,'OBS SLO poller admin') RETURNING id`).Scan(&admin); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`INSERT INTO users(mobile,role,is_active,full_name) VALUES('09999000042','customer',true,'OBS SLO poller customer') RETURNING id`).Scan(&customer); e != nil {
		t.Fatal(e)
	}
	m := New()
	m.SetDB(db)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orders/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.WriteHeader(200)
	})
	h := m.HTTP(mux, mux)

	pollCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { m.PollDependencies(pollCtx, redisClient); close(done) }()
	defer func() { cancel(); <-done }()

	// Ten slow reads, all inside the window the next tick evaluates.
	for i := 0; i < 10; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/orders/slow", nil))
	}
	deadline := time.Now().Add(40 * time.Second)
	for {
		var count int
		if e := db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert' AND payload->>'alertCode'='slo_breach'`, admin).Scan(&count); e != nil {
			t.Fatal(e)
		}
		if count >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("poller did not deliver slo_breach to the admin inbox from real slow traffic")
		}
		time.Sleep(200 * time.Millisecond)
	}
	var customerCount int
	if e := db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert'`, customer).Scan(&customerCount); e != nil || customerCount != 0 {
		t.Fatalf("customer received %d alerts: %v", customerCount, e)
	}
}
