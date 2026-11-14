package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drup/platform/internal/pricing"
)

func TestMoneyJSONPreservesDecimalPrecisionBeforeFloatTransport(t *testing.T) {
	for _, body := range []string{`{"platformPct":0.29000000000000001}`, `{"mode":"percent","value":5.250000000000001}`, `{"mode":"fixed","value":100.00000000000001}`, `{"discounts":{"p":0.01000000000000000001}}`} {
		var target any
		if err := decodeMoneyJSON(strings.NewReader(body), &target); err == nil {
			t.Fatalf("accepted lossy decimal input %s", body)
		}
	}
	var settings pricing.PlatformPricingSettings
	if err := decodeMoneyJSON(strings.NewReader(`{"platformPct":0.29,"centralProfitPct":5.25}`), &settings); err != nil || settings.PlatformPct != .29 {
		t.Fatalf("valid exact decimal rejected: %+v %v", settings, err)
	}
}

func TestAddonRejectsInvalidMoneyBeforeWrite(t *testing.T) {
	for _, body := range []string{`{"priceMode":"fixed","priceValue":0.5}`, `{"priceMode":"percent","priceValue":0.001}`, `{"priceMode":"percent","priceValue":0.29000000000000001}`, `{"priceMode":"fixed","priceValue":9007199254740992}`} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		if _, ok := decodeAddonBody(w, r); ok {
			t.Fatalf("invalid addon accepted: %s", body)
		}
	}
}

func TestCatalogMoneyDecodeLeavesNonfinancialAttributeValuesAlone(t *testing.T) {
	var body any
	if err := decodeMoneyJSON(strings.NewReader(`{"attributes":{"dimension":{"value":1.2345}},"supplierBaseCost":100}`), &body, true); err != nil {
		t.Fatal(err)
	}
	if err := decodeMoneyJSON(strings.NewReader(`{"supplierBaseCost":100.00000000000001}`), &body, true); err == nil {
		t.Fatal("fractional cost accepted")
	}
}

func TestMoneyJSONValidatesCaseFoldAliasesAndDuplicateKeys(t *testing.T) {
	for _, body := range []string{`{"PlatformPct":1.00000000000000001}`, `{"MAXMARGINPCT":1.001}`, `{"PriceMode":"fixed","PriceValue":1.00000000000000001}`, `{"platformPct":1,"PlatformPct":2}`, `{"platformPct":1,"platformPct":2}`, `{"Mode":"percent","mode":"fixed","value":1}`} {
		var target any
		if err := decodeMoneyJSON(strings.NewReader(body), &target); err == nil {
			t.Fatalf("unchecked alias or duplicate accepted: %s", body)
		}
	}
	var settings pricing.PlatformPricingSettings
	if err := decodeMoneyJSON(strings.NewReader(`{"PlatformPct":0.29}`), &settings); err != nil || settings.PlatformPct != .29 {
		t.Fatalf("valid Go alias rejected: %+v %v", settings, err)
	}
}
