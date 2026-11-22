package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/google/uuid"
)

func TestAdminPendingPaymentsList(t *testing.T) {
	db := opsHTTPDB(t)
	jwt := auth.NewJWTService(strings.Repeat("s", 32))
	tokenFor := func(role string) string {
		t.Helper()
		token, err := jwt.GenerateToken(uuid.NewString(), "09123456789", "D4 test", role, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	finance, customer := tokenFor("finance_specialist"), tokenFor("customer")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/orders/pending-payment", RequireRoles(jwt, "finance", "finance_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleAdminPendingPayments(w, r, db)
	}))
	type row struct {
		ID           string
		CustomerName string
		AmountToman  int64
	}
	type page struct {
		Success    bool
		Orders     []row
		NextCursor string
		HasMore    bool
		Limit      int
	}
	request := func(token, query string, want int) page {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders/pending-payment"+query, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("HTTP %d, want %d: %s", w.Code, want, w.Body.String())
		}
		var result page
		if want == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result.Success || result.Orders == nil {
				t.Fatalf("invalid page: %s (%v)", w.Body.String(), err)
			}
			for _, forbidden := range []string{"customerPhone", "customerAddress", "customerPostalCode", "09000000000"} {
				if strings.Contains(w.Body.String(), forbidden) {
					t.Fatalf("included customer contact details")
				}
			}
		}
		return result
	}
	request("", "", http.StatusUnauthorized)
	request(customer, "", http.StatusForbidden)
	request(finance, "?cursor=bad", http.StatusBadRequest)
	if got := request(finance, "", http.StatusOK); len(got.Orders) != 0 || got.Limit != 50 || got.HasMore {
		t.Fatalf("expected empty default page, got %+v", got)
	}

	now := time.Now().UTC()
	seed := func(payment, status string, when time.Time) string {
		t.Helper()
		id := "pending-" + uuid.NewString()[:12]
		_, err := db.Exec(`INSERT INTO orders(id,order_number,store_id,store_name,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,payable_amount,total_amount,payment_status,status,created_at)
			VALUES($1,$1,(SELECT id FROM stores ORDER BY id LIMIT 1),'finance','fixture','09000000000','fixture','fixture','fixture','0000000000',$1,100000,100000,$2,$3,$4)`, id, payment, status, when)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	oldest := seed("pending", "pending", now.Add(-2*time.Hour))
	second := seed("pending", "pending", now.Add(-time.Hour))
	seed("paid", "pending", now.Add(-3*time.Hour))
	seed("pending", "cancelled", now.Add(-4*time.Hour))

	got := request(finance, "?limit=1", http.StatusOK)
	if len(got.Orders) != 1 || got.Orders[0] != (row{oldest, "fixture", 100000}) || !got.HasMore || got.NextCursor == "" || got.Limit != 1 {
		t.Fatalf("bad first page: %+v", got)
	}
	got = request(finance, "?limit=1&cursor="+url.QueryEscape(got.NextCursor), http.StatusOK)
	if len(got.Orders) != 1 || got.Orders[0].ID != second || got.HasMore || got.NextCursor != "" {
		t.Fatalf("bad second page: %+v", got)
	}
}

func TestAdminConfirmManualPaymentEvidenceGate(t *testing.T) {
	db := opsHTTPDB(t)
	jwt := auth.NewJWTService(strings.Repeat("s", 32))
	for _, tc := range []struct {
		role string
		want int
	}{{"admin", http.StatusBadRequest}, {"finance_specialist", http.StatusForbidden}} {
		t.Run(tc.role, func(t *testing.T) {
			token, err := jwt.GenerateToken(uuid.NewString(), "09123456789", "D4 test", tc.role, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/payments/order-id/confirm-manual", strings.NewReader(`{"evidenceKind":"whatever","evidenceReference":"fixture"}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Idempotency-Key", "test-operation")
			w := httptest.NewRecorder()
			RequireRoles(jwt, "admin")(func(w http.ResponseWriter, r *http.Request) {
				wr69AdminPayments(w, r, db)
			})(w, req)
			if w.Code != tc.want {
				t.Fatalf("HTTP %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestAdminConfirmManualPayment(t *testing.T) {
	if !manualConfirmRecordingChosen {
		t.Skip("owner must choose the evidence vocabulary and recording location")
	}
	t.Fatal("enable confirm only with paid, inventory, idempotency and evidence-recording assertions in this test")
}
