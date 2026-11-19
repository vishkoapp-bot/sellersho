package main

import (
	"database/sql"
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

func TestQual01CustomerWithdrawalReviewAtomicity(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTService("qual01-review-fixture-signing-key")
	jwt.SetCapabilityReader(&middlewareCapabilityReader{capabilities: []string{"admin"}})
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	user := func(t *testing.T, mobile string) string {
		t.Helper()
		id := uuid.NewString()
		exec(t, `INSERT INTO users(id,mobile,full_name) VALUES($1,$2,'Generated review fixture')`, id, mobile)
		t.Cleanup(func() { exec(t, `DELETE FROM users WHERE id=$1`, id) })
		return id
	}
	type reviewer struct{ id, token string }
	admin := func(mobile string) reviewer {
		id := user(t, mobile)
		token, err := jwt.GenerateTokenWithCapabilities(id, mobile, "Generated", "admin", []string{"admin"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return reviewer{id, token}
	}
	approver, rejecter := admin("09000040201"), admin("09000040202")
	const reason = "Generated review rejection"
	const balance, amount, otherHold = 10000, 6000, 1700
	const totalHold = amount + otherHold
	fixture := func(t *testing.T, mobile string, locked int64) (string, string, string) {
		t.Helper()
		owner := user(t, mobile)
		exec(t, `INSERT INTO customer_wallets(customer_id,balance,locked_balance) VALUES($1,$2,$3)`, owner, balance, locked)
		insert := func(amount int64) string {
			id := uuid.NewString()
			exec(t, `INSERT INTO customer_withdrawal_requests(id,customer_id,customer_name,customer_mobile,account_holder_name,sheba,bank_name,amount)
				VALUES($1,$2,'Generated',$3,'Generated','IR000000000000000000000000','Fixture',$4)`, id, owner, mobile, amount)
			return id
		}
		return owner, insert(amount), insert(otherHold)
	}
	review := func(id, status string, actor reviewer) *httptest.ResponseRecorder {
		path := "/api/v1/admin/customer-withdrawals/" + id + "/review"
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(fmt.Sprintf(`{"status":%q,"reason":%q}`, status, reason)))
		r.Header.Set("Authorization", "Bearer "+actor.token)
		w := httptest.NewRecorder()
		RequireRoles(jwt, "admin")(func(w http.ResponseWriter, r *http.Request) {
			handleT01AdminCustomerWithdrawalReview(w, r, db, id)
		})(w, r)
		return w
	}
	journal := func(t *testing.T) string {
		t.Helper()
		// Snapshot the full disposable journal, including counterpart accounts.
		var snapshot string
		if err := db.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.id), '[]'::jsonb)::text FROM ledger_entries e`).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	type requestState struct {
		status                                string
		by, reason                            sql.NullString
		reviewedAt, transferDate, confirmedAt sql.NullTime
		transferRef, confirmedBy              sql.NullString
		updatedAt                             time.Time
	}
	request := func(t *testing.T, id string) requestState {
		t.Helper()
		var s requestState
		if err := db.QueryRow(`SELECT status,reviewed_by,rejection_reason,reviewed_at,transfer_date,transfer_confirmed_at,
			transfer_reference,transfer_confirmed_by,updated_at FROM customer_withdrawal_requests WHERE id=$1`, id).
			Scan(&s.status, &s.by, &s.reason, &s.reviewedAt, &s.transferDate, &s.confirmedAt, &s.transferRef, &s.confirmedBy, &s.updatedAt); err != nil {
			t.Fatal(err)
		}
		return s
	}
	wallet := func(t *testing.T, owner string, wantLocked int64) {
		t.Helper()
		var gotBalance, gotLocked int64
		if err := db.QueryRow(`SELECT balance,locked_balance FROM customer_wallets WHERE customer_id=$1`, owner).Scan(&gotBalance, &gotLocked); err != nil {
			t.Fatal(err)
		}
		if gotBalance != balance || gotLocked != wantLocked {
			t.Fatalf("wallet balance/locked = %d/%d, want %d/%d", gotBalance, gotLocked, balance, wantLocked)
		}
	}
	check := func(t *testing.T, got *httptest.ResponseRecorder, want int) {
		t.Helper()
		if got.Code != want {
			t.Fatalf("review status = %d, want %d: %s", got.Code, want, got.Body)
		}
	}

	t.Run("competing reviews", func(t *testing.T) {
		owner, id, sibling := fixture(t, "09000040203", totalHold)
		before, other, ledger := request(t, id), request(t, sibling), journal(t)
		wallet(t, owner, totalHold)
		type result struct {
			status   string
			actor    reviewer
			response *httptest.ResponseRecorder
		}
		ready, start, results := make(chan struct{}, 2), make(chan struct{}), make(chan result, 2)
		for _, candidate := range []result{{status: "approved", actor: approver}, {status: "rejected", actor: rejecter}} {
			go func(r result) {
				ready <- struct{}{}
				<-start
				r.response = review(id, r.status, r.actor)
				results <- r
			}(candidate)
		}
		<-ready
		<-ready
		close(start)
		winner, loser := <-results, <-results
		if loser.response.Code == http.StatusOK {
			winner, loser = loser, winner
		}
		check(t, winner.response, http.StatusOK)
		check(t, loser.response, http.StatusConflict)
		wantLocked, wantReason := int64(totalHold), sql.NullString{}
		if winner.status == "rejected" {
			wantLocked = otherHold
			wantReason = sql.NullString{String: reason, Valid: true}
		}
		got := request(t, id)
		if got.status != winner.status || got.by != (sql.NullString{String: winner.actor.id, Valid: true}) ||
			got.reason != wantReason || !got.reviewedAt.Valid || !got.updatedAt.Equal(got.reviewedAt.Time) ||
			got.updatedAt.Before(before.updatedAt) || got.transferDate.Valid || got.confirmedAt.Valid || got.transferRef.Valid || got.confirmedBy.Valid {
			t.Fatalf("wrong committed state for %s: %+v", winner.status, got)
		}
		wallet(t, owner, wantLocked)
		if request(t, sibling) != other || journal(t) != ledger {
			t.Fatal("review changed sibling request or ledger")
		}
	})

	t.Run("wallet write failure rolls back review", func(t *testing.T) {
		owner, id, sibling := fixture(t, "09000040204", totalHold)
		before, other, ledger := request(t, id), request(t, sibling), journal(t)
		// PostgreSQL rejects only the generated wallet's post-release state.
		// This fails the wallet UPDATE after the handler writes the request row.
		exec(t, fmt.Sprintf(`ALTER TABLE customer_wallets ADD CONSTRAINT qual01_review_wallet_write
			CHECK (customer_id <> '%s'::uuid OR locked_balance = %d) NOT VALID`, owner, totalHold))
		t.Cleanup(func() { exec(t, `ALTER TABLE customer_wallets DROP CONSTRAINT qual01_review_wallet_write`) })
		check(t, review(id, "rejected", rejecter), http.StatusInternalServerError)
		wallet(t, owner, totalHold)
		if request(t, id) != before || request(t, sibling) != other || journal(t) != ledger {
			t.Fatal("failed wallet write changed a request or ledger")
		}
	})

	t.Run("undersized hold cannot release sibling funds", func(t *testing.T) {
		owner, id, sibling := fixture(t, "09000040205", otherHold)
		before, other, ledger := request(t, id), request(t, sibling), journal(t)
		check(t, review(id, "rejected", rejecter), http.StatusConflict)
		wallet(t, owner, otherHold)
		if request(t, id) != before || request(t, sibling) != other || journal(t) != ledger {
			t.Fatal("undersized hold changed a request or ledger")
		}
	})
}
