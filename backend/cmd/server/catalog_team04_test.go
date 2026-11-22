package main

// Team 04 live parity unit tests (no database required). Integration
// behaviour (active-only list, brand exact match, inactive detail 200,
// price-bounds shape, variant prices, search logging) is covered by
// catalog_team04_http_test.go against a disposable Postgres, and by
// the repository integration test.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drup/platform/internal/catalogpage"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
)

func TestTeam04DeriveDiscountPercent(t *testing.T) {
	cases := []struct {
		original, price int64
		want            int
	}{
		{72500000, 63800000, 12}, // live gold bracelet, central
		{1850000, 1320000, 29},   // (1850000-1320000)/1850000 = 28.6% -> 29
		{1000000, 1000000, 0},
		{1000000, 1100000, 0}, // price above original: no negative discount
		{0, 500000, 0},
	}
	for _, c := range cases {
		if got := deriveDiscountPercent(c.original, c.price); got != c.want {
			t.Errorf("deriveDiscountPercent(%d, %d) = %d, want %d", c.original, c.price, got, c.want)
		}
	}
}

func TestTeam04NormalizeSearchTerm(t *testing.T) {
	// Arabic Yeh/Kaf map to Persian Yeh/Kaf; whitespace collapses.
	if got := normalizeSearchTerm("  سلام  كتاب ي "); got != "سلام کتاب ی" {
		t.Errorf("normalizeSearchTerm = %q", got)
	}
	if got := normalizeSearchTerm("طلا"); got != "طلا" {
		t.Errorf("normalizeSearchTerm(طلا) = %q", got)
	}
}

func TestTeam04SpnURLHelpers(t *testing.T) {
	spn := int64(10009705)
	p := domain.Product{ID: "prod-1793551155965", Slug: "test-slug", SpnNumber: &spn}
	if got := BuildCanonicalProductURL(p, nil); got != "/product/spn-10009705/test-slug/" {
		t.Errorf("BuildCanonicalProductURL = %q", got)
	}
	variantNo := 20000001
	if got := BuildCanonicalProductURL(p, &variantNo); got != "/product/spn-10009705/test-slug/?spnc=20000001" {
		t.Errorf("BuildCanonicalProductURL with spnc = %q", got)
	}
	if got := BuildCanonicalProductURL(domain.Product{ID: "prod-x"}, nil); got != "/product/prod-x" {
		t.Errorf("BuildCanonicalProductURL without spn = %q", got)
	}
	// Persian digits in the path parse to the same number.
	n, slug, ok := ParseCanonicalProductPath("/product/spn-۱۰۰۰۹۷۰۵/test-slug/")
	if !ok || n != 10009705 || slug != "test-slug" {
		t.Errorf("ParseCanonicalProductPath Persian digits = (%d, %q, %v)", n, slug, ok)
	}
	if _, _, ok := ParseCanonicalProductPath("/product/prod-1"); ok {
		t.Errorf("ParseCanonicalProductPath accepted a non-spn path")
	}
	if _, err := parseSpnSegment("spn-abc"); err == nil {
		t.Errorf("parseSpnSegment accepted non-digits")
	}
	if n, err := parseSpnSegment("spn-۱۰۰"); err != nil || n != 100 {
		t.Errorf("parseSpnSegment Persian digits = (%d, %v)", n, err)
	}
}

func TestTeam04DefaultVariant(t *testing.T) {
	variants := []domain.ProductVariant{
		{ID: "v1", Spnc: "AAAA0001"},
		{ID: "v2", Spnc: "BBBB0002", IsDefault: true},
		{ID: "v3", Spnc: "CCCC0003", IsDefault: true},
	}
	if got := DefaultVariantForResponse(variants); got == nil || got.ID != "v2" {
		t.Errorf("DefaultVariantForResponse picked %v, want v2 (first default)", got)
	}
	noDefault := []domain.ProductVariant{{ID: "v1"}, {ID: "v2"}}
	if got := DefaultVariantForResponse(noDefault); got == nil || got.ID != "v1" {
		t.Errorf("DefaultVariantForResponse without default picked %v, want v1", got)
	}
	if got := DefaultVariantForResponse(nil); got != nil {
		t.Errorf("DefaultVariantForResponse(nil) = %v, want nil", got)
	}
}

func keysOf(t *testing.T, v interface{}) map[string]bool {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

func TestTeam04ProductResponseFrozenKeys(t *testing.T) {
	p := domain.Product{
		ID: "prod-1", Title: "T", Slug: "t", CategorySlug: "gold-silver",
		OriginalPrice: 72500000, SupplierBaseCost: 58000000, IsOriginal: true,
		Stock: 5, ImageURL: "img", Description: "d", WarehouseName: "w",
		NicheIDs: []string{}, IsActive: true, Brand: "brand",
	}
	res := pricing.ReferenceResolution{OriginalPrice: 72500000, FinalPrice: 63800000, PlatformFee: 0}
	resp := buildProductResponse(p, res, []domain.ProductVariant{{ID: "v1", Spnc: "FE16A2E1", Name: "default", Stock: 5, IsDefault: true}}, map[string]VariantPriceResponse{})
	got := keysOf(t, resp)
	want := []string{"productType", "schemaVersion", "id", "title", "slug", "category", "originalPrice", "discountEligible", "discountPercent", "price", "platformFeeF", "isOriginal", "stock", "image", "description", "warehouse", "nicheIds", "isActive", "brand", "variants", "variantPrices"}
	if len(got) != len(want) {
		t.Fatalf("list keys = %v, want exactly %d keys", got, len(want))
	}
	for _, k := range want {
		if !got[k] {
			t.Errorf("list response missing key %q", k)
		}
	}
	for _, forbidden := range []string{"supplierId", "supplierName", "baseCostB", "profitToman", "marginPercent", "installmentPrice"} {
		if got[forbidden] {
			t.Errorf("list response must not contain %q (D21)", forbidden)
		}
	}
	if resp.DiscountPercent != 12 || resp.Price != 63800000 {
		t.Errorf("list pricing = discount %d price %d, want 12 / 63800000", resp.DiscountPercent, resp.Price)
	}
}

func TestTeam04VariantPricesDiffer(t *testing.T) {
	// Mirrors the live gold-bracelet example (T04 section 2): the
	// default variant prices from the product cost, the bejewelled
	// variant from its own higher cost, via Team 02's resolver.
	p := domain.Product{
		ID: "prod-1793551155965", Title: "T", Slug: "t", CategorySlug: "gold-silver",
		OriginalPrice: 72500000, SupplierBaseCost: 58000000,
	}
	settings := &pricing.PlatformPricingSettings{PlatformPct: 0, CentralProfitPct: 10, DefaultMargin: 20}
	highCost := int64(64000000)
	variants := []domain.ProductVariant{
		{ID: "v-default", Spnc: "FE16A2E1", Name: "default", Stock: 5, IsDefault: true},
		{ID: "v-jewel", Spnc: "55F08797", Name: "نگین‌دار", Stock: 2, SupplierBaseCost: &highCost},
	}
	// FIX 4: live keys variantPrices by variant ID (UUID), not spnc,
	// and serves originalPrice per variant (team 04 G1 / team 02 G6).
	prices := priceVariantsForResponse(p, variants, domain.Store{}, settings, nil, map[string]pricing.StoreVariantOverride{}, nil)
	if prices["v-default"].Price != 63800000 || prices["v-default"].DiscountPercent != 12 || prices["v-default"].OriginalPrice != 72500000 {
		t.Errorf("default variant price = %+v, want 63800000/12/72500000", prices["v-default"])
	}
	if prices["v-jewel"].Price != 70400000 || prices["v-jewel"].DiscountPercent != 3 || prices["v-jewel"].OriginalPrice != 72500000 {
		t.Errorf("jewel variant price = %+v, want 70400000/3/72500000", prices["v-jewel"])
	}
	if _, ok := prices["FE16A2E1"]; ok {
		t.Errorf("variantPrices must not be keyed by spnc anymore")
	}
}

func TestTeam04ComputePriceBounds(t *testing.T) {
	b := computePriceBounds([]int64{198000, 63800000, 500000})
	if b.MinPrice != 198000 || b.MaxPrice != 63800000 || b.Count != 3 {
		t.Errorf("computePriceBounds = %+v", b)
	}
	b = computePriceBounds(nil)
	if b.MinPrice != 0 || b.MaxPrice != 0 || b.Count != 0 {
		t.Errorf("computePriceBounds(nil) = %+v, want 0/0/0", b)
	}
}

func TestTeam04DetailResponseKeys(t *testing.T) {
	// FIX 12: live's detail DTO carries exactly the 20 live keys.
	// specs/images are omitempty extensions: ABSENT when empty (the
	// production shape — assembleProductDetail leaves them nil, so the
	// spn route renders exactly like the prod route), present only
	// when data exists. spnNumber stays as the flagged extension.
	live := []string{"productType", "schemaVersion", "id", "title", "slug", "category", "originalPrice", "discountEligible", "discountPercent", "price", "platformFeeF", "isOriginal", "stock", "image", "description", "warehouse", "supplierWarranty", "nicheIds", "isActive", "storeCount", "variants", "variantPrices"}
	spn := int64(10009705)
	d := ProductDetailResponse{SpnNumber: &spn}
	got := keysOf(t, d)
	if len(got) != len(live)+1 {
		t.Fatalf("detail keys = %d, want %d live + spnNumber", len(got), len(live))
	}
	for _, k := range live {
		if !got[k] {
			t.Errorf("detail response missing live key %q", k)
		}
	}
	if !got["spnNumber"] {
		t.Errorf("detail response missing flagged extension key %q", "spnNumber")
	}
	if got["specs"] || got["images"] {
		t.Errorf("nil specs/images must be omitted like live")
	}
	// Non-empty extensions still serialize.
	d2 := ProductDetailResponse{Specs: map[string]interface{}{"k": "v"}, Images: []string{"a.webp"}}
	got2 := keysOf(t, d2)
	if !got2["specs"] || !got2["images"] {
		t.Errorf("non-empty specs/images must serialize")
	}
	for _, forbidden := range []string{"supplierId", "supplierName", "baseCostB", "profitToman", "marginPercent", "installmentPrice", "brand"} {
		if got[forbidden] {
			t.Errorf("detail response must not contain %q (D21)", forbidden)
		}
	}
}

// Batch 2, A4: parseCatalogPageFilter rejects an inverted price range
// with ErrInvalid (400), accepts a valid range, equal bounds (a
// single-price point), and a one-sided bound.
func TestParseCatalogPageFilterPriceRange(t *testing.T) {
	parse := func(query string) (catalogpage.Filter, error) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/products?"+query, nil)
		return parseCatalogPageFilter(req)
	}
	if _, err := parse("min_price=50000000&max_price=1000000"); !errors.Is(err, catalogpage.ErrInvalid) {
		t.Fatalf("inverted range err = %v, want ErrInvalid", err)
	}
	f, err := parse("min_price=1000000&max_price=50000000")
	if err != nil {
		t.Fatalf("valid range err = %v", err)
	}
	if f.MinPrice == nil || *f.MinPrice != 1000000 || f.MaxPrice == nil || *f.MaxPrice != 50000000 {
		t.Fatalf("valid range parsed = %+v", f)
	}
	if _, err := parse("min_price=1000000&max_price=1000000"); err != nil {
		t.Fatalf("equal bounds err = %v, want nil", err)
	}
	if _, err := parse("min_price=-1&max_price=1000000"); !errors.Is(err, catalogpage.ErrInvalid) {
		t.Fatalf("negative min err = %v, want ErrInvalid", err)
	}
	if _, err := parse("min_price=1000000"); err != nil {
		t.Fatalf("min-only err = %v, want nil", err)
	}
	if _, err := parse("max_price=1000000"); err != nil {
		t.Fatalf("max-only err = %v, want nil", err)
	}
}

// Batch 2, A1/E2: the match-time normaliser strips ZWNJ on top of the
// logging normalisation, agrees exactly with the repository-side helper
// (catalogpage.NormalizeSearchForMatch), and never changes the logging
// normaliser itself (E2-5: a 1-char query still matches; only logging
// is suppressed).
func TestNormalizeSearchTermForMatch(t *testing.T) {
	for _, term := range []string{"\u0637\u0644\u0627", "  \u0633\u0644\u0627\u0645  \u0643\u062a\u0627\u0628 \u064a ", "هندز\u200cفری", "هندزفر\u064a", "هندز فری", "ساعت  هوشمند", "\u200c"} {
		if got, want := normalizeSearchTermForMatch(term), catalogpage.NormalizeSearchForMatch(term); got != want {
			t.Errorf("normalizeSearchTermForMatch(%q) = %q, catalogpage = %q", term, got, want)
		}
	}
	if got := normalizeSearchTermForMatch("هندز\u200cفری"); got != "هندزفری" {
		t.Errorf("normalizeSearchTermForMatch(ZWNJ) = %q, want \u0647\u0646\u062f\u0632\u0641\u0631\u06cc", got)
	}
	// The logging normaliser keeps the ZWNJ: its behaviour is unchanged.
	if got := normalizeSearchTerm("هندز\u200cفری"); got != "هندز\u200cفری" {
		t.Errorf("normalizeSearchTerm must keep ZWNJ, got %q", got)
	}
	// E2 threshold lockstep: the logging guard (here) and the insight
	// hardening (supplierSearchInsightMinTermRunes, asserted == 2 in the
	// repository tests) must both stay at 2.
	if minSearchLogTermRunes != 2 {
		t.Fatalf("minSearchLogTermRunes = %d, want 2", minSearchLogTermRunes)
	}
}
