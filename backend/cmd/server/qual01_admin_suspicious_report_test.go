package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestQual01AdminSuspiciousReportProjection(t *testing.T) {
	db := opsHTTPDB(t)
	// Migrations include a seeded order. Keep an empty fixture on this test's
	// connection so the report never depends on or deletes shared seed data.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TEMP TABLE orders (LIKE public.orders INCLUDING DEFAULTS)`); err != nil {
		t.Fatal(err)
	}
	service := auth.NewJWTService(uuid.NewString())
	reader := &middlewareCapabilityReader{}
	service.SetCapabilityReader(reader)
	repo := repository.NewAdminRepository(db)
	handler := RequireRoles(service, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleGetSuspiciousTransactions(w, r, r.Context(), repo)
	})
	request := func(method string, capabilities []string, want int) *httptest.ResponseRecorder {
		t.Helper()
		reader.capabilities = capabilities
		token, err := service.GenerateTokenWithCapabilities("fixture-user", "", "", "admin", []string{"admin"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/api/v1/admin/reports/suspicious-transactions", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != want {
			t.Fatalf("%s with %v returned %d, want %d: %s", method, capabilities, w.Code, want, w.Body)
		}
		return w
	}
	decode := func(w *httptest.ResponseRecorder) []map[string]any {
		t.Helper()
		var envelope map[string]json.RawMessage
		var rows []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || len(envelope) != 2 || string(envelope["success"]) != "true" {
			t.Fatalf("unexpected report envelope: %s (%v)", w.Body, err)
		}
		if err := json.Unmarshal(envelope["suspiciousTransactions"], &rows); err != nil || rows == nil {
			t.Fatalf("expected report array: %s (%v)", w.Body, err)
		}
		return rows
	}
	request(http.MethodGet, []string{"support_specialist"}, http.StatusForbidden)
	request(http.MethodGet, nil, http.StatusForbidden)
	admin := []string{"admin"}
	request(http.MethodPost, admin, http.StatusMethodNotAllowed)
	if rows := decode(request(http.MethodGet, admin, http.StatusOK)); len(rows) != 0 {
		t.Fatalf("empty fixture returned synthetic report rows: %v", rows)
	}
	order := "qual01-report-" + uuid.NewString()[:12]
	if _, err := db.Exec(`INSERT INTO orders(id,order_number,store_id,store_name,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,payable_amount,total_amount,status,created_at)
		VALUES($1,$1,'fixture','fixture','fixture','fixture','fixture','fixture','fixture','fixture',$1,10000000,10000000,'processing',now())`, order); err != nil {
		t.Fatal(err)
	}
	rows := decode(request(http.MethodGet, admin, http.StatusOK))
	if len(rows) != 1 {
		t.Fatalf("expected only persisted order %q, got %v", order, rows)
	}
	row := rows[0]
	var keys []string
	for key := range row {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	wantKeys := []string{"amount", "createdAt", "flagReason", "id", "orderNumber", "riskScore"}
	if !slices.Equal(keys, wantKeys) || row["id"] != order || row["amount"] != float64(10000000) {
		t.Fatalf("unexpected report row: %v", row)
	}
}
