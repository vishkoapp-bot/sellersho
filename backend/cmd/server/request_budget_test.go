package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/requestbudget"
)

func TestOPS02RouteDeadlineCancelsSQLAndPoolRecovers(t *testing.T) {
	db := opsHTTPDB(t)
	db.SetMaxOpenConns(1)
	cfg := requestbudget.DefaultConfig()
	cfg.API = 40 * time.Millisecond
	reg := observability.New()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/budget-sql", func(w http.ResponseWriter, r *http.Request) {
		_, err := db.ExecContext(r.Context(), `SELECT pg_sleep(2)`)
		if err == nil {
			t.Error("SQL unexpectedly finished")
		}
		http.Error(w, "query failed", 500)
	})
	finished := make(chan struct{})
	observed := reg.HTTP(mux, requestbudget.New(cfg)(mux))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(finished); observed.ServeHTTP(w, r) }))
	defer server.Close()
	started := time.Now()
	res, err := server.Client().Get(server.URL + "/api/v1/budget-sql")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 504 || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("SQL escaped budget %d", res.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("pool did not recover after cancellation")
	}
	<-finished
	metrics := httptest.NewRecorder()
	reg.ServeHTTP(metrics, httptest.NewRequest("GET", "/metrics", nil))
	text := metrics.Body.String()
	if !strings.Contains(text, `status="504",outcome="timeout"`) {
		t.Fatal("actual deadline response not classified as timeout")
	}
}
func TestOPS02CommittedCheckoutSurvivesDeadline(t *testing.T)   { ops02CommittedCheckoutCase(t, false) }
func TestOPS02CommittedCheckoutSurvivesDisconnect(t *testing.T) { ops02CommittedCheckoutCase(t, true) }
func ops02CommittedCheckoutCase(t *testing.T, disconnect bool) {
	rp := checkoutHTTPFixture(t)
	body := checkoutBody(t, rp, "ops02-lost-response")
	var beforeLedger, beforePayments int
	rp.db.QueryRow(`SELECT count(*) FROM ledger_entries`).Scan(&beforeLedger)
	rp.db.QueryRow(`SELECT count(*) FROM payment_transactions`).Scan(&beforePayments)
	cfg := requestbudget.DefaultConfig()
	// Leave the actual checkout transaction its production budget. A tiny
	// deadline can expire during fixture SQL under concurrent build load,
	// which tests a pre-commit failure instead of lost post-commit output.
	committed := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orders", func(w http.ResponseWriter, r *http.Request) {
		buffer := httptest.NewRecorder()
		ctx := context.WithValue(r.Context(), userClaimsKey, &auth.UserClaims{UserID: gap13UserID, Mobile: "09120000000", Role: "customer", Capabilities: []string{"customer"}})
		handleCreateOrder(buffer, r.WithContext(ctx), ctx, rp.orderRepo, rp.catalogRepo, rp.storeRepo, rp.shipmentRepo, rp.pricingRepo, rp.supplierRepo, rp.db)
		if buffer.Code == 201 {
			close(committed)
			io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
		}
		for k, v := range buffer.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(buffer.Code)
		w.Write(buffer.Body.Bytes())
	})
	server := httptest.NewServer(requestbudget.New(cfg)(mux))
	defer server.Close()
	if disconnect {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/api/v1/orders", strings.NewReader(body))
		done := make(chan error, 1)
		go func() {
			res, err := server.Client().Do(req)
			if res != nil {
				res.Body.Close()
			}
			done <- err
		}()
		select {
		case <-committed:
		case <-time.After(12 * time.Second):
			t.Fatal("checkout did not commit")
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("lost-client response %v", err)
		}
	} else {
		res, err := server.Client().Post(server.URL+"/api/v1/orders", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 504 || !strings.Contains(string(response), `"outcome":"unknown"`) {
			t.Fatalf("deadline falsely acknowledged %d %s", res.StatusCode, response)
		}
	}
	retry := gap13CreateOrder(t, rp, body)
	if retry.Code != 200 {
		t.Fatalf("committed checkout not replayed %d %s", retry.Code, retry.Body.String())
	}
	for _, check := range []struct {
		q    string
		want int
	}{{`SELECT count(*) FROM orders`, 1}, {`SELECT count(*) FROM order_items`, 1}, {`SELECT count(*) FROM supplier_shipments`, 1}, {`SELECT stock FROM products WHERE id='p-direct-both'`, 10}, {`SELECT count(*) FROM ledger_entries`, beforeLedger}, {`SELECT count(*) FROM payment_transactions`, beforePayments}} {
		var got int
		if err := rp.db.QueryRow(check.q).Scan(&got); err != nil || got != check.want {
			t.Fatalf("duplicate business effect %s =%d want%d err%v", check.q, got, check.want, err)
		}
	}

}
