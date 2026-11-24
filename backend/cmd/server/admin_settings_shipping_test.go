package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/testdb"
)

func setupAdminShippingSettings(t *testing.T, charge any) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEAM04_TEST_DATABASE_URL to run the shipping settings handler tests")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	var oldCharge, oldWeight sql.NullInt64
	row := db.QueryRow(`SELECT supplier_return_postal_charge, default_item_weight_grams
		FROM public.platform_settings WHERE id = 1`)
	if err := row.Scan(&oldCharge, &oldWeight); err != nil {
		t.Fatalf("read canonical platform_settings fixture: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`UPDATE public.platform_settings
			SET supplier_return_postal_charge = $1, default_item_weight_grams = $2 WHERE id = 1`, oldCharge, oldWeight); err != nil {
			t.Errorf("restore platform_settings fixture: %v", err)
		}
	})
	if _, err := db.Exec(`
		UPDATE public.platform_settings SET supplier_return_postal_charge = $1, default_item_weight_grams = 300 WHERE id = 1
	`, charge); err != nil {
		t.Fatalf("seed platform_settings fixture: %v", err)
	}
	return db
}

func requestAdminShippingSettings(db *sql.DB, method, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/api/v1/admin/settings/shipping", strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	handleT01AdminSettingsShipping(response, request, db)
	return response
}

func assertAdminShippingSettings(t *testing.T, response *httptest.ResponseRecorder, wantCharge string) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, ok := body["supplierReturnPostalCharge"]; !ok || string(got) != wantCharge {
		t.Errorf("supplierReturnPostalCharge = %s (present: %t), want %s", got, ok, wantCharge)
	}
	if got := string(body["defaultItemWeightGrams"]); got != "300" {
		t.Errorf("defaultItemWeightGrams = %s, want 300", got)
	}
}

func TestAdminShippingSettingsGetPreservesNull(t *testing.T) {
	db := setupAdminShippingSettings(t, nil)
	assertAdminShippingSettings(t, requestAdminShippingSettings(db, http.MethodGet, ""), "null")
}

func TestAdminShippingSettingsFixturePreservesCanonicalTable(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("TEAM04_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	type snapshot struct {
		tableOID       int64
		charge, weight sql.NullInt64
	}
	read := func() (s snapshot) {
		t.Helper()
		if err := db.QueryRow(`SELECT 'public.platform_settings'::regclass::oid,
				supplier_return_postal_charge, default_item_weight_grams
			FROM public.platform_settings WHERE id=1`).Scan(&s.tableOID, &s.charge, &s.weight); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := read()
	t.Run("temporary value", func(t *testing.T) {
		fixture := setupAdminShippingSettings(t, nil)
		assertAdminShippingSettings(t, requestAdminShippingSettings(fixture, http.MethodGet, ""), "null")
	})
	if after := read(); after != before {
		t.Fatalf("shipping fixture changed canonical settings: before=%+v after=%+v", before, after)
	}
}

func TestAdminShippingSettingsPutAcceptsZeroAsFree(t *testing.T) {
	db := setupAdminShippingSettings(t, nil)
	assertAdminShippingSettings(t, requestAdminShippingSettings(db, http.MethodPut, `{"supplierReturnPostalCharge": 0}`), "0")
	assertAdminShippingSettings(t, requestAdminShippingSettings(db, http.MethodGet, ""), "0")
}

func TestAdminShippingSettingsPutAcceptsConfiguredCharge(t *testing.T) {
	db := setupAdminShippingSettings(t, nil)
	assertAdminShippingSettings(t, requestAdminShippingSettings(db, http.MethodPut, `{"supplierReturnPostalCharge": 200000}`), "200000")
	assertAdminShippingSettings(t, requestAdminShippingSettings(db, http.MethodGet, ""), "200000")
}

func TestAdminShippingSettingsPutNullClearsToUnconfigured(t *testing.T) {
	db := setupAdminShippingSettings(t, 200000)
	assertAdminShippingSettings(t, requestAdminShippingSettings(db, http.MethodPut, `{"supplierReturnPostalCharge": null}`), "null")
	assertAdminShippingSettings(t, requestAdminShippingSettings(db, http.MethodGet, ""), "null")
}

func TestAdminShippingSettingsPutRejectsInvalidValues(t *testing.T) {
	db := setupAdminShippingSettings(t, nil)
	for name, body := range map[string]string{
		"negative":    `{"supplierReturnPostalCharge": -1}`,
		"fractional":  `{"supplierReturnPostalCharge": 1.5}`,
		"string":      `{"supplierReturnPostalCharge": "200000"}`,
		"unknown key": `{"unknownKey": 1}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := requestAdminShippingSettings(db, http.MethodPut, body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
			}
		})
	}
}
