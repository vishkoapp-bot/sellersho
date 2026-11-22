package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestT01SellerAnalyticsWindowConsistency(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires a guarded disposable PostgreSQL database")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}

	owner, buyer := uuid.NewString(), uuid.NewString()
	storeID, productID := uuid.NewString(), uuid.NewString()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,mobile,full_name,role) VALUES ($1,$2,'Analytics seller','seller'),($3,$4,'Analytics buyer','customer')`,
		owner, "09"+owner[:12], buyer, "09"+buyer[:12])
	store := domain.Store{ID: storeID, Slug: "analytics-" + storeID, Name: "Analytics", OwnerType: "dropshipper", OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8]}
	if err := repository.NewStoreRepository(db).CreateStore(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,is_active)
		VALUES ($1,'Analytics product',$1,(SELECT id FROM suppliers ORDER BY id LIMIT 1),200,100,0,10,TRUE)`, productID)
	exec(`INSERT INTO store_products(store_id,product_id,position) VALUES($1,$2,0)`, storeID, productID)

	today := t01TehranDay(time.Now())
	start := today.AddDate(0, 0, -29)
	if start.UTC().Format(time.DateOnly) == start.Format(time.DateOnly) {
		t.Fatal("Tehran-midnight fixture must have a different UTC date")
	}
	seedOrder := func(at time.Time, status, payment string, quantity int) (string, string) {
		t.Helper()
		id, item := uuid.NewString()[:20], uuid.NewString()
		exec(`INSERT INTO orders(id,order_number,store_id,store_name,customer_full_name,customer_phone,customer_province,
				customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payable_amount,total_amount,
				payment_status,status,created_at,dropshipper_profit)
			VALUES($1,$1,$2,'Analytics','Buyer','09123456789','x','x','x','1234512345',$1,$3,$4,$4,$5,$6,$7,$8)`,
			id, storeID, buyer, 200*quantity, payment, status, at, 25*quantity)
		exec(`INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price)
			VALUES($1,$2,$3,$4,100,0,25,200)`, item, id, productID, quantity)
		return id, item
	}
	seedOrder(start, "delivered", "paid", 2)
	oldOrder, oldItem := seedOrder(start.Add(-time.Second), "delivered", "paid", 5)
	seedOrder(start.Add(time.Second), "cancelled", "paid", 9)
	seedOrder(start.Add(time.Second), "processing", "pending", 9)
	returnTx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer returnTx.Rollback()
	if _, err := returnTx.Exec(`SELECT set_config('sellersho.fin005_writer','v1',true)`); err != nil {
		t.Fatal(err)
	}
	ret := uuid.NewString()
	if _, err := returnTx.Exec(`INSERT INTO customer_returns(id,order_id,customer_id,status,created_at)
		VALUES($1,$2,$3,'received',$4)`, ret, oldOrder, buyer, start.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := returnTx.Exec(`INSERT INTO return_items(return_id,order_item_id,qty,qc_status,received_at)
		VALUES($1,$2,4,'ok',$3)`, ret, oldItem, start.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := returnTx.Commit(); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO store_view_daily(store_slug,day,views) VALUES($1,$2::date,4),($1,$3::date,99),($1,$4::date,99)`,
		store.Slug, start.Format(time.DateOnly), start.AddDate(0, 0, -1).Format(time.DateOnly), today.AddDate(0, 0, 1).Format(time.DateOnly))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/seller/analytics?storeId="+storeID, nil)
	r = r.WithContext(context.WithValue(r.Context(), userClaimsKey, &auth.UserClaims{UserID: owner, Role: "seller"}))
	w := httptest.NewRecorder()
	handleT01SellerAnalytics(w, r, db)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	type period struct{ Orders, Revenue, Profit int }
	type product struct {
		Quantity, Revenue, Profit, DaysLeft int
		ReturnRate                          float64
	}
	var got struct {
		OrderCount, TotalRevenue, SellerProfit, Views int
		ConversionRate                                float64
		PeriodComparison                              struct{ Current, Previous period }
		StatusBreakdown                               []struct {
			Status string
			Count  int
		}
		Daily []struct {
			Date            string
			Orders, Revenue int
		}
		TopProducts, ProductRows []product
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.OrderCount != 2 || got.TotalRevenue != 1400 || got.SellerProfit != 175 ||
		len(got.StatusBreakdown) != 1 || got.StatusBreakdown[0].Status != "delivered" || got.StatusBreakdown[0].Count != 2 {
		t.Fatalf("all-time sales or statuses: %+v", got)
	}
	if got.PeriodComparison.Current != (period{1, 400, 50}) || got.PeriodComparison.Previous != (period{1, 1000, 125}) ||
		got.Views != 4 || got.ConversionRate != 0.25 {
		t.Fatalf("period or conversion: %+v", got)
	}
	if len(got.Daily) != 30 {
		t.Fatalf("daily length %d", len(got.Daily))
	}
	for i, day := range got.Daily {
		wantOrders, wantRevenue := 0, 0
		if i == 0 {
			wantOrders, wantRevenue = 1, 400
		}
		if day.Date != start.AddDate(0, 0, i).Format(time.DateOnly) || day.Orders != wantOrders || day.Revenue != wantRevenue {
			t.Fatalf("daily bucket %d: %+v", i, day)
		}
	}
	if len(got.TopProducts) != 1 || got.TopProducts[0].Quantity != 7 || got.TopProducts[0].Revenue != 1400 ||
		len(got.ProductRows) != 1 || got.ProductRows[0] != (product{7, 1400, 175, 150, 4.0 / 7}) {
		t.Fatalf("product aggregates: top=%+v rows=%+v", got.TopProducts, got.ProductRows)
	}
}
