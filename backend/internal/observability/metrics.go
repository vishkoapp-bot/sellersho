// Package observability collects bounded, value-free service telemetry. Callers
// must use registered operation names; unknown values collapse to "other".
package observability

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

var Default = New()
var buckets = []float64{.005, .01, .025, .05, .1, .25, .3, .5, .75, 1, 2, 5, 10, 30}

type sample struct {
	Count                       int64
	Sum                         float64
	Buckets                     []int64
	RequestBytes, ResponseBytes int64
}
type Registry struct {
	alertMu sync.Mutex
	pending map[string]bool
	mu      sync.Mutex
	http    map[string]*sample
	ops     map[string]*sample
	active  int64
	db      *sql.DB
	gauges  map[string]float64
	// rejects counts admission rejections by bounded reason (D-06):
	// "request_overloaded" (request-budget 503), "service_draining"
	// (drain 503), "request_body_too_large" (oversize-body 413/414
	// reject). They are distinct from the generic 5xx samples in the
	// HTTP histogram, so saturation rejects are countable as rejects.
	rejects map[string]int64
	// inflight is the current admission-slot occupancy per lane
	// ("api" = ordinary slots, "upload" = upload slots).
	inflight map[string]int
	// G-03 ring buffer (ring.go): last 1000 completed requests plus
	// pending in-flight records, behind ringMu — a mutex SEPARATE
	// from mu, so lookups never block /metrics scrapes and scrapes
	// never block lookups. The two locks are never held together.
	ringMu   sync.Mutex
	ringBuf  [requestRingCapacity]*requestRecord
	ringHead int
	ringLen  int
	// ringInflight holds pending records keyed by request ID; the
	// name differs from the admission `inflight` lane map above.
	ringInflight map[string]*requestRecord
}

func New() *Registry {
	return &Registry{http: map[string]*sample{}, ops: map[string]*sample{}, gauges: map[string]float64{}, pending: map[string]bool{}, rejects: map[string]int64{}, inflight: map[string]int{}, ringInflight: map[string]*requestRecord{}}
}
func (m *Registry) SetDB(db *sql.DB) { m.mu.Lock(); defer m.mu.Unlock(); m.db = db }
func add(dst map[string]*sample, key string, d float64, req, res int64) {
	s := dst[key]
	if s == nil {
		s = &sample{Buckets: make([]int64, len(buckets))}
		dst[key] = s
	}
	s.Count++
	s.Sum += d
	s.RequestBytes += req
	s.ResponseBytes += res
	for i, b := range buckets {
		if d <= b {
			s.Buckets[i]++
		}
	}
}
func bounded(v string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return a
		}
	}
	return "other"
}
func (m *Registry) Observe(component, operation, outcome string, d time.Duration) {
	component = bounded(component, "redis", "provider", "sql", "worker", "backup")
	// Worker operations are the six job IDs (D-05): one distinct
	// operation per job. There is no job named "reconciliation" and
	// no shared label; an unknown job ID collapses to "other".
	operation = bounded(operation, "get", "set", "setnx", "del", "eval", "evalsha", "ping", "pipeline", "request", "tapin_request", "ippanel_request", "verify", "refund", "unpaid-order-expiry", "ad-cleanup", "qc-response-deadline", "supplier-sla", "delivery-lock-releaser", "monthly-settlement", "backup", "query")
	outcome = bounded(outcome, "success", "error", "timeout", "canceled", "unknown", "overloaded")
	key := fmt.Sprintf(`component=%q,operation=%q,outcome=%q`, component, operation, outcome)
	m.mu.Lock()
	defer m.mu.Unlock()
	add(m.ops, key, d.Seconds(), 0, 0)
}

type contextKey struct{}

func RequestID(ctx context.Context) string { v, _ := ctx.Value(contextKey{}).(string); return v }
func NewContext(ctx context.Context) context.Context {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("request ID entropy unavailable")
	}
	return context.WithValue(ctx, contextKey{}, hex.EncodeToString(b[:]))
}

// RecordReject counts one admission rejection under a bounded reason.
// Reasons: "request_overloaded", "service_draining",
// "request_body_too_large". Anything else is dropped, never a new series.
func (m *Registry) RecordReject(reason string) {
	reason = bounded(reason, "request_overloaded", "service_draining", "request_body_too_large")
	if reason == "other" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejects[reason]++
}

// SetAdmissionInflight records current admission-slot occupancy for a lane
// ("api" or "upload"). Unknown lanes are dropped, never a new series.
func (m *Registry) SetAdmissionInflight(lane string, n int) {
	lane = bounded(lane, "api", "upload")
	if lane == "other" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inflight[lane] = n
}

// LogOperation never emits arguments, URLs, errors or business identifiers.
// It also attaches a span to the request's pending ring record, if the
// context carries a request ID with one (RecordSpan drops the span
// silently for background work with no HTTP request).
func LogOperation(ctx context.Context, component, operation, outcome string, d time.Duration) {
	Default.Observe(component, operation, outcome, d)
	Default.RecordSpan(ctx, component, operation, outcome, d)
	log.Printf("operation request_id=%s component=%s operation=%s outcome=%s duration_ms=%d", RequestID(ctx), bounded(component, "redis", "provider", "sql", "worker", "backup"), bounded(operation, "request", "tapin_request", "ippanel_request", "verify", "refund", "unpaid-order-expiry", "ad-cleanup", "qc-response-deadline", "supplier-sla", "delivery-lock-releaser", "monthly-settlement", "backup", "query"), bounded(outcome, "success", "error", "timeout", "canceled", "unknown", "overloaded"), d.Milliseconds())
}

type response struct {
	http.ResponseWriter
	status int
	n      int64
}

func (w *response) WriteHeader(s int) {
	if s >= 100 && s < 200 {
		w.ResponseWriter.WriteHeader(s)
		return
	}
	if w.status == 0 {
		w.status = s
		w.ResponseWriter.WriteHeader(s)
	}
}
func (w *response) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, e := w.ResponseWriter.Write(b)
	w.n += int64(n)
	return n, e
}
func (w *response) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *response) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	http.NewResponseController(w.ResponseWriter).Flush()
}

type body struct {
	io.ReadCloser
	n int64
}

func (b *body) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	b.n += int64(n)
	return n, e
}
func (m *Registry) HTTP(mux *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		r = r.WithContext(NewContext(r.Context()))
		id := RequestID(r.Context())
		w.Header().Set("X-Request-ID", id)
		_, route := mux.Handler(r)
		if route == "" {
			route = "unmatched"
		} else if strings.HasSuffix(route, "/") {
			route += "*"
		}
		class := "other"
		if strings.HasPrefix(route, "/api/") {
			class = "api"
		} else if route == "/internal/domains/allow" {
			class = "domain_allow"
		} else if strings.HasPrefix(route, "/static/") {
			class = "asset"
		}
		// D-12: read vs mutation classes, matching the agreed SLO
		// (read p95 300 ms / p99 1,000 ms; mutation p95 750 ms /
		// p99 2,000 ms). GET/HEAD/OPTIONS are reads; every other
		// method is a mutation. The label is bounded to two values.
		opclass := "mutation"
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
			opclass = "read"
		}
		method := bounded(r.Method, "GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD")
		// G-03: open the pending ring record; the end-of-request defer
		// below fills status/outcome/duration and moves it into the ring.
		m.beginRequest(id, method, route, class, opclass, start)
		rw := &response{ResponseWriter: w}
		var rb *body
		if r.Body != nil {
			rb = &body{ReadCloser: r.Body}
			r.Body = rb
		}
		m.mu.Lock()
		m.active++
		m.mu.Unlock()
		defer func() {
			p := recover()
			status := rw.status
			if p != nil {
				status = 500
			}
			if status == 0 {
				status = 200
			}
			outcome := "completed"
			if status == http.StatusGatewayTimeout || r.Context().Err() == context.DeadlineExceeded {
				outcome = "timeout"
			} else if r.Context().Err() == context.Canceled {
				outcome = "canceled"
			}
			dur := time.Since(start)
			// G-03: move the pending record into the ring (all
			// completed requests, not only slow/error ones).
			m.completeRequest(id, status, outcome, dur)
			n := int64(0)
			if rb != nil {
				n = rb.n
			}
			key := fmt.Sprintf(`class=%q,method=%q,route=%q,status=%q,outcome=%q,opclass=%q`, class, method, route, fmt.Sprint(status), outcome, opclass)
			m.mu.Lock()
			m.active--
			add(m.http, key, dur.Seconds(), n, rw.n)
			m.mu.Unlock()
			if status >= 500 || dur >= 300*time.Millisecond {
				log.Printf("http request_id=%s class=%s route=%s status=%d duration_ms=%d", id, class, route, status, dur.Milliseconds())
			}
			if p != nil {
				panic(p)
			}
		}()
		next.ServeHTTP(rw, r)
	})
}
func emit(w io.Writer, name, key string, s *sample) {
	fmt.Fprintf(w, "%s_count{%s} %d\n%s_sum{%s} %g\n", name, key, s.Count, name, key, s.Sum)
	for i, b := range buckets {
		fmt.Fprintf(w, "%s_bucket{%s,le=%q} %d\n", name, key, fmt.Sprint(b), s.Buckets[i])
	}
	fmt.Fprintf(w, "%s_bucket{%s,le=\"+Inf\"} %d\n", name, key, s.Count)
}

// ServeHTTP renders Prometheus text format 0.0.4 from the hand-rolled
// registry (there is no Prometheus client library in go.mod).
// NOTE (D-03/D-14, OBS-01): this emitter does NOT attach exemplars:
// exemplar syntax exists only in the OpenMetrics text format, and this
// endpoint advertises version 0.0.4. The per-request correlation record
// is the in-memory ring (ring.go: last 1000 completed requests with
// spans), served by GET /api/v1/admin/observability/requests — the
// threshold-gated `http request_id=...` log line (status >= 500 or
// duration >= 300 ms) remains as the stdout backup. Adding exemplars
// means switching this endpoint to OpenMetrics content type and storing
// one exemplar per histogram series; that is a follow-up, not done here,
// because an unvalidated exposition change could break the scrape.
func (m *Registry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	m.mu.Lock()
	defer m.mu.Unlock()
	fmt.Fprintf(w, "sellersho_http_active %d\n", m.active)
	for _, group := range []struct {
		name   string
		values map[string]*sample
	}{{"sellersho_http_duration_seconds", m.http}, {"sellersho_operation_duration_seconds", m.ops}} {
		keys := make([]string, 0, len(group.values))
		for k := range group.values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s := group.values[k]
			emit(w, group.name, k, s)
			if group.name == "sellersho_http_duration_seconds" {
				fmt.Fprintf(w, "sellersho_http_request_bytes_total{%s} %d\nsellersho_http_response_bytes_total{%s} %d\n", k, s.RequestBytes, k, s.ResponseBytes)
			}
		}
	}
	for name, value := range m.gauges {
		fmt.Fprintf(w, "sellersho_%s %g\n", name, value)
	}
	// D-06: admission rejects (counter by reason) and admission-slot
	// occupancy (gauge by lane). Sorted for stable output.
	rejectReasons := make([]string, 0, len(m.rejects))
	for reason := range m.rejects {
		rejectReasons = append(rejectReasons, reason)
	}
	sort.Strings(rejectReasons)
	for _, reason := range rejectReasons {
		fmt.Fprintf(w, "sellersho_http_rejects_total{reason=%q} %d\n", reason, m.rejects[reason])
	}
	inflightLanes := make([]string, 0, len(m.inflight))
	for lane := range m.inflight {
		inflightLanes = append(inflightLanes, lane)
	}
	sort.Strings(inflightLanes)
	for _, lane := range inflightLanes {
		fmt.Fprintf(w, "sellersho_admission_inflight{lane=%q} %d\n", lane, m.inflight[lane])
	}
	for name, value := range processMetrics() {
		fmt.Fprintf(w, "sellersho_%s %g\n", name, value)
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	fmt.Fprintf(w, "sellersho_go_goroutines %d\nsellersho_go_heap_bytes %d\nsellersho_go_gc_pause_seconds_total %g\n", runtime.NumGoroutine(), mem.HeapAlloc, float64(mem.PauseTotalNs)/1e9)
	if m.db != nil {
		s := m.db.Stats()
		fmt.Fprintf(w, "sellersho_db_open %d\nsellersho_db_in_use %d\nsellersho_db_idle %d\nsellersho_db_wait_total %d\nsellersho_db_wait_seconds_total %g\n", s.OpenConnections, s.InUse, s.Idle, s.WaitCount, s.WaitDuration.Seconds())
	}
}
func ValidateListenAddress(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid metrics address")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A name (localhost, example.com, ...) is never an acceptable
		// metrics bind: names resolve through DNS / /etc/hosts, so a
		// "loopback" name is not a guarantee about the bound address.
		return fmt.Errorf("metrics listener must use a literal IP, not a hostname")
	}
	if ip.IsLoopback() || ip.Equal(net.IPv4zero) {
		return nil
	}
	return fmt.Errorf("metrics listener must bind loopback or all interfaces (0.0.0.0)")
}

// SECURITY-BOUNDARY MOVE (OBS-01 deploy-fix, 2026-11-18; see
// docs/operations/observability.md): the old contract allowed ONLY a
// literal loopback bind inside the container. That made the listener
// unreachable at the container's eth0 IP, and Docker port-publishing
// forwards exactly there — so a 127.0.0.1:9090:9090 publish could never
// reach it, and the backend Prometheus target stayed down. The boundary
// therefore moved from "in-container loopback" to "host loopback-only
// publish" (docker-compose.metrics-ports.yml publishes 127.0.0.1:9090
// only; the port is never public and never proxied). A 0.0.0.0-bound
// in-container listener is additionally reachable from any container
// on the platform docker network — acceptable for this read-only,
// value-free counters endpoint. The IPv6 unspecified address ("::") is
// deliberately NOT allowed: no IPv6 listener is configured anywhere in
// this stack, so the re-scope stays minimal. A specific non-loopback IP
// (e.g. a container IP) is still rejected: binds are loopback,
// all-interfaces, or nothing.
// Start serves /metrics on its own socket; the public mux never registers
// metrics or pprof. A local agent may scrape or forward over an
// authenticated tunnel; in production the scrape arrives via the
// loopback-only host publish (see above).
func (m *Registry) Start(addr string) (*http.Server, error) {
	if err := ValidateListenAddress(addr); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", m)
	s := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	go s.Serve(ln)
	return s, nil
}
