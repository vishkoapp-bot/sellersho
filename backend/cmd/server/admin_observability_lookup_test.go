package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/observability"
)

// OBS-01 G-03 gate drill, end to end on a disposable DB with a
// synthetic test admin (never prod, never a real admin's inbox):
// drive one slow (>300 ms) request through the real HTTP middleware,
// copy its X-Request-ID from the response, and look it up through the
// admin endpoint. The handler attaches a SQL span via LogOperation, the
// way the SQL driver does in production.
//
// LogOperation is Default-bound by design (production wires
// observability.Default everywhere), so the middleware, the handler
// span and the lookup all use observability.Default here; the request
// ID is a fresh 128-bit random value, so no other test can collide
// with it.
func TestAdminRequestLookupEndToEnd(t *testing.T) {
	f := supplierNotificationsSetup(t)
	admin := f.seedRoleUser(t, "09999000103", "admin")
	customer := f.seedRoleUser(t, "09999000104", "customer")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orders/slow", func(w http.ResponseWriter, r *http.Request) {
		observability.LogOperation(r.Context(), "sql", "query", "success", 12*time.Millisecond)
		time.Sleep(350 * time.Millisecond)
		w.WriteHeader(200)
	})
	h := observability.Default.HTTP(mux, mux)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/orders/slow", nil))
	if recorder.Code != 200 {
		t.Fatalf("slow request status %d", recorder.Code)
	}
	id := recorder.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("slow request carried no X-Request-ID")
	}

	callLookup := func(token string) (int, string) {
		return f.callRole(t, "GET", "/api/v1/admin/observability/requests?request_id="+id, token, "", []string{"admin"},
			func(w http.ResponseWriter, r *http.Request) { handleAdminRequestLookup(w, r, observability.Default) })
	}
	adminToken := f.token(t, admin, "09999000103", "admin")

	code, body := callLookup(adminToken)
	if code != 200 {
		t.Fatalf("admin lookup status %d, body %s", code, body)
	}
	var rec struct {
		RequestID  string `json:"request_id"`
		Method     string `json:"method"`
		Route      string `json:"route"`
		Class      string `json:"class"`
		Opclass    string `json:"opclass"`
		Status     int    `json:"status"`
		Outcome    string `json:"outcome"`
		DurationMs int64  `json:"duration_ms"`
		StartedAt  string `json:"started_at"`
		Spans      []struct {
			Component  string `json:"component"`
			Operation  string `json:"operation"`
			Outcome    string `json:"outcome"`
			DurationMs int64  `json:"duration_ms"`
		} `json:"spans"`
		DroppedSpans int `json:"dropped_spans"`
	}
	if err := json.Unmarshal([]byte(body), &rec); err != nil {
		t.Fatalf("decode lookup response: %v (body %s)", err, body)
	}
	if rec.RequestID != id || rec.Method != "GET" || rec.Route != "/api/v1/orders/slow" || rec.Class != "api" || rec.Opclass != "read" || rec.Status != 200 || rec.Outcome != "completed" {
		t.Fatalf("lookup breakdown wrong: %+v", rec)
	}
	if rec.DurationMs < 300 {
		t.Fatalf("slow request duration_ms=%d, want >= 300", rec.DurationMs)
	}
	if rec.StartedAt == "" || rec.DroppedSpans != 0 {
		t.Fatalf("lookup breakdown incomplete: %+v", rec)
	}
	if len(rec.Spans) != 1 || rec.Spans[0].Component != "sql" || rec.Spans[0].Operation != "query" || rec.Spans[0].Outcome != "success" || rec.Spans[0].DurationMs != 12 {
		t.Fatalf("lookup spans wrong: %+v", rec.Spans)
	}
	// No secret or header value may appear in the breakdown.
	if strings.Contains(body, "09999000103") {
		t.Fatalf("identifier leaked into lookup response: %s", body)
	}

	// Unknown ID: honest 404, never a fabricated breakdown.
	code, body = f.callRole(t, "GET", "/api/v1/admin/observability/requests?request_id=does-not-exist", adminToken, "", []string{"admin"},
		func(w http.ResponseWriter, r *http.Request) { handleAdminRequestLookup(w, r, observability.Default) })
	if code != 404 || !strings.Contains(body, "last 1000") {
		t.Fatalf("unknown ID: status %d, body %s, want 404 naming the ring bound", code, body)
	}
	// Missing request_id: 400.
	code, _ = f.callRole(t, "GET", "/api/v1/admin/observability/requests", adminToken, "", []string{"admin"},
		func(w http.ResponseWriter, r *http.Request) { handleAdminRequestLookup(w, r, observability.Default) })
	if code != 400 {
		t.Fatalf("missing request_id: status %d, want 400", code)
	}
	// Customer token: 403 (same admin gate as the inbox handlers).
	code, _ = callLookup(f.token(t, customer, "09999000104", "customer"))
	if code != 403 {
		t.Fatalf("customer lookup: status %d, want 403", code)
	}
	// No token: 401.
	code, _ = callLookup("")
	if code != 401 {
		t.Fatalf("anonymous lookup: status %d, want 401", code)
	}
}
