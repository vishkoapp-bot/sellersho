package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestQual01FinanceRevenueProjection(t *testing.T) {
	db, _, order, _ := fin003HTTPPaidOrder(t)
	// Exercise the existing noncancelled-order projection, including pending
	// payment. This fixture is not evidence of accounting revenue or cash.
	if _, err := db.Exec(`UPDATE orders SET created_at='2099-02-15T12:00:00Z', payment_status='pending', platform_profit=1500,dropshipper_profit=3500 WHERE id=$1`, order); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewFinanceRepository(db)
	service := auth.NewJWTService(uuid.NewString())
	reader := &middlewareCapabilityReader{}
	service.SetCapabilityReader(reader)
	gate := RequireRoles(service, "finance", "finance_specialist", "admin")
	mux := http.NewServeMux()
	const base = "/api/v1/finance/reports/revenue"
	mux.HandleFunc(base, gate(func(w http.ResponseWriter, r *http.Request) {
		handleGetRevenueReport(w, r, r.Context(), repo)
	}))
	mux.HandleFunc(base+"/monthly", gate(func(w http.ResponseWriter, r *http.Request) {
		handleT01FinanceRevenueMonthly(w, r, db)
	}))
	request := func(method, path string, caps []string, want int) []byte {
		t.Helper()
		reader.capabilities = caps
		token, err := service.GenerateTokenWithCapabilities("fixture-user", "", "", "finance_specialist", []string{"finance_specialist"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s with %v: got %d, want %d: %s", method, path, caps, w.Code, want, w.Body)
		}
		return w.Body.Bytes()
	}
	finance := []string{"finance_specialist"}
	for _, path := range []string{base, base + "/monthly"} {
		request(http.MethodGet, path, []string{"support_specialist"}, http.StatusForbidden)
		request(http.MethodGet, path, nil, http.StatusForbidden)
		request(http.MethodPost, path, finance, http.StatusMethodNotAllowed)
	}
	request(http.MethodGet, base+"?period=2099-Q9", finance, http.StatusBadRequest)
	for _, tc := range []struct {
		period  string
		counted bool
	}{{"2099-02", true}, {"2099-Q1", true}, {"2099-03", false}} {
		var got map[string]any
		want := map[string]any{"period": tc.period, "totalRevenue": float64(0), "platformFees": float64(0), "sellerProfit": float64(0), "sellerPayouts": float64(0), "supplierPayouts": float64(0)}
		if tc.counted {
			want["totalRevenue"] = float64(10000000)
			want["platformFees"] = float64(1500)
			want["sellerProfit"] = float64(3500)
		}
		if body := request(http.MethodGet, base+"?period="+tc.period, finance, http.StatusOK); json.Unmarshal(body, &got) != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("period %q: got %s, want %v", tc.period, body, want)
		}
	}
	var monthly map[string][]map[string]any
	body := request(http.MethodGet, base+"/monthly", finance, http.StatusOK)
	if err := json.Unmarshal(body, &monthly); err != nil || len(monthly) != 1 {
		t.Fatalf("unexpected monthly report envelope: %s (%v)", body, err)
	}
	wantMonth := map[string]any{"month": "2099-02", "orders": float64(1), "revenue": float64(10000000), "platformProfit": float64(1500)}
	for _, row := range monthly["months"] {
		if row["month"] == "2099-02" {
			if !reflect.DeepEqual(row, wantMonth) {
				t.Fatalf("unexpected monthly row: %v", row)
			}
			return
		}
	}
	t.Fatalf("fixture month missing from monthly report: %s", body)
}
