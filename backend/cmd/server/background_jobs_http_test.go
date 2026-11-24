package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testredis"
	"github.com/google/uuid"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func jobPaymentFixture(t *testing.T, db *sql.DB) (string, string) {
	t.Helper()
	id := "job-pay-" + uuid.NewString()[:12]
	var product string
	// TEST-02 round 4: the picked product's supplier row may not exist.
	// TestAdminRequestLookupEndToEnd (alphabetically earlier) DROPs and
	// reseeds suppliers, so the migrated products' supplier_ids can all
	// point at deleted rows; CreatePaymentTransaction's finance guard
	// then fails locking the missing supplier ("acquire transaction row
	// guard: sql: no rows in result set") and the token endpoint 500s.
	// Re-seed the picked product's supplier (same pattern as the
	// settlement supplier seed in finance_lifecycle_test.go).
	if err := db.QueryRow(`SELECT id FROM products WHERE stock>2 LIMIT 1`).Scan(&product); err != nil {
		t.Fatal(err)
	}
	var supplierID string
	if err := db.QueryRow(`SELECT supplier_id FROM products WHERE id=$1`, product).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	// Two parameters (not one $1 reused): a single untyped $1 used both
	// in the SELECT list and the WHERE clause makes Postgres deduce
	// TEXT for one use and VARCHAR(50) for the other (42P08).
	if _, err := db.Exec(`INSERT INTO suppliers(id, name, approval_status) SELECT $1, 'Payment Fixture Supplier', 'approved' WHERE NOT EXISTS (SELECT 1 FROM suppliers WHERE id=$2)`, supplierID, supplierID); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code) VALUES($1,$1,'store-central','job',2000,2000,'job','09123456789','x','x','x','1234512345',$1)`, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price) VALUES($1,$2,$3,1,1000,0,1000,2000)`, uuid.NewString(), id, product)
	if err != nil {
		t.Fatal(err)
	}
	return id, product
}
func TestPaymentInitiationHoldEndsAtRecordedDeadline(t *testing.T) {
	db := opsHTTPDB(t)
	id, product := jobPaymentFixture(t, db)
	redis := &repository.RedisClient{Client: testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))}
	deadline := time.Now().Add(2200 * time.Millisecond)
	if _, err := db.Exec(`UPDATE orders SET payment_expires_at=$2 WHERE id=$1`, id, deadline); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{Role: "admin"})
	req := httptest.NewRequest("POST", "/api/v1/payments/shaparak/token", strings.NewReader(`{"orderId":"`+id+`"}`)).WithContext(ctx)
	w := httptest.NewRecorder()
	handleShaparakToken(w, req, ctx, repository.NewOrderRepository(db), repository.NewCatalogRepository(db), redis)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	ttl, err := redis.Client.PTTL(ctx, "inventory:hold_qty:"+product+":"+id).Result()
	if err != nil {
		t.Fatal(err)
	}
	// PERF-04: the quantity key outlives the hold deadline by a 60 s grace
	// (so expiry cleanup can decrement the maintained aggregate exactly);
	// the hold itself still ends at the deadline — asserted behaviorally
	// below via HasInventoryHold. The TTL must therefore exceed the
	// deadline but stay within deadline + grace.
	if ttl <= 2200*time.Millisecond || ttl > 63*time.Second {
		t.Fatalf("hold quantity key TTL out of range: TTL=%s", ttl)
	}
	time.Sleep(time.Until(deadline) + 30*time.Millisecond)
	held, err := redis.HasInventoryHold(ctx, id, product, "", 1)
	if err != nil || held {
		t.Fatalf("hold survived payment deadline: held=%v err=%v", held, err)
	}
}
func TestPaymentNullDeadlineRequiresReviewButPaidReplayWorks(t *testing.T) {
	db := opsHTTPDB(t)
	id, _ := jobPaymentFixture(t, db)
	if _, err := db.Exec(`UPDATE orders SET payment_expires_at=NULL WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES($1,$2,$2,'job-existing-token',2000,'pending')`, uuid.NewString(), id); err != nil {
		t.Fatal(err)
	}
	request := func() *httptest.ResponseRecorder {
		ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{Role: "admin"})
		req := httptest.NewRequest("POST", "/api/v1/payments/shaparak/token", strings.NewReader(`{"orderId":"`+id+`"}`)).WithContext(ctx)
		w := httptest.NewRecorder()
		handleShaparakToken(w, req, ctx, repository.NewOrderRepository(db), nil, nil)
		return w
	}
	w := request()
	var response map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	if w.Code != 409 || response["code"] != "payment_deadline_review_required" {
		t.Fatalf("missing authority admitted: %d %s", w.Code, w.Body.String())
	}
	if _, err := db.Exec(`UPDATE orders SET status='cancelled' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	w = request()
	if w.Code != 409 || !strings.Contains(w.Body.String(), "payment_expired") {
		t.Fatalf("cancelled unpaid initiation admitted: %d %s", w.Code, w.Body.String())
	}
	if _, err := db.Exec(`UPDATE payment_transactions SET status='paid' WHERE order_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	w = request()
	if w.Code != 200 || !strings.Contains(w.Body.String(), "job-existing-token") {
		t.Fatalf("paid replay unavailable: %d %s", w.Code, w.Body.String())
	}
}

func TestManualDeliveryJobReceivesRequestCancellation(t *testing.T) {
	db := opsHTTPDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/api/v1/admin/jobs/delivery-lock-releaser/run", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	var canceled bool
	handleT01AdminJobRun(w, req, db, "delivery-lock-releaser", func(got context.Context) error { canceled = got.Err() == context.Canceled; return got.Err() })
	if !canceled || w.Code == 200 {
		t.Fatalf("manual job detached from request: canceled=%v status=%d", canceled, w.Code)
	}
}

func TestOperationsHealthIncludesMissingAndReviewBlockers(t *testing.T) {
	db := opsHTTPDB(t)
	id, _ := jobPaymentFixture(t, db)
	if _, err := db.Exec(`UPDATE orders SET payment_expires_at=NULL WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/v1/admin/operations", nil)
	w := httptest.NewRecorder()
	handleOperationsOverview(w, req, db)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var payload struct {
		Health []repository.JobHealth `json:"jobHealth"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	// Keep this list independent of the worker and repository registries
	// so an accidentally unmonitored job fails at the HTTP boundary.
	wantJobs := [...]string{
		"unpaid-order-expiry",
		"ad-cleanup",
		"qc-response-deadline",
		"supplier-sla",
		"delivery-lock-releaser",
		"monthly-settlement",
		"refund-reconciliation",
	}
	if len(payload.Health) != len(wantJobs) {
		t.Fatalf("job health returned %d entries, want %d: %+v", len(payload.Health), len(wantJobs), payload.Health)
	}
	healthByJob := make(map[string]repository.JobHealth, len(wantJobs))
	for _, h := range payload.Health {
		if _, exists := healthByJob[h.Job]; exists {
			t.Fatalf("duplicate job health for %q", h.Job)
		}
		healthByJob[h.Job] = h
	}
	for _, job := range wantJobs {
		if _, exists := healthByJob[job]; !exists {
			t.Errorf("missing job health for %q: %+v", job, payload.Health)
		}
	}
	h := healthByJob["unpaid-order-expiry"]
	if !h.Missing || !h.Stale || h.Review < 1 {
		t.Fatalf("false healthy status: %+v", h)
	}
	if settlement := healthByJob["monthly-settlement"]; !settlement.Disabled || settlement.Stale {
		t.Fatalf("disabled job alarmed: %+v", settlement)
	}
	if refund := healthByJob["refund-reconciliation"]; refund.Disabled {
		t.Fatalf("refund reconciler is not monitored: %+v", refund)
	}
}
