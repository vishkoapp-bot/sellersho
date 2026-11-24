package main

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

// FIN-011 G-12 (Q-04): re-enablement gate. With referral flipped ON in a
// DISPOSABLE database only, one real checkout's commission must equal
// TruncatedPercent(platformProfit, 2.0) against a RE-PROVEN base — the
// base is not assumed (D33 caveat, reference_pricing.go:97-99): the
// order's platform_profit must equal BOTH the sum of its per-line
// platform_fee values AND the hand-derived value from the seed inputs.
//
// This test NEVER touches live config: it runs against the disposable
// test DB only, and it restores nothing live because nothing live is
// touched. The live toggle flip belongs to the DEPLOY/VERIFY leg, after
// the fix is deployed and verified on prod.
//
// Hand derivation (capacity fixture seed.sql:14, platform_pct set to
// 10.00 below): cap-product-1 supplier_base_cost 100,000, original_price
// 120,000. referenceCost = MarkedUpPrice(100,000, 10%) = 110,000, so
// the platform fee on one unit is 10,000 and the commission is
// TruncatedPercent(10,000, 2.0) = 200. The margin is the max-default
// 35% (store default abolished Nov 5; profit_margin_percent is not
// consulted), so final unit price = 148,500.
func TestFIN011ReenablementCommissionBase(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_FLOW_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires the disposable supplier-flow test database")
	}
	target, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := target.Query()
	query.Set("application_name", "fin011-g12")
	target.RawQuery = query.Encode()
	db, err := testdb.Open(t, target.String())
	if err != nil {
		t.Fatal(err)
	}
	seed, err := os.ReadFile("../capacity-fixture/seed.sql")
	if err != nil {
		t.Fatal(err)
	}
	seedSQL := strings.NewReplacer("generate_series(1,1000)", "generate_series(1,3)", "generate_series(1,10000)", "generate_series(1,1)", "generate_series(1,50000)", "generate_series(1,5)", "generate_series(1,100000)", "generate_series(1,0)").Replace(string(seed))
	// TEST-02 round 4: the capacity seed is deterministic and
	// idempotent (every INSERT ends ON CONFLICT DO NOTHING in
	// cmd/capacity-fixture/seed.sql), so re-running it is safe even
	// though TestFIN007HTTPCheckoutAdmissionRace (coupon_checkout) ran
	// the same seed earlier in the package — and re-running is REQUIRED
	// here, because intervening fixtures (e.g. discount_quote_gate)
	// DROP products and wipe the seeded cap-product-1 while the cap-*
	// users survive, so a "seed if absent" check on users alone would
	// skip the re-seed and leave the quote without its product. The
	// UPDATEs below re-assert this test's own inputs regardless of who
	// seeded the world.
	if _, err = db.Exec(seedSQL); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE stores SET profit_margin_percent=20 WHERE id='cap-store-2'`); err != nil {
		t.Fatal(err)
	}
	// Global platform pct 10% (seed default is 0) so the hand-derived
	// base is 10,000; referral percent 2.00 is the live default.
	if _, err = db.Exec(`UPDATE platform_settings SET platform_pct=10.00 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// Referrer store for cap-store-2 (FK target for the snapshot).
	refOwner := uuid.NewString()
	if _, err = db.Exec(`INSERT INTO users(id,mobile,full_name,role) VALUES($1::uuid,'09999999999','fin011 g12 referrer owner','seller')`, refOwner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO stores(id,name,slug,owner_type,owner_name,owner_user_id,active_plan_id,referral_code) VALUES('fin011-g12-referrer','fin011 g12 referrer','fin011-g12-referrer','dropshipper','fin011 g12 referrer',$1::uuid,'trial','fin011-g12-referrer')`, refOwner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE stores SET referrer_seller_id='fin011-g12-referrer' WHERE id='cap-store-2'`); err != nil {
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
	quoteReq := httptest.NewRequest("POST", "/api/v1/shipping/quote", strings.NewReader(`{"items":[`+items+`],"destination":{"province":"Tehran","city":"Tehran","address":"Synthetic FIN011 address","postalCode":"1111111111"}}`)).WithContext(ctx)
	quote := httptest.NewRecorder()
	handleShippingQuote(quote, quoteReq, ctx, rp.catalogRepo, rp.supplierRepo, db)
	if quote.Code != 200 {
		t.Fatalf("canonical fixture quote %d %s", quote.Code, quote.Body.String())
	}
	var q struct{ QuoteID string }
	json.Unmarshal(quote.Body.Bytes(), &q)
	createOrder := func() string {
		body := `{"storeId":"cap-store-2","items":[` + items + `],"customerInfo":{"fullName":"Synthetic FIN011 customer","phone":"09900000001","province":"Tehran","city":"Tehran","address":"Synthetic FIN011 address","postalCode":"1111111111"},"paymentMethod":"cash","shippingQuoteId":"` + q.QuoteID + `"}`
		req := httptest.NewRequest("POST", "/api/v1/orders", strings.NewReader(body)).WithContext(ctx)
		out := httptest.NewRecorder()
		handleCreateOrder(out, req, ctx, rp.orderRepo, rp.catalogRepo, rp.storeRepo, rp.shipmentRepo, rp.pricingRepo, rp.supplierRepo, db)
		if out.Code != 201 {
			t.Fatalf("create order %d %s", out.Code, out.Body.String())
		}
		var created struct{ ID string }
		if err := json.Unmarshal(out.Body.Bytes(), &created); err != nil || created.ID == "" {
			t.Fatalf("create order response has no id: %s err=%v", out.Body.String(), err)
		}
		return created.ID
	}
	readCommission := func(orderID string) (platformProfit, commission int64, referrer string) {
		var ref sql.NullString
		if err := db.QueryRow(`SELECT platform_profit, referral_commission, referrer_seller_id FROM orders WHERE id=$1`, orderID).Scan(&platformProfit, &commission, &ref); err != nil {
			t.Fatalf("read order %s: %v", orderID, err)
		}
		if ref.Valid {
			referrer = ref.String
		}
		return platformProfit, commission, referrer
	}

	// 1. Referral OFF (explicit): the order snapshots the referrer but
	// accrues zero commission.
	if _, err = db.Exec(`UPDATE platform_settings SET referral_is_enabled=false WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	offID := createOrder()
	if profit, commission, referrer := readCommission(offID); commission != 0 || referrer != "fin011-g12-referrer" {
		t.Fatalf("referral OFF: commission=%d referrer=%q want 0/fin011-g12-referrer (snapshot stored, nothing accrued)", commission, referrer)
	} else if profit != 10000 {
		t.Fatalf("referral OFF: platform_profit=%d want 10000 (base must hold with the flag off too)", profit)
	}

	// 2. Referral ON at 2.00%: the commission is TruncatedPercent of a
	// re-proven base.
	if _, err = db.Exec(`UPDATE platform_settings SET referral_is_enabled=true, referral_commission_percent=2.00 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	onID := createOrder()
	profit, commission, referrer := readCommission(onID)
	if referrer != "fin011-g12-referrer" {
		t.Fatalf("referral ON: referrer snapshot=%q want fin011-g12-referrer", referrer)
	}
	// Base re-proof 1: the order's platform profit equals the sum of
	// its per-line platform fees.
	var lineFees int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(platform_fee*quantity),0) FROM order_items WHERE order_id=$1`, onID).Scan(&lineFees); err != nil {
		t.Fatal(err)
	}
	if profit != lineFees {
		t.Fatalf("base NOT proven: platform_profit=%d but sum(platform_fee*quantity)=%d", profit, lineFees)
	}
	// Base re-proof 2: the hand derivation from the seed inputs
	// (cost 100,000, platform_pct 10% -> fee 10,000).
	if profit != 10000 {
		t.Fatalf("base NOT proven: platform_profit=%d want hand-derived 10000 (MarkedUpPrice(100000, 10%%) = 110,000)", profit)
	}
	if commission != 200 {
		t.Fatalf("commission=%d want 200 (TruncatedPercent(10000, 2.0))", commission)
	}
}
