package main

// Supplier notification read-path coverage (GET/POST
// /api/v1/supplier/notifications[/read]) plus a regression check that
// the customer /api/v1/notifications envelope is unchanged by the
// shared-helper extraction.
//
// Runs ONLY when SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL points at a
// DISPOSABLE local Postgres (never the live database); otherwise
// skips, like the other DB-gated suites in this package. The DDL is
// the minimal faithful subset the middleware + handlers touch: users,
// suppliers, supplier_members (GetSupplierAccess member path),
// notification_outbox.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	_ "github.com/lib/pq"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

const supplierNotificationsDDL = `
DROP TABLE IF EXISTS notification_outbox, supplier_members, suppliers, users CASCADE;
CREATE TABLE users (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	mobile VARCHAR(15) UNIQUE NOT NULL,
	full_name VARCHAR(100) NOT NULL DEFAULT '',
	role VARCHAR(20) NOT NULL DEFAULT 'customer',
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	email VARCHAR(255),
	username VARCHAR(255),
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	failed_login_attempts integer NOT NULL DEFAULT 0,
	locked_until timestamp with time zone,
	password_hash text,
	warehouse_city character varying(50) NOT NULL DEFAULT 'تهران'::character varying);
CREATE TABLE suppliers (
	id VARCHAR(50) PRIMARY KEY,
	owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
	name VARCHAR(200) NOT NULL,
	approval_status VARCHAR(20) DEFAULT 'approved',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	warehouse_city VARCHAR(255) NOT NULL DEFAULT 'تهران',
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	approved_at timestamp with time zone,
	approved_by character varying(50),
	badge character varying(50),
	default_markup_type character varying(10),
	default_markup_value numeric(10,2),
	delivery_score integer DEFAULT 100,
	description text,
	logo_url text,
	rating numeric(3,2) DEFAULT 5.00,
	rejection_reason text,
	signed_contract_version integer NOT NULL DEFAULT 1,
	supports_direct_shipping boolean DEFAULT false,
	suspended_at timestamp with time zone,
	suspended_by text,
	suspension_reason text,
	tapin_shop_id text,
	tapin_token text,
	total_products integer DEFAULT 0,
	warehouse_city_confirmed boolean NOT NULL DEFAULT false,
	warehouse_city_source character varying(20));
CREATE TABLE supplier_members (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	supplier_id VARCHAR(50) NOT NULL,
	user_id UUID NOT NULL,
	role_key VARCHAR(50) NOT NULL DEFAULT 'manager',
	permissions JSONB NOT NULL DEFAULT '[]'::jsonb,
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	invited_by uuid,
	updated_at timestamp with time zone NOT NULL DEFAULT now());
DO $$
BEGIN
	IF to_regclass('stores') IS NULL THEN
		CREATE TABLE stores (
			id VARCHAR(50) PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			slug VARCHAR(100) UNIQUE NOT NULL,
			owner_name VARCHAR(100) NOT NULL,
			owner_user_id UUID
		);
	ELSIF EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = to_regclass('stores')
		AND attname = 'owner_user_id' AND atttypid <> 'uuid'::regtype) THEN
		ALTER TABLE stores ALTER COLUMN owner_user_id TYPE uuid USING CASE
			WHEN owner_user_id ~* '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$' THEN owner_user_id::uuid
			ELSE md5(owner_user_id)::uuid
		END;
	END IF;
END $$;
DO $$
BEGIN
	IF to_regclass('support_tickets') IS NULL THEN
		CREATE TABLE support_tickets (
			id VARCHAR(50) PRIMARY KEY,
			requester_role VARCHAR(30) NOT NULL,
			requester_user_id UUID,
			subject VARCHAR(200) NOT NULL
		);
	ELSIF EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = to_regclass('support_tickets')
		AND attname = 'requester_user_id' AND atttypid <> 'uuid'::regtype) THEN
		ALTER TABLE support_tickets ALTER COLUMN requester_user_id TYPE uuid USING CASE
			WHEN requester_user_id ~* '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$' THEN requester_user_id::uuid
			ELSE md5(requester_user_id)::uuid
		END;
	END IF;
END $$;
CREATE TABLE IF NOT EXISTS pending_approvals (
	id VARCHAR(50) PRIMARY KEY,
	target_id VARCHAR(50) NOT NULL,
	entity_type VARCHAR(30) NOT NULL,
	title VARCHAR(200) NOT NULL,
	requested_data JSONB NOT NULL,
	requested_by VARCHAR(100) NOT NULL
);
CREATE TABLE notification_outbox (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	recipient_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind VARCHAR(50) NOT NULL,
	title VARCHAR(200) NOT NULL,
	body TEXT NOT NULL,
	payload JSONB NOT NULL DEFAULT '{}'::jsonb,
	status VARCHAR(20) NOT NULL DEFAULT 'pending'
		CHECK (status IN ('pending', 'sent', 'read')),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	sent_at TIMESTAMPTZ,
	read_at TIMESTAMPTZ
);
`

type supplierNotificationsFixture struct {
	db         *sql.DB
	repo       *repository.SupplierRepository
	jwtService *auth.JWTService
	ownerA     string
	ownerB     string
	customerC  string
}

func supplierNotificationsSetup(t *testing.T) *supplierNotificationsFixture {
	t.Helper()
	dsn := os.Getenv("SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL not set; skipping disposable-DB integration test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	if _, err := db.Exec(supplierNotificationsDDL); err != nil {
		t.Fatalf("seed DDL: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	f := &supplierNotificationsFixture{
		db:         db,
		repo:       repository.NewSupplierRepository(db),
		jwtService: auth.NewJWTService("supplier-notifications-test-secret"),
	}
	// Two approved suppliers with distinct owners, plus one customer.
	for _, u := range []struct {
		id  *string
		mob string
		sup string
	}{
		{&f.ownerA, "09120000101", "sup-ntf-a"},
		{&f.ownerB, "09120000102", "sup-ntf-b"},
	} {
		if err := db.QueryRow(`INSERT INTO users (mobile, full_name, role) VALUES ($1, 'Owner', 'supplier') RETURNING id`, u.mob).Scan(u.id); err != nil {
			t.Fatalf("seed owner: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO suppliers (id, owner_user_id, name) VALUES ($1, $2, 'Supplier')`, u.sup, *u.id); err != nil {
			t.Fatalf("seed supplier: %v", err)
		}
	}
	if err := db.QueryRow(`INSERT INTO users (mobile, full_name, role) VALUES ('09120000103', 'Customer', 'customer') RETURNING id`).Scan(&f.customerC); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return f
}

func (f *supplierNotificationsFixture) seedOutbox(t *testing.T, recipient, kind, title, status string) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(`INSERT INTO notification_outbox (recipient_user_id, kind, title, body, payload, status)
		VALUES ($1, $2, $3, 'body', '{"proposal_id":"prop-x","event":"proposal_rejected","entity_type":"proposed_product","product_title":"کالا"}'::jsonb, $4)
		RETURNING id`, recipient, kind, title, status).Scan(&id); err != nil {
		t.Fatalf("seed outbox: %v", err)
	}
	return id
}

func (f *supplierNotificationsFixture) seedOutboxWithBody(t *testing.T, recipient, kind, title, body string, payload map[string]interface{}) {
	repository.NotifyBestEffort(context.Background(), f.db, recipient, kind, title, body, payload, t.Name())
}

func (f *supplierNotificationsFixture) token(t *testing.T, userID, mobile, role string) string {
	t.Helper()
	tok, err := f.jwtService.GenerateToken(userID, mobile, "Test", role, time.Hour)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return tok
}

// callSupplier runs a request through the real middleware chain for
// the supplier routes (RequireSupplierAccess + handler).
func (f *supplierNotificationsFixture) callSupplier(t *testing.T, method, path, token, body string) (int, string) {
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
	h := RequireSupplierAccess(f.jwtService, f.repo)
	if strings.HasSuffix(path, "/read") {
		h(func(w http.ResponseWriter, r *http.Request) { handleSupplierNotificationsRead(w, r, f.db) })(rec, req)
	} else {
		h(func(w http.ResponseWriter, r *http.Request) { handleSupplierNotifications(w, r, f.db) })(rec, req)
	}
	return rec.Code, rec.Body.String()
}

func (f *supplierNotificationsFixture) callCustomer(t *testing.T, token string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	RequireRoles(f.jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) {
		handleT01Notifications(w, r, f.db)
	})(rec, req)
	return rec.Code, rec.Body.String()
}

type notificationsEnvelope struct {
	Success       bool `json:"success"`
	UnreadCount   int  `json:"unreadCount"`
	Notifications []struct {
		ID      string                 `json:"id"`
		Kind    string                 `json:"kind"`
		Title   string                 `json:"title"`
		Body    string                 `json:"body"`
		Payload map[string]interface{} `json:"payload"`
		Status  string                 `json:"status"`
		Created string                 `json:"createdAt"`
	} `json:"notifications"`
}

func TestSupplierNotificationsReadPath(t *testing.T) {
	f := supplierNotificationsSetup(t)
	pendingA := f.seedOutbox(t, f.ownerA, "proposal_rejected", "پیشنهاد «کالا» رد شد", "pending")
	f.seedOutbox(t, f.ownerA, "proposal_rejected", "پیشنهاد «کالای قدیمی» رد شد", "read")
	f.seedOutbox(t, f.ownerB, "proposal_rejected", "پیشنهاد «کالای ب» رد شد", "pending")

	tokA := f.token(t, f.ownerA, "09120000101", "supplier")
	tokB := f.token(t, f.ownerB, "09120000102", "supplier")
	tokC := f.token(t, f.customerC, "09120000103", "customer")

	// Owner A sees only their own rows, with the correct unread count.
	code, body := f.callSupplier(t, http.MethodGet, "/api/v1/supplier/notifications", tokA, "")
	if code != http.StatusOK {
		t.Fatalf("GET as owner A: status %d, body %s", code, body)
	}
	var env notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode envelope: %v (body %s)", err, body)
	}
	if !env.Success || len(env.Notifications) != 2 || env.UnreadCount != 1 {
		t.Fatalf("owner A envelope = %+v, want success + 2 rows + unreadCount 1", env)
	}
	for _, n := range env.Notifications {
		if n.Kind != "proposal_rejected" || n.Payload["event"] != "proposal_rejected" {
			t.Fatalf("row %+v: kind/payload mismatch", n)
		}
	}

	// Owner B sees only their own single row.
	code, body = f.callSupplier(t, http.MethodGet, "/api/v1/supplier/notifications", tokB, "")
	if code != http.StatusOK {
		t.Fatalf("GET as owner B: status %d, body %s", code, body)
	}
	var envB notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &envB); err != nil {
		t.Fatalf("decode envelope B: %v", err)
	}
	if len(envB.Notifications) != 1 || envB.UnreadCount != 1 {
		t.Fatalf("owner B envelope = %+v, want 1 row + unreadCount 1", envB)
	}
	if envB.Notifications[0].Title != "پیشنهاد «کالای ب» رد شد" {
		t.Fatalf("owner B row title = %q (cross-supplier leak?)", envB.Notifications[0].Title)
	}

	// A non-supplier gets 403 on the supplier route.
	code, _ = f.callSupplier(t, http.MethodGet, "/api/v1/supplier/notifications", tokC, "")
	if code != http.StatusForbidden {
		t.Fatalf("GET as customer: status %d, want 403", code)
	}
	// No token at all → 401.
	code, _ = f.callSupplier(t, http.MethodGet, "/api/v1/supplier/notifications", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("GET without token: status %d, want 401", code)
	}

	// Mark one row read by id; unread count drops to 0.
	code, body = f.callSupplier(t, http.MethodPost, "/api/v1/supplier/notifications/read", tokA, `{"id":"`+pendingA+`"}`)
	if code != http.StatusOK {
		t.Fatalf("POST read {id}: status %d, body %s", code, body)
	}
	var readResp struct {
		Success bool `json:"success"`
		Updated int  `json:"updated"`
	}
	if err := json.Unmarshal([]byte(body), &readResp); err != nil || !readResp.Success || readResp.Updated != 1 {
		t.Fatalf("read {id} response = %s, want success + updated 1", body)
	}
	// Re-reading the same id is idempotent (updated 0, still 200).
	code, body = f.callSupplier(t, http.MethodPost, "/api/v1/supplier/notifications/read", tokA, `{"id":"`+pendingA+`"}`)
	if code != http.StatusOK {
		t.Fatalf("POST read same id again: status %d", code)
	}
	if err := json.Unmarshal([]byte(body), &readResp); err != nil || readResp.Updated != 0 {
		t.Fatalf("re-read response = %s, want updated 0", body)
	}
	// Mark-all with an empty body still succeeds (nothing left unread).
	code, body = f.callSupplier(t, http.MethodPost, "/api/v1/supplier/notifications/read", tokA, "")
	if code != http.StatusOK {
		t.Fatalf("POST read mark-all: status %d, body %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &readResp); err != nil || !readResp.Success {
		t.Fatalf("mark-all response = %s, want success", body)
	}
	code, body = f.callSupplier(t, http.MethodGet, "/api/v1/supplier/notifications", tokA, "")
	var env2 notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &env2); err != nil {
		t.Fatalf("decode envelope after read: %v", err)
	}
	if env2.UnreadCount != 0 {
		t.Fatalf("unreadCount after reads = %d, want 0", env2.UnreadCount)
	}
}

func TestCustomerNotificationsEnvelopeUnchanged(t *testing.T) {
	f := supplierNotificationsSetup(t)
	f.seedOutbox(t, f.customerC, "order_status", "سفارش شما ارسال شد", "pending")
	f.seedOutbox(t, f.customerC, "proposal_rejected", "پیشنهاد «کالا» رد شد", "read")

	code, body := f.callCustomer(t, f.token(t, f.customerC, "09120000103", "customer"))
	if code != http.StatusOK {
		t.Fatalf("customer GET: status %d, body %s", code, body)
	}
	var env notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode customer envelope: %v (body %s)", err, body)
	}
	if !env.Success || len(env.Notifications) != 2 || env.UnreadCount != 1 {
		t.Fatalf("customer envelope = %+v, want success + 2 rows + unreadCount 1", env)
	}
	// Newest first; payload survives the extraction as raw JSON.
	if env.Notifications[0].Payload["event"] != "proposal_rejected" {
		t.Fatalf("first row payload = %v, want proposal_rejected event", env.Notifications[0].Payload)
	}
	// A supplier token must not open the customer route.
	code, _ = f.callCustomer(t, f.token(t, f.ownerA, "09120000101", "supplier"))
	if code != http.StatusForbidden {
		t.Fatalf("customer GET as supplier: status %d, want 403", code)
	}
}

func TestSupplierNotificationsSupplierKindFilter(t *testing.T) {
	type seed struct {
		kind, title, body string
	}
	keep := []seed{
		{"proposal_rejected", "Proposal rejected", "مدیر پلتفرم پیشنهاد کالای «کالا» را تأیید نکرد. دلیلی ثبت نشده است؛ جزئیات را در بخش «پیشنهادها» ببینید و در صورت نیاز پیشنهاد را ویرایش و دوباره ارسال کنید."}, // supplier_repo.go:813
		{"proposal_approved", "Proposal approved", "پیشنهاد کالای «کالا» تایید شد و کالا در کاتالوگ منتشر شد."},                                                                                                     // notifications.go:811
		{"supplier_approved", "Supplier approved", "حساب تأمین‌کننده شما تایید و فعال شد؛ می‌توانید وارد پنل تأمین‌کننده شوید."},                                                                                    // notifications.go:866
		{"supplier_rejected", "Supplier rejected", "حساب تأمین‌کننده شما تایید نشد."},                                                                                                                               // notifications.go:871
		{"approval_rejected", "Approval rejected", "درخواست «کالا» تایید نشد."},                                                                                                                                     // notifications.go:830
		{"ticket_reply", "Ticket reply", "پشتیبانی در تیکت «کالا» پاسخ داد: پاسخ تست"},                                                                                                                              // notifications.go:917
		{"order_placed", "Order placed", "سفارش ORD-FILTER در فروشگاه شما ثبت شد. برای آماده‌سازی و ارسال، به بخش سفارش‌ها بروید."},                                                                                 // notifications.go:342
		{"payment_confirmed", "Payment confirmed", "پرداخت سفارش ORD-FILTER تایید شد و آماده‌سازی آن آزاد شد. مبلغ قابل پرداخت: 500,000 تومان."},                                                                    // notifications.go:358
		{"low_stock", "Low stock", "موجودی کالای «تیشرت زنانه» به 5 عدد رسید. برای جلوگیری از اتمام موجودی، آن را شارژ کنید."},                                                                                      // notifications.go:542
		{"profit_released", "Profit released", "مهلت مرجوعی سفارش ORD-FILTER پایان یافت و سود فروشنده آن آزاد شد."},                                                                                                 // notifications.go:468
		{"finance_adjustment", "Finance adjustment", "سند تعدیل مالی ADJ-1 برای شما صادر شد: بدهکار (کاهش مانده) به مبلغ 100,000 تومان. علت: تعدیل تست"},                                                            // notifications.go:696
		{"payout_reviewed", "Payout reviewed", "درخواست تسویه شما به مبلغ 100,000 تومان تایید شد و برای واریز بانکی در نوبت قرار گرفت."},                                                                            // notifications.go:729
		{"payout_transfer_confirmed", "Payout transfer confirmed", "مبلغ 100,000 تومان به حساب شبای شما واریز شد. شماره مرجع واریز: REF-1"},                                                                         // notifications.go:752
		{"supplier_return_reviewed", "Supplier return reviewed", "درخواست مرجوعی کالای شما تایید شد."},                                                                                                              // notifications.go:1070
		{"supplier_parcel_charge", "Supplier parcel charge", "هزینه حمل مرسوله مرجوعی به مبلغ 100,000 تومان تایید و از حساب شما کسر شد."},                                                                           // notifications.go:1089
		{"return_qc_charged", "Return QC charged", "در مرجوعی RET-1 (سفارش ORD-FILTER) هزینه حمل 100,000 تومان بابت 1 قلم کالا از حساب شما کسر شد."},                                                                // notifications.go:1015
		{"return_shipped", "Return shipped", "مشتری مرجوعی سفارش ORD-FILTER را ارسال کرد. کد رهگیری: TRK123"},                                                                                                       // notifications.go:971
		{"storage_fee_charged", "Storage fee charged", "هزینه انبارداری 1 جایگاه کالای شما به مبلغ 100,000 تومان محاسبه و از موجودی شما کسر شد. جزئیات را در بخش مالی پنل تأمین‌کننده ببینید."},                     // notifications.go:1120
	}
	absent := []seed{
		{"order_status", "Order packed", "سفارش ORD-FILTER بسته‌بندی شد و آماده ارسال است."},                                                                                                                                      // warehouse_repo.go:1367
		{"payment_failed", "Payment failed", "پرداخت سفارش ORD-FILTER توسط درگاه تایید نشد و مبلغی از حساب شما کسر نشده است. می‌توانید دوباره برای پرداخت اقدام کنید."},                                                           // notifications.go:380
		{"carrier_failed", "Carrier failed", "شرکت حمل‌ونقل ارسال سفارش ORD-FILTER را ناموفق اعلام کرد؛ پرونده آن برای بررسی به پشتیبانی ارجاع شد. نتیجه بررسی به شما اطلاع داده می‌شود."},                                        // notifications.go:485
		{"supplier_order_accepted", "Supplier order accepted", "سفارش ORD-FILTER پذیرفته شد و در حال آماده‌سازی است."},                                                                                                            // notifications.go:502
		{"refund_issued", "Refund issued", "مبلغ 100,000 تومان بابت سفارش ORD-FILTER به کیف پول شما بازگشت."},                                                                                                                     // notifications.go:667
		{"return_approved", "Return approved", "درخواست مرجوعی سفارش ORD-FILTER تایید شد؛ کالا را طبق دستورالعمل مرجوعی ارسال کنید."},                                                                                             // notifications.go:947
		{"return_rejected", "Return rejected", "درخواست مرجوعی سفارش ORD-FILTER تایید نشد."},                                                                                                                                      // notifications.go:951
		{"return_cod_refused", "Return COD refused", "مرسوله مرجوعی سفارش ORD-FILTER به‌صورت پس‌کرایه (پرداخت در مقصد) ارسال شده بود و انبار آن را تحویل نگرفت. مرجوعی را با پرداخت پیش‌کرایه دوباره ارسال کنید."},                // notifications.go:1032
		{"dispute_filed", "Dispute filed", "مشتری برای سفارش ORD-FILTER اختلاف ثبت کرد (نوع: damaged، مبلغ ادعا: 100,000 تومان). برای داوری به بخش اختلاف‌ها بروید."},                                                             // notifications.go:599
		{"payout_requested", "Payout requested", "درخواست تسویه به مبلغ 100,000 تومان ثبت شد و در انتظار بررسی مالی است."},                                                                                                        // notifications.go:768
		{"supplier_registered", "Supplier registered", "تأمین‌کننده «کالا» ثبت‌نام کرد و حساب آن فعال شد."},                                                                                                                       // notifications.go:849
		{"settlement_skipped", "Settlement skipped", "تسویه دوره 1405-08 به مبلغ 100,000 تومان به دلیل نبود اطلاعات بانکی (شبا) انجام نشد و مبلغ در حساب شما باقی ماند. برای دریافت تسویه، اطلاعات بانکی فروشگاه را تکمیل کنید."}, // notifications.go:783
		{"settlement_created", "Settlement created", "تسویه ماهانه دوره 1405-08 به مبلغ 100,000 تومان برای شما ثبت شد و در انتظار بررسی مالی است."},                                                                               // notifications.go:798
		{"return_requested", "Return requested", "مشتری برای سفارش ORD-FILTER درخواست مرجوعی ثبت کرد. علت: آسیب‌دیده"},                                                                                                            // notifications.go:932
		{"inbound_created", "Inbound created", "تأمین‌کننده کالا یک محموله ورودی ثبت کرد. برای تحویل و کنترل کیفیت به بخش ورودی‌ها بروید."},                                                                                       // notifications.go:1047
		{"inbound_dispatched", "Inbound dispatched", "تأمین‌کننده کالا محموله ورودی را ارسال کرد و در راه انبار است."},                                                                                                            // notifications.go:1052
		{"supplier_return_in_transit", "Supplier return in transit", "یک مرجوعی تأمین‌کننده تحویل پست شد و در راه مقصد است؛ برای تحویل آن آماده باشید."},                                                                          // notifications.go:1104
	}
	dual := []seed{
		{"delivery_confirmed", "Supplier delivery confirmed", "تحویل سفارش ORD-FILTER تایید شد. سود فروشنده پس از پایان مهلت مرجوعی آزاد می‌شود."},                                              // notifications.go:440
		{"order_cancelled", "Supplier order cancelled", "سفارش ORD-FILTER لغو شد؛ آماده‌سازی آن را متوقف کنید."},                                                                                // notifications.go:396
		{"dispute_resolved", "Supplier dispute resolved", "در پرونده اختلاف سفارش ORD-FILTER مسئولیت با تأمین‌کننده اعلام شد (مبلغ ادعا: 100,000 تومان). جزئیات را در پنل تأمین‌کننده ببینید."}, // notifications.go:632
	}
	otherCopies := []seed{
		{"order_status", "Order shipped", "سفارش ORD-FILTER به اداره پست تحویل داده شد و در راه است. کد رهگیری: TRK123"},                                                       // warehouse_repo.go:1458
		{"delivery_confirmed", "Customer delivery confirmed", "تحویل سفارش ORD-FILTER تایید شد. اگر مشکلی در کالا هست، تا پایان مهلت مرجوعی از بخش مرجوعی اقدام کنید."},        // notifications.go:438
		{"delivery_confirmed", "Store delivery confirmed", "تحویل سفارش ORD-FILTER تایید شد. سود شما پس از پایان مهلت مرجوعی آزاد می‌شود."},                                    // notifications.go:442
		{"order_cancelled", "Customer order expired", "مهلت پرداخت سفارش ORD-FILTER به پایان رسید و سفارش به‌صورت خودکار لغو شد. برای خرید دوباره، سفارش جدید ثبت کنید."},      // notifications.go:417
		{"order_cancelled", "Store order cancelled", "سفارش ORD-FILTER لغو شد."},                                                                                               // notifications.go:398
		{"dispute_resolved", "Customer dispute upheld", "پرونده اختلاف سفارش ORD-FILTER به نفع شما حل شد و درخواست بازپرداخت مبلغ 100,000 تومان ثبت شد. طرف مسئول: supplier."}, // notifications.go:623
		{"dispute_resolved", "Customer dispute rejected", "پرونده اختلاف سفارش ORD-FILTER بررسی شد و ادعای شما رد شد؛ مبلغی بازپرداخت نمی‌شود. طرف مسئول: seller."},            // notifications.go:621
		{"dispute_resolved", "Seller dispute resolved", "در پرونده اختلاف سفارش ORD-FILTER مسئولیت با فروشنده اعلام شد (مبلغ ادعا: 100,000 تومان)."},                           // notifications.go:640
	}

	partition := map[string]bool{}
	for _, group := range [][]seed{keep, dual, absent} {
		for _, row := range group {
			if partition[row.kind] {
				t.Errorf("kind %q is repeated in the partition", row.kind)
			}
			partition[row.kind] = true
		}
	}
	for _, kind := range repository.AllNotifKinds {
		if !partition[kind] {
			t.Errorf("registry kind %q has no partition entry or is repeated in the registry", kind)
		}
		delete(partition, kind)
	}
	for kind := range partition {
		t.Errorf("partition kind %q is missing from AllNotifKinds", kind)
	}

	f := supplierNotificationsSetup(t)
	want := map[string]seed{}
	for _, group := range [][]seed{keep, dual} {
		for _, row := range group {
			want[row.title] = row
		}
	}
	for _, group := range [][]seed{keep, dual, absent, otherCopies} {
		for _, row := range group {
			f.seedOutboxWithBody(t, f.ownerA, row.kind, row.title, row.body, map[string]interface{}{})
		}
	}
	var seeded int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM notification_outbox WHERE recipient_user_id = $1", f.ownerA).Scan(&seeded); err != nil || seeded != len(keep)+len(dual)+len(absent)+len(otherCopies) {
		t.Fatalf("seeded %d outbox rows, query error %v", seeded, err)
	}

	token := f.token(t, f.ownerA, "09120000101", "supplier")
	code, body := f.callSupplier(t, http.MethodGet, "/api/v1/supplier/notifications", token, "")
	var env notificationsEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil || code != http.StatusOK || !env.Success {
		t.Fatalf("GET status %d, body %s, decode error %v", code, body, err)
	}
	if env.UnreadCount != len(want) {
		t.Errorf("unreadCount = %d, want %d", env.UnreadCount, len(want))
	}
	for _, row := range env.Notifications {
		expected, ok := want[row.Title]
		if !ok {
			t.Errorf("unexpected row %q (%s)", row.Title, row.Kind)
			continue
		}
		if row.Kind != expected.kind || row.Body != expected.body || row.Status != "pending" {
			t.Errorf("unexpected kind, body or status for %q: %+v", row.Title, row)
		}
		delete(want, row.Title)
	}
	for title := range want {
		t.Errorf("missing row %q", title)
	}
}

func TestSupplierNotificationsMarkReadDoesNotClearCustomerRows(t *testing.T) {
	for _, mode := range []string{"all", "by_id"} {
		t.Run(mode, func(t *testing.T) {
			f := supplierNotificationsSetup(t)
			customerID := f.seedOutbox(t, f.ownerA, "order_status", "Customer row", "pending")
			supplierID := f.seedOutbox(t, f.ownerA, "proposal_rejected", "Supplier row", "pending")
			token := f.token(t, f.ownerA, "09120000101", "supplier")
			var request string
			if mode == "by_id" {
				request = `{"ids":["` + customerID + `","` + supplierID + `"]}`
			}
			code, body := f.callSupplier(t, http.MethodPost, "/api/v1/supplier/notifications/read", token, request)
			var response struct {
				Success bool `json:"success"`
				Updated int  `json:"updated"`
			}
			if err := json.Unmarshal([]byte(body), &response); err != nil || code != http.StatusOK || !response.Success || response.Updated != 1 {
				t.Errorf("want one updated row, got status %d, body %s, decode error %v", code, body, err)
			}
			for id, want := range map[string]string{customerID: "pending", supplierID: "read"} {
				var status string
				if err := f.db.QueryRow("SELECT status FROM notification_outbox WHERE id = $1", id).Scan(&status); err != nil || status != want {
					t.Errorf("row %s: status = %q, want %q, query error %v", id, status, want, err)
				}
			}
		})
	}
}
