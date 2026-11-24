package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"net/http"
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
	dsn := os.Getenv("SUPPLIER_FLOW_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires the disposable supplier-flow test database")
	}
	target, err := url.Parse(dsn)
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
	seedCapacityCheckoutCodes(t, db, vid.String())
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

// TestDiscountCodeDeleteG8 pins the G8 (Batch 3) delete endpoint:
// POST create -> DELETE -> GET list no longer contains the code (200);
// a code with used_count > 0, and a 0-use code referenced by an order,
// both refuse with 409 and the row survives; another store's code, an
// unknown id, or a stranger's slug all refuse with 403, never delete.
// The 0-use guard is server-side (DeleteDiscountCode): deleting a
// 0-use code cannot change any placed order's total.
func TestDiscountCodeDeleteG8(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo := repository.NewStoreRepository(db)
	fin := repository.NewFinanceRepository(db)
	owner := uuid.NewString()
	stranger := uuid.NewString()
	for _, id := range []string{owner, stranger} {
		if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role,is_active) VALUES($1,$2,'Seller panel test','seller',true)`, id, "09"+id[:13]); err != nil {
			t.Fatal(err)
		}
	}
	store := domain.Store{ID: "s-" + uuid.NewString(), Name: "G8 test", Slug: "s-" + uuid.NewString()[:20], OwnerType: "dropshipper", OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8]}
	other := domain.Store{ID: "s-" + uuid.NewString(), Name: "G8 other", Slug: "s-" + uuid.NewString()[:20], OwnerType: "dropshipper", OwnerUserID: &stranger, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8]}
	for _, s := range []domain.Store{store, other} {
		if err := repo.CreateStore(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		// The FIN007 writer fence (migration 202611170001) requires
		// the session flag for any orders write carrying a
		// discount_code; the flag is transaction-local, set the same
		// way coupon_reservations.go does for lifecycle writes.
		tx, err := db.Begin()
		if err != nil {
			return
		}
		defer tx.Rollback()
		tx.Exec(`SELECT set_config('sellersho.fin007_writer','v1',true)`)
		tx.Exec(`DELETE FROM orders WHERE store_id IN ($1,$2)`, store.ID, other.ID)
		tx.Exec(`DELETE FROM seller_discount_codes WHERE store_id IN ($1,$2)`, store.ID, other.ID)
		tx.Exec(`DELETE FROM stores WHERE id IN ($1,$2)`, store.ID, other.ID)
		tx.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, owner, stranger)
		tx.Commit()
	})
	request := func(user, slug, method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/stores/"+slug+"/discount-codes", bytes.NewBufferString(body))
		r = r.WithContext(context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: user, Role: "seller"}))
		w := httptest.NewRecorder()
		handleSellerStore(w, r, repo, fin, slug, "discount-codes", "")
		return w
	}
	create := func(code string) string {
		start := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
		end := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
		body := fmt.Sprintf(`{"code":%q,"mode":"fixed","value":1000,"startsAt":%q,"endsAt":%q}`, code, start, end)
		w := request(owner, store.Slug, http.MethodPost, body)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", code, w.Code, w.Body.String())
		}
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.ID == "" {
			t.Fatalf("create %s: bad response %s (%v)", code, w.Body.String(), err)
		}
		return created.ID
	}
	listIDs := func() []string {
		w := request(owner, store.Slug, http.MethodGet, "")
		if w.Code != http.StatusOK {
			t.Fatalf("list: %d %s", w.Code, w.Body.String())
		}
		var items []repository.DiscountCode
		if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
			t.Fatalf("list: bad response %s (%v)", w.Body.String(), err)
		}
		ids := make([]string, len(items))
		for i, c := range items {
			ids[i] = c.ID
		}
		return ids
	}
	contains := func(ids []string, id string) bool {
		for _, v := range ids {
			if v == id {
				return true
			}
		}
		return false
	}
	deleteCode := func(user, slug, id string) *httptest.ResponseRecorder {
		return request(user, slug, http.MethodDelete, fmt.Sprintf(`{"id":%q}`, id))
	}

	// 0-use code: create -> delete (200) -> list no longer contains it.
	freeID := create("G8FREE")
	if w := deleteCode(owner, store.Slug, freeID); w.Code != http.StatusOK {
		t.Fatalf("delete 0-use: %d %s", w.Code, w.Body.String())
	}
	if ids := listIDs(); contains(ids, freeID) {
		t.Fatalf("deleted code still listed: %v", ids)
	}

	// Used code: admit it through the order-owned reservation
	// lifecycle (a 'reserved' reservation + used_count 1) -> DELETE
	// is 409, row survives.
	usedID := create("G8USED")
	admitCoupon(t, db, "g8-"+uuid.NewString()[:8], usedID, store.ID, "G8USED", owner)
	if w := deleteCode(owner, store.Slug, usedID); w.Code != http.StatusConflict {
		t.Fatalf("delete used code: %d %s, want 409", w.Code, w.Body.String())
	}
	if ids := listIDs(); !contains(ids, usedID) {
		t.Fatalf("used code vanished after refused delete: %v", ids)
	}

	// 0-use code referenced by an order: admit the code, then cancel
	// the order, which releases the reservation and returns used_count
	// to 0 while the order still carries the code string. The
	// belt-and-braces audit check must still refuse with 409.
	refID := create("G8REF")
	refOrderID := "g8-" + uuid.NewString()[:8]
	admitCoupon(t, db, refOrderID, refID, store.ID, "G8REF", owner)
	releaseCoupon(t, db, refOrderID, refID)
	var refUsed int
	if err := db.QueryRow(`SELECT used_count FROM seller_discount_codes WHERE id=$1`, refID).Scan(&refUsed); err != nil || refUsed != 0 {
		t.Fatalf("released fixture used_count=%d err=%v, want 0", refUsed, err)
	}
	if w := deleteCode(owner, store.Slug, refID); w.Code != http.StatusConflict {
		t.Fatalf("delete order-referenced code: %d %s, want 409", w.Code, w.Body.String())
	}
	if ids := listIDs(); !contains(ids, refID) {
		t.Fatalf("order-referenced code vanished after refused delete: %v", ids)
	}

	// Another store's code, deleted as the owner: 403, row survives.
	otherCode, err := repo.CreateDiscountCode(ctx, other.ID, repository.DiscountCode{Code: "G8OTHER", Mode: "fixed", Value: 1000, StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if w := deleteCode(owner, store.Slug, otherCode.ID); w.Code != http.StatusForbidden {
		t.Fatalf("delete foreign code: %d %s, want 403", w.Code, w.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM seller_discount_codes WHERE id=$1`, otherCode.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("foreign code deleted or lost: count=%d err=%v", n, err)
	}
	// Stranger calling the owner's slug: 403 (OwnedStore at entry).
	if w := deleteCode(stranger, store.Slug, usedID); w.Code != http.StatusForbidden {
		t.Fatalf("delete as stranger: %d %s, want 403", w.Code, w.Body.String())
	}
	// Unknown id and empty id: 403 / 400, never a delete.
	if w := deleteCode(owner, store.Slug, "dcode-does-not-exist"); w.Code != http.StatusForbidden {
		t.Fatalf("delete unknown id: %d %s, want 403", w.Code, w.Body.String())
	}
	if w := request(owner, store.Slug, http.MethodDelete, `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("delete empty id: %d %s, want 400", w.Code, w.Body.String())
	}
}

// admitCoupon replicates the order-owned reservation lifecycle for
// one code: an order carrying the code, a 'reserved' admission
// reservation, the 'reserved' event, and used_count bumped to 1 —
// the same three writes coupon_reservations.go performs at checkout,
// in one writer-flagged tx so the FIN007 fences pass. used_count may
// NOT be bumped by a bare UPDATE: the deferred consistency trigger
// requires it to equal the count of non-released reservations.
func admitCoupon(t *testing.T, db *sql.DB, orderID, couponID, storeID, code, customerUserID string) {
	t.Helper()
	var revision int64
	if err := db.QueryRow(`SELECT revision FROM seller_discount_codes WHERE id=$1`, couponID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT set_config('sellersho.fin007_writer','v1',true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status,status,inventory_captured,seller_discount_amount,coupon_discount_amount,discount_code,payment_expires_at) VALUES($1,$1,$2,'G8 test',2000,1000,'G8 customer','09123456789','x','x','x','1234512345',$3,$4,'pending','processing',false,1000,1000,$5,NOW()+INTERVAL '1 hour')`, orderID, storeID, "TRK-"+uuid.NewString()[:8], customerUserID, code); err != nil {
		t.Fatal(err)
	}
	var admitted bool
	err = tx.QueryRow(`WITH instant AS MATERIALIZED(SELECT clock_timestamp() at),
	 added AS(INSERT INTO order_coupon_reservations(order_id,coupon_id,store_id,code,seller_discount_amount,coupon_discount_amount,coupon_revision,state,evidence_kind,reason,created_at)
	 SELECT $1,$2,$3,$4,$5,$6,$7,'reserved','admission','checkout',instant.at FROM instant,seller_discount_codes c WHERE c.id=$2 AND c.starts_at<=instant.at AND c.ends_at>instant.at RETURNING order_id),
	 counter AS(UPDATE seller_discount_codes SET used_count=used_count+1 WHERE id=$2 AND EXISTS(SELECT 1 FROM added) RETURNING id),
	 event AS(INSERT INTO order_coupon_events(order_id,event_kind,new_state,reason) SELECT order_id,'reserved','reserved','checkout' FROM added RETURNING order_id)
	 SELECT EXISTS(SELECT 1 FROM event)`, orderID, couponID, storeID, code, 1000, 1000, revision).Scan(&admitted)
	if err != nil || !admitted {
		t.Fatalf("admit coupon %s: admitted=%v err=%v", code, admitted, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// releaseCoupon replicates the cancellation path for an admitted
// order: the order is cancelled, the reservation moves to
// 'released', used_count returns to 0, and the 'released' event is
// recorded. The order still carries the code string afterwards —
// the exact state the delete guard's order-reference check exists
// for. One writer-flagged tx; the FIN007 deferred consistency
// triggers validate the final state at commit.
func releaseCoupon(t *testing.T, db *sql.DB, orderID, couponID string) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT set_config('sellersho.fin007_writer','v1',true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE orders SET status='cancelled' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE order_coupon_reservations SET state='released',reason='cancelled',updated_at=clock_timestamp() WHERE order_id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE seller_discount_codes SET used_count=used_count-1 WHERE id=$1 AND used_count>0`, couponID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_coupon_events(order_id,event_kind,new_state,reason) VALUES($1,'released','released','cancelled')`, orderID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestDiscountCodeDeleteG8Routing(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	stores := repository.NewStoreRepository(db)
	jwt := auth.NewJWTService(strings.Repeat("s", 32))
	owner, stranger := uuid.NewString(), uuid.NewString()
	tokenFor := func(id string) string {
		t.Helper()
		mobile := "09" + id[:13]
		if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role,is_active) VALUES($1,$2,'G8 routing test','seller',true)`, id, mobile); err != nil {
			t.Fatal(err)
		}
		token, err := jwt.GenerateToken(id, mobile, "G8 routing test", "seller", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	ownerToken, strangerToken := tokenFor(owner), tokenFor(stranger)
	store := domain.Store{
		ID: "s-" + uuid.NewString(), Slug: "s-" + uuid.NewString()[:20],
		Name: "G8 routing test", OwnerType: "dropshipper", OwnerUserID: &owner,
		ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8],
	}
	if err := stores.CreateStore(ctx, store); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/stores/", handleStoresBlock(jwt, stores, repository.NewFinanceRepository(db), repository.NewCatalogRepository(db), repository.NewPricingRepository(db), repository.NewOrderRepository(db)))
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/stores/"+store.Slug+path, nil)
		if body != "" {
			r = httptest.NewRequest(method, r.URL.Path, strings.NewReader(body))
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	assertStatus := func(t *testing.T, w *httptest.ResponseRecorder, want int) {
		t.Helper()
		if w.Code != want {
			t.Fatalf("HTTP %d, want %d: %s", w.Code, want, w.Body.String())
		}
	}
	create := func(code string) string {
		t.Helper()
		c, err := stores.CreateDiscountCode(ctx, store.ID, repository.DiscountCode{
			Code: code, Mode: "fixed", Value: 1000, StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	assertPresent := func(t *testing.T, id string, want bool) {
		t.Helper()
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM seller_discount_codes WHERE id=$1)`, id).Scan(&exists); err != nil || exists != want {
			t.Fatalf("code %s exists=%v, want %v (err %v)", id, exists, want, err)
		}
	}
	const codes = "/discount-codes"
	free := create("G8ROUTEFREE")
	used := create("G8ROUTEUSED")
	admitCoupon(t, db, "g8-"+uuid.NewString()[:8], used, store.ID, "G8ROUTEUSED", owner)
	ref := create("G8ROUTEREF")
	orderID := "g8-" + uuid.NewString()[:8]
	admitCoupon(t, db, orderID, ref, store.ID, "G8ROUTEREF", owner)
	releaseCoupon(t, db, orderID, ref)
	t.Run("authorization", func(t *testing.T) {
		assertStatus(t, request(http.MethodDelete, codes+"/"+free, "", ""), http.StatusUnauthorized)
		assertStatus(t, request(http.MethodDelete, codes+"/"+free, strangerToken, ""), http.StatusForbidden)
		assertPresent(t, free, true)
	})
	t.Run("bodyless delete", func(t *testing.T) {
		w := request(http.MethodDelete, codes+"/"+free, ownerToken, "")
		assertStatus(t, w, http.StatusOK)
		var body struct{ Success bool }
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || !body.Success {
			t.Fatalf("missing success response: %s (%v)", w.Body.String(), err)
		}
		assertPresent(t, free, false)
	})
	t.Run("path overrides body", func(t *testing.T) {
		pathID, bodyID := create("G8ROUTEPATH"), create("G8ROUTEBODY")
		assertStatus(t, request(http.MethodDelete, codes+"/"+pathID+"/", ownerToken, fmt.Sprintf(`{"id":%q}`, bodyID)), http.StatusOK)
		assertPresent(t, pathID, false)
		assertPresent(t, bodyID, true)
		assertStatus(t, request(http.MethodDelete, codes+"/", ownerToken, fmt.Sprintf(`{"id":%q}`, bodyID)), http.StatusOK)
		assertPresent(t, bodyID, false)
	})
	t.Run("used and order-referenced codes", func(t *testing.T) {
		for _, id := range []string{used, ref} {
			assertStatus(t, request(http.MethodDelete, codes+"/"+id, ownerToken, ""), http.StatusConflict)
			assertPresent(t, id, true)
		}
	})
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPost, http.MethodPatch, http.MethodHead} {
		t.Run("three-part "+method, func(t *testing.T) {
			assertStatus(t, request(method, codes+"/"+used, "", ""), http.StatusUnauthorized)
			assertStatus(t, request(method, codes+"/"+used, ownerToken, ""), http.StatusNotFound)
		})
	}
	for _, action := range []string{"settings", "products", "report", "balance", "wallet", "payouts", "early-withdrawals", "discount-codes", "change-requests", "services", "subscribers"} {
		t.Run("two-part unauthorized "+action, func(t *testing.T) {
			assertStatus(t, request(http.MethodGet, "/"+action, "", ""), http.StatusUnauthorized)
		})
	}
	t.Run("two-part unauthorized ad-widget", func(t *testing.T) {
		assertStatus(t, request(http.MethodPut, "/ad-widget", "", ""), http.StatusUnauthorized)
	})
	t.Run("unmatched does not use public dispatch", func(t *testing.T) {
		for _, path := range []string{"/payouts/id", "/report/id"} {
			assertStatus(t, request(http.MethodDelete, path, "", ""), http.StatusNotFound)
		}
		assertStatus(t, request(http.MethodGet, "/bogus", "", ""), http.StatusNotFound)
		assertStatus(t, request(http.MethodGet, "/bogus", ownerToken, ""), http.StatusNotFound)
		w := request(http.MethodGet, "", "", "")
		assertStatus(t, w, http.StatusOK)
		var public struct{ Slug string }
		if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil || public.Slug != store.Slug {
			t.Fatalf("expected public store response: %s (%v)", w.Body.String(), err)
		}
		// The existing quote action is POST-only and still validates its body.
		assertStatus(t, request(http.MethodGet, "/discount-quote", "", ""), http.StatusMethodNotAllowed)
		assertStatus(t, request(http.MethodPost, "/discount-quote", "", "{}"), http.StatusBadRequest)
	})
}
