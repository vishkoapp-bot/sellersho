package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/drup/platform/internal/auth"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func fin003HTTPPaidOrder(t *testing.T) (*sql.DB, string, string, string) {
	t.Helper()
	db := opsHTTPDB(t)
	user := uuid.NewString()
	order := "fin-http-" + uuid.NewString()[:12]
	pay := "pay-" + uuid.NewString()[:12]
	for _, statement := range []struct {
		q string
		a []any
	}{
		{`INSERT INTO users(id,mobile,full_name) VALUES($1,$2,'refund HTTP fixture')`, []any{user, "f" + user[:12]}},
		{`INSERT INTO orders(id,order_number,store_id,store_name,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payable_amount,total_amount,payment_status) VALUES($1,$1,(SELECT id FROM stores ORDER BY id LIMIT 1),'finance','fixture','09000000000','fixture','fixture','fixture','0000000000',$1,$2,10000000,10000000,'paid')`, []any{order, user}},
		{`INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES($1,$2,$2,$1,10000000,'paid')`, []any{pay, order}},
	} {
		if _, err := db.Exec(statement.q, statement.a...); err != nil {
			t.Fatal(err)
		}
	}
	return db, user, order, pay
}

func TestFIN003AdminRefundCannotSpendPendingCancellation(t *testing.T) {
	db, user, order, pay := fin003HTTPPaidOrder(t)
	if err := repository.NewOrderRepository(db).CancelOrder(context.Background(), order, user, false); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"amount":2000000,"reason":"competing route"}`))
	r.Header.Set("Idempotency-Key", "competing-admin-request")
	w := httptest.NewRecorder()
	wr69AdminPaymentRefund(w, r, db, pay)
	if w.Code != 409 {
		t.Fatalf("admin spent pending cancellation principal: %d %s", w.Code, w.Body.String())
	}
	var total int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM ledger_entries WHERE related_order_or_dispute=$1 AND reason LIKE 'استرداد وجه سفارش:%'`, order).Scan(&total); err != nil || total != 0 {
		t.Fatalf("admin recorded a second refund %d %v", total, err)
	}
}

func TestFIN003AdminRefundIsAnIdempotentRequestNotBankSuccess(t *testing.T) {
	db, _, order, pay := fin003HTTPPaidOrder(t)
	post := func(body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		wr69AdminPaymentRefund(w, r, db, pay)
		return w
	}
	body := `{"amount":2000000,"reason":"owner requested partial refund"}`
	if w := post(body, ""); w.Code != 400 {
		t.Fatalf("missing operation identity accepted: %d", w.Code)
	}
	for i := 0; i < 3; i++ {
		if w := post(body, "admin-request-1"); w.Code != 202 || !strings.Contains(w.Body.String(), `"status":"unknown"`) || strings.Contains(w.Body.String(), `"refundedAmount"`) {
			t.Fatalf("admin request pretended cash moved %d %s", w.Code, w.Body.String())
		}
	}
	if w := post(`{"amount":3000000,"reason":"owner requested partial refund"}`, "admin-request-1"); w.Code != 409 {
		t.Fatalf("changed request key reused %d %s", w.Code, w.Body.String())
	}
	var n int
	var amount int64
	var preserved bool
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM order_refund_obligations WHERE order_id=$1),(SELECT SUM(amount_toman) FROM order_refund_obligations WHERE order_id=$1),o.payment_status='paid' AND p.status='paid' FROM orders o JOIN payment_transactions p ON p.order_id=o.id WHERE o.id=$1`, order).Scan(&n, &amount, &preserved); err != nil || n != 1 || amount != 2000000 || !preserved {
		t.Fatalf("admin reservation/evidence %d/%d/%v %v", n, amount, preserved, err)
	}
}

func TestFIN003CustomerCancellationReturnsOwnedPersistentRefundState(t *testing.T) {
	db, user, order, _ := fin003HTTPPaidOrder(t)
	repo := repository.NewOrderRepository(db)
	ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: user, Role: "customer"})
	call := func(ctx context.Context) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		handleOrderCancel(w, req, ctx, order, repo)
		return w
	}
	other := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: uuid.NewString(), Role: "customer"})
	if w := call(other); w.Code != 404 {
		t.Fatalf("another customer's financial state exposed: %d", w.Code)
	}
	w := call(ctx)
	if w.Code != 200 {
		t.Fatalf("cancellation %d %s", w.Code, w.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	summary, ok := response["refund"].(map[string]any)
	if !ok || summary["status"] != "unknown" || summary["amountToman"] != float64(10000000) || summary["destination"] != "original_payment" || summary["needsReview"] != true {
		t.Fatalf("honest cancellation summary missing %s", w.Body.String())
	}
	// The same persisted summary must survive the normal buyer-history read.
	orders, err := repo.GetOrdersByCustomerUserID(ctx, user)
	if err != nil || len(orders) != 1 {
		t.Fatalf("buyer history %d %v", len(orders), err)
	}
	encoded, err := json.Marshal(orders[0])
	if err != nil {
		t.Fatal(err)
	}
	var reloaded map[string]any
	json.Unmarshal(encoded, &reloaded)
	a, _ := json.Marshal(summary)
	b, _ := json.Marshal(reloaded["refund"])
	if string(a) != string(b) {
		t.Fatalf("reload lost refund state %s != %s", a, b)
	}
	if strings.Contains(string(encoded), "gateway_token") || strings.Contains(string(encoded), "provider_reference") {
		t.Fatal("refund read exposed provider internals")
	}
}

func TestFIN003DisputeHTTPDoesNotAssertBankRefundSuccess(t *testing.T) {
	db, _, order, _ := fin003HTTPPaidOrder(t)
	id := "http-dispute-" + uuid.NewString()[:12]
	if _, err := db.Exec(`INSERT INTO disputes(id,order_id,order_number,type,customer_name,product_title,claim_amount,reason,status) VALUES($1,$2,$2,'delivery_undelivered','fixture','fixture',2000000,'partial dispute','pending')`, id, order); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"action":"refund_carrier","responsibleParty":"carrier","claimAmount":2000000}`))
	w := httptest.NewRecorder()
	handleResolveDispute(w, req, context.Background(), id, repository.NewFinanceRepository(db), repository.NewOrderRepository(db))
	if w.Code != 200 {
		t.Fatalf("dispute %d %s", w.Code, w.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["orderRefunded"] != false || result["refundRequested"] != true {
		t.Fatalf("adjudication pretended PSP completed: %s", w.Body.String())
	}
}

func TestFIN003AdminRequestReplayKeepsAcceptedSourceAfterLaterPaymentHistory(t *testing.T) {
	db, _, order, paymentID := fin003HTTPPaidOrder(t)
	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/", strings.NewReader(`{"amount":2000000,"reason":"same logical request"}`))
		req.Header.Set("Idempotency-Key", "stable-admin-request")
		w := httptest.NewRecorder()
		wr69AdminPaymentRefund(w, req, db, order)
		return w
	}
	first := post()
	if first.Code != 202 {
		t.Fatalf("initial request %d %s", first.Code, first.Body.String())
	}
	later := "later-pay-" + uuid.NewString()[:12]
	if _, err := db.Exec(`INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status,created_at) VALUES($1,$2,$2,$1,3000000,'refunded',NOW()+INTERVAL '1 second')`, later, order); err != nil {
		t.Fatal(err)
	}
	replay := post()
	if replay.Code != 202 || replay.Body.String() != first.Body.String() {
		t.Fatalf("same request did not replay its accepted source after later history: %d %s", replay.Code, replay.Body.String())
	}
	var n int
	var source string
	if err := db.QueryRow(`SELECT COUNT(*),MIN(source_payment_id) FROM order_refund_obligations WHERE order_id=$1 AND purpose='admin_refund'`, order).Scan(&n, &source); err != nil || n != 1 || source != paymentID {
		t.Fatalf("replay redirected or duplicated source %d/%s %v", n, source, err)
	}
}
