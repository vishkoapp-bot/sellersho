package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/google/uuid"
)

// TestAdminCallbackRetriesList — D1 (Batch 4a): the new read-only admin
// GET /api/v1/admin/callback-retries.
//
// Gap proof (pre-change, boot against disposable PG, server healthy):
// GET /api/v1/admin/callback-retries anonymously returned 404 while the
// sibling GET /api/v1/admin/newsletter/subscribers returned 401 — the
// route did not exist. After the route lands, anonymous returns 401
// (auth gate) and an admin token returns 200.
func TestAdminCallbackRetriesList(t *testing.T) {
	db := supplierFlowDB(t)
	suffix := uuid.NewString()[:8]
	retryID := "cbr-" + suffix
	eventID := "cbe-" + suffix
	pspRef := "psp-" + suffix
	orderID := "ord-" + suffix

	jwtService := auth.NewJWTService(strings.Repeat("s", 32))
	// No capability reader: RefreshAuthState is a no-op and the token's
	// own capabilities are used as issued (admin -> ["admin"]).
	adminToken, err := jwtService.GenerateToken("admin-user-"+suffix, "09123456789", "Admin", "admin", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	customerToken, err := jwtService.GenerateToken("customer-user-"+suffix, "09123456780", "Customer", "customer", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	call := func(token, method string) *httptest.ResponseRecorder {
		h := RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
			handleT01AdminCallbackRetries(w, r, db)
		})
		req := httptest.NewRequest(method, "/api/v1/admin/callback-retries", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h(w, req)
		return w
	}
	decode := func(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		var body map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %d: %v (body %s)", w.Code, err, w.Body.String())
		}
		return body
	}

	// 1. Empty table -> 200 with an empty list (never 404, never 500).
	w := call(adminToken, http.MethodGet)
	if w.Code != http.StatusOK {
		t.Fatalf("empty list: got status %d, want %d (%s)", w.Code, http.StatusOK, w.Body.String())
	}
	body := decode(t, w)
	rows, ok := body["retries"].([]interface{})
	if !ok {
		t.Fatalf("empty list: no retries array in %s", w.Body.String())
	}
	if len(rows) != 0 {
		t.Fatalf("empty list: got %d rows, want 0", len(rows))
	}

	// 2. Seed one retry row + one linked event row (same psp_reference).
	if _, err := db.Exec(`INSERT INTO payment_callback_retries
		(id, psp_reference, order_id, order_number, raw_payload, attempt_count, max_attempts, last_error, status)
		VALUES ($1, $2, $3, $4, '{}', 2, 5, 'verification failed', 'needs_manual_review')`,
		retryID, pspRef, orderID, "ORD-"+suffix); err != nil {
		t.Fatalf("seed retry: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM payment_callback_retries WHERE id = $1`, retryID) })
	if _, err := db.Exec(`INSERT INTO payment_callback_events
		(id, psp_reference, order_id, status, error, retry_count)
		VALUES ($1, $2, $3, 'failed', 'gateway timeout', 2)`,
		eventID, pspRef, orderID); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM payment_callback_events WHERE id = $1`, eventID) })

	w = call(adminToken, http.MethodGet)
	if w.Code != http.StatusOK {
		t.Fatalf("seeded list: got status %d, want %d (%s)", w.Code, http.StatusOK, w.Body.String())
	}
	body = decode(t, w)
	rows, ok = body["retries"].([]interface{})
	if !ok || len(rows) != 1 {
		t.Fatalf("seeded list: got %d rows, want 1 (%s)", len(rows), w.Body.String())
	}
	row, ok := rows[0].(map[string]interface{})
	if !ok {
		t.Fatalf("seeded list: row is not an object: %v", rows[0])
	}
	if row["id"] != retryID {
		t.Errorf("id = %v, want %s", row["id"], retryID)
	}
	if row["pspReference"] != pspRef {
		t.Errorf("pspReference = %v, want %s", row["pspReference"], pspRef)
	}
	if row["orderNumber"] != "ORD-"+suffix {
		t.Errorf("orderNumber = %v, want ORD-%s", row["orderNumber"], suffix)
	}
	if row["attemptCount"] != float64(2) {
		t.Errorf("attemptCount = %v, want 2", row["attemptCount"])
	}
	if row["status"] != "needs_manual_review" {
		t.Errorf("status = %v, want needs_manual_review", row["status"])
	}
	// The retry row's own last_error wins over the linked event's error.
	if row["lastError"] != "verification failed" {
		t.Errorf("lastError = %v, want 'verification failed'", row["lastError"])
	}
	if row["eventStatus"] != "failed" {
		t.Errorf("eventStatus = %v, want failed", row["eventStatus"])
	}
	if row["eventError"] != "gateway timeout" {
		t.Errorf("eventError = %v, want 'gateway timeout'", row["eventError"])
	}
	if row["createdAt"] == nil || row["createdAt"] == "" {
		t.Errorf("createdAt missing in %s", w.Body.String())
	}
	if row["updatedAt"] == nil || row["updatedAt"] == "" {
		t.Errorf("updatedAt missing in %s", w.Body.String())
	}

	// 3. Anonymous -> 401.
	w = call("", http.MethodGet)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: got status %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// 4. Non-admin (customer) -> 403.
	w = call(customerToken, http.MethodGet)
	if w.Code != http.StatusForbidden {
		t.Errorf("non-admin: got status %d, want %d", w.Code, http.StatusForbidden)
	}

	// 5. Read-only: POST -> 405.
	w = call(adminToken, http.MethodPost)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: got status %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestAdminCallbackRetriesRouteRegistered pins the route registration in
// main.go: the handler must stay reachable through the real route table,
// not just callable directly. (TestRouteTableNoConflicts separately guards
// the whole table against pattern conflicts/duplicates.)
func TestAdminCallbackRetriesRouteRegistered(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	pattern := `"/api/v1/admin/callback-retries"`
	if n := strings.Count(string(src), pattern); n != 1 {
		t.Fatalf("main.go registers %s %d times, want exactly 1", pattern, n)
	}
	if !strings.Contains(string(src), "handleT01AdminCallbackRetries") {
		t.Fatal("main.go does not wire handleT01AdminCallbackRetries")
	}
}
