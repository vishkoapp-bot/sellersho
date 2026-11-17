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
);
CREATE TABLE suppliers (
	id VARCHAR(50) PRIMARY KEY,
	owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
	name VARCHAR(200) NOT NULL,
	approval_status VARCHAR(20) DEFAULT 'approved',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE supplier_members (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	supplier_id VARCHAR(50) NOT NULL,
	user_id UUID NOT NULL,
	role_key VARCHAR(50) NOT NULL DEFAULT 'manager',
	permissions JSONB NOT NULL DEFAULT '[]'::jsonb,
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
