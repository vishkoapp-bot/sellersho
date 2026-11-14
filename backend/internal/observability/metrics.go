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
}

func New() *Registry {
	return &Registry{http: map[string]*sample{}, ops: map[string]*sample{}, gauges: map[string]float64{}, pending: map[string]bool{}}
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
	operation = bounded(operation, "get", "set", "setnx", "del", "eval", "evalsha", "ping", "pipeline", "request", "verify", "refund", "expiry", "reconciliation", "settlement", "backup", "query")
	outcome = bounded(outcome, "success", "error", "timeout", "canceled", "unknown")
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

// LogOperation never emits arguments, URLs, errors or business identifiers.
func LogOperation(ctx context.Context, component, operation, outcome string, d time.Duration) {
	Default.Observe(component, operation, outcome, d)
	log.Printf("operation request_id=%s component=%s operation=%s outcome=%s duration_ms=%d", RequestID(ctx), bounded(component, "redis", "provider", "sql", "worker", "backup"), bounded(operation, "request", "verify", "refund", "expiry", "reconciliation", "settlement", "backup", "query"), bounded(outcome, "success", "error", "timeout", "canceled", "unknown"), d.Milliseconds())
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
		method := bounded(r.Method, "GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD")
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
			if r.Context().Err() == context.Canceled {
				outcome = "canceled"
			} else if r.Context().Err() == context.DeadlineExceeded {
				outcome = "timeout"
			}
			n := int64(0)
			if rb != nil {
				n = rb.n
			}
			key := fmt.Sprintf(`class=%q,method=%q,route=%q,status=%q,outcome=%q`, class, method, route, fmt.Sprint(status), outcome)
			m.mu.Lock()
			m.active--
			add(m.http, key, time.Since(start).Seconds(), n, rw.n)
			m.mu.Unlock()
			if status >= 500 || time.Since(start) >= 300*time.Millisecond {
				log.Printf("http request_id=%s class=%s route=%s status=%d duration_ms=%d", id, class, route, status, time.Since(start).Milliseconds())
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
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("metrics listener must use literal loopback IP")
	}
	return nil
}

// Start uses a separate loopback socket; the public mux never registers metrics
// or pprof. A local agent may scrape or forward over an authenticated tunnel.
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
