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
	// sqlErrors counts SQL failures by value-free SQLSTATE code
	// (PERF-03 G7): e.g. "40001" for a serialization failure. The
	// code alone can carry no parameter values.
	sqlErrors map[string]int64
	// domainAllow counts allow decisions by bounded outcome (EDGE-01
	// step 1): "allowed" (204), "denied" (403), "error" (503 / repo
	// unavailable). allowDuration is the allow-handler latency
	// histogram (handleDomainPermission only, so it reconciles with
	// the domain_allow HTTP class counter); allowDBQueries counts
	// AllowedDomain DB queries, one per allow check that reaches the
	// repository — from handleDomainPermission AND handleDomainStore,
	// so the gap between this counter and the allow total is exactly
	// the domain-store lookups.
	allow          map[string]int64
	allowDuration  *sample
	allowDBQueries int64
	// FIN-013 finance signals: event counters by bounded label only.
	// No account/order/user ID and no payment field (token, SHABA,
	// reference, amount) ever enters a series — the labels below are
	// the complete closed sets. financeInvariant counts detected
	// conservation violations by check; financeReservationRejected
	// counts rejections that protected availability by reason;
	// financeReconDiff counts non-zero differences found by a
	// reconciliation run by oracle check; jobRetries counts bounded
	// retries per job (the poller already publishes per-job backlog /
	// oldest-age; this is the missing retry dimension).
	financeInvariant           map[string]int64
	financeReservationRejected map[string]int64
	financeReconDiff           map[string]int64
	jobRetries                 map[string]int64
	// EDGE-01 step 3: allow-cache counters. allowCacheHits/Misses
	// count AllowedDomain cache outcomes (a miss is a load that
	// reached the DB); allowCacheEvictions counts LRU evictions at
	// the entry cap. All three are value-free (no domain ever enters
	// a series). hits + misses reconcile against the allow total
	// minus the primary short-circuit (which never reaches the repo).
	allowCacheHits      int64
	allowCacheMisses    int64
	allowCacheEvictions int64
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
	// dbWait is the pool-wait histogram (PERF-03 G5): one sample per
	// sampling interval that saw new waits, equal to that interval's
	// MEAN wait. database/sql exposes only cumulative wait counters,
	// so per-wait observations do not exist; see sampleDBWaitLocked.
	// dbWaitPrev/dbWaitSeen are the previous Stats snapshot.
	dbWait     *sample
	dbWaitPrev sql.DBStats
	dbWaitSeen bool
	dbWaitStop chan struct{}
}

func New() *Registry {
	return &Registry{http: map[string]*sample{}, ops: map[string]*sample{}, gauges: map[string]float64{}, pending: map[string]bool{}, rejects: map[string]int64{}, inflight: map[string]int{}, ringInflight: map[string]*requestRecord{}, sqlErrors: map[string]int64{}, allow: map[string]int64{}, financeInvariant: map[string]int64{}, financeReservationRejected: map[string]int64{}, financeReconDiff: map[string]int64{}, jobRetries: map[string]int64{}}
}
func (m *Registry) SetDB(db *sql.DB) { m.mu.Lock(); defer m.mu.Unlock(); m.db = db }

// StartDBWaitSampling starts the pool-wait sampler (PERF-03 G5): every
// interval it folds db.Stats() into the wait histogram (see
// sampleDBWaitLocked). A nil db or non-positive interval is a no-op;
// calling it twice is a no-op. The sampler runs until
// StopDBWaitSampling.
func (m *Registry) StartDBWaitSampling(db *sql.DB, interval time.Duration) {
	if db == nil || interval <= 0 {
		return
	}
	m.mu.Lock()
	if m.dbWaitStop != nil {
		m.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	m.dbWaitStop = stop
	m.mu.Unlock()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				m.mu.Lock()
				m.sampleDBWaitLocked(db.Stats())
				m.mu.Unlock()
			}
		}
	}()
}

// StopDBWaitSampling stops the sampler started by StartDBWaitSampling.
// Safe to call when no sampler runs.
func (m *Registry) StopDBWaitSampling() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dbWaitStop != nil {
		close(m.dbWaitStop)
		m.dbWaitStop = nil
	}
}

// sampleDBWaitLocked folds one sql.DBStats snapshot into the wait
// histogram; the caller holds m.mu. database/sql exposes only
// cumulative wait counters (WaitCount, WaitDuration), so each tick
// with new waits contributes ONE sample: the MEAN wait of that
// interval (duration delta / count delta). The histogram is therefore
// a histogram of per-interval mean waits, not of individual waits —
// its p95 answers "in 95% of sampled intervals the mean wait was
// below X", which is the measurable form of the spec's
// pool-wait-p95<25ms gate. It underestimates the true per-wait tail;
// the largest interval mean is exposed alongside as a bound
// (sellersho_db_wait_max_mean_seconds), so a p95 that looks fine next
// to a large max mean is not mistaken for a clean bill of health.
func (m *Registry) sampleDBWaitLocked(s sql.DBStats) {
	if !m.dbWaitSeen {
		m.dbWaitPrev = s
		m.dbWaitSeen = true
		return
	}
	dCount := s.WaitCount - m.dbWaitPrev.WaitCount
	dDur := s.WaitDuration - m.dbWaitPrev.WaitDuration
	m.dbWaitPrev = s
	if dCount <= 0 || dDur <= 0 {
		return
	}
	mean := dDur.Seconds() / float64(dCount)
	if m.dbWait == nil {
		m.dbWait = &sample{Buckets: make([]int64, len(buckets))}
	}
	m.dbWait.Count++
	m.dbWait.Sum += mean
	for i, b := range buckets {
		if mean <= b {
			m.dbWait.Buckets[i]++
		}
	}
	m.gauges["db_wait_last_mean_seconds"] = mean
	if max, ok := m.gauges["db_wait_max_mean_seconds"]; !ok || mean > max {
		m.gauges["db_wait_max_mean_seconds"] = mean
	}
}
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
	// Worker operations are the seven job IDs (D-05 + FIN-013): one
	// distinct operation per job. There is no job named
	// "reconciliation" and no shared label; an unknown job ID
	// collapses to "other". The FIN-013 finance query operations
	// (treasury_cash_position, store_balance, settlement_candidates,
	// refund_work_queue) are per-query latency series for the §2.6
	// hot paths; they carry no account or order identifiers.
	operation = bounded(operation, "get", "set", "setnx", "del", "eval", "evalsha", "ping", "pipeline", "request", "tapin_request", "ippanel_request", "verify", "refund", "unpaid-order-expiry", "ad-cleanup", "qc-response-deadline", "supplier-sla", "delivery-lock-releaser", "monthly-settlement", "refund-reconciliation", "backup", "query", "hold", "release", "has_hold", "otp_reserve", "otp_verify", "treasury_cash_position", "store_balance", "settlement_candidates", "refund_work_queue")
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

// RecordFinanceInvariantFailure counts one detected finance
// conservation violation under a bounded check name (FIN-013 §5):
// "balance_conservation" (ledger vs orders/payouts/obligations),
// "cumulative_refund" (refunds would exceed captured),
// "reservation_availability" (a reservation would exceed signed
// availability), "sum_parts_parent" (parts do not sum to the parent),
// "money_validation" (a money value failed FIN-012 validation).
// Anything else collapses to "other" and is still counted, never a
// new series. Callers record the detection, never the amounts.
func (m *Registry) RecordFinanceInvariantFailure(check string) {
	check = bounded(check, "balance_conservation", "cumulative_refund", "reservation_availability", "sum_parts_parent", "money_validation")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.financeInvariant[check]++
}

// FinanceInvariantFailuresTotal returns the cumulative invariant
// failures across all checks (the poller's new-failure delta input).
func (m *Registry) FinanceInvariantFailuresTotal() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	for _, n := range m.financeInvariant {
		total += n
	}
	return total
}

// RecordFinanceReservationRejected counts one availability-protecting
// rejection under a bounded reason (FIN-013 §5): "exceeds_availability"
// (payout/reservation above signed availability), "duplicate_cycle"
// (a second reservation for the same period/store), "coupon_exhausted"
// (last-use coupon already taken). Unknown reasons are dropped, never
// a new series. Legitimate rejections are expected traffic — the alert
// fires on a rate spike / invariant context, never on one rejection.
func (m *Registry) RecordFinanceReservationRejected(reason string) {
	reason = bounded(reason, "exceeds_availability", "duplicate_cycle", "coupon_exhausted")
	if reason == "other" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.financeReservationRejected[reason]++
}

// RecordFinanceReconciliationDiff counts one non-zero difference found
// by a reconciliation run, by oracle check (FIN-013 §6.5): the ten
// check names are the oracle's. Unknown kinds are dropped.
func (m *Registry) RecordFinanceReconciliationDiff(kind string) {
	kind = bounded(kind, "orders_payments", "payments_ledger", "cancellations_obligations", "obligations_outcomes", "settlement", "coupons", "referral", "storage", "treasury", "statements")
	if kind == "other" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.financeReconDiff[kind]++
}

// FinanceReconciliationDiffsTotal returns the cumulative
// reconciliation differences across all oracle checks.
func (m *Registry) FinanceReconciliationDiffsTotal() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	for _, n := range m.financeReconDiff {
		total += n
	}
	return total
}

// RecordJobRetry counts one bounded retry of a scheduled job, by job ID
// (FIN-013 §5: the retry counter the six-job gauges never had). The
// seven job IDs are the closed set; anything else is dropped.
func (m *Registry) RecordJobRetry(job string) {
	job = bounded(job, "unpaid-order-expiry", "ad-cleanup", "qc-response-deadline", "supplier-sla", "delivery-lock-releaser", "monthly-settlement", "refund-reconciliation")
	if job == "other" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobRetries[job]++
}

// RecordSQLError counts one SQL failure under its value-free
// SQLSTATE code (PERF-03 G7). An empty code is dropped, never a new
// series.
func (m *Registry) RecordSQLError(code string) {
	if code == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sqlErrors[code]++
}

// RecordDomainAllow counts one allow decision under a bounded outcome
// (EDGE-01 step 1): "allowed", "denied", "error". Anything else is
// dropped, never a new series. The outcome is the handler's decision,
// never a domain value — no tenant data enters the series.
func (m *Registry) RecordDomainAllow(outcome string) {
	outcome = bounded(outcome, "allowed", "denied", "error")
	if outcome == "other" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allow[outcome]++
}

// ObserveDomainAllow records one allow-handler latency sample
// (EDGE-01 step 1) into the allow-duration histogram, using the same
// buckets as the HTTP histograms.
func (m *Registry) ObserveDomainAllow(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.allowDuration == nil {
		m.allowDuration = &sample{Buckets: make([]int64, len(buckets))}
	}
	s := m.allowDuration
	secs := d.Seconds()
	s.Count++
	s.Sum += secs
	for i, b := range buckets {
		if secs <= b {
			s.Buckets[i]++
		}
	}
}

// RecordDomainAllowDBQuery counts one AllowedDomain DB query (EDGE-01
// step 1). It is called from the repository, the single place every
// allow check funnels through, so it attributes the query to the allow
// path regardless of which handler triggered it.
func (m *Registry) RecordDomainAllowDBQuery() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowDBQueries++
}

// RecordDomainAllowCacheHit counts one AllowedDomain call answered
// from the allow cache (EDGE-01 step 3), without a DB query.
func (m *Registry) RecordDomainAllowCacheHit() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowCacheHits++
}

// RecordDomainAllowCacheMiss counts one AllowedDomain call that missed
// the allow cache and loaded from the DB (EDGE-01 step 3) — whether
// the load succeeded, denied, or failed. hits + misses therefore
// reconcile against the allow total minus the primary short-circuit
// (which never reaches the repo).
func (m *Registry) RecordDomainAllowCacheMiss() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowCacheMisses++
}

// RecordDomainAllowCacheEviction counts one LRU eviction from the
// allow cache (EDGE-01 step 3), wired via the cache's OnEvict hook.
func (m *Registry) RecordDomainAllowCacheEviction() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowCacheEvictions++
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
	log.Printf("operation request_id=%s component=%s operation=%s outcome=%s duration_ms=%d", RequestID(ctx), bounded(component, "redis", "provider", "sql", "worker", "backup"), bounded(operation, "request", "tapin_request", "ippanel_request", "verify", "refund", "unpaid-order-expiry", "ad-cleanup", "qc-response-deadline", "supplier-sla", "delivery-lock-releaser", "monthly-settlement", "refund-reconciliation", "backup", "query"), bounded(outcome, "success", "error", "timeout", "canceled", "unknown", "overloaded"), d.Milliseconds())
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
	// FIN-013 finance signals: every known label is always emitted
	// (zero included) so the series exist from boot and the alert
	// rules can use increase() without a missing-series gap.
	for _, check := range []string{"balance_conservation", "cumulative_refund", "reservation_availability", "sum_parts_parent", "money_validation", "other"} {
		fmt.Fprintf(w, "sellersho_finance_invariant_failures_total{check=%q} %d\n", check, m.financeInvariant[check])
	}
	for _, reason := range []string{"exceeds_availability", "duplicate_cycle", "coupon_exhausted"} {
		fmt.Fprintf(w, "sellersho_finance_reservation_rejected_total{reason=%q} %d\n", reason, m.financeReservationRejected[reason])
	}
	for _, kind := range []string{"orders_payments", "payments_ledger", "cancellations_obligations", "obligations_outcomes", "settlement", "coupons", "referral", "storage", "treasury", "statements"} {
		fmt.Fprintf(w, "sellersho_finance_reconciliation_differences_total{kind=%q} %d\n", kind, m.financeReconDiff[kind])
	}
	for _, job := range []string{"unpaid-order-expiry", "ad-cleanup", "qc-response-deadline", "supplier-sla", "delivery-lock-releaser", "monthly-settlement", "refund-reconciliation"} {
		fmt.Fprintf(w, "sellersho_job_retries_total{job=%q} %d\n", job, m.jobRetries[job])
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
	// PERF-03 G7: SQL failures by SQLSTATE code (sorted for stable output).
	sqlCodes := make([]string, 0, len(m.sqlErrors))
	for code := range m.sqlErrors {
		sqlCodes = append(sqlCodes, code)
	}
	sort.Strings(sqlCodes)
	for _, code := range sqlCodes {
		fmt.Fprintf(w, "sellersho_sql_errors_total{code=%q} %d\n", code, m.sqlErrors[code])
	}
	// EDGE-01 step 1: allow decisions. The outcome counter is emitted
	// for all three outcomes even at zero, so the series always exists
	// for reconciliation against the domain_allow HTTP class counter
	// (sellersho_http_duration_seconds_count{class="domain_allow",...}).
	// The DB-query counter is likewise always emitted.
	for _, outcome := range []string{"allowed", "denied", "error"} {
		fmt.Fprintf(w, "sellersho_domain_allow_total{outcome=%q} %d\n", outcome, m.allow[outcome])
	}
	fmt.Fprintf(w, "sellersho_domain_allow_db_queries_total %d\n", m.allowDBQueries)
	// EDGE-01 step 3: allow-cache outcomes, always emitted (like the
	// DB-query counter) so the series exist from boot.
	fmt.Fprintf(w, "sellersho_domain_allow_cache_hits_total %d\n", m.allowCacheHits)
	fmt.Fprintf(w, "sellersho_domain_allow_cache_misses_total %d\n", m.allowCacheMisses)
	fmt.Fprintf(w, "sellersho_domain_allow_cache_evictions_total %d\n", m.allowCacheEvictions)
	// Allow-handler latency histogram (no labels; same bucket set as
	// the HTTP histograms). Emitted only once at least one allow call
	// has been observed, like the pool-wait histogram below.
	if m.allowDuration != nil && m.allowDuration.Count > 0 {
		s := m.allowDuration
		fmt.Fprintf(w, "sellersho_domain_allow_duration_seconds_count %d\nsellersho_domain_allow_duration_seconds_sum %g\n", s.Count, s.Sum)
		for i, b := range buckets {
			fmt.Fprintf(w, "sellersho_domain_allow_duration_seconds_bucket{le=%q} %d\n", fmt.Sprint(b), s.Buckets[i])
		}
		fmt.Fprintf(w, "sellersho_domain_allow_duration_seconds_bucket{le=\"+Inf\"} %d\n", s.Count)
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
	// PERF-03 G5: pool-wait histogram (per-interval mean waits; see
	// sampleDBWaitLocked). Emitted only once at least one interval
	// with waits has been sampled.
	if m.dbWait != nil && m.dbWait.Count > 0 {
		s := m.dbWait
		fmt.Fprintf(w, "sellersho_db_wait_seconds_count %d\nsellersho_db_wait_seconds_sum %g\n", s.Count, s.Sum)
		for i, b := range buckets {
			fmt.Fprintf(w, "sellersho_db_wait_seconds_bucket{le=%q} %d\n", fmt.Sprint(b), s.Buckets[i])
		}
		fmt.Fprintf(w, "sellersho_db_wait_seconds_bucket{le=\"+Inf\"} %d\n", s.Count)
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
