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

// Regression for both previously reproduced findings on the canonical schema.
func TestQual01WalletWithdrawalRegression(t *testing.T) {
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
	serve := func(method, path, token, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	check := func(w *httptest.ResponseRecorder, status int) *httptest.ResponseRecorder {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body)
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
	body := func(amount int64) string {
		return fmt.Sprintf(`{"amount":%d,"sheba":"IR000000000000000000000000","bankName":"Fixture","accountHolderName":"Fixture"}`, amount)
	}
	post := func(token, key string, amount int64) *httptest.ResponseRecorder {
		return serve(http.MethodPost, path, token, body(amount), key)
	}
	responseID := func(w *httptest.ResponseRecorder) string {
		t.Helper()
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
	quote := func(method, path, body string, enabled, sufficient bool, minimum int64) {
		t.Helper()
		w := check(serve(method, path, ownerToken, body, ""), http.StatusOK)
		var got struct {
			WithdrawEnabled bool  `json:"withdrawEnabled"`
			Sufficient      bool  `json:"sufficient"`
			Minimum         int64 `json:"minimumAmount"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.WithdrawEnabled != enabled || got.Sufficient != sufficient || got.Minimum != minimum {
			t.Fatalf("unexpected quote: %s", w.Body)
		}
	}
	exec(`UPDATE platform_settings SET wallet_withdraw_enabled=false,wallet_min_withdrawal=6000 WHERE id=1`)
	quote(http.MethodGet, quotePath+"?amount=6000", "", false, false, 6000)
	check(post(ownerToken, "disabled-intent", 6000), http.StatusForbidden)
	state(owner, 0, 0)

	exec(`UPDATE platform_settings SET wallet_withdraw_enabled=true WHERE id=1`)
	quote(http.MethodPost, quotePath, `{"amount":5999}`, true, false, 6000)
	quote(http.MethodGet, quotePath+"?amount=6000", "", true, true, 6000)
	check(post(ownerToken, "below-limit", 5999), http.StatusBadRequest)
	check(post(ownerToken, "", 6000), http.StatusBadRequest)
	check(post(ownerToken, strings.Repeat("x", 129), 6000), http.StatusBadRequest)
	check(post(ownerToken, "negative", -1), http.StatusBadRequest)
	check(post(ownerToken, "larger-than-balance", 9223372036854775807), http.StatusBadRequest)
	check(serve(http.MethodPost, path, ownerToken, strings.Replace(body(6000), "6000", "6000.5", 1), "fractional"), http.StatusBadRequest)
	state(owner, 0, 0)

	// Break policy reads on the disposable schema only, then restore it even
	// on failure. Neither quote nor a new submission may fall back to enabled.
	func() {
		exec(`ALTER TABLE platform_settings RENAME COLUMN wallet_min_withdrawal TO qual01_hidden_minimum`)
		defer exec(`ALTER TABLE platform_settings RENAME COLUMN qual01_hidden_minimum TO wallet_min_withdrawal`)
		check(serve(http.MethodGet, quotePath+"?amount=6000", ownerToken, "", ""), http.StatusInternalServerError)
		check(post(ownerToken, "missing-policy", 6000), http.StatusInternalServerError)
		state(owner, 0, 0)
	}()

	// Race retries of one intent against each other. Exactly one creates a
	// withdrawal and all callers recover the same ID even after funds are held.
	const sharedKey = "equal-intent"
	const tries = 8
	results := make(chan *httptest.ResponseRecorder, tries)
	start := make(chan struct{})
	for i := 0; i < tries; i++ {
		go func() { <-start; results <- post(ownerToken, sharedKey, 6000) }()
	}
	close(start)
	var first string
	created := 0
	for i := 0; i < tries; i++ {
		w := <-results
		switch w.Code {
		case http.StatusCreated:
			created++
		case http.StatusOK:
		default:
			t.Errorf("concurrent request status %d: %s", w.Code, w.Body)
			continue
		}
		id := responseID(w)
		if first == "" {
			first = id
		} else if first != id {
			t.Errorf("replay returned a different withdrawal ID")
		}
	}
	if created != 1 {
		t.Fatalf("created %d requests, want one", created)
	}
	state(owner, 6000, 1)
	check(post(ownerToken, sharedKey, 6001), http.StatusConflict)
	check(serve(http.MethodPost, path, ownerToken, strings.Replace(body(6000), "accountHolderName\":\"Fixture", "accountHolderName\":\"Changed", 1), sharedKey), http.StatusConflict)
	state(owner, 6000, 1)

	exec(`UPDATE platform_settings SET wallet_withdraw_enabled=false WHERE id=1`)
	if responseID(check(post(ownerToken, sharedKey, 6000), http.StatusOK)) != first {
		t.Fatal("committed intent was lost after policy changed")
	}
	exec(`UPDATE platform_settings SET wallet_withdraw_enabled=true,wallet_min_withdrawal=4000 WHERE id=1`)
	a := responseID(check(post(otherToken, sharedKey, 4000), http.StatusCreated))
	b := responseID(check(post(otherToken, "second-intent", 4000), http.StatusCreated))
	if a == first || a == b {
		t.Fatal("a separate customer or deliberate new intent reused an existing withdrawal ID")
	}
	state(other, 8000, 2)

	for _, tc := range []struct{ token, own, foreign string }{{ownerToken, first, a}, {otherToken, a, first}} {
		w := check(serve(http.MethodGet, path+"?customerId="+owner, tc.token, "", ""), http.StatusOK)
		if !strings.Contains(w.Body.String(), tc.own) || strings.Contains(w.Body.String(), tc.foreign) {
			t.Fatal("withdrawal list did not enforce principal ownership")
		}
	}
	state(owner, 6000, 1)
}
