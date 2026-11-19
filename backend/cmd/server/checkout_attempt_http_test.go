package main

import (
	"context"
	"encoding/json"
	"github.com/drup/platform/internal/auth"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestCheckoutAttemptChangedPayloadRejectsReplay(t *testing.T) {
	rp := checkoutHTTPFixture(t)
	items := `{"productId":"p-direct-both","quantity":1}`
	body := strings.TrimSuffix(gap13OrderBody(items, gap13QuoteID(t, rp, items)), "}") + `,"idempotencyKey":"checkout-first"}`
	gap13OrderID(t, gap13CreateOrder(t, rp, body))
	changed := strings.Replace(body, `"quantity":1`, `"quantity":2`, 1)
	response := gap13CreateOrder(t, rp, changed)
	if response.Code != 409 {
		t.Fatalf("changed checkout payload accepted: %d %s", response.Code, response.Body.String())
	}
}

func checkoutHTTPFixture(t *testing.T) *gap13Repos {
	t.Helper()
	rp := gap13Setup(t)
	// TEST-02 round 4: the migration below re-adds product_title_snapshot
	// with a bare ADD COLUMN. gap13Setup's order_items rebuild now
	// carries the full migrated shape (including that column), so drop
	// it first — the same defensive pattern as the DROP TABLE / DROP
	// FUNCTION lines beside it, which exist so this migration stays
	// re-runnable against a rebuilt table.
	for _, q := range []string{`DROP TABLE IF EXISTS checkout_attempts`, `DROP FUNCTION IF EXISTS guard_checkout_attempt()`, `DROP FUNCTION IF EXISTS guard_checkout_item_title()`, `ALTER TABLE order_items DROP COLUMN IF EXISTS product_title_snapshot`, `INSERT INTO users(id,mobile,full_name) VALUES('11111111-1111-1111-1111-111111111111','09120000000','checkout test') ON CONFLICT(id) DO NOTHING`} {
		if _, err := rp.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../../migrations/202611150005_checkout_attempts.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rp.db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	return rp
}
func checkoutBody(t *testing.T, rp *gap13Repos, key string) string {
	t.Helper()
	items := `{"productId":"p-direct-both","quantity":1}`
	return strings.TrimSuffix(gap13OrderBody(items, gap13QuoteID(t, rp, items)), "}") + `,"idempotencyKey":"` + key + `"}`
}
func checkoutRecover(rp *gap13Repos, owner, key, method string) *httptest.ResponseRecorder {
	ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: owner, Role: "customer"})
	req := httptest.NewRequest(method, "/api/v1/checkout-attempts/"+key, strings.NewReader(`{"action":"abandon"}`)).WithContext(ctx)
	w := httptest.NewRecorder()
	handleCheckoutAttempt(w, req, rp.orderRepo)
	return w
}
func TestCheckoutAttemptConcurrentRequestsReturnOneStoredOrder(t *testing.T) {
	rp := checkoutHTTPFixture(t)
	body := checkoutBody(t, rp, "concurrent-checkout")
	var ledger, movements, payments int
	var stock int
	for _, v := range []struct {
		q    string
		dest *int
	}{{`SELECT count(*) FROM ledger_entries`, &ledger}, {`SELECT count(*) FROM inventory_movements`, &movements}, {`SELECT count(*) FROM payment_transactions`, &payments}, {`SELECT stock FROM products WHERE id='p-direct-both'`, &stock}} {
		if err := rp.db.QueryRow(v.q).Scan(v.dest); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); responses <- gap13CreateOrder(t, rp, body) }()
	}
	wg.Wait()
	close(responses)
	id := ""
	created := 0
	for w := range responses {
		if w.Code != 200 && w.Code != 201 {
			t.Fatalf("concurrent create %d %s", w.Code, w.Body.String())
		}
		if w.Code == 201 {
			created++
		}
		var out struct{ ID string }
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if id != "" && id != out.ID {
			t.Fatal("different orders for one logical attempt")
		}
		id = out.ID
	}
	if created != 1 {
		t.Fatalf("created %d orders", created)
	}
	for _, v := range []struct {
		q    string
		want int
	}{{`SELECT count(*) FROM orders`, 1}, {`SELECT count(*) FROM order_items`, 1}, {`SELECT count(*) FROM supplier_shipments`, 1}, {`SELECT count(*) FROM ledger_entries`, ledger}, {`SELECT count(*) FROM inventory_movements`, movements}, {`SELECT count(*) FROM payment_transactions`, payments}, {`SELECT stock FROM products WHERE id='p-direct-both'`, stock}} {
		var got int
		if err := rp.db.QueryRow(v.q).Scan(&got); err != nil || got != v.want {
			t.Fatalf("invariant %s got=%d want=%d err=%v", v.q, got, v.want, err)
		}
	}
	if w := checkoutRecover(rp, gap13UserID, "concurrent-checkout", "GET"); w.Code != 200 || !strings.Contains(w.Body.String(), id) {
		t.Fatalf("recovery %d %s", w.Code, w.Body.String())
	}
	if w := checkoutRecover(rp, "22222222-2222-2222-2222-222222222222", "concurrent-checkout", "GET"); w.Code != 404 {
		t.Fatalf("other account recovery %d", w.Code)
	}
}
func TestCheckoutAttemptReplaySurvivesDeletedSavedAddressAndRepricing(t *testing.T) {
	rp := checkoutHTTPFixture(t)
	if _, err := rp.db.Exec(`INSERT INTO user_addresses(id,user_id,label,receiver_full_name,receiver_phone,province_name,city_name,postal_code,address_detail) VALUES('33333333-3333-3333-3333-333333333333',$1,'test','test','09120000000','تهران','تهران','1234567890','خیابان تست ۱')`, gap13UserID); err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(checkoutBody(t, rp, "address-checkout"), "}") + `,"addressId":"33333333-3333-3333-3333-333333333333"}`
	first := gap13CreateOrder(t, rp, body)
	id := gap13OrderID(t, first)
	if _, err := rp.db.Exec(`DELETE FROM user_addresses WHERE id='33333333-3333-3333-3333-333333333333';UPDATE products SET supplier_base_cost=99999999,is_active=false WHERE id='p-direct-both'`); err != nil {
		t.Fatal(err)
	}
	retry := gap13CreateOrder(t, rp, body)
	if retry.Code != 200 || !strings.Contains(retry.Body.String(), id) {
		t.Fatalf("stored replay revalidated mutable address/catalog: %d %s", retry.Code, retry.Body.String())
	}
	var a, b map[string]any
	json.Unmarshal(first.Body.Bytes(), &a)
	json.Unmarshal(retry.Body.Bytes(), &b)
	if a["payableAmount"] != b["payableAmount"] {
		t.Fatal("replay repriced")
	}
}
func TestCheckoutAttemptAbandonFencesDelayedCreateAndKeepsCompletedOrder(t *testing.T) {
	rp := checkoutHTTPFixture(t)
	body := checkoutBody(t, rp, "delayed-checkout")
	if w := checkoutRecover(rp, gap13UserID, "delayed-checkout", "POST"); w.Code != 200 || !strings.Contains(w.Body.String(), "abandoned") {
		t.Fatalf("abandon %d %s", w.Code, w.Body.String())
	}
	if w := gap13CreateOrder(t, rp, body); w.Code != 409 {
		t.Fatalf("late create after abandonment %d %s", w.Code, w.Body.String())
	}
	body = checkoutBody(t, rp, "completed-checkout")
	id := gap13OrderID(t, gap13CreateOrder(t, rp, body))
	if w := checkoutRecover(rp, gap13UserID, "completed-checkout", "POST"); w.Code != 200 || !strings.Contains(w.Body.String(), id) || !strings.Contains(w.Body.String(), "completed") {
		t.Fatalf("abandon erased committed order %d %s", w.Code, w.Body.String())
	}
	if w := gap13CreateOrder(t, rp, body); w.Code != 200 {
		t.Fatalf("completed attempt no longer replayable %d", w.Code)
	}
}
func TestCheckoutAttemptLegacyKeyRequiresRecovery(t *testing.T) {
	rp := checkoutHTTPFixture(t)
	body := checkoutBody(t, rp, "legacy-key")
	legacy := strings.Replace(body, `,"idempotencyKey":"legacy-key"`, "", 1)
	id := gap13OrderID(t, gap13CreateOrder(t, rp, legacy))
	if _, err := rp.db.Exec(`UPDATE orders SET idempotency_key='legacy-key' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if w := gap13CreateOrder(t, rp, body); w.Code != 409 {
		t.Fatalf("invented legacy fingerprint %d %s", w.Code, w.Body.String())
	}
	if w := checkoutRecover(rp, gap13UserID, "legacy-key", "GET"); w.Code != 200 || !strings.Contains(w.Body.String(), id) {
		t.Fatalf("legacy recovery %d %s", w.Code, w.Body.String())
	}
}

func TestCheckoutAttemptLegacyAbandonPreservesRecovery(t *testing.T) {
	rp := checkoutHTTPFixture(t)
	body := checkoutBody(t, rp, "legacy-abandon")
	legacy := strings.Replace(body, `,"idempotencyKey":"legacy-abandon"`, "", 1)
	id := gap13OrderID(t, gap13CreateOrder(t, rp, legacy))
	if _, err := rp.db.Exec(`UPDATE orders SET idempotency_key='legacy-abandon' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if w := checkoutRecover(rp, gap13UserID, "legacy-abandon", "POST"); w.Code != 200 || !strings.Contains(w.Body.String(), id) {
		t.Fatalf("legacy abandonment hid an existing committed order %d %s", w.Code, w.Body.String())
	}
}
func TestCheckoutAttemptRecoveryPreservesCommittedProductTitle(t *testing.T) {
	rp := checkoutHTTPFixture(t)
	body := checkoutBody(t, rp, "snapshot-checkout")
	first := gap13CreateOrder(t, rp, body)
	gap13OrderID(t, first)
	var original struct {
		Items []struct {
			ProductTitle string `json:"productTitle"`
		}
	}
	if err := json.Unmarshal(first.Body.Bytes(), &original); err != nil || len(original.Items) != 1 {
		t.Fatalf("first representation %v", err)
	}
	if _, err := rp.db.Exec(`UPDATE products SET title='CHANGED AFTER CHECKOUT' WHERE id='p-direct-both'`); err != nil {
		t.Fatal(err)
	}
	for _, w := range []*httptest.ResponseRecorder{gap13CreateOrder(t, rp, body), checkoutRecover(rp, gap13UserID, "snapshot-checkout", "GET")} {
		if w.Code != 200 {
			t.Fatalf("recovery %d %s", w.Code, w.Body.String())
		}
		var out map[string]json.RawMessage
		json.Unmarshal(w.Body.Bytes(), &out)
		data := w.Body.Bytes()
		if order := out["order"]; len(order) > 0 {
			data = order
		}
		var recovered struct {
			Items []struct {
				ProductTitle string `json:"productTitle"`
			}
		}
		if err := json.Unmarshal(data, &recovered); err != nil || len(recovered.Items) != 1 || recovered.Items[0].ProductTitle != original.Items[0].ProductTitle {
			t.Fatalf("committed description changed: %s", w.Body.String())
		}
	}
}
func TestCheckoutAttemptRecoveryDoesNotAcknowledgeFailedItemRead(t *testing.T) {
	rp := checkoutHTTPFixture(t)
	body := checkoutBody(t, rp, "read-failure-checkout")
	gap13OrderID(t, gap13CreateOrder(t, rp, body))
	if _, err := rp.db.Exec(`ALTER TABLE order_items RENAME COLUMN final_unit_price TO unavailable_final_unit_price`); err != nil {
		t.Fatal(err)
	}
	// TEST-02 round 4: this rename simulates a failed item read, but the
	// shared disposable DB outlives the test — every later test that
	// inserts order_items (capacity seed, FIN011, return-bin) needs
	// final_unit_price back. Restore it on cleanup, even if the
	// assertion below fails.
	t.Cleanup(func() {
		_, _ = rp.db.Exec(`ALTER TABLE order_items RENAME COLUMN unavailable_final_unit_price TO final_unit_price`)
	})
	w := checkoutRecover(rp, gap13UserID, "read-failure-checkout", "GET")
	if w.Code < 500 {
		t.Fatalf("incomplete order was acknowledged: %d %s", w.Code, w.Body.String())
	}
}
