package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestSPNCXRetryCaptureHTTP(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	spnc := "B4" + strings.ToUpper(uuid.NewString()[:6])
	variant, lot := lookupHTTPSeed(t, db, f, spnc)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE inventory_balances SET supplier_warehouse_id=$2 WHERE id=$1`, lot, f.warehouse)
	seedOrder := func(paymentStatus string) string {
		t.Helper()
		order := "retry-" + uuid.NewString()[:12]
		item := uuid.NewString()
		exec(`INSERT INTO orders(id,order_number,store_name,tracking_code,total_amount,payable_amount,
			customer_user_id,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,payment_status)
			VALUES($1,$1,'test',$1,2000,2000,$2,'test','09123456789','test','test','test','1234512345','pending')`, order, f.user)
		exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price)
			VALUES($1,$2,$3,$4,2,1000,0,0,1000)`, item, order, f.product, variant)
		exec(`INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,supplier_warehouse_id,origin_label,quantity)
			VALUES($1,$2,$3,0,'supplier',$4,$5,'test',2)`, uuid.NewString(), order, item, f.supplier, f.warehouse)
		exec(`INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,status,amount)
			VALUES($1,$1,$1,$1,$2,2000)`, order, paymentStatus)
		exec(`INSERT INTO spncx_failed_captures(order_id,failure,mode) VALUES($1,'insufficient_inventory','pre_strict')`, order)
		return order
	}
	order := seedOrder("pending")
	refunded := seedOrder("refunded")
	insufficient := seedOrder("pending")
	orderRepo := repository.NewOrderRepository(db)
	jwtService := auth.NewJWTService(strings.Repeat("s", 32))
	tokenFor := func(capability string) string {
		t.Helper()
		token, err := jwtService.GenerateTokenWithCapabilities(f.user, "09123456789", "Retry test", "admin", []string{capability}, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	superAdmin := tokenFor("super_admin")
	dispatch := RequireCapabilities(jwtService, "super_admin")(func(w http.ResponseWriter, r *http.Request) {
		handleAdminOrdersSubtree(w, r, db, orderRepo)
	})
	call := func(method, path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		dispatch(w, req)
		return w
	}

	const prefix = "/api/v1/admin/orders/"
	const action = "/retry-capture"
	path := prefix + order + action
	checkSPNCXStatus(t, call(http.MethodPost, path, ""), http.StatusUnauthorized, `{"error":"Unauthorized"}`)
	checkSPNCXStatus(t, call(http.MethodPost, path, tokenFor("admin")), http.StatusForbidden, `{"error":"Forbidden"}`)
	checkSPNCXStatus(t, call(http.MethodGet, path, superAdmin), http.StatusNotFound, "404 page not found")
	for _, invalid := range []string{order + "/something-else", action, order + action + "/", order + "/extra" + action, order + "%2Fextra" + action} {
		checkSPNCXStatus(t, call(http.MethodPost, prefix+invalid, superAdmin), http.StatusNotFound, "404 page not found")
	}
	checkSPNCXJSON(t, call(http.MethodPost, prefix+uuid.NewString()+action, superAdmin), http.StatusNotFound, `{"error":"order_not_found"}`)
	checkSPNCXJSON(t, call(http.MethodPost, path, superAdmin), http.StatusOK,
		`{"order_id":"`+order+`","captured":true,"unit_codes":["`+spnc+`-X0001","`+spnc+`-X0002"]}`)
	checkSPNCXJSON(t, call(http.MethodPost, path, superAdmin), http.StatusConflict, `{"error":"capture_already_completed"}`)
	checkSPNCXJSON(t, call(http.MethodPost, prefix+refunded+action, superAdmin), http.StatusConflict, `{"error":"payment_not_retryable"}`)
	checkSPNCXJSON(t, call(http.MethodPost, prefix+insufficient+action, superAdmin), http.StatusConflict, `{"error":"insufficient_inventory"}`)
}
