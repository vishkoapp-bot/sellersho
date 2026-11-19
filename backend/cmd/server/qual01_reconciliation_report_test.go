package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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
	reservationID, movementID := "qual-res-"+uuid.NewString(), "qual-mov-"+uuid.NewString()
	if _, err := db.Exec(`INSERT INTO inventory_reservations (id,product_id,supplier_id,location_key,quantity,reference_type,reference_id,status,idempotency_key) VALUES ($1,$2,'sup-technolife','qual01-location',1,'test','reservation-reference','active',$3)`, reservationID, product, "retry-"+reservationID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO inventory_movements (id,product_id,supplier_id,quantity,to_status,to_location_key,reference_type,reference_id,idempotency_key) VALUES ($1,$2,'sup-technolife',1,'sellable','qual01-location','test','movement-reference',$3)`, movementID, product, "retry-"+movementID); err != nil {
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
		if err := json.Unmarshal(body, &envelope); err != nil || len(envelope) != 5 || string(envelope["readOnly"]) != "true" {
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
			want := []string{"activeReservations", "canonicalQuantity", "canonicalRows", "canonicalSellableQuantity", "classification", "classifications", "difference", "evidenceIds", "evidenceInsufficient", "evidenceTotal", "evidenceTruncated", "hasInboundEvidence", "hasLedgerEvidence", "hasMeasurement", "hasPutawayEvidence", "hasQCEvidence", "legacyStock", "locationBreakdown", "measurementClassification", "measurementEvidence", "measurementStatus", "movementQuantity", "movements", "physicalInventory", "physicalQuantity", "productId", "productTitle", "reviewStatus", "suggestedAction", "supplierId", "unreconciledRows", "unresolved"}
			if !slices.Equal(keys, want) || row["canonicalSellableQuantity"] != float64(2) || row["difference"] != float64(0) {
				t.Fatalf("unexpected reconciliation row: %v", row)
			}
			for _, check := range []struct{ field, id, ref string }{
				{"activeReservations", reservationID, "reservation-reference"},
				{"movements", movementID, "movement-reference"},
			} {
				entries, ok := row[check.field].([]any)
				if !ok || len(entries) != 1 {
					t.Fatalf("missing %s evidence: %v", check.field, row[check.field])
				}
				entry, ok := entries[0].(map[string]any)
				if !ok || entry["id"] != check.id || entry["referenceId"] != check.ref {
					t.Fatalf("wrong %s evidence: %v", check.field, entries[0])
				}
				if _, present := entry["idempotencyKey"]; present {
					t.Fatalf("%s exposed an internal retry identifier", check.field)
				}
			}
		}
		if !found {
			t.Fatal("report omitted persisted fixture")
		}
	}
}

func TestQual01HistoricalReconciliationReviewAdmission(t *testing.T) {
	db := opsHTTPDB(t)
	product := "qual-review-" + uuid.NewString()[:12]
	if _, err := db.Exec(`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,fulfillment_method) VALUES ($1,'Review fixture',$1,'digital','sup-technolife',100000,50000,1000,TRUE,2,'warehouse')`, product); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewSupplierRepository(db)
	service := auth.NewJWTService(uuid.NewString())
	reader := &middlewareCapabilityReader{}
	service.SetCapabilityReader(reader)
	handler := requireGlobalWarehouseRoles(service, "logistics_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleReviewHistoricalInventory(w, r, r.Context(), repo, nil)
	})
	request := func(method, id, actor, body string, caps []string, ttl time.Duration, want int) string {
		t.Helper()
		reader.capabilities = caps
		token, err := service.GenerateTokenWithCapabilities(actor, "", "", "logistics_specialist", []string{"logistics_specialist"}, ttl)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/api/v1/warehouse/inventory/reconciliation-reviews/"+id, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != want {
			t.Fatalf("%s with %v: got %d, want %d: %s", method, caps, w.Code, want, w.Body)
		}
		return w.Body.String()
	}
	const body = `{"status":"in_review","reason":"manual review","evidence":["fixture-a","fixture-b"]}`
	admin := []string{"admin"}
	request(http.MethodPost, product, "fixture-reviewer", body, []string{"warehouse_staff", "admin"}, time.Hour, http.StatusForbidden)
	request(http.MethodPost, product, "fixture-reviewer", body, nil, time.Hour, http.StatusForbidden)
	request(http.MethodPost, product, "fixture-reviewer", body, admin, -time.Hour, http.StatusUnauthorized)
	request(http.MethodGet, product, "fixture-reviewer", body, admin, time.Hour, http.StatusMethodNotAllowed)
	request(http.MethodPost, product, "fixture-reviewer", `{"status":"unknown"}`, admin, time.Hour, http.StatusConflict)
	request(http.MethodPost, "missing-"+product, "fixture-reviewer", body, admin, time.Hour, http.StatusConflict)
	result := request(http.MethodPost, product, strings.Repeat("x", 81), body, admin, time.Hour, http.StatusInternalServerError)
	if strings.Contains(result, "pq:") || strings.Contains(result, "character varying") {
		t.Fatalf("database detail returned to client: %s", result)
	}
	var reviews, audit int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM historical_reconciliation_reviews WHERE product_id=$1), (SELECT COUNT(*) FROM audit_logs WHERE action='historical_reconciliation_review' AND details LIKE '%'||$1||'%')`, product).Scan(&reviews, &audit); err != nil || reviews != 0 || audit != 0 {
		t.Fatalf("failed requests wrote review or audit: %d/%d (%v)", reviews, audit, err)
	}
	result = request(http.MethodPost, product, "fixture-reviewer", body, []string{"logistics_specialist"}, time.Hour, http.StatusOK)
	var saved repository.HistoricalReview
	if err := json.Unmarshal([]byte(result), &saved); err != nil || saved.ProductID != product || saved.ReviewerUserID != "fixture-reviewer" || saved.ReviewStatus != "in_review" {
		t.Fatalf("unexpected authenticated review: %s (%v)", result, err)
	}
}
