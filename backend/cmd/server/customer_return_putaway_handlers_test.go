package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestReturnBinHandlerRejectsMalformedBeforeDatabase(t *testing.T) {
	path := "/api/v1/warehouse/customer-returns/00000000-0000-0000-0000-000000000001/items/00000000-0000-0000-0000-000000000002/putaway"
	for _, body := range []string{`{}`, `{"binCode":"A","quantity":null,"reason":"QC"}`, `{"binCode":"A","quantity":1.5,"reason":"QC"}`, `{"binCode":"A","quantity":0,"reason":"QC"}`, `{"binCode":"A","quantity":2147483648,"reason":"QC"}`, `{"binCode":"A","quantity":1,"reason":" "}`, `{"binCode":"A","quantity":1,"reason":"QC"} {}`} {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Idempotency-Key", "one")
		req = req.WithContext(context.WithValue(req.Context(), userClaimsKey, &auth.UserClaims{UserID: "staff"}))
		rec := httptest.NewRecorder()
		handleCustomerReturnBinEntry(rec, req, nil)
		if rec.Code != 400 {
			t.Fatalf("body=%s HTTP=%d %s", body, rec.Code, rec.Body.String())
		}
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(`{"binCode":"A","quantity":1,"reason":"QC"}`))
	rec := httptest.NewRecorder()
	handleCustomerReturnBinEntry(rec, req, nil)
	if rec.Code != 401 {
		t.Fatalf("anonymous HTTP=%d", rec.Code)
	}
	req = req.WithContext(context.WithValue(req.Context(), userClaimsKey, &auth.UserClaims{UserID: "staff"}))
	rec = httptest.NewRecorder()
	handleCustomerReturnBinEntry(rec, req, nil)
	if rec.Code != 400 {
		t.Fatalf("missing key HTTP=%d", rec.Code)
	}
}

func TestReturnBinHTTPMissingIdentityAndSourceConflict(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	createReturnBinSourcesView(t, db)
	createEffectiveMeasurementsView(t, db)
	order := "return-http-" + uuid.NewString()[:12]
	oi, ret, item := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO orders(id,order_number,store_name,tracking_code,total_amount,payable_amount,customer_user_id,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code) VALUES($1,$1,'test',$1,2000,2000,$2,'test','09123456789','test','test','test','1234512345')`, []any{order, f.user}},
		{`INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price) VALUES($1,$2,$3,1,1000,0,1000,2000)`, []any{oi, order, f.product}},
	} {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	// Explicit received-return fixture under the current financial parent guard;
	// no putaway/custody source is invented by this setup.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = repository.LockOrderAccountsTx(ctx, tx, order); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('sellersho.fin005_writer','v1',true)`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO customer_returns(id,order_id,customer_id,status,reason) VALUES($1,$2,$3,'received','test')`, ret, order, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO return_items(id,return_id,order_item_id,qty,qc_status,received_at) VALUES($1,$2,$3,1,'ok',NOW())`, item, ret, oi); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewWarehouseRepository(db)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Idempotency-Key", uuid.NewString())
		req = req.WithContext(context.WithValue(req.Context(), userClaimsKey, &auth.UserClaims{UserID: f.user}))
		rec := httptest.NewRecorder()
		handleCustomerReturnBinEntry(rec, req, repo)
		return rec
	}
	base := "/api/v1/warehouse/customer-returns/" + ret
	for _, test := range []struct {
		path string
		want int
	}{{base + "/items/" + item + "/putaway", 409}, {base + "/items/" + uuid.NewString() + "/putaway", 404}, {"/api/v1/warehouse/customer-returns/" + uuid.NewString() + "/items/" + item + "/putaway", 404}} {
		rec := send("POST", test.path, `{"binCode":"absent","quantity":1,"reason":"no source"}`)
		if rec.Code != test.want {
			t.Fatalf("%s HTTP=%d body=%s", test.path, rec.Code, rec.Body.String())
		}
	}
	rec := send("GET", base+"/putaway-sources", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"sourceProven":false`) || !strings.Contains(rec.Body.String(), `"availableQuantity":0`) {
		t.Fatalf("unproved source acknowledgement=%d %s", rec.Code, rec.Body.String())
	}
	rec = send("GET", "/api/v1/warehouse/customer-returns/"+uuid.NewString()+"/putaway-sources", "")
	if rec.Code != 404 {
		t.Fatalf("missing return HTTP=%d", rec.Code)
	}
}
