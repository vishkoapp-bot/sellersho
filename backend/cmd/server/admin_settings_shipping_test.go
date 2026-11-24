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
	// The handler reads unqualified platform_settings. Shadow it only
	// for this fixture's connection; other server tests share the migrated
	// public settings table, including pricing and settlement columns.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`
		CREATE TEMP TABLE platform_settings (
			id INTEGER PRIMARY KEY,
			supplier_return_postal_charge BIGINT,
			default_item_weight_grams INTEGER NOT NULL
		);
	`); err != nil {
		t.Fatalf("create platform_settings fixture: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO pg_temp.platform_settings (id, supplier_return_postal_charge, default_item_weight_grams)
		VALUES (1, $1, 300)
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
