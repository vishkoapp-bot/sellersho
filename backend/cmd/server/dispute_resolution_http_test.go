package main

import (
	"context"
	"encoding/json"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFIN005ResolveRequiresAuthenticatedActor(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/disputes/d/resolve", strings.NewReader(`{"action":"release_to_seller","responsibleParty":"seller","operator":"spoof"}`))
	w := httptest.NewRecorder()
	handleResolveDispute(w, req, context.Background(), "d", nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated resolve returned %d want 401", w.Code)
	}
}
func TestFIN005DisputeHTTPAuthoritativeReplayAndRecovery(t *testing.T) {
	db, actor, order, _ := fin003HTTPPaidOrder(t)
	repo := repository.NewFinanceRepository(db)
	id := "HTTP-FIN005-" + uuid.NewString()[:12]
	if _, err := db.Exec(`UPDATE orders SET status='shipped' WHERE id=$1`, order); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateDispute(context.Background(), domain.Dispute{ID: id, OrderID: order, Type: "delivery_undelivered", ProductTitle: "fixture", ClaimAmount: 2000000, Reason: "fixture"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: actor, Role: "admin"})
	post := func(target, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		handleResolveDispute(w, r, ctx, target, repo, repository.NewOrderRepository(db))
		return w
	}
	for _, body := range []string{`{"action":"refund_arbitrary","responsibleParty":"carrier"}`, `{"action":"refund_supplier","responsibleParty":"carrier"}`, `{"action":"refund_carrier","responsibleParty":"carrier","claimAmount":0}`} {
		if w := post(id, body); w.Code != 400 {
			t.Fatalf("invalid %d %s", w.Code, w.Body.String())
		}
	}
	if w := post("missing", `{"action":"release_to_seller","responsibleParty":"seller"}`); w.Code != 404 {
		t.Fatalf("missing status %d", w.Code)
	}
	body := `{"action":"refund_carrier","responsibleParty":"carrier","operator":"spoofed"}`
	// A deferred fixture fault removes the projection relation only at commit;
	// the decision/obligations commit first, while the response is uncertain.
	if _, err := db.Exec(`CREATE FUNCTION fin005_http_projection_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN ALTER TABLE order_refund_events RENAME TO fin005_hidden_refund_events; RETURN NULL; END $$; CREATE CONSTRAINT TRIGGER fin005_http_projection_fault AFTER INSERT ON dispute_financial_decisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fin005_http_projection_fault()`); err != nil {
		t.Fatal(err)
	}
	first := post(id, body)
	if first.Code != 500 || !strings.Contains(first.Body.String(), "could not be confirmed") {
		t.Fatalf("uncertain response %d %s", first.Code, first.Body.String())
	}
	if _, err := db.Exec(`ALTER TABLE fin005_hidden_refund_events RENAME TO order_refund_events; DROP TRIGGER fin005_http_projection_fault ON dispute_financial_decisions; DROP FUNCTION fin005_http_projection_fault()`); err != nil {
		t.Fatal(err)
	}
	retry := post(id, body)
	if retry.Code != 200 {
		t.Fatalf("retry %d %s", retry.Code, retry.Body.String())
	}
	var outcome domain.DisputeOutcome
	if err := json.Unmarshal(retry.Body.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	if !outcome.Replay || outcome.ActorUserID != actor || outcome.ClaimAmount != 2000000 || outcome.RefundScope != "order" || outcome.Refund.Status == "succeeded" {
		t.Fatalf("untruthful/spoofed outcome %+v", outcome)
	}
	if w := post(id, `{"action":"refund_supplier","responsibleParty":"supplier"}`); w.Code != 409 {
		t.Fatalf("conflict %d", w.Code)
	}
	get := httptest.NewRecorder()
	handleGetDisputes(get, httptest.NewRequest("GET", "/", nil), ctx, repo)
	if get.Code != 200 {
		t.Fatalf("GET recovery %d %s", get.Code, get.Body.String())
	}
	var disputes []domain.Dispute
	if err := json.Unmarshal(get.Body.Bytes(), &disputes); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range disputes {
		if d.ID == id {
			found = d.Outcome != nil && d.Outcome.Refund.AmountToman == 2000000
		}
	}
	if !found {
		t.Fatal("GET lost authoritative outcome")
	}
	var effects int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1`, id).Scan(&effects); err != nil || effects != 1 {
		t.Fatalf("duplicate adjustment %d %v", effects, err)
	}
}
