package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
)

func TestMoneyHTTPAliasesRejectedBeforeSettingsPersistence(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable current schema")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var before float64
	if err = db.QueryRow(`SELECT platform_pct FROM platform_settings WHERE id=1`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`UPDATE platform_settings SET platform_pct=$1 WHERE id=1`, before)
	for _, body := range []string{`{"PlatformPct":1.00000000000000001}`, `{"platformPct":1,"PlatformPct":2}`, `{"platformPct":1,"platformPct":2}`} {
		req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		res := httptest.NewRecorder()
		handleAdminPricingSettings(res, req, repository.NewPricingRepository(db))
		var after float64
		if err = db.QueryRow(`SELECT platform_pct FROM platform_settings WHERE id=1`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if res.Code != http.StatusBadRequest || after != before {
			t.Fatalf("unsafe settings wrote: status=%d before=%v after=%v body=%s", res.Code, before, after, body)
		}
	}
}

func TestMoneyHTTPAdminCeilingRejectsBeforePersistence(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable current schema")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var productID string
	if err = db.QueryRow(`SELECT id FROM products ORDER BY id LIMIT 1`).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO product_margin_ceilings(product_id,max_margin_pct) VALUES($1,35) ON CONFLICT(product_id) DO UPDATE SET max_margin_pct=35`, productID); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM product_margin_ceilings WHERE product_id=$1`, productID)
	for _, body := range []string{`{"maxMarginPct":1.001}`, `{"MAXMARGINPCT":1.00000000000000001}`, `{"maxMarginPct":10,"markupType":"percent","markupValue":1.001}`} {
		req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		res := httptest.NewRecorder()
		wr69AdminProductPricingPut(res, req, db, productID)
		var after float64
		if err = db.QueryRow(`SELECT max_margin_pct FROM product_margin_ceilings WHERE product_id=$1`, productID).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if res.Code != http.StatusBadRequest || after != 35 {
			t.Fatalf("unsafe ceiling wrote: status=%d after=%v body=%s", res.Code, after, body)
		}
	}
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"MaxMarginPct":5.25}`))
	res := httptest.NewRecorder()
	wr69AdminProductPricingPut(res, req, db, productID)
	var valid float64
	if err = db.QueryRow(`SELECT max_margin_pct FROM product_margin_ceilings WHERE product_id=$1`, productID).Scan(&valid); err != nil || valid != 5.25 || res.Code != http.StatusOK {
		t.Fatalf("valid ceiling rejected: status=%d value=%v err=%v", res.Code, valid, err)
	}
}
