package main

// CHECKOUT-02 backend tests (Q6 / D-12, G-12 backend drill).
//
// Q6: allocations join the server quoteId hash (shippingQuoteIDFor);
// no TTL/version. Q2 needs no server change — the CHECKOUT-01
// fingerprint stays inclusive of shippingQuoteId, so this file
// deliberately does NOT touch it.
//
// The quote endpoint (handleShippingQuote) and the order-time
// recompute (handleCreateOrder) both call computeShippingQuote +
// priceTapinQuoteLegs over lines built the same way (request order,
// resolveQuoteOrigins, sortQuoteLines), so the pure tests below prove
// the shared hash and the HTTP tests prove the submit-time gate.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

var checkout02Rates = shippingRates{BaseRateRial: 100000, PerKgRial: 20000, VolumetricDiv: 5000, DefaultWeightG: 500}

const checkout02Dest = "تهران|تهران|خیابان|1234567890"

// sameLegs asserts two quotes price identically — same legs (fare and
// weight) and same total — so any quoteId difference can only come
// from the allocations.
func checkout02SameLegs(t *testing.T, a, b shippingQuote) {
	t.Helper()
	if len(a.Legs) != len(b.Legs) {
		t.Fatalf("legs = %d vs %d, want identical legs for an allocations-only test", len(a.Legs), len(b.Legs))
	}
	for i := range a.Legs {
		if a.Legs[i].Fare != b.Legs[i].Fare || a.Legs[i].WeightGrams != b.Legs[i].WeightGrams || a.Legs[i].OriginID != b.Legs[i].OriginID {
			t.Fatalf("leg %d differs: %+v vs %+v, want identical legs", i, a.Legs[i], b.Legs[i])
		}
	}
	if a.TotalShipping != b.TotalShipping {
		t.Fatalf("total = %d vs %d, want identical totals", a.TotalShipping, b.TotalShipping)
	}
}

// (a) The quote id differs when allocations differ, with identical
// legs, total and destination.
func TestCheckout02QuoteIDDiffersWhenAllocationsDiffer(t *testing.T) {
	t.Run("different product same weight", func(t *testing.T) {
		x := computeShippingQuote([]shippingQuoteLine{
			{ProductID: "p1", Quantity: 2, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
		}, checkout02Rates, checkout02Dest)
		y := computeShippingQuote([]shippingQuoteLine{
			{ProductID: "p2", Quantity: 2, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
		}, checkout02Rates, checkout02Dest)
		checkout02SameLegs(t, x, y)
		if x.QuoteID == y.QuoteID {
			t.Fatalf("quote id identical (%s) for different products with same legs/total/destination; allocations must join the hash", x.QuoteID)
		}
	})
	t.Run("different variant same product", func(t *testing.T) {
		x := computeShippingQuote([]shippingQuoteLine{
			{ProductID: "p1", VariantID: "v1", Quantity: 1, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
		}, checkout02Rates, checkout02Dest)
		y := computeShippingQuote([]shippingQuoteLine{
			{ProductID: "p1", VariantID: "v2", Quantity: 1, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
		}, checkout02Rates, checkout02Dest)
		checkout02SameLegs(t, x, y)
		if x.QuoteID == y.QuoteID {
			t.Fatalf("quote id identical (%s) for different variants with same legs/total/destination", x.QuoteID)
		}
	})
	t.Run("same lines split differently", func(t *testing.T) {
		x := computeShippingQuote([]shippingQuoteLine{
			{ProductID: "p1", Quantity: 2, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
		}, checkout02Rates, checkout02Dest)
		y := computeShippingQuote([]shippingQuoteLine{
			{ProductID: "p1", Quantity: 1, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
			{ProductID: "p1", Quantity: 1, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
		}, checkout02Rates, checkout02Dest)
		checkout02SameLegs(t, x, y)
		if len(y.Allocations) != 2 {
			t.Fatalf("allocations = %d, want 2 for the split lines", len(y.Allocations))
		}
		if x.QuoteID == y.QuoteID {
			t.Fatalf("quote id identical (%s) for the same lines split differently", x.QuoteID)
		}
	})
	t.Run("quantity change still differs", func(t *testing.T) {
		x := computeShippingQuote([]shippingQuoteLine{
			{ProductID: "p1", Quantity: 1, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
		}, checkout02Rates, checkout02Dest)
		y := computeShippingQuote([]shippingQuoteLine{
			{ProductID: "p1", Quantity: 2, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
		}, checkout02Rates, checkout02Dest)
		if x.QuoteID == y.QuoteID {
			t.Fatalf("quote id identical (%s) for different quantities", x.QuoteID)
		}
	})
}

// (b) The submit-time recompute uses the same hash: a quote issued
// for allocations X is accepted for X (identical recompute) and
// rejected for Y (different recompute). Both paths call the same
// computeShippingQuote, so equality here is the accept/reject
// decision the order handler makes at order_handlers.go:892-894.
func TestCheckout02QuoteRecomputeUsesSameHash(t *testing.T) {
	linesX := []shippingQuoteLine{
		{ProductID: "p1", Quantity: 2, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
	}
	linesY := []shippingQuoteLine{
		{ProductID: "p2", Quantity: 2, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
	}
	issued := computeShippingQuote(linesX, checkout02Rates, checkout02Dest)
	recomputedSame := computeShippingQuote(linesX, checkout02Rates, checkout02Dest)
	if recomputedSame.QuoteID != issued.QuoteID {
		t.Fatalf("recompute for the same allocations differs: issued %s, recomputed %s", issued.QuoteID, recomputedSame.QuoteID)
	}
	recomputedOther := computeShippingQuote(linesY, checkout02Rates, checkout02Dest)
	if recomputedOther.QuoteID == issued.QuoteID {
		t.Fatalf("recompute for different allocations matches the issued id %s; the order gate would accept a stale quote", issued.QuoteID)
	}
}

// checkout02CleanupLots removes the fixture lots this test's setup
// seeded. The disposable DB is SHARED by every test in one `go test`
// run, and tapinTestSetup does not drop inventory_balances — so a
// second setup in the same run would re-seed the same
// (product, location, sellable) lots and violate
// inventory_balances_unallocated_unique. (Pre-existing latent issue:
// two existing tapin tests run together fail the same way, e.g.
// TestTapinQuoteUnconnectedLegKeepsFormula +
// TestTapinOrderCreationAcceptsTapinPricedQuote in one invocation.)
// Cleanup keeps the CHECKOUT-02 DB tests runnable together; orders
// and products are dropped by the next setup anyway.
func checkout02CleanupLots(t *testing.T, rp *tapinRepos) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := rp.db.Exec(`DELETE FROM product_units WHERE product_id IN ('p-tapin','p-heavy','p-plain','p-central','p-central2')`); err != nil {
			t.Errorf("cleanup fixture codes: %v", err)
		}
		if _, err := rp.db.Exec(`DELETE FROM inventory_balances WHERE product_id IN ('p-tapin','p-heavy','p-plain','p-central','p-central2')`); err != nil {
			t.Errorf("cleanup fixture lots: %v", err)
		}
	})
}

// checkout02OrderCount counts orders in the disposable test DB.
func checkout02OrderCount(t *testing.T, rp *tapinRepos) int {
	t.Helper()
	var n int
	if err := rp.db.QueryRow(`SELECT COUNT(*) FROM orders`).Scan(&n); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	return n
}

func checkout02OrderBody(items, quoteID string) string {
	return `{"storeId":"central","paymentMethod":"cash","customerInfo":{"fullName":"مشتری تست","phone":"09120000000","province":"قم","city":"قم","address":"خیابان تست","postalCode":"3711111111"},"items":[` + items + `],"shippingQuoteId":"` + quoteID + `"}`
}

// (c) G-12 backend drill: submit an order with a stale quoteId and
// assert the server answers 400 {"error":"shipping_quote_changed"}
// with no order created. The accept leg first proves a quote issued
// for these exact lines IS accepted by the same recompute.
func TestCheckout02StaleQuoteRejectedNoOrderCreated(t *testing.T) {
	rp := tapinTestSetup(t)
	checkout02CleanupLots(t, rp)
	newTapinMock(t)

	// Quote for the warehouse product p-central x1 (formula leg, no
	// Tapin call: unconnected supplier).
	rec := tapinQuoteRequest(t, rp, `{"productId":"p-central","quantity":1}`, tapinQuoteDestQom)
	if rec.Code != http.StatusOK {
		t.Fatalf("quote status = %d, body %s", rec.Code, rec.Body.String())
	}
	var q tapinQuoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil || q.QuoteID == "" {
		t.Fatalf("decode quote: %v (%s)", err, rec.Body.String())
	}

	// Accept leg: the same lines with the issued quoteId create exactly one order.
	before := checkout02OrderCount(t, rp)
	orec := tapinCreateOrder(t, rp, checkout02OrderBody(`{"productId":"p-central","quantity":1}`, q.QuoteID))
	_ = gap13OrderID(t, orec)
	if got := checkout02OrderCount(t, rp); got != before+1 {
		t.Fatalf("orders = %d, want %d after the accepted submit", got, before+1)
	}

	// Stale leg: the same quoteId submitted for DIFFERENT lines is rejected, no order created.
	bad := tapinCreateOrder(t, rp, checkout02OrderBody(`{"productId":"p-plain","quantity":1}`, q.QuoteID))
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "shipping_quote_changed") {
		t.Fatalf("stale quote: status %d body %s, want 400 shipping_quote_changed", bad.Code, bad.Body.String())
	}
	if got := checkout02OrderCount(t, rp); got != before+1 {
		t.Fatalf("orders = %d after the stale submit, want %d (no order created)", got, before+1)
	}
}

// End-to-end allocations proof at the HTTP level: two different
// warehouse products with the same weight and origin price
// identically, but their quoteIds differ — and the order gate
// rejects the cross-submitted id. p-central2 is seeded by this test
// only (the shared tapin fixture is untouched).
func TestCheckout02SameLegsDifferentAllocationsHTTP(t *testing.T) {
	rp := tapinTestSetup(t)
	checkout02CleanupLots(t, rp)
	newTapinMock(t)

	if _, err := rp.db.Exec(`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,weight_grams) VALUES ('p-central2','کالای انباری ۲','central-good-2','digital','sup-plain',500000,400000,0,TRUE,10,'img','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'برند',50000105,1000)`); err != nil {
		t.Fatalf("seed p-central2: %v", err)
	}
	seedShippingInventoryFixtureLot(t, rp.db, "p-central2", "sup-plain", "wh-central", "", 10)

	qa := tapinQuoteRequest(t, rp, `{"productId":"p-central","quantity":1}`, tapinQuoteDestQom)
	if qa.Code != http.StatusOK {
		t.Fatalf("quote A status = %d, body %s", qa.Code, qa.Body.String())
	}
	qb := tapinQuoteRequest(t, rp, `{"productId":"p-central2","quantity":1}`, tapinQuoteDestQom)
	if qb.Code != http.StatusOK {
		t.Fatalf("quote B status = %d, body %s", qb.Code, qb.Body.String())
	}
	var a, b tapinQuoteResponse
	if err := json.Unmarshal(qa.Body.Bytes(), &a); err != nil {
		t.Fatalf("decode quote A: %v", err)
	}
	if err := json.Unmarshal(qb.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode quote B: %v", err)
	}
	if len(a.Legs) != 1 || len(b.Legs) != 1 || a.Legs[0].Fare != b.Legs[0].Fare || a.Total != b.Total {
		t.Fatalf("quotes must price identically: A (%s, %d, %+v) vs B (%s, %d, %+v)", a.QuoteID, a.Total, a.Legs, b.QuoteID, b.Total, b.Legs)
	}
	if a.QuoteID == b.QuoteID {
		t.Fatalf("quote ids identical (%s) for different allocations with identical legs/total", a.QuoteID)
	}

	// Cross-submit: p-central2 lines with p-central's quoteId is stale.
	before := checkout02OrderCount(t, rp)
	bad := tapinCreateOrder(t, rp, checkout02OrderBody(`{"productId":"p-central2","quantity":1}`, a.QuoteID))
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "shipping_quote_changed") {
		t.Fatalf("cross-submitted quote: status %d body %s, want 400 shipping_quote_changed", bad.Code, bad.Body.String())
	}
	if got := checkout02OrderCount(t, rp); got != before {
		t.Fatalf("orders = %d after the cross-submit, want %d (no order created)", got, before)
	}

	// Matching submit: p-central2 lines with p-central2's quoteId succeeds.
	orec := tapinCreateOrder(t, rp, checkout02OrderBody(`{"productId":"p-central2","quantity":1}`, b.QuoteID))
	_ = gap13OrderID(t, orec)
	if got := checkout02OrderCount(t, rp); got != before+1 {
		t.Fatalf("orders = %d, want %d after the matching submit", got, before+1)
	}
}

func TestSpncxQuoteAndCheckoutCapCodedQuantity(t *testing.T) {
	rp := tapinTestSetup(t)
	checkout02CleanupLots(t, rp)
	newTapinMock(t)
	if _, err := rp.db.Exec(`UPDATE inventory_balances SET quantity=5 WHERE product_id='p-central' AND status='sellable'`); err != nil {
		t.Fatal(err)
	}
	if _, err := rp.db.Exec(`UPDATE product_units SET status='quarantined' WHERE product_id='p-central' AND sequence>3`); err != nil {
		t.Fatal(err)
	}
	threes := `{"productId":"p-central","quantity":3}`
	fours := `{"productId":"p-central","quantity":4}`
	bad := tapinQuoteRequest(t, rp, fours, tapinQuoteDestQom)
	if bad.Code != http.StatusConflict || !strings.Contains(bad.Body.String(), "inventory_origin_unavailable") {
		t.Fatalf("uncoded quote status=%d body=%s", bad.Code, bad.Body.String())
	}
	rec := tapinQuoteRequest(t, rp, threes, tapinQuoteDestQom)
	if rec.Code != http.StatusOK {
		t.Fatalf("coded quote status=%d body=%s", rec.Code, rec.Body.String())
	}
	var q tapinQuoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil || q.QuoteID == "" {
		t.Fatalf("coded quote: %v %s", err, rec.Body.String())
	}
	bad = tapinCreateOrder(t, rp, checkout02OrderBody(fours, q.QuoteID))
	if bad.Code != http.StatusConflict || checkout02OrderCount(t, rp) != 0 {
		t.Fatalf("uncoded checkout status=%d body=%s", bad.Code, bad.Body.String())
	}
	ok := tapinCreateOrder(t, rp, checkout02OrderBody(threes, q.QuoteID))
	if ok.Code != http.StatusCreated || checkout02OrderCount(t, rp) != 1 {
		t.Fatalf("coded checkout status=%d body=%s", ok.Code, ok.Body.String())
	}
}
