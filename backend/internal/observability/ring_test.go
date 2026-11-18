package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// OBS-01 G-03: the ring keeps the last 1000 completed requests and
// evicts the oldest on insert; unknown IDs miss.
func TestRequestRingPushEvictLookup(t *testing.T) {
	m := New()
	for i := 0; i < requestRingCapacity+5; i++ {
		id := fmt.Sprintf("req-%04d", i)
		m.beginRequest(id, "GET", "/api/v1/orders/*", "api", "read", time.Now())
		m.completeRequest(id, 200, "completed", 12*time.Millisecond)
	}
	if _, ok := m.Lookup("req-0000"); ok {
		t.Fatal("oldest record must be evicted past capacity")
	}
	if _, ok := m.Lookup("req-0004"); ok {
		t.Fatal("record 4 must be evicted once 1005 were pushed")
	}
	rec, ok := m.Lookup("req-0005")
	if !ok {
		t.Fatal("record 5 must still be in the ring")
	}
	if rec.Method != "GET" || rec.Route != "/api/v1/orders/*" || rec.Class != "api" || rec.Opclass != "read" || rec.Status != 200 || rec.Outcome != "completed" || rec.DurationMs != 12 {
		t.Fatalf("record fields wrong: %+v", rec)
	}
	if rec.Spans == nil {
		t.Fatal("spans must encode as [] not null")
	}
	if _, ok := m.Lookup("req-does-not-exist"); ok {
		t.Fatal("unknown ID must miss")
	}
	// A request still in flight is not lookable yet.
	m.beginRequest("req-pending", "POST", "/api/v1/orders", "api", "mutation", time.Now())
	if _, ok := m.Lookup("req-pending"); ok {
		t.Fatal("in-flight record must not be lookable before completion")
	}
	m.completeRequest("req-pending", 201, "completed", time.Millisecond)
	if _, ok := m.Lookup("req-pending"); !ok {
		t.Fatal("completed record must be lookable")
	}
	// Lookup returns a copy: mutating it must not corrupt the ring.
	rec, _ = m.Lookup("req-pending")
	rec.Status = 500
	rec.Spans = append(rec.Spans, requestSpan{Component: "sql", Operation: "query", Outcome: "success"})
	rec2, _ := m.Lookup("req-pending")
	if rec2.Status != 201 || len(rec2.Spans) != 0 {
		t.Fatalf("lookup copy mutated ring state: %+v", rec2)
	}
	// Completing an unknown ID is a no-op, never a panic.
	m.completeRequest("req-never-begun", 200, "completed", time.Millisecond)
}

// OBS-01 G-03 (reverify3 residual R7): in-flight entries older than
// inFlightMaxAge are evicted on the next insert; fresh and recent
// entries survive, and completing an evicted entry is a no-op.
func TestRequestRingStaleInFlightEvicted(t *testing.T) {
	m := New()
	// 11 minutes old: stale — must be swept on the next insert.
	m.beginRequest("req-stale", "GET", "/api/v1/orders/*", "api", "read", time.Now().Add(-11*time.Minute))
	// 9 minutes old: not yet stale — must survive the sweep.
	m.beginRequest("req-recent", "GET", "/api/v1/orders/*", "api", "read", time.Now().Add(-9*time.Minute))
	// Fresh entry: must survive.
	m.beginRequest("req-fresh", "GET", "/api/v1/orders/*", "api", "read", time.Now())
	// One more insert triggers another sweep; nothing fresh may go.
	m.beginRequest("req-trigger", "GET", "/api/v1/orders/*", "api", "read", time.Now())

	// The stale entry is gone: completing it is a no-op, so it never
	// reaches the ring and Lookup misses.
	m.completeRequest("req-stale", 200, "completed", time.Millisecond)
	if _, ok := m.Lookup("req-stale"); ok {
		t.Fatal("stale in-flight entry must be evicted, not completed into the ring")
	}
	// The recent and fresh entries complete normally.
	m.completeRequest("req-recent", 200, "completed", time.Millisecond)
	if _, ok := m.Lookup("req-recent"); !ok {
		t.Fatal("9-minute-old in-flight entry must not be evicted early")
	}
	m.completeRequest("req-fresh", 200, "completed", time.Millisecond)
	if _, ok := m.Lookup("req-fresh"); !ok {
		t.Fatal("fresh in-flight entry must survive the sweep")
	}
}

// OBS-01 G-03: spans cap at 64 per record; the rest are dropped and
// counted, so one pathological request cannot bloat the ring.
func TestRequestRingSpanCap(t *testing.T) {
	m := New()
	m.beginRequest("req-span-cap", "GET", "/api/v1/orders/*", "api", "read", time.Now())
	ctx := context.WithValue(context.Background(), contextKey{}, "req-span-cap")
	for i := 0; i < maxSpansPerRequest+6; i++ {
		m.RecordSpan(ctx, "sql", "query", "success", time.Millisecond)
	}
	m.completeRequest("req-span-cap", 200, "completed", time.Millisecond)
	rec, ok := m.Lookup("req-span-cap")
	if !ok {
		t.Fatal("record missing")
	}
	if len(rec.Spans) != maxSpansPerRequest || rec.DroppedSpans != 6 {
		t.Fatalf("spans=%d dropped=%d, want %d/6", len(rec.Spans), rec.DroppedSpans, maxSpansPerRequest)
	}
}

// OBS-01 G-03: LogOperation attaches spans to the pending record of
// the request in ctx, and drops spans for background work with no
// pending record. LogOperation is Default-bound by design (production
// wires observability.Default everywhere), so this test drives the
// middleware on Default and looks the request up there; the request ID
// is a fresh 128-bit random value, so no other test can collide with it.
func TestRequestRingSpanAttachmentViaLogOperation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orders/", func(w http.ResponseWriter, r *http.Request) {
		LogOperation(r.Context(), "sql", "query", "success", 12*time.Millisecond)
		LogOperation(r.Context(), "provider", "tapin_request", "success", 30*time.Millisecond)
		w.WriteHeader(200)
	})
	h := Default.HTTP(mux, mux)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/orders/1", nil))
	id := recorder.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("middleware did not set X-Request-ID")
	}
	rec, ok := Default.Lookup(id)
	if !ok {
		t.Fatal("completed request missing from the ring")
	}
	if rec.Route != "/api/v1/orders/*" || rec.Status != 200 || rec.DurationMs < 0 {
		t.Fatalf("record fields wrong: %+v", rec)
	}
	if len(rec.Spans) != 2 || rec.Spans[0].Component != "sql" || rec.Spans[0].Operation != "query" || rec.Spans[1].Component != "provider" || rec.Spans[1].Operation != "tapin_request" {
		t.Fatalf("spans wrong: %+v", rec.Spans)
	}
	// Background work (no request ID in ctx) must not panic or attach.
	LogOperation(context.Background(), "worker", "ad-cleanup", "success", time.Millisecond)
}

// OBS-01 G-03: the Redis hook records a span (command name only —
// arguments are never read), for single commands and pipelines, and a
// lookup of that request carries the span without the secret argument.
func TestRedisHookRecordsSpan(t *testing.T) {
	m := New()
	m.beginRequest("req-redis", "GET", "/api/v1/orders/*", "api", "read", time.Now())
	ctx := context.WithValue(context.Background(), contextKey{}, "req-redis")
	h := RedisHook{Registry: m}
	cmd := redis.NewCmd(ctx, "eval", "SECRET", 0, "private-key")
	if err := h.ProcessHook(func(context.Context, redis.Cmder) error { return nil })(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	if err := h.ProcessPipelineHook(func(context.Context, []redis.Cmder) error { return nil })(ctx, []redis.Cmder{cmd}); err != nil {
		t.Fatal(err)
	}
	m.completeRequest("req-redis", 200, "completed", time.Millisecond)
	rec, ok := m.Lookup("req-redis")
	if !ok {
		t.Fatal("record missing")
	}
	if len(rec.Spans) != 2 || rec.Spans[0].Component != "redis" || rec.Spans[0].Operation != "eval" || rec.Spans[1].Operation != "pipeline" {
		t.Fatalf("redis spans wrong: %+v", rec.Spans)
	}
	// The recorded spans carry no argument values (G-10 redaction).
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "private-key") {
		t.Fatalf("secret argument leaked into ring record: %s", raw)
	}
}

// OBS-01 G-03 (re-verifier gap G4): a lookup never blocks a scrape and
// a scrape never blocks a lookup — the ring mutex (ringMu) and the
// metrics mutex (mu) are separate and never held together. Each
// direction is proven by holding one lock and requiring the operation
// that needs only the other lock to return promptly.
func TestLookupNeverBlocksScrape(t *testing.T) {
	m := New()
	m.beginRequest("req-scrape", "GET", "/api/v1/orders/*", "api", "read", time.Now())
	m.completeRequest("req-scrape", 200, "completed", time.Millisecond)

	// Hold ringMu: the /metrics scrape (mu only) must still return.
	m.ringMu.Lock()
	scrapeDone := make(chan string, 1)
	go func() {
		w := httptest.NewRecorder()
		m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
		scrapeDone <- w.Body.String()
	}()
	select {
	case body := <-scrapeDone:
		// sellersho_http_active is emitted on every scrape, even
		// with no HTTP samples yet; its presence proves the scrape
		// ran (promptly) while ringMu was held.
		if !strings.Contains(body, "sellersho_http_active") {
			t.Fatalf("scrape while ringMu held returned no metrics: %q", body)
		}
	case <-time.After(2 * time.Second):
		m.ringMu.Unlock()
		t.Fatal("scrape blocked while ringMu was held (lookup would block scrapes)")
	}
	m.ringMu.Unlock()

	// Hold mu: a lookup (ringMu only) must still return.
	m.mu.Lock()
	lookupDone := make(chan bool, 1)
	go func() {
		_, ok := m.Lookup("req-scrape")
		lookupDone <- ok
	}()
	select {
	case ok := <-lookupDone:
		if !ok {
			t.Fatal("lookup while mu held missed the record")
		}
	case <-time.After(2 * time.Second):
		m.mu.Unlock()
		t.Fatal("lookup blocked while mu was held (scrape would block lookups)")
	}
	m.mu.Unlock()

	// Sustained mixed load (~2 s): concurrent records, lookups and
	// scrapes must all finish promptly with no deadlock.
	var wg sync.WaitGroup
	done := make(chan struct{})
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-done:
					return
				default:
				}
				id := fmt.Sprintf("req-load-%d-%d", n, i)
				m.beginRequest(id, "GET", "/api/v1/orders/*", "api", "read", time.Now())
				m.RecordSpan(context.WithValue(context.Background(), contextKey{}, id), "sql", "query", "success", time.Millisecond)
				m.completeRequest(id, 200, "completed", time.Millisecond)
				if _, ok := m.Lookup(id); !ok {
					t.Errorf("record %s missing under load", id)
					return
				}
				w := httptest.NewRecorder()
				m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
			}
		}(g)
	}
	time.Sleep(2 * time.Second)
	close(done)
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("mixed record/lookup/scrape load deadlocked")
	}
}
