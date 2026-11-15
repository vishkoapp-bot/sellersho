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
	if err := db.QueryRow(`SELECT id FROM products WHERE stock>2 LIMIT 1`).Scan(&product); err != nil {
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
	if ttl <= 0 || ttl > 2300*time.Millisecond {
		t.Fatalf("late initiation extended authoritative window: TTL=%s", ttl)
	}
	time.Sleep(time.Until(deadline) + 30*time.Millisecond)
	held, err := redis.HasInventoryHold(ctx, id, product, 1)
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
	if len(payload.Health) != 6 {
		t.Fatalf("missing jobs hidden: %+v", payload.Health)
	}
	h := payload.Health[0]
	if !h.Missing || !h.Stale || h.Review < 1 {
		t.Fatalf("false healthy status: %+v", h)
	}
	for _, h := range payload.Health {
		if h.Job == "monthly-settlement" && (!h.Disabled || h.Stale) {
			t.Fatalf("disabled job alarmed: %+v", h)
		}
	}
}
