package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestQual01CustomerWithdrawalList(t *testing.T) {
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
	user := func(mobile string) string {
		t.Helper()
		id := uuid.NewString()
		exec(`INSERT INTO users(id, mobile, full_name) VALUES ($1, $2, 'List fixture')`, id, mobile)
		t.Cleanup(func() { exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	const ownerMobile = "09000040111"
	const otherMobile = "09000040112"
	owner, other, empty := user(ownerMobile), user(otherMobile), user("09000040113")

	// Each pair shares a timestamp. The oldest row has the largest ID,
	// so sorting only by ID cannot pass the expected order below.
	prefix := uuid.NewString()
	exec(`INSERT INTO customer_withdrawal_requests
		(id, customer_id, customer_name, customer_mobile, account_holder_name, sheba, bank_name, amount, created_at)
		SELECT $1 || '-' || lpad(n::text, 3, '0'), $2, 'List fixture', $3,
			'Holder fixture', 'IR000000000000000000000000', 'Bank fixture', 1234,
			TIMESTAMPTZ '2026-01-01 00:00:00+00' + (CASE WHEN n = 202 THEN -1 ELSE (n-1)/2 END) * INTERVAL '1 second'
		FROM generate_series(1, 202) AS s(n) ORDER BY n`, prefix, owner, ownerMobile)
	exec(`INSERT INTO customer_withdrawal_requests
		(id, customer_id, customer_name, customer_mobile, account_holder_name, sheba, bank_name, amount, created_at)
		VALUES ($1, $2, 'Other fixture', $3, 'Other holder', 'IR000000000000000000000000',
			'Bank fixture', 4321, TIMESTAMPTZ '2026-01-02 00:00:00+00')`, prefix+"-other", other, otherMobile)

	jwt := auth.NewJWTService("qual01-withdrawal-list-fixture-key")
	handler := RequireRoles(jwt, "customer")(func(w http.ResponseWriter, r *http.Request) {
		handleT01CustomerWithdrawals(w, r, db)
	})
	get := func(t *testing.T, id, query string) []map[string]json.RawMessage {
		t.Helper()
		token, err := jwt.GenerateTokenWithCapabilities(id, "", "", "customer", []string{"customer"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, "/api/v1/customer/wallet/withdrawals"+query, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler(w, r)
		var body map[string][]map[string]json.RawMessage
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body) != 1 || body["withdrawals"] == nil {
			t.Fatalf("expected withdrawals array envelope: %s (decode error: %v)", w.Body, err)
		}
		return body["withdrawals"]
	}
	for _, tc := range []struct {
		name, query string
		count       int
	}{
		{"default", "", 100},
		{"minimum with foreign customer parameter", "?limit=1&customerId=" + other, 1},
		{"within tied timestamp", "?limit=2", 2},
		{"maximum", "?limit=200", 200},
		{"above maximum", "?limit=201", 100},
		{"zero", "?limit=0", 100},
		{"malformed", "?limit=bad", 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := get(t, owner, tc.query)
			if len(rows) != tc.count {
				t.Fatalf("got %d withdrawals, want %d", len(rows), tc.count)
			}
			for i, row := range rows {
				want := fmt.Sprintf(`"%s-%03d"`, prefix, 201-i)
				if got := string(row["id"]); got != want {
					t.Fatalf("withdrawal %d: id=%s, want %s", i, got, want)
				}
			}
			if tc.query == "" {
				want := map[string]string{
					"id": fmt.Sprintf(`"%s-201"`, prefix), "customerName": `"List fixture"`,
					"customerMobile": `"` + ownerMobile + `"`, "accountHolderName": `"Holder fixture"`,
					"sheba": `"IR000000000000000000000000"`, "bankName": `"Bank fixture"`,
					"amount": "1234", "status": `"pending"`, "rejectionReason": `""`,
					"createdAt": `"2026-01-01T00:01:40Z"`,
				}
				if len(rows[0]) != len(want) {
					t.Fatalf("got %d item fields, want %d", len(rows[0]), len(want))
				}
				for key, value := range want {
					if got := string(rows[0][key]); got != value {
						t.Errorf("%s: got %s, want %s", key, got, value)
					}
				}
			}
		})
	}
	t.Run("empty owner cannot read other withdrawals", func(t *testing.T) {
		if rows := get(t, empty, "?customerId="+owner); len(rows) != 0 {
			t.Fatalf("got %d withdrawals for an owner with none", len(rows))
		}
	})
}
