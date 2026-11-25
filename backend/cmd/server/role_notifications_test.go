package main

// Seller / admin / warehouse notification read-path coverage (Phase 2,
// W2): GET/POST /api/v1/{seller,admin,warehouse}/notifications[/read],
// plus the P1-15 missing-table cases (GET envelope carries
// success + unreadCount; mark-all no longer 500s).
//
// Reuses the supplier_notifications_test.go fixture (same package):
// supplierNotificationsSetup seeds users/suppliers/supplier_members/
// notification_outbox on a DISPOSABLE local Postgres pointed at by
// SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL; without it every test
// here skips, like the other DB-gated suites in this package.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

// callRole runs a request through the same RequireRoles gate the
// route in main.go uses, then the role handler.
func (f *supplierNotificationsFixture) callRole(t *testing.T, method, path, token, body string, roles []string, handler func(http.ResponseWriter, *http.Request)) (int, string) {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	RequireRoles(f.jwtService, roles...)(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r)
	})(rec, req)
	return rec.Code, rec.Body.String()
}

func (f *supplierNotificationsFixture) seedRoleUser(t *testing.T, mobile, role string) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(`INSERT INTO users (mobile, full_name, role) VALUES ($1, 'Role', $2) RETURNING id`, mobile, role).Scan(&id); err != nil {
		t.Fatalf("seed role user %s/%s: %v", mobile, role, err)
	}
	return id
}

func TestSellerNotificationsReadPath(t *testing.T) {
	f := supplierNotificationsSetup(t)
	seller := f.seedRoleUser(t, "09120000111", "seller")
	otherSeller := f.seedRoleUser(t, "09120000112", "seller")
	pending := f.seedOutbox(t, seller, "order_placed", "سفارش جدید برای فروشگاه شما ثبت شد", "pending")
	f.seedOutbox(t, seller, "profit_released", "تسویه ماهانه انجام شد", "read")
	f.seedOutbox(t, otherSeller, "order_placed", "سفارش فروشگاه دیگر", "pending")

	tokSeller := f.token(t, seller, "09120000111", "seller")
	tokOther := f.token(t, otherSeller, "09120000112", "seller")
	tokCustomer := f.token(t, f.customerC, "09120000103", "customer")

	sellerHandler := func(w http.ResponseWriter, r *http.Request) { handleSellerNotifications(w, r, f.db) }
	sellerReadHandler := func(w http.ResponseWriter, r *http.Request) { handleSellerNotificationsRead(w, r, f.db) }

	// Seller sees only their own rows, with the correct unread count.
	code, body := f.callRole(t, http.MethodGet, "/api/v1/seller/notifications", tokSeller, "", []string{"seller"}, sellerHandler)
	if code != http.StatusOK {
		t.Fatalf("GET seller: status %d, body %s", code, body)
	}
	var env notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode envelope: %v (body %s)", err, body)
	}
	if !env.Success || len(env.Notifications) != 2 || env.UnreadCount != 1 {
		t.Fatalf("seller envelope = %+v, want success + 2 rows + unreadCount 1", env)
	}

	// The other seller sees only their own single row (no cross-store leak).
	code, body = f.callRole(t, http.MethodGet, "/api/v1/seller/notifications", tokOther, "", []string{"seller"}, sellerHandler)
	if code != http.StatusOK {
		t.Fatalf("GET other seller: status %d, body %s", code, body)
	}
	var envOther notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &envOther); err != nil {
		t.Fatalf("decode envelope other: %v", err)
	}
	if len(envOther.Notifications) != 1 || envOther.UnreadCount != 1 {
		t.Fatalf("other seller envelope = %+v, want 1 row + unreadCount 1", envOther)
	}

	// A customer gets 403 on the seller route; no token gets 401.
	code, _ = f.callRole(t, http.MethodGet, "/api/v1/seller/notifications", tokCustomer, "", []string{"seller"}, sellerHandler)
	if code != http.StatusForbidden {
		t.Fatalf("GET seller as customer: status %d, want 403", code)
	}
	code, _ = f.callRole(t, http.MethodGet, "/api/v1/seller/notifications", "", "", []string{"seller"}, sellerHandler)
	if code != http.StatusUnauthorized {
		t.Fatalf("GET seller without token: status %d, want 401", code)
	}

	// Mark one row read by id; unread count drops to 0.
	code, body = f.callRole(t, http.MethodPost, "/api/v1/seller/notifications/read", tokSeller, `{"id":"`+pending+`"}`, []string{"seller"}, sellerReadHandler)
	if code != http.StatusOK {
		t.Fatalf("POST seller read {id}: status %d, body %s", code, body)
	}
	var readResp struct {
		Success bool `json:"success"`
		Updated int  `json:"updated"`
	}
	if err := json.Unmarshal([]byte(body), &readResp); err != nil || !readResp.Success || readResp.Updated != 1 {
		t.Fatalf("seller read {id} response = %s, want success + updated 1", body)
	}
	assertNotificationIDs(t, f.sellerInbox(t, tokSeller), 0, map[string]bool{
		env.Notifications[0].ID: true,
		env.Notifications[1].ID: true,
	})
	// Mark-all with an empty body still succeeds (nothing left unread).
	code, body = f.callRole(t, http.MethodPost, "/api/v1/seller/notifications/read", tokSeller, "", []string{"seller"}, sellerReadHandler)
	if code != http.StatusOK {
		t.Fatalf("POST seller mark-all: status %d, body %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &readResp); err != nil || !readResp.Success {
		t.Fatalf("seller mark-all response = %s, want success", body)
	}
}

func TestAdminNotificationsReadPath(t *testing.T) {
	f := supplierNotificationsSetup(t)
	admin := f.seedRoleUser(t, "09120000121", "admin")
	superAdmin := f.seedRoleUser(t, "09120000122", "super_admin")
	f.seedOutbox(t, admin, "dispute_filed", "اختلاف جدید ثبت شد", "pending")
	f.seedOutbox(t, superAdmin, "dispute_filed", "اختلاف مدیر دیگر", "pending")

	tokAdmin := f.token(t, admin, "09120000121", "admin")
	tokSuper := f.token(t, superAdmin, "09120000122", "super_admin")
	tokSeller := f.token(t, f.seedRoleUser(t, "09120000123", "seller"), "09120000123", "seller")

	adminHandler := func(w http.ResponseWriter, r *http.Request) { handleAdminNotifications(w, r, f.db) }
	adminReadHandler := func(w http.ResponseWriter, r *http.Request) { handleAdminNotificationsRead(w, r, f.db) }

	// Admin sees only their own rows.
	code, body := f.callRole(t, http.MethodGet, "/api/v1/admin/notifications", tokAdmin, "", []string{"admin"}, adminHandler)
	if code != http.StatusOK {
		t.Fatalf("GET admin: status %d, body %s", code, body)
	}
	var env notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode envelope: %v (body %s)", err, body)
	}
	if !env.Success || len(env.Notifications) != 1 || env.UnreadCount != 1 {
		t.Fatalf("admin envelope = %+v, want success + 1 row + unreadCount 1", env)
	}
	if env.Notifications[0].Kind != "dispute_filed" {
		t.Fatalf("admin row kind = %q, want dispute_filed", env.Notifications[0].Kind)
	}

	// super_admin is admitted by the same RequireRoles("admin") gate.
	code, body = f.callRole(t, http.MethodGet, "/api/v1/admin/notifications", tokSuper, "", []string{"admin"}, adminHandler)
	if code != http.StatusOK {
		t.Fatalf("GET admin as super_admin: status %d, body %s", code, body)
	}
	var envSuper notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &envSuper); err != nil {
		t.Fatalf("decode envelope super: %v", err)
	}
	if len(envSuper.Notifications) != 1 || envSuper.UnreadCount != 1 {
		t.Fatalf("super_admin envelope = %+v, want own 1 row + unreadCount 1", envSuper)
	}

	// A seller gets 403 on the admin route.
	code, _ = f.callRole(t, http.MethodGet, "/api/v1/admin/notifications", tokSeller, "", []string{"admin"}, adminHandler)
	if code != http.StatusForbidden {
		t.Fatalf("GET admin as seller: status %d, want 403", code)
	}

	// Mark-all read works for the admin.
	code, body = f.callRole(t, http.MethodPost, "/api/v1/admin/notifications/read", tokAdmin, "", []string{"admin"}, adminReadHandler)
	if code != http.StatusOK {
		t.Fatalf("POST admin mark-all: status %d, body %s", code, body)
	}
	var readResp struct {
		Success bool `json:"success"`
		Updated int  `json:"updated"`
	}
	if err := json.Unmarshal([]byte(body), &readResp); err != nil || !readResp.Success || readResp.Updated != 1 {
		t.Fatalf("admin mark-all response = %s, want success + updated 1", body)
	}
}

func TestWarehouseNotificationsReadPath(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	f := &supplierNotificationsFixture{db: db, jwtService: auth.NewJWTService("warehouse-notifications-test-secret")}
	f.customerC = f.seedRoleUser(t, "09120000133", "customer")
	staff := f.seedRoleUser(t, "09120000131", "warehouse_staff")
	logistics := f.seedRoleUser(t, "09120000132", "logistics_specialist")
	if _, err := f.db.Exec(`
		INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name)
		VALUES ('qual01-local','QUAL01 Local',1,1,'Local','Tehran'), ('qual01-remote','QUAL01 Remote',2,2,'Remote','Shiraz');
		INSERT INTO inbound_shipments(id,warehouse_id,supplier_name,items_count,cartons_count)
		VALUES ('local-work','qual01-local','Fixture',1,1), ('remote-work','qual01-remote','Fixture',1,1);
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET warehouse_city=' Tehran ', warehouse_city_confirmed=true WHERE id=$1`, staff); err != nil {
		t.Fatal(err)
	}
	local := f.seedOutbox(t, staff, "inbound_created", "Local inbound", "pending")
	remote := f.seedOutbox(t, staff, "inbound_dispatched", "Remote inbound", "pending")
	unproven := f.seedOutbox(t, staff, "return_requested", "Unknown return", "pending")
	f.seedOutbox(t, staff, "supplier_return_in_transit", "Unproven transit", "pending")
	f.seedOutbox(t, staff, "inbound_arrived", "Unknown event", "pending")
	if _, err := f.db.Exec(`UPDATE notification_outbox SET payload = CASE id
		WHEN $1 THEN '{"inbound_id":"local-work"}'::jsonb
		WHEN $2 THEN '{"inbound_id":"remote-work","warehouse_id":"local"}'::jsonb
		ELSE '{"return_id":"not-a-uuid","warehouse_id":"local"}'::jsonb END
		WHERE id IN ($1,$2,$3)`, local, remote, unproven); err != nil {
		t.Fatal(err)
	}
	f.seedOutbox(t, logistics, "return_qc", "مرجوعی آماده QC است", "pending")

	tokStaff := f.token(t, staff, "09120000131", "warehouse_staff")
	tokLogistics := f.token(t, logistics, "09120000132", "logistics_specialist")
	tokCustomer := f.token(t, f.customerC, "09120000133", "customer")

	warehouseRoles := []string{"warehouse_staff", "logistics_specialist", "admin"}
	whHandler := func(w http.ResponseWriter, r *http.Request) { handleWarehouseNotifications(w, r, f.db) }
	whReadHandler := func(w http.ResponseWriter, r *http.Request) { handleWarehouseNotificationsRead(w, r, f.db) }

	// Warehouse staff sees only their own rows.
	code, body := f.callRole(t, http.MethodGet, "/api/v1/warehouse/notifications", tokStaff, "", warehouseRoles, whHandler)
	if code != http.StatusOK {
		t.Fatalf("GET warehouse as staff: status %d, body %s", code, body)
	}
	var env notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode envelope: %v (body %s)", err, body)
	}
	if !env.Success || len(env.Notifications) != 1 || env.UnreadCount != 1 {
		t.Fatalf("warehouse envelope = %+v, want success + 1 row + unreadCount 1", env)
	}
	if env.Notifications[0].Kind != "inbound_created" {
		t.Fatalf("warehouse row kind = %q, want inbound_created", env.Notifications[0].Kind)
	}
	code, body = f.callRole(t, http.MethodPost, "/api/v1/warehouse/notifications/read", tokStaff,
		`{"ids":["`+remote+`","`+unproven+`"]}`, warehouseRoles, whReadHandler)
	var blocked struct{ Updated int }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &blocked) != nil || blocked.Updated != 0 {
		t.Fatalf("out-of-scope mark-by-ID = %d %s, want updated 0", code, body)
	}

	// logistics_specialist is admitted by the same gate and sees only
	// their own rows.
	code, body = f.callRole(t, http.MethodGet, "/api/v1/warehouse/notifications", tokLogistics, "", warehouseRoles, whHandler)
	if code != http.StatusOK {
		t.Fatalf("GET warehouse as logistics: status %d, body %s", code, body)
	}
	var envLog notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &envLog); err != nil {
		t.Fatalf("decode envelope logistics: %v", err)
	}
	if len(envLog.Notifications) != 1 || envLog.Notifications[0].Kind != "return_qc" {
		t.Fatalf("logistics envelope = %+v, want own return_qc row", envLog)
	}

	// A customer gets 403 on the warehouse route.
	code, _ = f.callRole(t, http.MethodGet, "/api/v1/warehouse/notifications", tokCustomer, "", warehouseRoles, whHandler)
	if code != http.StatusForbidden {
		t.Fatalf("GET warehouse as customer: status %d, want 403", code)
	}

	// Mark-all read works for warehouse staff.
	code, body = f.callRole(t, http.MethodPost, "/api/v1/warehouse/notifications/read", tokStaff, "", warehouseRoles, whReadHandler)
	if code != http.StatusOK {
		t.Fatalf("POST warehouse mark-all: status %d, body %s", code, body)
	}
	var readResp struct {
		Success bool `json:"success"`
		Updated int  `json:"updated"`
	}
	if err := json.Unmarshal([]byte(body), &readResp); err != nil || !readResp.Success || readResp.Updated != 1 {
		t.Fatalf("warehouse mark-all response = %s, want success + updated 1", body)
	}
	if _, err := db.Exec(`UPDATE users SET is_active=false WHERE id=$1`, staff); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE notification_outbox SET status='pending', read_at=NULL WHERE id=$1`, local); err != nil {
		t.Fatal(err)
	}
	ctx := repository.WithWarehouseStaffID(context.WithValue(context.Background(), warehouseInboxKey{}, true), staff)
	items, unread, exists, err := listNotificationsForUser(ctx, db, staff)
	if err != nil || !exists || unread != 0 || len(items) != 0 {
		t.Fatalf("inactive staff warehouse inbox: got %v, unread %d, exists %v, error %v", items, unread, exists, err)
	}
	if updated, err := markNotificationsReadForUser(ctx, db, staff, []string{local}); err != nil || updated != 0 {
		t.Fatalf("inactive staff mark-by-ID: updated %d, error %v", updated, err)
	}
}

// TestRoleNotificationsMissingTable (P1-15): with notification_outbox
// dropped, every role GET answers 200 with the normal envelope shape
// (success + empty list + unreadCount 0), and mark-all / single-id
// reads answer 200 with success instead of 500ing on the UPDATE.
func TestRoleNotificationsMissingTable(t *testing.T) {
	f := supplierNotificationsSetup(t)
	seller := f.seedRoleUser(t, "09120000141", "seller")
	admin := f.seedRoleUser(t, "09120000142", "admin")
	if _, err := f.db.Exec(`DROP TABLE notification_outbox`); err != nil {
		t.Fatalf("drop outbox: %v", err)
	}

	tokSeller := f.token(t, seller, "09120000141", "seller")
	tokAdmin := f.token(t, admin, "09120000142", "admin")
	tokCustomer := f.token(t, f.customerC, "09120000103", "customer")

	sellerHandler := func(w http.ResponseWriter, r *http.Request) { handleSellerNotifications(w, r, f.db) }
	sellerReadHandler := func(w http.ResponseWriter, r *http.Request) { handleSellerNotificationsRead(w, r, f.db) }
	adminHandler := func(w http.ResponseWriter, r *http.Request) { handleAdminNotifications(w, r, f.db) }

	code, body := f.callRole(t, http.MethodGet, "/api/v1/seller/notifications", tokSeller, "", []string{"seller"}, sellerHandler)
	if code != http.StatusOK {
		t.Fatalf("GET seller missing table: status %d, body %s", code, body)
	}
	var env notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode envelope: %v (body %s)", err, body)
	}
	if !env.Success || len(env.Notifications) != 0 || env.UnreadCount != 0 {
		t.Fatalf("seller missing-table envelope = %+v, want success + empty + unreadCount 0", env)
	}

	code, body = f.callRole(t, http.MethodGet, "/api/v1/admin/notifications", tokAdmin, "", []string{"admin"}, adminHandler)
	if code != http.StatusOK {
		t.Fatalf("GET admin missing table: status %d, body %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode admin envelope: %v (body %s)", err, body)
	}
	if !env.Success || len(env.Notifications) != 0 || env.UnreadCount != 0 {
		t.Fatalf("admin missing-table envelope = %+v, want success + empty + unreadCount 0", env)
	}

	// Mark-all with no table: 200, not 500 (the P1-15 regression).
	code, body = f.callRole(t, http.MethodPost, "/api/v1/seller/notifications/read", tokSeller, "", []string{"seller"}, sellerReadHandler)
	if code != http.StatusOK {
		t.Fatalf("POST seller mark-all missing table: status %d, body %s", code, body)
	}
	var readResp struct {
		Success bool `json:"success"`
		Updated int  `json:"updated"`
	}
	if err := json.Unmarshal([]byte(body), &readResp); err != nil || !readResp.Success || readResp.Updated != 0 {
		t.Fatalf("seller mark-all missing-table response = %s, want success + updated 0", body)
	}

	// Single-id read with no table: 200 as well.
	code, body = f.callRole(t, http.MethodPost, "/api/v1/seller/notifications/read", tokSeller, `{"id":"00000000-0000-0000-0000-000000000000"}`, []string{"seller"}, sellerReadHandler)
	if code != http.StatusOK {
		t.Fatalf("POST seller read {id} missing table: status %d, body %s", code, body)
	}

	// The customer GET envelope matches too (same P1-15 fix).
	code, body = f.callCustomer(t, tokCustomer)
	if code != http.StatusOK {
		t.Fatalf("GET customer missing table: status %d, body %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode customer envelope: %v (body %s)", err, body)
	}
	if !env.Success || len(env.Notifications) != 0 || env.UnreadCount != 0 {
		t.Fatalf("customer missing-table envelope = %+v, want success + empty + unreadCount 0", env)
	}
}

func (f *supplierNotificationsFixture) seedOutboxFull(t *testing.T, recipient, kind, body, payload, status string) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(`INSERT INTO notification_outbox (recipient_user_id, kind, title, body, payload, status)
		VALUES ($1, $2, $2, $3, $4::jsonb, $5) RETURNING id`, recipient, kind, body, payload, status).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *supplierNotificationsFixture) sellerInbox(t *testing.T, token string) notificationsEnvelope {
	t.Helper()
	code, body := f.callRole(t, http.MethodGet, "/api/v1/seller/notifications", token, "", []string{"seller"},
		func(w http.ResponseWriter, r *http.Request) { handleSellerNotifications(w, r, f.db) })
	return decodeNotificationInbox(t, code, body)
}

func decodeNotificationInbox(t *testing.T, code int, body string) notificationsEnvelope {
	t.Helper()
	var env notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil || code != http.StatusOK || !env.Success {
		t.Fatalf("notification list: status %d, body %s, decode error %v", code, body, err)
	}
	return env
}

func assertNotificationIDs(t *testing.T, env notificationsEnvelope, unread int, want map[string]bool) {
	t.Helper()
	if len(env.Notifications) != len(want) || env.UnreadCount != unread {
		t.Fatalf("got %d notifications and %d unread, want %d notifications and %d unread", len(env.Notifications), env.UnreadCount, len(want), unread)
	}
	seen := make(map[string]bool)
	for _, row := range env.Notifications {
		if !want[row.ID] || seen[row.ID] {
			t.Errorf("unexpected or duplicate notification: %+v", row)
		}
		seen[row.ID] = true
	}
}

var sellerNotificationsIncludedByKind = []string{
	"order_placed", "payment_confirmed", "profit_released", "low_stock", "settlement_created", "settlement_skipped",
}

var sellerNotificationsExcludedKinds = []string{
	"order_status", "payment_failed", "carrier_failed", "supplier_order_accepted",
	"refund_issued", "return_approved", "return_rejected", "return_decision", "return_cod_refused",
	"wallet_withdrawal", "proposal_rejected", "proposal_approved", "supplier_approved", "supplier_rejected",
	"supplier_registered", "supplier_return_reviewed", "supplier_parcel_charge", "return_qc_charged",
	"storage_fee_charged", "return_shipped", "inbound_created", "inbound_dispatched",
	"supplier_return_in_transit", "payout_requested", "dispute_filed", "return_requested", "finance",
}

type sellerNotificationCase struct {
	kind, body, payload string
	included            bool
}

func sellerSharedNotificationCases(owned, otherOwned, foreign, sellerTicket, customerTicket, foreignTicket, ownedApproval, foreignApproval string) []sellerNotificationCase {
	store := func(id string) string { return fmt.Sprintf(`{"store_id":%q}`, id) }
	ticket := func(id string) string { return fmt.Sprintf(`{"ticket_id":%q}`, id) }
	approval := func(id, kind string) string {
		return fmt.Sprintf(`{"approval_id":%q,"entity_type":%q}`, id, kind)
	}
	const sellerDelivery = "سود شما پس از پایان مهلت مرجوعی آزاد می‌شود"
	const supplierDelivery = "سود فروشنده پس از پایان مهلت مرجوعی آزاد می‌شود"
	return []sellerNotificationCase{
		{"delivery_confirmed", sellerDelivery, store(owned), true},
		{"delivery_confirmed", supplierDelivery, store(otherOwned), true},
		{"delivery_confirmed", supplierDelivery, store(foreign), false},
		{"delivery_confirmed", "تحویل سفارش شما تایید شد", store(owned), false},
		{"dispute_resolved", "مسئولیت با فروشنده اعلام شد", store(owned), true},
		{"dispute_resolved", "مسئولیت با تأمین‌کننده اعلام شد", store(owned), false},
		{"dispute_resolved", "نتیجه بررسی اختلاف سفارش شما", store(owned), false},
		{"order_cancelled", "سفارش لغو شد", `{"event":"order_cancelled"}`, true},
		{"order_cancelled", "مهلت پرداخت سفارش به پایان رسید", `{"event":"order_expired","store_id":""}`, false},
		{"finance_adjustment", "", `{"target_kind":"seller"}`, true},
		{"finance_adjustment", "", `{"target_kind":"supplier"}`, false},
		{"payout_reviewed", "", store(owned), true},
		{"payout_reviewed", "", store(""), false},
		{"payout_reviewed", "", store(foreign), false},
		{"payout_transfer_confirmed", "", store(otherOwned), true},
		{"payout_transfer_confirmed", "", `{"store_id":null,"supplier_id":"sup-ntf-a"}`, false},
		{"payout_transfer_confirmed", "", store(foreign), false},
		{"ticket_reply", "", ticket(sellerTicket), true},
		{"ticket_reply", "", ticket(customerTicket), false},
		{"ticket_reply", "", ticket(foreignTicket), false},
		{"approval_rejected", "", approval(ownedApproval, "store_name"), true},
		{"approval_rejected", "", approval(ownedApproval, "store_slug"), true},
		{"approval_rejected", "", approval(ownedApproval, "custom_domain"), true},
		{"approval_rejected", "", approval(ownedApproval, "warranty_property"), false},
		{"approval_rejected", "", approval(foreignApproval, "store_name"), false},
	}
}

func TestSellerNotificationsClassifiesAllKinds(t *testing.T) {
	classified := make(map[string]bool)
	for _, kind := range sellerNotificationsIncludedByKind {
		classified[kind] = true
	}
	for _, kind := range sellerNotificationsExcludedKinds {
		classified[kind] = true
	}
	for _, row := range sellerSharedNotificationCases("", "", "", "", "", "", "", "") {
		classified[row.kind] = true
	}
	for _, kind := range repository.AllNotifKinds {
		if !classified[kind] {
			t.Errorf("notification kind %q needs a seller inbox audience classification", kind)
		}
	}
}

func TestSellerNotificationsKindMatrix(t *testing.T) {
	f := supplierNotificationsSetup(t)
	const mobile = "09120000151"
	seller := f.seedRoleUser(t, mobile, "seller")
	foreign := f.customerC
	seed := func(insert, delete string, args ...any) {
		t.Helper()
		if _, err := f.db.Exec(insert, args...); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := f.db.Exec(delete, args[0]); err != nil {
				t.Error(err)
			}
		})
	}
	makeStore := func(owner string) string {
		id := "store-" + uuid.NewString()
		seed(`INSERT INTO stores (id, slug, name, owner_name, owner_user_id) VALUES ($1,$1,'Inbox test','Owner',$2)`,
			`DELETE FROM stores WHERE id=$1`, id, owner)
		return id
	}
	makeTicket := func(role, requester string) string {
		id := "ticket-" + uuid.NewString()
		seed(`INSERT INTO support_tickets (id, requester_role, requester_user_id, subject) VALUES ($1,$2,$3,'Inbox test')`,
			`DELETE FROM support_tickets WHERE id=$1`, id, role, requester)
		return id
	}
	makeApproval := func(storeID string) string {
		id := "approval-" + uuid.NewString()
		seed(`INSERT INTO pending_approvals (id, target_id, entity_type, title, requested_data, requested_by)
			VALUES ($1,$2,'store_name','Inbox test','{}',$3)`, `DELETE FROM pending_approvals WHERE id=$1`, id, storeID, seller)
		return id
	}
	owned, otherOwned, foreignStore := makeStore(seller), makeStore(seller), makeStore(foreign)
	cases := sellerSharedNotificationCases(owned, otherOwned, foreignStore, makeTicket("seller", seller),
		makeTicket("customer", seller), makeTicket("seller", foreign), makeApproval(owned), makeApproval(foreignStore))
	for _, kind := range sellerNotificationsIncludedByKind {
		cases = append(cases, sellerNotificationCase{kind, "", "{}", true})
	}
	for _, kind := range sellerNotificationsExcludedKinds {
		payload := "{}"
		if kind == "wallet_withdrawal" {
			payload = `{"withdrawal_id":"CWD-test","status":"paid"}`
		}
		cases = append(cases, sellerNotificationCase{kind, "", payload, false})
	}
	sellerIDs, customerIDs := make(map[string]bool), make(map[string]bool)
	for _, row := range cases {
		id := f.seedOutboxFull(t, seller, row.kind, row.body, row.payload, "pending")
		if row.included {
			sellerIDs[id] = true
		}
		customerIDs[id] = true
	}

	token, err := f.jwtService.GenerateTokenWithCapabilities(seller, mobile, "Test", "seller", []string{"seller", "customer", "supplier"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	assertNotificationIDs(t, f.sellerInbox(t, token), len(sellerIDs), sellerIDs)
	code, body := f.callCustomer(t, token)
	assertNotificationIDs(t, decodeNotificationInbox(t, code, body), len(customerIDs), customerIDs)
}

func TestSellerNotificationsMarkReadIsolation(t *testing.T) {
	f := supplierNotificationsSetup(t)
	const mobile = "09120000152"
	seller := f.seedRoleUser(t, mobile, "seller")
	sellerToken := f.token(t, seller, mobile, "seller")
	customerToken := f.token(t, seller, mobile, "customer")
	buyerID := f.seedOutbox(t, seller, "order_status", "Buyer row", "pending")
	sellerID := f.seedOutbox(t, seller, "order_placed", "Seller row", "pending")

	sellerRead := func(w http.ResponseWriter, r *http.Request) { handleSellerNotificationsRead(w, r, f.db) }
	assertUpdated := func(code int, body string, want int) {
		t.Helper()
		var result struct {
			Success bool `json:"success"`
			Updated int  `json:"updated"`
		}
		if err := json.Unmarshal([]byte(body), &result); err != nil || code != http.StatusOK || !result.Success || result.Updated != want {
			t.Fatalf("mark read: status %d, body %s, want %d updates", code, body, want)
		}
	}
	code, body := f.callRole(t, http.MethodPost, "/api/v1/seller/notifications/read", sellerToken, fmt.Sprintf(`{"id":%q}`, buyerID), []string{"seller"}, sellerRead)
	assertUpdated(code, body, 0)
	code, body = f.callRole(t, http.MethodPost, "/api/v1/seller/notifications/read", sellerToken, "", []string{"seller"}, sellerRead)
	assertUpdated(code, body, 1)
	assertNotificationIDs(t, f.sellerInbox(t, sellerToken), 0, map[string]bool{sellerID: true})

	code, body = f.callCustomer(t, customerToken)
	env := decodeNotificationInbox(t, code, body)
	assertNotificationIDs(t, env, 1, map[string]bool{buyerID: true, sellerID: true})
	for _, row := range env.Notifications {
		if row.ID == buyerID && row.Status != "pending" {
			t.Fatalf("buyer row status = %q, want pending", row.Status)
		}
	}

	// The existing customer mark-all is recipient-wide, including seller rows.
	f.seedOutbox(t, seller, "profit_released", "Another seller row", "pending")
	code, body = f.callRole(t, http.MethodPost, "/api/v1/notifications/read", customerToken, "", []string{"customer"},
		func(w http.ResponseWriter, r *http.Request) { handleT01NotificationsRead(w, r, f.db) })
	assertUpdated(code, body, 2)
	if env := f.sellerInbox(t, sellerToken); env.UnreadCount != 0 {
		t.Fatalf("seller unread count after customer mark-all = %d, want 0", env.UnreadCount)
	}
}

func TestSellerNotificationsLegacyInboxIsEmpty(t *testing.T) {
	f := supplierNotificationsSetup(t)
	const mobile = "09120000153"
	seller := f.seedRoleUser(t, mobile, "seller")
	// Reproduce the ten audited payload shapes from DRP-28 §1.2.
	// Use fixture values where the brief provides field names without values.
	rows := []sellerNotificationCase{
		{kind: "proposal_rejected", body: "پیشنهاد ساعت هوشمند اولترا تست رد شد", payload: `{"event":"proposal_rejected","reason":"test","entity_type":"proposed_product","proposal_id":"prop-c55f7ec3-3105-412d-829b-13d1c8f7eb5f","product_title":"ساعت هوشمند اولترا تست"}`},
		{kind: "order_status", body: "تحویل سفارش ORD-1C7E8A6A تایید شد", payload: `{"event":"delivery_confirmed","order_id":"legacy-1","order_number":"ORD-1C7E8A6A","window_days":7}`},
		{kind: "order_status", body: "سفارش شما ارسال شد", payload: `{"event":"shipped","order_id":"legacy-1","order_number":"ORD-1C7E8A6A","tracking":"test-1"}`},
		{kind: "order_status", body: "سفارش شما بسته‌بندی شد", payload: `{"event":"packed","order_id":"legacy-1","order_number":"ORD-1C7E8A6A"}`},
		{kind: "order_status", body: "سفارش شما ارسال شد", payload: `{"event":"shipped","order_id":"legacy-2","order_number":"ORD-0724E351","tracking":"test-2"}`},
		{kind: "order_status", body: "سفارش شما بسته‌بندی شد", payload: `{"event":"packed","order_id":"legacy-3","order_number":"ORD-E5CFEE78"}`},
		{kind: "order_status", body: "تحویل سفارش ORD-DADE392E تایید شد", payload: `{"event":"delivery_confirmed","order_id":"legacy-4","order_number":"ORD-DADE392E","window_days":7}`},
		{kind: "return_decision", body: "درخواست مرجوعی شما تایید شد", payload: `{"decision":"approved","return_id":"legacy-return","order_number":"ORD-1C7E8A6A"}`},
		{kind: "wallet_withdrawal", payload: `{"amount":100000,"status":"approved","withdrawal_id":"CWD-bec52881-77f4-498a-8138-2c32856b9d79"}`},
		{kind: "wallet_withdrawal", payload: `{"amount":100000,"status":"paid","withdrawal_id":"CWD-bec52881-77f4-498a-8138-2c32856b9d79","transfer_reference":"TEST-LIVE-1404-0728"}`},
	}
	for _, row := range rows {
		f.seedOutboxFull(t, seller, row.kind, row.body, row.payload, "read")
	}
	assertNotificationIDs(t, f.sellerInbox(t, f.token(t, seller, mobile, "seller")), 0, nil)
}
