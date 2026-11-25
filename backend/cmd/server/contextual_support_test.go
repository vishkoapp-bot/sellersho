package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestContextualSupportOwnershipAndStaffReply(t *testing.T) {
	db := setupTestPostgresDB(t)
	t.Cleanup(func() { db.Close() })
	owner, stranger, supplierUser, staff := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	suffix := uuid.NewString()[:8]
	store, supplier := "support-store-"+suffix, "support-supplier-"+suffix
	orderID, orderNumber := "support-order-"+suffix, "SH-"+suffix
	shipmentID := uuid.NewString()
	payoutID := "support-payout-" + suffix
	adjustmentID := "support-adjustment-" + suffix
	for _, row := range []struct{ id, role, mobile string }{{owner, "seller", "098001"}, {stranger, "seller", "098002"}, {supplierUser, "supplier", "098003"}, {staff, "admin", "098004"}} {
		if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role,is_active) VALUES($1,$2,'Support test',$3,true)`, row.id, row.mobile+suffix, row.role); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO stores(id,name,slug,owner_name,owner_user_id) VALUES($1,'Support test',$1,'Seller',$2)`, []any{store, owner}},
		{`INSERT INTO suppliers(id,name,owner_user_id) VALUES($1,'Support test',$2)`, []any{supplier, supplierUser}},
		{`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code) VALUES ($1,$2,$3,'Store',100,100,'private name','private phone','A','B','private address','12345',$1)`, []any{orderID, orderNumber, store}},
		{`INSERT INTO shipments(id,shipment_code,supplier_id) VALUES($1,$2,$3)`, []any{shipmentID, "S-" + suffix, supplier}},
		{`INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount) VALUES($1,$2,'seller','Seller','IR123456789012345678901234','Bank',10000)`, []any{payoutID, store}},
		{`INSERT INTO ledger_entries(id,target_account,target_type,type,amount,reason,operator) VALUES($1,'supplier_payable:'||$2,'supplier','credit',100,'سند تعدیل آزمایشی','test')`, []any{adjustmentID, supplier}},
	} {
		if _, err := db.Exec(row.query, row.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM support_tickets WHERE requester_user_id IN ($1,$2)`, owner, supplierUser)
		db.Exec(`DELETE FROM shipments WHERE id=$1`, shipmentID)
		db.Exec(`DELETE FROM ledger_entries WHERE id=$1`, adjustmentID)
		db.Exec(`DELETE FROM payout_requests WHERE id=$1`, payoutID)
		db.Exec(`DELETE FROM orders WHERE id=$1`, orderID)
		db.Exec(`DELETE FROM suppliers WHERE id=$1`, supplier)
		db.Exec(`DELETE FROM stores WHERE id=$1`, store)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4)`, owner, stranger, supplierUser, staff)
	})
	storeRepo := repository.NewStoreRepository(db)
	supplierRepo := repository.NewSupplierRepository(db)
	call := func(handler http.HandlerFunc, user, method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req = req.WithContext(context.WithValue(req.Context(), userClaimsKey, &auth.UserClaims{UserID: user}))
		res := httptest.NewRecorder()
		handler(res, req)
		if res.Code != status {
			t.Fatalf("%s %s = %d instead of %d: %s", method, path, res.Code, status, res.Body.String())
		}
		return res
	}
	sellerContext := func(w http.ResponseWriter, r *http.Request) { handleSellerTicketContext(w, r, storeRepo) }
	sellerTickets := func(w http.ResponseWriter, r *http.Request) { handleSellerTickets(w, r, storeRepo) }
	supplierContext := func(w http.ResponseWriter, r *http.Request) {
		handleSupplierTicketContext(w, r, storeRepo, supplierRepo)
	}
	supplierTickets := func(w http.ResponseWriter, r *http.Request) { handleSupplierTickets(w, r, storeRepo, supplierRepo) }
	contextPath := "/api/v1/seller/tickets/context?kind=order&referenceId=" + orderID
	preview := call(sellerContext, owner, http.MethodGet, contextPath, "", 200)
	if !strings.Contains(preview.Body.String(), orderNumber) || strings.Contains(preview.Body.String(), "private") {
		t.Fatalf("unsafe order preview: %s", preview.Body.String())
	}
	call(sellerContext, stranger, http.MethodGet, contextPath, "", 404)
	call(sellerContext, owner, http.MethodGet, "/api/v1/seller/tickets/context?kind=financial&referenceType=payout&referenceId="+payoutID+"&storeId="+store, "", 200)
	call(sellerContext, stranger, http.MethodGet, "/api/v1/seller/tickets/context?kind=financial&referenceType=payout&referenceId="+payoutID+"&storeId="+store, "", 404)
	call(supplierContext, supplierUser, http.MethodGet, "/api/v1/supplier/tickets/context?kind=financial&referenceType=adjustment&referenceId="+adjustmentID, "", 200)
	body := `{"subject":"پیگیری سفارش","message":"متن درخواست","kind":"order","referenceId":"` + orderID + `"}`
	call(sellerTickets, stranger, http.MethodPost, "/api/v1/seller/tickets", body, 404)
	call(sellerTickets, owner, http.MethodGet, "/api/v1/seller/tickets", "", 200)
	var total int
	if err := db.QueryRow(`SELECT count(*) FROM support_tickets WHERE requester_user_id IN ($1,$2,$3)`, owner, stranger, supplierUser).Scan(&total); err != nil || total != 0 {
		t.Fatalf("preview or denied request wrote %d tickets: %v", total, err)
	}
	call(sellerTickets, owner, http.MethodPost, "/api/v1/seller/tickets", body, 201)
	call(supplierContext, supplierUser, http.MethodGet, "/api/v1/supplier/tickets/context?kind=shipment&referenceType=carton&referenceId="+shipmentID, "", 200)
	posted := call(supplierTickets, supplierUser, http.MethodPost, "/api/v1/supplier/tickets", `{"subject":"پیگیری محموله","message":"متن درخواست","kind":"shipment","referenceType":"carton","referenceId":"`+shipmentID+`"}`, 201)
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(posted.Body.Bytes(), &result); err != nil || result.ID == "" {
		t.Fatalf("missing created ticket ID: %s %v", posted.Body.String(), err)
	}
	var role, account, category, canonical, status string
	if err := db.QueryRow(`SELECT requester_role,requester_account_id,category,reference_id,status FROM support_tickets WHERE id=$1`, result.ID).Scan(&role, &account, &category, &canonical, &status); err != nil || role != "supplier" || account != supplier || category != "shipment" || canonical != shipmentID || status != "open" {
		t.Fatalf("unexpected ticket metadata: %q %q %q %q %q %v", role, account, category, canonical, status, err)
	}
	staffReply := func(w http.ResponseWriter, r *http.Request) { handleT01AdminTicketReply(w, r, db, result.ID) }
	call(staffReply, staff, http.MethodPost, "/api/v1/admin/tickets/"+result.ID+"/reply", `{"body":"private note","isInternal":true}`, 200)
	call(staffReply, staff, http.MethodPost, "/api/v1/admin/tickets/"+result.ID+"/reply", `{"body":"staff reply"}`, 200)
	if err := db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='ticket_reply' AND payload->>'ticket_id'=$2`, supplierUser, result.ID).Scan(&total); err != nil || total != 1 {
		t.Fatalf("expected one public reply notification, got %d: %v", total, err)
	}
	inbox := call(supplierTickets, supplierUser, http.MethodGet, "/api/v1/supplier/tickets", "", 200)
	if !strings.Contains(inbox.Body.String(), "staff reply") || !strings.Contains(inbox.Body.String(), "assigned") || strings.Contains(inbox.Body.String(), "private note") || strings.Contains(inbox.Body.String(), orderID) {
		t.Fatalf("unexpected supplier inbox: %s", inbox.Body.String())
	}
	staffTicket := func(w http.ResponseWriter, r *http.Request) { handleAdminSupportTicket(w, r, db, result.ID) }
	call(staffTicket, staff, http.MethodPatch, "/api/v1/admin/tickets/"+result.ID, `{"status":"resolved"}`, 200)
	resolved := call(supplierTickets, supplierUser, http.MethodGet, "/api/v1/supplier/tickets", "", 200)
	if !strings.Contains(resolved.Body.String(), `"status":"resolved"`) {
		t.Fatalf("staff resolution missing from requester inbox: %s", resolved.Body.String())
	}
}
