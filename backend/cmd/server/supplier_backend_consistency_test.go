package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func supplierConsistencyFixtureMobile(key string) string {
	value, _ := strconv.ParseUint(key, 16, 32)
	return fmt.Sprintf("09%09d", value%1000000000)
}

func TestSupplierAnalyticsMatchesSalesReportAndScopesDailyTotals(t *testing.T) {
	db := opsHTTPDB(t)
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	newProduct := func() (string, string) {
		t.Helper()
		key := uuid.NewString()[:8]
		_, supplier, err := repo.RegisterSupplier(ctx, "Analytics fixture", "Fixture owner", "analytics-"+key, "AnalyticsFixture!42", supplierConsistencyFixtureMobile(key), "", "Tehran")
		if err != nil {
			t.Fatal(err)
		}
		product := "analytics-" + key
		if err := repo.CreateDirectProduct(ctx, domain.Product{ID: product, Slug: product, Title: "Fixture product", CategorySlug: "digital", SupplierID: supplier.ID, SupplierBaseCost: 1000, Stock: 10}); err != nil {
			t.Fatal(err)
		}
		return supplier.ID, product
	}
	supplierA, productA := newProduct()
	_, productB := newProduct()
	line := func(order, product string, quantity, wholesale int) string {
		t.Helper()
		var id string
		err := db.QueryRow(`INSERT INTO order_items (order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price)
			VALUES ($1, $2, $3, $4, 100, 150, $5) RETURNING id::text`, order, product, quantity, wholesale, wholesale+250).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	_, buyer, mixed, _ := fin003HTTPPaidOrder(t)
	lineA := line(mixed, productA, 2, 1000)
	line(mixed, productB, 1, 5000)
	_, _, foreignOnly, _ := fin003HTTPPaidOrder(t)
	line(foreignOnly, productB, 3, 6000)
	_, cancelledBuyer, cancelled, _ := fin003HTTPPaidOrder(t)
	line(cancelled, productA, 4, 8000)
	if err := repository.NewOrderRepository(db).CancelOrder(ctx, cancelled, cancelledBuyer, false); err != nil {
		t.Fatal(err)
	}
	returnTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer returnTx.Rollback()
	if _, err := returnTx.Exec(`SELECT set_config('sellersho.fin005_writer', 'v1', true)`); err != nil {
		t.Fatal(err)
	}
	var returnID string
	if err := returnTx.QueryRow(`INSERT INTO customer_returns (order_id, customer_id, status, reason)
		VALUES ($1, $2, 'received', 'Fixture return') RETURNING id::text`, mixed, buyer).Scan(&returnID); err != nil {
		t.Fatal(err)
	}
	if _, err := returnTx.Exec(`INSERT INTO return_items (return_id, order_item_id, qty, qc_status, received_at)
		VALUES ($1, $2, 2, 'ok', NOW())`, returnID, lineA); err != nil {
		t.Fatal(err)
	}
	if err := returnTx.Commit(); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetSupplierAnalytics(ctx, supplierA)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Daily) != 30 || got.Daily[29].Revenue != 2000 || got.Daily[29].Orders != 1 || got.Daily[28].Revenue != 0 {
		t.Fatalf("daily series should only include this supplier's noncancelled paid lines: %+v", got.Daily)
	}
	report, err := repo.GetSalesReport(ctx, supplierA, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalRevenue != 2000 || got.TotalBaseAmount != report.TotalRevenue || got.TotalQuantity != report.ItemsSold || got.OrderCount != report.OrdersCount || got.OrderCount != 1 {
		t.Fatalf("analytics totals %+v differ from supplier sales report %+v", got, report)
	}
	if len(got.Products) != 1 || got.Products[0].Quantity != 2 || got.Products[0].BaseAmount != 2000 || got.Products[0].ReturnRate != 1 {
		t.Fatalf("unexpected per-product analytics %+v", got.Products)
	}
	if got.Returns.TotalReturned != 2 || got.Returns.QcOk != 2 || len(got.Returns.TopReasons) != 1 || got.Returns.TopReasons[0].Count != 2 {
		t.Fatalf("returns should count units instead of lines: %+v", got.Returns)
	}
}

func TestSupplierContractAcceptanceRequiresDisplayedVersion(t *testing.T) {
	db := opsHTTPDB(t)
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	key := uuid.NewString()[:8]
	_, supplier, err := repo.RegisterSupplier(ctx, "Contract fixture", "Fixture owner", "contract-"+key, "ContractFixture!42", supplierConsistencyFixtureMobile(key), "", "Tehran")
	if err != nil {
		t.Fatal(err)
	}
	version, err := repo.GetSupplierContractVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE platform_settings SET supplier_contract_version=$1 WHERE id=1`, version+1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`UPDATE platform_settings SET supplier_contract_version=$1 WHERE id=1`, version)
	})
	handlers := NewSupplierHandlers(repo, nil, false)
	call := func(handler http.HandlerFunc, body string, want int) map[string]any {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/contract/accept", strings.NewReader(body))
		request = request.WithContext(context.WithValue(request.Context(), supplierIDKey, supplier.ID))
		response := httptest.NewRecorder()
		handler(response, request)
		var payload map[string]any
		if response.Code != want || json.Unmarshal(response.Body.Bytes(), &payload) != nil {
			t.Fatalf("contract acceptance response %d, want %d: %s", response.Code, want, response.Body.String())
		}
		return payload
	}
	for _, handler := range []http.HandlerFunc{handlers.HandleSupplierContractAccept, handlers.HandleSupplierContractReconfirm} {
		call(handler, "{}", http.StatusBadRequest)
		call(handler, fmt.Sprintf(`{"version":%d}`, version), http.StatusConflict)
	}
	current, err := repo.GetSupplierByID(ctx, supplier.ID)
	if err != nil || current == nil || current.SignedContractVersion != supplier.SignedContractVersion {
		t.Fatalf("stale request modified the supplier's signed version: %+v %v", current, err)
	}
	body := fmt.Sprintf(`{"version":%d}`, version+1)
	accepted := call(handlers.HandleSupplierContractAccept, body, http.StatusOK)
	if accepted["signedContractVersion"] != float64(version+1) {
		t.Fatalf("unexpected accepted version: %+v", accepted)
	}
	replayed := call(handlers.HandleSupplierContractReconfirm, body, http.StatusOK)
	if replayed["alreadyCurrent"] != true || replayed["signedContractVersion"] != float64(version+1) {
		t.Fatalf("accepting the same version again should stay current: %+v", replayed)
	}
}
