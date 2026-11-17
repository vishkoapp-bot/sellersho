package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestWarehouseMeasurementVerifiesExactSKUWithoutChangingParent(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	sku := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO product_variants(id,product_id,variant_name,spnc,stock,length_mm,width_mm,height_mm,weight_grams) VALUES($1,$2,'red','RED',0,100,100,100,1000)`, sku, f.product); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: f.user})
	request := httptest.NewRequest("POST", "/measurements", strings.NewReader(`{"variantId":"`+sku+`","lengthMm":100,"widthMm":100,"heightMm":100,"weightGrams":1000,"reason":"measured red SKU"}`))
	response := httptest.NewRecorder()
	handleRecordProductMeasurement(response, request, ctx, f.product, repository.NewSupplierRepository(db))
	if response.Code != 200 {
		t.Fatalf("measurement HTTP=%d %s", response.Code, response.Body.String())
	}
	var result struct {
		VariantID string                 `json:"variantId"`
		Physical  repository.SKUPhysical `json:"physical"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.VariantID != sku || !result.Physical.Verified || result.Physical.Revision == "" || result.Physical.Source != "sku" {
		t.Fatalf("exact measurement acknowledgment=%+v err=%v", result, err)
	}
	var parentWeight, skuWeight int
	var verified bool
	if err := db.QueryRow(`SELECT p.weight_grams,v.weight_grams,m.verified FROM products p JOIN product_variants v ON v.product_id=p.id JOIN inventory_effective_measurements m ON m.product_id=p.id AND m.variant_id=v.id::text WHERE v.id=$1`, sku).Scan(&parentWeight, &skuWeight, &verified); err != nil || parentWeight != 100 || skuWeight != 1000 || !verified {
		t.Fatalf("parent=%d SKU=%d verified=%t err=%v", parentWeight, skuWeight, verified, err)
	}
	request = httptest.NewRequest("POST", "/measurements", strings.NewReader(`{"variantId":"`+uuid.NewString()+`","lengthMm":1,"widthMm":1,"heightMm":1,"weightGrams":1,"reason":"foreign"}`))
	response = httptest.NewRecorder()
	handleRecordProductMeasurement(response, request, ctx, f.product, repository.NewSupplierRepository(db))
	if response.Code < 400 {
		t.Fatal("foreign SKU silently changed parent")
	}
}
