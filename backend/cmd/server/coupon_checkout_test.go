package main

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFIN007HTTPCheckoutErrorsAndRecovery(t *testing.T) {
	for _, e := range []error{repository.ErrCouponExhausted, repository.ErrCouponInvalid, repository.ErrCouponChanged} {
		r := httptest.NewRecorder()
		writeCheckoutError(r, fmt.Errorf("admission: %w", e))
		if r.Code != 409 || !strings.Contains(r.Body.String(), `"code":"`+e.Error()+`"`) {
			t.Fatalf("coupon HTTP contract %d %s", r.Code, r.Body.String())
		}
	}
}

func TestFIN007HTTPCheckoutAdmissionRace(t *testing.T) {
	target, err := url.Parse(os.Getenv("SUPPLIER_FLOW_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	query := target.Query()
	query.Set("application_name", "fin007-http")
	target.RawQuery = query.Encode()
	db, err := testdb.Open(t, target.String())
	if err != nil {
		t.Fatal(err)
	}
	seed, err := os.ReadFile("../capacity-fixture/seed.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.NewReplacer("generate_series(1,1000)", "generate_series(1,3)", "generate_series(1,10000)", "generate_series(1,1)", "generate_series(1,50000)", "generate_series(1,5)", "generate_series(1,100000)", "generate_series(1,0)").Replace(string(seed))
	if _, err = db.Exec(sql); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE stores SET profit_margin_percent=20 WHERE id='cap-store-2'`); err != nil {
		t.Fatal(err)
	}
	customer := md5.Sum([]byte("cap-customer-1"))
	uid, _ := uuid.FromBytes(customer[:])
	sku := md5.Sum([]byte("cap-sku-1"))
	vid, _ := uuid.FromBytes(sku[:])
	rp := &gap13Repos{db: db, orderRepo: repository.NewOrderRepository(db), catalogRepo: repository.NewCatalogRepository(db), storeRepo: repository.NewStoreRepository(db), shipmentRepo: repository.NewSupplierShipmentRepository(db), pricingRepo: repository.NewPricingRepository(db), supplierRepo: repository.NewSupplierRepository(db)}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: uid.String(), Mobile: "09900000001", Role: "customer", Capabilities: []string{"customer"}})
	items := `{"productId":"cap-product-1","quantity":1,"variantId":"` + vid.String() + `"}`
	quoteReq := httptest.NewRequest("POST", "/api/v1/shipping/quote", strings.NewReader(`{"items":[`+items+`],"destination":{"province":"Tehran","city":"Tehran","address":"Synthetic FIN007 address","postalCode":"1111111111"}}`)).WithContext(ctx)
	quote := httptest.NewRecorder()
	handleShippingQuote(quote, quoteReq, ctx, rp.catalogRepo, rp.supplierRepo, db)
	if quote.Code != 200 {
		t.Fatalf("canonical fixture quote %d %s", quote.Code, quote.Body.String())
	}
	var q struct{ QuoteID string }
	json.Unmarshal(quote.Body.Bytes(), &q)
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/orders", strings.NewReader(body)).WithContext(ctx)
		out := httptest.NewRecorder()
		handleCreateOrder(out, req, ctx, rp.orderRepo, rp.catalogRepo, rp.storeRepo, rp.shipmentRepo, rp.pricingRepo, rp.supplierRepo, db)
		return out
	}
	for _, keyed := range []bool{false, true} {
		code := "UNKEYED"
		if keyed {
			code = "KEYED"
		}
		if _, err = db.Exec(`INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at,max_uses) VALUES($1,'cap-store-2',$1,'fixed',1000,NOW()-INTERVAL '1 day',NOW()+INTERVAL '1 day',1)`, code); err != nil {
			t.Fatal(err)
		}
		body := func(key string) string {
			suffix := ""
			if keyed {
				suffix = `,"idempotencyKey":"` + key + `"`
			}
			return `{"storeId":"cap-store-2","items":[` + items + `],"customerInfo":{"fullName":"Synthetic FIN007 customer","phone":"09900000001","province":"Tehran","city":"Tehran","address":"Synthetic FIN007 address","postalCode":"1111111111"},"paymentMethod":"cash","shippingQuoteId":"` + q.QuoteID + `","discountCode":"` + code + `"` + suffix + `}`
		}
		blocker, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = blocker.ExecContext(ctx, `SELECT id FROM seller_discount_codes WHERE id=$1 FOR UPDATE`, code); err != nil {
			t.Fatal(err)
		}
		type result struct {
			body string
			out  *httptest.ResponseRecorder
		}
		done := make(chan result, 2)
		for i := 0; i < 2; i++ {
			b := body(fmt.Sprintf("fin007-%s-%d", code, i))
			go func() { done <- result{b, call(b)} }()
		}
		for {
			var n int
			if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='fin007-http' AND wait_event_type='Lock' AND query LIKE '%SELECT id FROM seller_discount_codes%'`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n >= 2 {
				break
			}
			if ctx.Err() != nil {
				t.Fatal(ctx.Err())
			}
		}
		if err = blocker.Commit(); err != nil {
			t.Fatal(err)
		}
		created, exhausted := 0, 0
		var winner result
		for i := 0; i < 2; i++ {
			r := <-done
			if r.out.Code == 201 {
				created++
				winner = r
			} else if r.out.Code == 409 && strings.Contains(r.out.Body.String(), "coupon_exhausted") {
				exhausted++
			} else {
				t.Fatalf("handler admission %d %s", r.out.Code, r.out.Body.String())
			}
		}
		if created != 1 || exhausted != 1 {
			t.Fatalf("handler race created=%d exhausted=%d", created, exhausted)
		}
		advisory := call(body("advisory-exhausted"))
		if advisory.Code != 409 || !strings.Contains(advisory.Body.String(), "coupon_exhausted") {
			t.Fatalf("advisory exhaustion %d %s", advisory.Code, advisory.Body.String())
		}
		if keyed {
			if _, err = db.Exec(`UPDATE seller_discount_codes SET is_active=false WHERE id=$1`, code); err != nil {
				t.Fatal(err)
			}
			replay := call(winner.body)
			if replay.Code != 200 {
				t.Fatalf("original replay after deactivation %d %s", replay.Code, replay.Body.String())
			}
			var a, b map[string]interface{}
			json.Unmarshal(winner.out.Body.Bytes(), &a)
			json.Unmarshal(replay.Body.Bytes(), &b)
			if a["id"] != b["id"] || a["payableAmount"] != b["payableAmount"] {
				t.Fatal("recovery lost original snapshot")
			}
		}
		var count int
		if err = db.QueryRow(`SELECT used_count FROM seller_discount_codes WHERE id=$1`, code).Scan(&count); err != nil || count != 1 {
			t.Fatalf("handler count %d %v", count, err)
		}
	}
}
