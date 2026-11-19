package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

// This is a manually opted-in finding reproducer, not an acceptance test for
// the desired policy. Replace it with a regression when the finding is fixed.
func TestQual01WalletFindingsProbe(t *testing.T) {
	if os.Getenv("QUAL01_WALLET_PROBE") != "1" {
		t.Skip("explicit wallet finding reproduction required")
	}
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	jwt := auth.NewJWTService("qual01-wallet-fixture-signing-key")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/customer/wallet", RequireRoles(jwt, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01CustomerWallet(w, r, db) }))
	mux.HandleFunc("/api/v1/customer/wallet/quote", RequireRoles(jwt, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01CustomerWalletQuote(w, r, db) }))
	mux.HandleFunc("/api/v1/customer/wallet/withdrawals", RequireRoles(jwt, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01CustomerWithdrawals(w, r, db) }))
	user := func(mobile string) (id, token string) {
		t.Helper()
		id = uuid.NewString()
		exec(`INSERT INTO users(id,mobile,full_name) VALUES($1,$2,'Generated QUAL-01 user')`, id, mobile)
		t.Cleanup(func() { exec(`DELETE FROM users WHERE id=$1`, id) })
		exec(`INSERT INTO customer_wallets(customer_id,balance,locked_balance) VALUES($1,10000,0)`, id)
		token, err := jwt.GenerateTokenWithCapabilities(id, mobile, "Generated", "customer", []string{"customer"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return id, token
	}
	call := func(method, path, token, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s returned %d, want %d: %s", method, path, w.Code, status, w.Body)
		}
		return w
	}
	state := func(customer string, wantLocked int64, wantCount int) {
		t.Helper()
		var balance, locked int64
		var count int
		if err := db.QueryRow(`SELECT balance,locked_balance,(SELECT count(*) FROM customer_withdrawal_requests WHERE customer_id=w.customer_id AND status='pending') FROM customer_wallets w WHERE customer_id=$1`, customer).Scan(&balance, &locked, &count); err != nil {
			t.Fatal(err)
		}
		if balance != 10000 || locked != wantLocked || count != wantCount {
			t.Fatalf("balance=%d locked=%d pending=%d, want 10000/%d/%d", balance, locked, count, wantLocked, wantCount)
		}
	}
	const path = "/api/v1/customer/wallet/withdrawals"
	const quotePath = "/api/v1/customer/wallet/quote"
	post := func(token string, amount int) string {
		t.Helper()
		body := fmt.Sprintf(`{"amount":%d,"sheba":"IR000000000000000000000000","bankName":"Fixture","accountHolderName":"Fixture"}`, amount)
		w := call(http.MethodPost, path, token, body, http.StatusCreated)
		var out struct{ ID string }
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.ID == "" {
			t.Fatalf("missing request ID: %s", w.Body)
		}
		return out.ID
	}
	var originalEnabled bool
	var originalMinimum int64
	if err := db.QueryRow(`SELECT wallet_withdraw_enabled,wallet_min_withdrawal FROM platform_settings WHERE id=1`).Scan(&originalEnabled, &originalMinimum); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		exec(`UPDATE platform_settings SET wallet_withdraw_enabled=$1,wallet_min_withdrawal=$2 WHERE id=1`, originalEnabled, originalMinimum)
	})
	owner, ownerToken := user("09000040101")
	other, otherToken := user("09000040102")
	state(owner, 0, 0)
	state(other, 0, 0)
	exec(`UPDATE platform_settings SET wallet_withdraw_enabled=false,wallet_min_withdrawal=6000 WHERE id=1`)
	response := call(http.MethodGet, quotePath+"?amount=5000", ownerToken, "", http.StatusOK)
	var quote struct {
		WithdrawEnabled bool `json:"withdrawEnabled"`
		Sufficient      bool `json:"sufficient"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &quote); err != nil || quote.WithdrawEnabled || quote.Sufficient {
		t.Fatalf("expected disabled quote: %s", response.Body)
	}
	first := post(ownerToken, 5000)
	state(owner, 5000, 1)
	t.Log("QUAL-01-01 reproduced: disabled quote, but submission committed a pending request and locked 5000")

	exec(`UPDATE platform_settings SET wallet_withdraw_enabled=true,wallet_min_withdrawal=6000 WHERE id=1`)
	response = call(http.MethodPost, quotePath, otherToken, `{"amount":4000}`, http.StatusOK)
	if err := json.Unmarshal(response.Body.Bytes(), &quote); err != nil || !quote.WithdrawEnabled || !quote.Sufficient {
		t.Fatalf("expected source finding: below-minimum quote treated as sufficient: %s", response.Body)
	}
	a, b := post(otherToken, 4000), post(otherToken, 4000)
	if a == b {
		t.Fatal("no duplicate withdrawal ID was observed")
	}
	state(other, 8000, 2)
	t.Log("QUAL-01-01 minimum finding reproduced; QUAL-01-02 reproduced: identical submissions committed distinct pending requests and locked 8000")

	for _, tc := range []struct{ token, own, foreign string }{{ownerToken, first, a}, {otherToken, a, first}} {
		w := call(http.MethodGet, path+"?customerId="+owner, tc.token, "", http.StatusOK)
		if !strings.Contains(w.Body.String(), tc.own) || strings.Contains(w.Body.String(), tc.foreign) {
			t.Fatal("withdrawal list did not enforce principal ownership")
		}
	}
	state(owner, 5000, 1)
}
