package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestQual01HistoricalReconciliationReportProjection(t *testing.T) {
	db := opsHTTPDB(t)
	product := "qual-reconcile-" + uuid.NewString()[:12]
	if _, err := db.Exec(`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,fulfillment_method) VALUES ($1,'Reconciliation fixture',$1,'digital','sup-technolife',100000,50000,1000,TRUE,2,'warehouse')`, product); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES ($1,$2,'sup-technolife','supplier','bin','qual01-location','sellable',2,TRUE)`, "qual-"+uuid.NewString(), product); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewSupplierRepository(db)
	service := auth.NewJWTService(uuid.NewString())
	reader := &middlewareCapabilityReader{}
	service.SetCapabilityReader(reader)
	handler := requireGlobalWarehouseRoles(service, "logistics_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		handleGetHistoricalInventoryReport(w, r, r.Context(), repo)
	})
	request := func(method string, caps []string, want int) []byte {
		t.Helper()
		reader.capabilities = caps
		token, err := service.GenerateTokenWithCapabilities("fixture-user", "", "", "logistics_specialist", []string{"logistics_specialist"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/api/v1/warehouse/inventory/reconciliation-report", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != want {
			t.Fatalf("%s with %v: got %d, want %d: %s", method, caps, w.Code, want, w.Body)
		}
		return w.Body.Bytes()
	}
	request(http.MethodGet, []string{"warehouse_staff"}, http.StatusForbidden)
	request(http.MethodGet, []string{"logistics_specialist", "warehouse_staff"}, http.StatusForbidden)
	request(http.MethodGet, []string{"admin", "warehouse_staff"}, http.StatusForbidden)
	request(http.MethodGet, nil, http.StatusForbidden)
	request(http.MethodPost, []string{"logistics_specialist"}, http.StatusMethodNotAllowed)
	for _, role := range []string{"logistics_specialist", "admin"} {
		body := request(http.MethodGet, []string{role}, http.StatusOK)
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(body, &envelope); err != nil || len(envelope) != 2 || string(envelope["readOnly"]) != "true" {
			t.Fatalf("unexpected reconciliation envelope: %s (%v)", body, err)
		}
		var rows []map[string]any
		if err := json.Unmarshal(envelope["rows"], &rows); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row["productId"] != product {
				continue
			}
			found = true
			keys := make([]string, 0, len(row))
			for k := range row {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			want := []string{"activeReservations", "canonicalQuantity", "canonicalRows", "canonicalSellableQuantity", "classification", "classifications", "difference", "evidenceIds", "evidenceInsufficient", "hasInboundEvidence", "hasLedgerEvidence", "hasMeasurement", "hasPutawayEvidence", "hasQCEvidence", "legacyStock", "locationBreakdown", "measurementClassification", "measurementEvidence", "measurementStatus", "movementQuantity", "movements", "physicalInventory", "physicalQuantity", "productId", "productTitle", "reviewStatus", "suggestedAction", "supplierId", "unreconciledRows", "unresolved"}
			if !slices.Equal(keys, want) || row["canonicalSellableQuantity"] != float64(2) || row["difference"] != float64(0) {
				t.Fatalf("unexpected reconciliation row: %v", row)
			}
		}
		if !found {
			t.Fatal("report omitted persisted fixture")
		}
	}
}
