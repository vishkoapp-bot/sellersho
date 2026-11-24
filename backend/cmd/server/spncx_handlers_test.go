package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func checkSPNCXJSON(t *testing.T, recorder *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	var got, want any
	if err := json.Unmarshal([]byte(body), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil || recorder.Code != status ||
		recorder.Header().Get("Content-Type") != "application/json" || !reflect.DeepEqual(got, want) {
		t.Fatalf("got HTTP %d %s (%v), want HTTP %d %s", recorder.Code, recorder.Body, err, status, body)
	}
}

func TestSPNCXPrintHTTPValidation(t *testing.T) {
	path := "/api/v1/warehouse/units/print"
	key := uuid.NewString()
	for _, tc := range []struct {
		method string
		body   string
		status int
		want   string
	}{
		{http.MethodGet, "", 405, `{"error":"Method not allowed"}`},
		{http.MethodPost, `{"unit_codes":["U"]}`, 422, `{"error":"idempotency_key_required"}`},
		{http.MethodPost, `{"unit_codes":["U","U"],"idempotency_key":"` + key + `"}`, 422, `{"error":"duplicate_unit_codes"}`},
		{http.MethodPost, `{"unit_codes":[],"receipt_line_id":"line","idempotency_key":"` + key + `"}`, 422, `{"error":"conflicting_unit_selection"}`},
		{http.MethodPost, `{"receipt_line_id":"line","count":1,"idempotency_key":"` + key + `"}`, 400, `{"error":"invalid_request"}`},
	} {
		req := httptest.NewRequest(tc.method, path, strings.NewReader(tc.body))
		recorder := httptest.NewRecorder()
		HandlePrintWarehouseUnits(recorder, req, nil)
		checkSPNCXJSON(t, recorder, tc.status, tc.want)
	}
}

func TestSPNCXPrintHTTPEnvelopes(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	other := supplierFlowSeed(t, db)
	h := &SupplierHandlers{supplierRepo: repository.NewSupplierRepository(db)}
	ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: f.user})
	const prefix = "/api/v1/supplier/products/"
	jwtService := auth.NewJWTService(strings.Repeat("s", 32))
	jwtService.SetCapabilityReader(repository.NewCapabilityRepository(db))
	token, err := jwtService.GenerateToken(f.user, "09123456789", "Print fixture", "supplier", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	supplierHandler := RequireSupplierAccess(jwtService, h.supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		productID, action := supplierUnitLabelRoute(strings.TrimPrefix(r.URL.Path, prefix))
		switch action {
		case "units":
			h.HandleGetProductUnitLabels(w, r, productID)
		case "print":
			h.HandlePrintProductUnitLabels(w, r, productID)
		default:
			t.Fatalf("unexpected label path %s", r.URL.Path)
		}
	})
	supplierRequest := func(method, productID, action, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, prefix+productID+"/labels/"+action, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		supplierHandler(recorder, req)
		return recorder
	}
	key := uuid.NewString()
	list := supplierRequest(http.MethodGet, f.product, "units", "")
	var page repository.UnitLabelListResponse
	err = json.Unmarshal(list.Body.Bytes(), &page)
	if err != nil || list.Code != http.StatusOK || page.ProductTitle != "Audit product" ||
		page.Units == nil || len(page.Units) != 0 || page.TotalForFilter != 0 || page.NextCursor != nil {
		t.Fatalf("empty list: HTTP %d %s (%v)", list.Code, list.Body, err)
	}
	body := `{"unit_codes":["UNKNOWN-X0001"],"idempotency_key":"` + key + `"}`
	checkSPNCXJSON(t, supplierRequest(http.MethodPost, f.product, "print", body),
		422, `{"error":"unit_not_printable","errors":[{"code":"UNKNOWN-X0001","error":"unit_not_found"}]}`)
	for _, id := range []string{other.product, "unknown-product"} {
		checkSPNCXJSON(t, supplierRequest(http.MethodGet, id, "units", ""), 404, `{"error":"product_not_found"}`)
		checkSPNCXJSON(t, supplierRequest(http.MethodPost, id, "print", body), 404, `{"error":"product_not_found"}`)
	}

	line := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO inbound_inventory_lines
		(id,shipment_id,supplier_id,warehouse_id,product_id,source_kind,external_reference,reason,quantity)
		VALUES($1,$1,$2,'test-site',$3,'external_replenishment',$1,'test fixture',1)`, line, f.supplier, f.product); err != nil {
		t.Fatal(err)
	}
	warehousePrint := func(receipt string) *httptest.ResponseRecorder {
		body := `{"receipt_line_id":"` + receipt + `","idempotency_key":"` + key + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/warehouse/units/print", strings.NewReader(body)).WithContext(ctx)
		recorder := httptest.NewRecorder()
		HandlePrintWarehouseUnits(recorder, req, db)
		return recorder
	}
	checkSPNCXJSON(t, warehousePrint(uuid.NewString()), 404, `{"error":"receipt_line_not_found"}`)
	checkSPNCXJSON(t, warehousePrint(line), 200, `{"units":[]}`)
}
