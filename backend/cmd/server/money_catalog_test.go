package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
)

func TestQuarantinedVariantHasNoPublicPrice(t *testing.T) {
	badCost := int64(-1)
	p := domain.Product{ID: "p", SupplierBaseCost: 100000}
	variants := []domain.ProductVariant{{ID: "bad", SupplierBaseCost: &badCost}}
	settings := pricing.DefaultPlatformPricingSettings()
	prices := priceVariantsForResponse(p, variants, domain.Store{}, settings, nil, nil, nil)
	response := buildProductResponse(p, pricing.ResolveReferencePricing(p, domain.Store{}, settings, nil), variants, prices)
	if _, exists := response.VariantPrices["bad"]; exists || len(response.Variants) != 0 {
		t.Fatalf("quarantined variant advertised: %+v", response)
	}
}

func TestMoneyCatalogQuarantineCurrentSchema(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable current schema")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// TEST-02 round 4: the catalog list needs the central store for its
	// scope (resolveListStore returns nil without it -> 500), but an
	// earlier fixture DROP TABLEs stores without restoring the
	// migrated pre-seed row. Restore it — same values as migration
	// 000002_seed_data.up.sql.
	if _, err = db.Exec(`INSERT INTO stores (id, name, slug, owner_type, owner_name, profit_margin_percent, active_plan_id, signed_contract_version, total_sales) VALUES ('store-central', 'فروشگاه رسمی مرکزی سلرشو', 'central', 'central', 'مدیریت مرکزی سلرشو', 0.00, 'vip', 2, 184500000) ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	var productID string
	if err = db.QueryRow(`SELECT id FROM products WHERE is_active ORDER BY id LIMIT 1`).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO platform_pct_products(product_id,pct) VALUES($1,1.001) ON CONFLICT(product_id) DO UPDATE SET pct=1.001`, productID); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM platform_pct_products WHERE product_id=$1`, productID)
	catalog := repository.NewCatalogRepository(db)
	stores := repository.NewStoreRepository(db)
	prices := repository.NewPricingRepository(db)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	rec := httptest.NewRecorder()
	handleProductsList(rec, req, req.Context(), catalog, stores, prices)
	if rec.Code != http.StatusOK {
		t.Fatalf("list=%d %s", rec.Code, rec.Body.String())
	}
	// PERF-01: paged envelope; the bare-array shape is retired.
	var listEnvelope struct {
		Items []ProductResponse `json:"items"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatal(err)
	}
	products := listEnvelope.Items
	for _, p := range products {
		if p.ID == productID {
			t.Fatal("quarantined product advertised in catalog")
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/products/"+productID, nil)
	rec = httptest.NewRecorder()
	handleProductDetail(rec, req, req.Context(), catalog, stores, prices, productID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("detail=%d %s", rec.Code, rec.Body.String())
	}
}

func TestQuarantinedProductDetailRejectsBeforeCatalogRead(t *testing.T) {
	_, err := assembleProductDetail(context.Background(), nil, nil, nil, nil, pricing.DefaultPlatformPricingSettings(), domain.Product{SupplierBaseCost: -1}, false)
	if !errors.Is(err, pricing.ErrInvalidMoney) {
		t.Fatalf("detail error=%v", err)
	}
}

func TestShippingQuoteRejectsOverflowInsteadOfZeroFare(t *testing.T) {
	for _, line := range []shippingQuoteLine{{ProductID: "p", Quantity: 2, WeightG: math.MaxInt}, {ProductID: "p", Quantity: 1, WeightG: 1, LengthMM: math.MaxInt, WidthMM: math.MaxInt, HeightMM: math.MaxInt}} {
		quote := computeShippingQuote([]shippingQuoteLine{line}, shippingRates{BaseRateRial: math.MaxInt64, PerKgRial: math.MaxInt64, VolumetricDiv: 1}, "fixture")
		if quote.PricingError == nil || quote.QuoteID != "" {
			t.Fatalf("invalid quote became authoritative: %+v", quote)
		}
	}
}

func TestTapinRialConversionNearLimit(t *testing.T) {
	if got := rialToToman(math.MaxInt64); got != 922337203685477581 {
		t.Fatalf("rial boundary converted to %d", got)
	}
}

func TestTapinGoodsRejectConversionAndAggregateOverflow(t *testing.T) {
	for _, lines := range [][]shippingQuoteLine{{{Quantity: 1, UnitPriceToman: math.MaxInt64}}, {{Quantity: 2, UnitPriceToman: math.MaxInt64 / 10}}, {{Quantity: 2, WeightG: math.MaxInt}}, {{Quantity: 1, UnitPriceToman: -1}}} {
		if _, _, _, err := checkedTapinGoods(lines); err == nil {
			t.Fatal("unrepresentable goods accepted")
		}
	}
	products, weight, value, err := checkedTapinGoods([]shippingQuoteLine{{Quantity: 2, WeightG: 500, UnitPriceToman: 100}})
	if err != nil || len(products) != 1 || products[0].AmountPerCount != 1000 || weight != 1000 || value != 2000 {
		t.Fatalf("goods mismatch: %+v %d/%d %v", products, weight, value, err)
	}
}
