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
	// TEST-02 round 4: this test reads inventory_effective_measurements
	// directly, but the view is CASCADE-dropped whenever a fixture
	// DROP TABLEs products, and no fixture before this test restores
	// it. Recreate it — DDL copied verbatim from
	// migrations/202611140010_sku_measurement_revision.up.sql
	// (lines 50-66); see also createEffectiveMeasurementsView in
	// internal/repository/test02_view_fixture_test.go.
	if _, err := db.Exec(`CREATE OR REPLACE VIEW inventory_effective_measurements AS
 WITH effective AS (
  SELECT p.id::text AS product_id,''::text AS variant_id,COALESCE(p.length_mm,0) AS length_mm,COALESCE(p.width_mm,0) AS width_mm,COALESCE(p.height_mm,0) AS height_mm,COALESCE(p.weight_grams,0) AS weight_grams,
   'product'::text AS source,'product'::text AS scope,''::text AS revision_variant,p.measurement_status AS stored_status,COALESCE(p.measurement_revision_id,'') AS revision,TRUE AS complete
  FROM products p
  UNION ALL
  SELECT p.id::text,v.id::text,COALESCE(v.length_mm,p.length_mm,0),COALESCE(v.width_mm,p.width_mm,0),COALESCE(v.height_mm,p.height_mm,0),COALESCE(v.weight_grams,p.weight_grams,0),
   CASE WHEN s.overridden THEN 'sku' ELSE 'product' END,CASE WHEN s.overridden THEN 'sku' ELSE 'product' END,CASE WHEN s.overridden THEN v.id::text ELSE '' END,
   CASE WHEN s.overridden THEN v.measurement_status ELSE p.measurement_status END,COALESCE(CASE WHEN s.overridden THEN v.measurement_revision_id ELSE p.measurement_revision_id END,''),
   NOT s.overridden OR (v.length_mm IS NOT NULL AND v.width_mm IS NOT NULL AND v.height_mm IS NOT NULL AND v.weight_grams IS NOT NULL)
  FROM products p JOIN product_variants v ON v.product_id=p.id
  CROSS JOIN LATERAL (SELECT v.length_mm IS NOT NULL OR v.width_mm IS NOT NULL OR v.height_mm IS NOT NULL OR v.weight_grams IS NOT NULL AS overridden) s
 )
 SELECT e.product_id,e.variant_id,e.length_mm,e.width_mm,e.height_mm,e.weight_grams,e.source,e.revision,
  COALESCE(r.verified,FALSE) AND e.stored_status='verified' AND e.complete AS verified,
  CASE WHEN e.stored_status='disputed' THEN 'disputed' WHEN COALESCE(r.verified,FALSE) AND e.stored_status='verified' AND e.complete THEN 'verified' ELSE 'unverified' END AS measurement_status
 FROM effective e LEFT JOIN inventory_measurement_revisions r ON r.id=e.revision AND r.product_id=e.product_id AND r.variant_id=e.revision_variant AND r.scope=e.scope AND (r.length_mm,r.width_mm,r.height_mm,r.weight_grams)=(e.length_mm,e.width_mm,e.height_mm,e.weight_grams)`); err != nil {
		t.Fatal(err)
	}
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
