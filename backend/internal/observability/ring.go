package observability

import (
	"context"
	"time"
)

// G-03 (OBS-01): bounded in-memory ring of completed HTTP requests, so a
// slow request can be explained by its correlation ID without a log
// store. The ring lives in the Registry and is served by
// GET /api/v1/admin/observability/requests?request_id=<id>
// (backend/cmd/server, admin auth only).
//
// REDACTION (G-10): a record stores ONLY request_id, method, the bounded
// ServeMux route pattern, class, opclass, status, outcome, duration and
// started_at, plus per-span component/operation/outcome/duration. It
// NEVER stores headers, bodies, tokens, query strings, URLs, SQL text,
// command arguments, or any other PII/business identifier — the same
// value-free rule as the metrics registry. Span fields pass through the
// same bounded() allowlists as Observe(), so an unknown value collapses
// to "other" and can never grow the record without bound.
//
// CONCURRENCY: the ring and the in-flight map live behind ringMu, a
// mutex SEPARATE from the metrics mutex (mu). A lookup therefore never
// blocks a /metrics scrape and a scrape never blocks a lookup; the two
// locks are never held at the same time. TestLookupNeverBlocksScrape
// proves both directions.
const (
	// requestRingCapacity is the number of completed requests kept.
	// Oldest evicted on insert; see docs/operations/correlation-lookup.md
	// for the traffic-dependent time retention this implies.
	requestRingCapacity = 1000
	// maxSpansPerRequest caps spans per record; beyond it spans are
	// dropped and counted, so one pathological request cannot bloat
	// the ring. Worst-case memory is ~1000 * (record ~300 B +
	// 64 spans * ~120 B) ~= 8 MB.
	maxSpansPerRequest = 64
	// inFlightMaxAge evicts stale in-flight entries (a request whose
	// end-of-request defer never runs, e.g. a panic path) on insert.
	// 10 minutes is far above the 8 s API budget plus margins.
	inFlightMaxAge = 10 * time.Minute
)

// requestSpan is one operation span attached to a request: a SQL query,
// a provider call, a Redis command, or a worker run. Values are
// bounded allowlist labels only (see the redaction note above).
type requestSpan struct {
	Component  string `json:"component"`
	Operation  string `json:"operation"`
	Outcome    string `json:"outcome"`
	DurationMs int64  `json:"duration_ms"`
}

// requestRecord is one completed HTTP request as returned by the
// lookup endpoint. StartedAt is the server-local start time.
type requestRecord struct {
	RequestID    string        `json:"request_id"`
	Method       string        `json:"method"`
	Route        string        `json:"route"`
	Class        string        `json:"class"`
	Opclass      string        `json:"opclass"`
	Status       int           `json:"status"`
	Outcome      string        `json:"outcome"`
	DurationMs   int64         `json:"duration_ms"`
	StartedAt    time.Time     `json:"started_at"`
	Spans        []requestSpan `json:"spans"`
	DroppedSpans int           `json:"dropped_spans"`
}

// beginRequest opens a pending record for a request that has just
// started. Called by Registry.HTTP; the record moves into the ring
// when completeRequest runs in the end-of-request defer.
func (m *Registry) beginRequest(id, method, route, class, opclass string, startedAt time.Time) {
	m.ringMu.Lock()
	defer m.ringMu.Unlock()
	now := time.Now()
	for oldID, rec := range m.ringInflight {
		if now.Sub(rec.StartedAt) > inFlightMaxAge {
			delete(m.ringInflight, oldID)
		}
	}
	m.ringInflight[id] = &requestRecord{
		RequestID: id,
		Method:    method,
		Route:     route,
		Class:     class,
		Opclass:   opclass,
		StartedAt: startedAt,
		Spans:     []requestSpan{},
	}
}

// RecordSpan appends one operation span to the pending record for the
// request ID carried in ctx. Calls with no pending record (background
// jobs, or a request that already completed) are dropped silently —
// G-03 is about HTTP requests. Component/operation/outcome are bounded
// with the same allowlists as Observe, so span values can never grow
// the record with arbitrary strings.
func (m *Registry) RecordSpan(ctx context.Context, component, operation, outcome string, d time.Duration) {
	id := RequestID(ctx)
	if id == "" {
		return
	}
	m.ringMu.Lock()
	defer m.ringMu.Unlock()
	rec := m.ringInflight[id]
	if rec == nil {
		return
	}
	if len(rec.Spans) >= maxSpansPerRequest {
		rec.DroppedSpans++
		return
	}
	rec.Spans = append(rec.Spans, requestSpan{
		Component:  bounded(component, "redis", "provider", "sql", "worker", "backup"),
		Operation:  bounded(operation, "get", "set", "setnx", "del", "eval", "evalsha", "ping", "pipeline", "request", "tapin_request", "ippanel_request", "verify", "refund", "unpaid-order-expiry", "ad-cleanup", "qc-response-deadline", "supplier-sla", "delivery-lock-releaser", "monthly-settlement", "backup", "query"),
		Outcome:    bounded(outcome, "success", "error", "timeout", "canceled", "unknown", "overloaded"),
		DurationMs: d.Milliseconds(),
	})
}

// completeRequest fills in the outcome of a pending record and moves it
// into the ring, evicting the oldest completed record past capacity.
// A request with no pending record (never begun) is ignored.
func (m *Registry) completeRequest(id string, status int, outcome string, d time.Duration) {
	m.ringMu.Lock()
	defer m.ringMu.Unlock()
	rec := m.ringInflight[id]
	if rec == nil {
		return
	}
	delete(m.ringInflight, id)
	rec.Status = status
	rec.Outcome = outcome
	rec.DurationMs = d.Milliseconds()
	if m.ringLen < requestRingCapacity {
		m.ringBuf[(m.ringHead+m.ringLen)%requestRingCapacity] = rec
		m.ringLen++
	} else {
		m.ringBuf[m.ringHead] = rec
		m.ringHead = (m.ringHead + 1) % requestRingCapacity
	}
}

// Lookup returns a copy of the completed record for id, or false when
// the ID is not in the ring (evicted, never seen, or still in flight).
// The copy means callers can never mutate ring state. Lookup takes
// only ringMu, never the metrics mutex, so it cannot block a scrape.
func (m *Registry) Lookup(id string) (requestRecord, bool) {
	m.ringMu.Lock()
	defer m.ringMu.Unlock()
	for i := 0; i < m.ringLen; i++ {
		rec := m.ringBuf[(m.ringHead+i)%requestRingCapacity]
		if rec != nil && rec.RequestID == id {
			out := *rec
			// Copy from an empty non-nil slice so a spanless record
			// still encodes as "spans": [] rather than null.
			out.Spans = append([]requestSpan{}, rec.Spans...)
			return out, true
		}
	}
	return requestRecord{}, false
}
