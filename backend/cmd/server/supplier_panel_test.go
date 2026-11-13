package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestSupplierPanelScopeAndPersistence(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	suffix := uuid.NewString()[:8]
	owner, supplier, err := repo.RegisterSupplier(ctx, "Panel Test", "Owner", "panel-"+suffix, "TestPanelPassword!", fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), "", "Tehran")
	if err != nil {
		t.Fatal(err)
	}
	productID := "panel-product-" + suffix
	t.Cleanup(func() {
		for _, table := range []string{"payout_requests", "supplier_campaigns", "inbound_shipments", "qc_discrepancies"} {
			_, _ = db.Exec("DELETE FROM "+table+" WHERE supplier_id = $1", supplier.ID)
		}
		_, _ = db.Exec(`DELETE FROM pending_approvals WHERE requested_by = $1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM products WHERE id = $1`, productID)
		_, _ = db.Exec(`DELETE FROM suppliers WHERE id = $1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, owner.ID)
	})
	if err := repo.CreateDirectProduct(ctx, domain.Product{ID: productID, Slug: productID, Title: "Panel product", CategorySlug: "digital", SupplierID: supplier.ID, SupplierBaseCost: 1000, Stock: 10}); err != nil {
		t.Fatal(err)
	}
	for name, action := range map[string]func() error{
		"foreign stock":  func() error { return repo.UpdateProductStock(ctx, "foreign", productID, 99, "") },
		"negative stock": func() error { return repo.UpdateProductStock(ctx, supplier.ID, productID, -1, "") },
		"foreign price":  func() error { return repo.UpdateProductPricing(ctx, "foreign", productID, 99, "") },
		"invalid price":  func() error { return repo.UpdateProductPricing(ctx, supplier.ID, productID, 0, "") },
		"foreign supply": func() error { return repo.UpdateProductSupplyOption(ctx, "foreign", productID, "3day") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	if err := repo.UpdateProductStock(ctx, supplier.ID, productID, 0, ""); err != nil {
		t.Fatal(err)
	}
	var stock int
	if err := db.QueryRow(`SELECT stock FROM products WHERE id = $1`, productID).Scan(&stock); err != nil || stock != 0 {
		t.Fatalf("stock %d: %v", stock, err)
	}
	if _, err := repo.ProposeProduct(ctx, supplier.ID, map[string]interface{}{"title": "Scoped proposal", "supplierBaseCost": 1000}); err != nil {
		t.Fatal(err)
	}
	other, err := repo.GetSupplierProposals(ctx, "foreign")
	if err != nil || len(other) != 0 {
		t.Fatalf("foreign proposals leaked: %v %v", other, err)
	}
	own, err := repo.GetSupplierProposals(ctx, supplier.ID)
	if err != nil || len(own) != 1 {
		t.Fatalf("own proposals: %v %v", own, err)
	}
	inbounds, err := repo.GetSupplierInbounds(ctx, supplier.ID)
	if err != nil || inbounds == nil || len(inbounds) != 0 {
		t.Fatalf("empty inbounds: %v %v", inbounds, err)
	}
	if _, err := repo.GetInboundByID(ctx, "missing"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateInbound(ctx, supplier.ID, map[string]interface{}{}); err == nil {
		t.Fatal("empty inbound accepted")
	}
	inbound := map[string]interface{}{"items": []interface{}{map[string]interface{}{"product_id": productID, "quantity": float64(5), "carton_count": float64(1)}}}
	if _, err := repo.CreateInbound(ctx, "foreign", inbound); err == nil {
		t.Fatal("foreign inbound accepted")
	}
	if _, err := repo.CreateInbound(ctx, supplier.ID, inbound); err != nil {
		t.Fatal(err)
	}
	campaign := map[string]interface{}{"campaign_name": "Panel campaign", "budget": float64(1000), "cpc_bid": float64(100), "product_ids": []interface{}{productID}, "target_categories": []interface{}{}}
	if _, err := repo.CreateCampaign(ctx, "foreign", campaign); err == nil {
		t.Fatal("foreign campaign accepted")
	}
	campaignID, err := repo.CreateCampaign(ctx, supplier.ID, campaign)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := repo.GetCampaignStats(ctx, supplier.ID, campaignID)
	if err != nil || stats == nil || stats.Impressions != 0 || stats.Clicks != 0 {
		t.Fatalf("fabricated campaign stats: %v %v", stats, err)
	}
	stats, err = repo.GetCampaignStats(ctx, "foreign", campaignID)
	if err != nil || stats != nil {
		t.Fatalf("foreign stats: %v %v", stats, err)
	}
	balance, err := repo.GetSettlementBalance(ctx, supplier.ID)
	if err != nil || balance.Available != 0 {
		t.Fatalf("empty balance: %v %v", balance, err)
	}
	if _, err := repo.RequestSettlement(ctx, supplier.ID, 1, "", "", "Owner"); err == nil {
		t.Fatal("overdrawn settlement accepted")
	}
	report, err := repo.GetSalesReport(ctx, supplier.ID, "", "")
	if err != nil || report.TotalRevenue != 0 {
		t.Fatalf("empty report: %v %v", report, err)
	}
}

func TestSupplierHandlerRejectsInvalidPayload(t *testing.T) {
	h := NewSupplierHandlers(nil, auth.NewJWTService("panel-test"), false)
	cases := []struct {
		path, body string
		handler    http.HandlerFunc
	}{
		{"/api/v1/supplier/products/p/stock", `{"new_stock":-1}`, h.HandleUpdateStock},
		{"/api/v1/supplier/products/p/pricing", `{"new_supplier_base_cost":0}`, h.HandleUpdatePricing},
		{"/api/v1/supplier/settlement/request", `{"amount":-1,"shaba":"IR000","account_holder":"x"}`, h.HandleRequestSettlement},
		{"/api/v1/supplier/sales-report?start_date=invalid", `{}`, h.HandleGetSalesReport},
	}
	for _, item := range cases {
		t.Run(item.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			item.handler(w, httptest.NewRequest(http.MethodPost, item.path, strings.NewReader(item.body)))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSupplierProposalNotPublishedBeforeApproval(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	repo := repository.NewSupplierRepository(db)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	owner, supplier, err := repo.RegisterSupplier(ctx, "Proposal Test", "Owner", "proposal-"+suffix, "TestProposalPassword!", fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), "", "Tehran")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM pending_approvals WHERE requested_by=$1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM products WHERE supplier_id=$1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM suppliers WHERE id=$1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner.ID)
	})
	h := NewSupplierHandlers(repo, auth.NewJWTService("panel-test"), false)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/products/propose", strings.NewReader(`{"title":"Pending product","categorySlug":"digital","supplierBaseCost":1000,"stock":10,"weightGrams":500,"lengthMm":100,"widthMm":100,"heightMm":100,"fulfillmentMethod":"supplier_direct"}`))
	req = req.WithContext(context.WithValue(req.Context(), supplierIDKey, supplier.ID))
	w := httptest.NewRecorder()
	h.HandleProposeProduct(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		ProductID  string `json:"product_id"`
		ProposalID string `json:"proposal_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM products WHERE id=$1`, response.ProductID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("proposal published early: %d %v", count, err)
	}
	if _, err := repo.ApproveProposal(ctx, response.ProposalID, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM products WHERE id=$1 AND supplier_id=$2`, response.ProductID, supplier.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("approval did not publish: %d %v", count, err)
	}
	var fulfillment string
	if err := db.QueryRow(`SELECT fulfillment_method FROM products WHERE id=$1`, response.ProductID).Scan(&fulfillment); err != nil {
		t.Fatal(err)
	}
	if fulfillment != "supplier_direct" {
		t.Fatalf("approved fulfillment = %q, want supplier_direct", fulfillment)
	}
}
