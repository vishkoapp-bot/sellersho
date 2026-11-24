package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestSPNCXReceiveHTTP(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	const spnc = "B3HTTP001"
	variant, _ := lookupHTTPSeed(t, db, f, spnc)
	supplier := repository.NewSupplierRepository(db)
	warehouse := repository.NewWarehouseRepository(db)
	orders := repository.NewOrderRepository(db)
	ctx := context.Background()
	site := "w-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'Test','Test')`, site); err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	shipment, err := supplier.CreateCartonShipmentFromSource(ctx, f.supplier, []repository.CartonShipmentItem{{VariantID: variant, ExpectedQty: 2}},
		repository.CartonInboundSource{Kind: "external_replenishment", ExternalReference: key, Reason: "test delivery"}, site, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := supplier.ChangeCartonDispatch(ctx, f.supplier, shipment.ID, false); err != nil {
		t.Fatal(err)
	}
	var line string
	if err := db.QueryRow(`SELECT id FROM inbound_inventory_lines WHERE shipment_id=$1 AND variant_id=$2`, shipment.ID, variant).Scan(&line); err != nil {
		t.Fatal(err)
	}

	jwt := auth.NewJWTService(strings.Repeat("s", 32))
	token, err := jwt.GenerateToken(f.user, "09123456789", "Receipt test", "admin", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	guard := requireWarehouseCityRoles(jwt, "admin", "warehouse_staff")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/warehouse/inbound-lines/", guard(func(w http.ResponseWriter, r *http.Request) {
		dispatchInboundLineReceipt(w, r, supplier)
	}))
	mux.HandleFunc("/api/v1/warehouse/qc/", guard(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/qc/")
		dispatchWarehouseQCPass(w, r, strings.TrimSuffix(path, "/pass"), warehouse)
	}))
	mux.HandleFunc("/api/v1/warehouse/returns/", guard(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/returns/")
		dispatchWarehouseReturnReceipt(w, r, strings.TrimSuffix(path, "/receive"), supplier, orders)
	}))
	mux.HandleFunc("/api/v1/warehouse/shipments/", guard(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/shipments/")
		handleReceiveWarehouseShipment(w, r, r.Context(), strings.TrimSuffix(path, "/receive"), supplier)
	}))
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/warehouse/"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	checkSPNCXJSON(t, call(http.MethodGet, "inbound-lines/"+line+"/receive", ""), 405, `{"error":"Method not allowed"}`)
	checkSPNCXJSON(t, call(http.MethodPost, "inbound-lines/missing/receive", `{"received_quantity":1}`), 404, `{"error":"inbound_line_not_found"}`)
	bad := call(http.MethodPost, "inbound-lines/"+line+"/receive", `{"received_quantity":1,"unit_codes":["WRONG-X0001"]}`)
	var mismatch struct {
		Error  string                    `json:"error"`
		LineID string                    `json:"line_id"`
		Errors []repository.UnitMismatch `json:"errors"`
	}
	if err := json.Unmarshal(bad.Body.Bytes(), &mismatch); err != nil || bad.Code != 422 || mismatch.Error != "unit_mismatch" || mismatch.LineID != line || len(mismatch.Errors) != 1 {
		t.Fatalf("mismatch HTTP %d: %s, decode=%v", bad.Code, bad.Body, err)
	}
	if got := mismatch.Errors[0]; got != (repository.UnitMismatch{Code: "WRONG-X0001", Error: "unit_mismatch", Expected: spnc, Actual: "WRONG"}) {
		t.Fatalf("mismatch row=%+v", got)
	}
	shipmentReceipt := call(http.MethodPost, "shipments/"+shipment.ShipmentCode+"/receive", fmt.Sprintf(`{"items":[{"variant_id":%q,"received_qty":2,"qc_status":"pass","unit_codes":[%q,%q]}]}`, variant, spnc+"-X0003", spnc+"-X0004"))
	var batch struct {
		Success bool                           `json:"success"`
		Status  string                         `json:"status"`
		Items   []repository.UnitReceiptResult `json:"items"`
	}
	if err := json.Unmarshal(shipmentReceipt.Body.Bytes(), &batch); err != nil || shipmentReceipt.Code != 200 || !batch.Success || batch.Status != "received" || len(batch.Items) != 1 || batch.Items[0].LineID != line || len(batch.Items[0].UnitCodes) != 2 || batch.Items[0].Mismatched == nil {
		t.Fatalf("shipment receipt HTTP %d: %s, decode=%v", shipmentReceipt.Code, shipmentReceipt.Body, err)
	}
	pass := call(http.MethodPost, "qc/"+line+"/pass", fmt.Sprintf(`{"quantity":1,"unit_codes":[%q]}`, spnc+"-X0004"))
	var passed repository.UnitReceiptResult
	if err := json.Unmarshal(pass.Body.Bytes(), &passed); err != nil || pass.Code != 200 || passed.LineID != line || passed.Quantity != 1 || len(passed.UnitCodes) != 1 || passed.UnitCodes[0] != spnc+"-X0004" {
		t.Fatalf("line pass HTTP %d: %s, decode=%v", pass.Code, pass.Body, err)
	}
	var exactReceipt bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM warehouse_storage_receipt_lots l
		JOIN inventory_movements m ON m.id=l.receipt_movement_id
		WHERE l.receipt_line_id=$1 AND m.reference_type='inbound_receive' AND m.reference_id=$1 AND l.initial_quantity=1)`, line).Scan(&exactReceipt); err != nil || !exactReceipt {
		t.Fatalf("accepted lot is not linked to the actual ASN mint movement: exact=%v err=%v", exactReceipt, err)
	}
	legacy := fmt.Sprintf(`{"inboundId":%q,"receiptLineId":%q,"quantity":2}`, shipment.ID, line)
	checkSPNCXJSON(t, call(http.MethodPost, "qc/"+f.product+"/pass", `{"quantity":1,"unit_codes":[]}`), 400, `{"error":"unit_codes_require_receipt_line"}`)
	for i := 0; i < 2; i++ {
		w := call(http.MethodPost, "qc/"+f.product+"/pass", legacy)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"passed"`) {
			t.Fatalf("legacy cumulative pass HTTP %d: %s", w.Code, w.Body)
		}
	}
	missingReturn := uuid.NewString()
	checkSPNCXJSON(t, call(http.MethodPost, "returns/"+missingReturn+"/receive", `{"items":[{"order_item_id":"test","qty":1}]}`), 404, `{"error":"Return or item not found, or return not approved"}`)
	checkSPNCXJSON(t, call(http.MethodPost, "returns/missing/receive", `{"items":[{"order_item_id":"test","qty":1}]}`), 404, `{"error":"Return or item not found, or return not approved"}`)
	checkSPNCXStatus(t, call(http.MethodPost, "returns/"+missingReturn+"/receive", ""), 409, "idempotency key required")
}
