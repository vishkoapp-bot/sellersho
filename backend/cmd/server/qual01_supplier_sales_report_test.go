package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestQual01SupplierSalesReportScope(t *testing.T) {
	db := opsHTTPDB(t)
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	service := auth.NewJWTService(uuid.NewString())
	reader := &middlewareCapabilityReader{capabilities: []string{"supplier"}}
	service.SetCapabilityReader(reader)
	h := NewSupplierHandlers(repo, service, false)
	handler := RequireSupplierAccess(service, repo)(h.HandleGetSalesReport)
	fixture := func(i int) (user, supplier, product string) {
		t.Helper()
		suffix := uuid.NewString()[:8]
		owner, seller, err := repo.RegisterSupplier(ctx, "Sales fixture", "Sales fixture", "sales-"+suffix, "LocalFixture!42", fmt.Sprintf("090000407%02d", i), "", "Tehran")
		if err != nil {
			t.Fatal(err)
		}
		product = "qual-sales-" + suffix
		if err := repo.CreateDirectProduct(ctx, domain.Product{ID: product, Slug: product, Title: "Generated sales fixture", CategorySlug: "digital", SupplierID: seller.ID, SupplierBaseCost: 1000, Stock: 10}); err != nil {
			t.Fatal(err)
		}
		return owner.ID, seller.ID, product
	}
	ownerA, supplierA, productA := fixture(1)
	ownerB, supplierB, productB := fixture(2)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	addOrder := func(date string) string {
		t.Helper()
		_, _, order, _ := fin003HTTPPaidOrder(t)
		exec(`UPDATE orders SET created_at=$2 WHERE id=$1`, order, date)
		return order
	}
	line := func(order, product string, quantity, wholesale int) {
		t.Helper()
		exec(`INSERT INTO order_items(order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price)
			VALUES($1,$2,$3,$4,100,150,$5)`, order, product, quantity, wholesale, wholesale+250)
	}
	mixed := addOrder("2099-02-02 12:00:00+00")
	line(mixed, productA, 2, 1000)
	line(mixed, productB, 3, 5000)
	line(addOrder("2099-02-02 18:00:00+00"), productA, 1, 1001)
	line(addOrder("2099-02-03 12:00:00+00"), productA, 2, 9000)
	request := func(method, user, query string, wantStatus int, want map[string]int64) {
		t.Helper()
		token, err := service.GenerateTokenWithCapabilities(user, "", "", "supplier", []string{"supplier"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/api/v1/supplier/sales-report?"+query, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != wantStatus {
			t.Fatalf("report status %d, want %d: %s", w.Code, wantStatus, w.Body)
		}
		if want != nil {
			var got map[string]int64
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("report %s, want %v: %v", w.Body, want, err)
			}
		}
	}
	const dateRange = "start_date=2099-02-02&end_date=2099-02-02"
	request(http.MethodGet, ownerA, dateRange+"&supplierId="+supplierB, http.StatusOK, map[string]int64{"total_revenue": 3001, "items_sold": 3, "orders_count": 2, "average_order": 1500})
	request(http.MethodGet, ownerB, dateRange+"&supplierId="+supplierA, http.StatusOK, map[string]int64{"total_revenue": 15000, "items_sold": 3, "orders_count": 1, "average_order": 15000})
	request(http.MethodGet, ownerA, "start_date=2099-02-03&end_date=2099-02-02", http.StatusBadRequest, nil)
	request(http.MethodPost, ownerA, dateRange, http.StatusMethodNotAllowed, nil)
	exec(`UPDATE users SET is_active=FALSE WHERE id=$1`, ownerA)
	request(http.MethodGet, ownerA, dateRange, http.StatusForbidden, nil)
}
