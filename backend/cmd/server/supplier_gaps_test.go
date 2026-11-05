package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

// Gaps 19b + 20 handler coverage, DB-gated like the other suites in
// this package (TEAM04_TEST_DATABASE_URL; skips when unset). The DDL
// is the minimal faithful subset of the real schema the exercised
// handlers and repo functions touch.

const supplierGapsDDL = `
DROP TABLE IF EXISTS warranty_property_requests, warranty_properties, inventory_balances,
	product_variants, product_niches, niches, product_images, products,
	pending_approvals, platform_settings, suppliers, users CASCADE;
CREATE TABLE users (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	mobile VARCHAR(15) UNIQUE NOT NULL,
	full_name VARCHAR(100) NOT NULL DEFAULT '',
	role VARCHAR(20) NOT NULL DEFAULT 'customer',
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	username VARCHAR(50) DEFAULT '',
	password_hash VARCHAR(255) DEFAULT '',
	email VARCHAR(254),
	failed_login_attempts INT NOT NULL DEFAULT 0,
	locked_until TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE suppliers (
	id VARCHAR(50) PRIMARY KEY,
	owner_user_id UUID NOT NULL,
	name VARCHAR(200) NOT NULL,
	logo_url TEXT,
	rating DECIMAL(3,2) DEFAULT 0,
	total_products INT DEFAULT 0,
	delivery_score INT DEFAULT 100,
	warehouse_city VARCHAR(100) DEFAULT '',
	description TEXT,
	badge VARCHAR(50) DEFAULT '',
	supports_direct_shipping BOOLEAN DEFAULT FALSE,
	approval_status VARCHAR(20) DEFAULT 'pending_approval',
	approved_by VARCHAR(50),
	approved_at TIMESTAMPTZ,
	rejection_reason TEXT,
	signed_contract_version INT,
	tapin_shop_id VARCHAR(50),
	warehouse_city_confirmed BOOLEAN DEFAULT FALSE,
	warehouse_city_source VARCHAR(30),
	default_markup_type VARCHAR(20),
	default_markup_value DECIMAL(10,2),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE platform_settings (
	id INT PRIMARY KEY,
	supplier_contract_version INT
);
INSERT INTO platform_settings (id, supplier_contract_version) VALUES (1, 7);
CREATE TABLE pending_approvals (
	id VARCHAR(50) PRIMARY KEY,
	entity_type VARCHAR(30) NOT NULL,
	target_id VARCHAR(50) NOT NULL DEFAULT '',
	title VARCHAR(200) NOT NULL DEFAULT '',
	requested_data JSONB NOT NULL DEFAULT '{}'::jsonb,
	requested_by VARCHAR(100) NOT NULL DEFAULT '',
	status VARCHAR(20) NOT NULL DEFAULT 'pending',
	rejection_reason TEXT,
	reviewed_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE products (
	id VARCHAR(50) PRIMARY KEY,
	title VARCHAR(255) DEFAULT '',
	slug VARCHAR(255) DEFAULT '',
	category_slug VARCHAR(100) DEFAULT '',
	supplier_id VARCHAR(50),
	original_price BIGINT DEFAULT 0,
	supplier_base_cost BIGINT DEFAULT 0,
	platform_margin BIGINT DEFAULT 0,
	is_original BOOLEAN DEFAULT TRUE,
	stock INT DEFAULT 0,
	image_url TEXT DEFAULT '',
	description TEXT DEFAULT '',
	warehouse_name VARCHAR(200) DEFAULT '',
	preparation_days INT DEFAULT 0,
	supplier_warranty VARCHAR(200) DEFAULT '',
	specs JSONB DEFAULT '{}'::jsonb,
	fulfillment_method VARCHAR(30) DEFAULT '',
	barcode VARCHAR(100),
	brand VARCHAR(100) DEFAULT '',
	length_mm INT,
	width_mm INT,
	height_mm INT,
	weight_grams INT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE product_images (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	product_id VARCHAR(50) NOT NULL,
	image_url TEXT NOT NULL,
	sort_order INT DEFAULT 0
);
CREATE TABLE niches (
	id VARCHAR(50) PRIMARY KEY,
	category_slug VARCHAR(100) DEFAULT ''
);
CREATE TABLE product_niches (
	product_id VARCHAR(50) NOT NULL,
	niche_id VARCHAR(50) NOT NULL,
	PRIMARY KEY (product_id, niche_id)
);
CREATE TABLE product_variants (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	product_id VARCHAR(50) NOT NULL,
	spnc VARCHAR(20),
	variant_name VARCHAR(200) DEFAULT '',
	attributes JSONB DEFAULT '{}'::jsonb,
	is_default BOOLEAN DEFAULT FALSE,
	stock INT DEFAULT 0,
	supplier_base_cost BIGINT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE inventory_balances (
	id VARCHAR(80) PRIMARY KEY,
	product_id VARCHAR(50) NOT NULL,
	supplier_id VARCHAR(50),
	owner_type VARCHAR(20) NOT NULL,
	location_type VARCHAR(20) NOT NULL,
	location_key VARCHAR(150) NOT NULL,
	warehouse_name VARCHAR(100),
	status VARCHAR(30) NOT NULL,
	quantity INT NOT NULL DEFAULT 0,
	reconciled BOOLEAN NOT NULL DEFAULT FALSE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (product_id, owner_type, location_type, location_key, status)
);
CREATE TABLE warranty_properties (
	id VARCHAR(60) PRIMARY KEY,
	name VARCHAR(200) NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	status VARCHAR(20) NOT NULL DEFAULT 'active',
	created_by VARCHAR(50),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE warranty_property_requests (
	id VARCHAR(60) PRIMARY KEY,
	supplier_id VARCHAR(50),
	product_id VARCHAR(50),
	proposed_name VARCHAR(200) DEFAULT '',
	description TEXT DEFAULT '',
	image_urls JSONB NOT NULL DEFAULT '[]'::jsonb,
	status VARCHAR(20) NOT NULL DEFAULT 'pending',
	rejection_reason TEXT,
	approved_property_id VARCHAR(60),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	reviewed_at TIMESTAMPTZ
);
`

type supplierGapsFixture struct {
	db           *sql.DB
	ctx          context.Context
	userRepo     *repository.UserRepository
	supplierRepo *repository.SupplierRepository
	adminRepo    *repository.AdminRepository
	jwtService   *auth.JWTService
}

func supplierGapsSetup(t *testing.T) *supplierGapsFixture {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEAM04B_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set TEAM04_TEST_DATABASE_URL to run the supplier-gaps handler tests")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, supplierGapsDDL); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	return &supplierGapsFixture{
		db:           db,
		ctx:          ctx,
		userRepo:     repository.NewUserRepository(db),
		supplierRepo: repository.NewSupplierRepository(db),
		adminRepo:    repository.NewAdminRepository(db),
		jwtService:   auth.NewJWTService("supplier-gaps-test-secret"),
	}
}

func (f *supplierGapsFixture) seedSupplierUser(t *testing.T, idSuffix, username, password string, isActive bool, approvalStatus string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	userID := "22222222-2222-2222-2222-222222" + idSuffix
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO users (id, mobile, full_name, role, is_active, username, password_hash)
		VALUES ($1, $2, $3, 'supplier', $4, $5, $6)`,
		userID, "0913"+idSuffix+"4444", "Owner "+username, isActive, username, string(hash)); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var rejectionReason interface{}
	if approvalStatus == "rejected" {
		rejectionReason = "مدارک ناقص است"
	}
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO suppliers (id, owner_user_id, name, warehouse_city, approval_status, rejection_reason)
		VALUES ($1, $2, $3, 'قم', $4, $5)`,
		"sup-"+idSuffix, userID, "شرکت "+username, approvalStatus, rejectionReason); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
}

func (f *supplierGapsFixture) login(username, password string) (int, string) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlePasswordLogin(rec, req, f.ctx, f.userRepo, f.jwtService, f.supplierRepo)
	return rec.Code, rec.Body.String()
}

// GAP 19b: the password-login state machine. State is revealed ONLY
// after the password verifies; a wrong password always gets the
// generic failure with no state leak.
func TestPasswordLoginSupplierStates(t *testing.T) {
	f := supplierGapsSetup(t)
	f.seedSupplierUser(t, "000001", "pendingsup", "secret123", false, "pending_approval")
	f.seedSupplierUser(t, "000002", "rejectedsup", "secret123", false, "rejected")
	f.seedSupplierUser(t, "000003", "blockedapproved", "secret123", false, "approved")
	f.seedSupplierUser(t, "000004", "activesup", "secret123", true, "approved")

	// Pending + correct password → 403 with the pending code + message.
	code, body := f.login("pendingsup", "secret123")
	if code != http.StatusForbidden {
		t.Fatalf("pending login status = %d, body %s", code, body)
	}
	var pending map[string]interface{}
	if err := json.Unmarshal([]byte(body), &pending); err != nil {
		t.Fatalf("pending body not JSON: %v (%s)", err, body)
	}
	if pending["code"] != "supplier_pending_approval" ||
		pending["error"] != "حساب تأمین‌کننده شما در انتظار تأیید مدیریت است" ||
		pending["supplierName"] != "شرکت pendingsup" ||
		pending["warehouseCity"] != "قم" {
		t.Fatalf("pending body = %v", pending)
	}

	// Pending + WRONG password → generic 401, NO state leak.
	code, body = f.login("pendingsup", "wrong-password")
	if code != http.StatusUnauthorized {
		t.Fatalf("wrong-password status = %d, body %s", code, body)
	}
	if strings.Contains(body, "supplier_pending_approval") || strings.Contains(body, "در انتظار تأیید") {
		t.Fatalf("wrong password leaked account state: %s", body)
	}

	// Rejected + correct password → 403 with code + recorded reason.
	code, body = f.login("rejectedsup", "secret123")
	if code != http.StatusForbidden {
		t.Fatalf("rejected login status = %d, body %s", code, body)
	}
	var rejected map[string]interface{}
	if err := json.Unmarshal([]byte(body), &rejected); err != nil {
		t.Fatalf("rejected body not JSON: %v (%s)", err, body)
	}
	if rejected["code"] != "supplier_rejected" ||
		rejected["error"] != "درخواست تأمین‌کننده شما رد شده است" ||
		rejected["rejectionReason"] != "مدارک ناقص است" {
		t.Fatalf("rejected body = %v", rejected)
	}

	// Approved but blocked → the existing disabled behaviour.
	code, body = f.login("blockedapproved", "secret123")
	if code != http.StatusForbidden || !strings.Contains(body, "Account is disabled") {
		t.Fatalf("blocked-approved = %d %s, want 403 Account is disabled", code, body)
	}

	// Approved + active → success with a token.
	code, body = f.login("activesup", "secret123")
	if code != http.StatusOK || !strings.Contains(body, `"token"`) {
		t.Fatalf("active login = %d %s, want 200 with token", code, body)
	}

	// Unknown user → generic 401.
	code, _ = f.login("nosuchuser", "secret123")
	if code != http.StatusUnauthorized {
		t.Fatalf("unknown user status = %d, want 401", code)
	}
}

func (f *supplierGapsFixture) seedProposal(t *testing.T, id string) {
	t.Helper()
	data := `{"supplier_base_cost":800,"stock":3,"category":"digital","images":[]}`
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO pending_approvals (id, entity_type, target_id, title, requested_data, requested_by, status)
		VALUES ($1, 'proposed_product', $1, 'کالای تستی', $2, 'sup-1', 'pending')`, id, data); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
}

func (f *supplierGapsFixture) productCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM products`).Scan(&n); err != nil {
		t.Fatalf("count products: %v", err)
	}
	return n
}

func (f *supplierGapsFixture) approvalRow(t *testing.T, id string) (string, sql.NullString) {
	t.Helper()
	var status string
	var reason sql.NullString
	if err := f.db.QueryRowContext(f.ctx, `
		SELECT status, rejection_reason FROM pending_approvals WHERE id = $1`, id).Scan(&status, &reason); err != nil {
		t.Fatalf("read approval: %v", err)
	}
	return status, reason
}

// GAP 20a: the dedicated approve route honours {approved:false}, and
// the pre-existing no-field call keeps approving (back-compat).
func TestDedicatedApproveRouteHonoursApprovedFlag(t *testing.T) {
	f := supplierGapsSetup(t)
	handlers := NewSupplierHandlers(f.supplierRepo, f.jwtService, false)
	call := func(id, body string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/approve/"+id, strings.NewReader(body))
		rec := httptest.NewRecorder()
		handlers.HandleApproveProposal(rec, req)
		return rec.Code, rec.Body.String()
	}

	// {approved:false} → rejected with the reason, no product.
	f.seedProposal(t, "pa-ded-1")
	code, body := call("pa-ded-1", `{"approved":false,"reason":"قیمت نامعتبر"}`)
	if code != http.StatusOK || !strings.Contains(body, `"status":"rejected"`) {
		t.Fatalf("dedicated reject = %d %s", code, body)
	}
	if n := f.productCount(t); n != 0 {
		t.Fatalf("products after reject = %d, want 0", n)
	}
	if status, reason := f.approvalRow(t, "pa-ded-1"); status != "rejected" || !reason.Valid || reason.String != "قیمت نامعتبر" {
		t.Fatalf("approval row = (%q, %+v), want rejected + reason", status, reason)
	}

	// No field at all (the current admin UI) → still approves.
	f.seedProposal(t, "pa-ded-2")
	code, body = call("pa-ded-2", `{}`)
	if code != http.StatusOK || !strings.Contains(body, `"status":"approved"`) {
		t.Fatalf("dedicated approve (empty body) = %d %s", code, body)
	}
	if n := f.productCount(t); n != 1 {
		t.Fatalf("products after approve = %d, want 1", n)
	}

	// Explicit {approved:true} → approves.
	f.seedProposal(t, "pa-ded-3")
	code, _ = call("pa-ded-3", `{"approved":true}`)
	if code != http.StatusOK {
		t.Fatalf("dedicated approve (true) = %d", code)
	}
	if n := f.productCount(t); n != 2 {
		t.Fatalf("products after second approve = %d, want 2", n)
	}
}

// GAP 20b: the generic inbox review runs proposals through the SAME
// ApproveProposal/RejectProposal pipeline (product created exactly
// once; a second review is the guard's conflict), while other entity
// types keep their generic behaviour exactly.
func TestGenericInboxReviewRoutesProposalsThroughPipeline(t *testing.T) {
	f := supplierGapsSetup(t)
	review := func(id, body string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/approvals/"+id+"/review", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handleReviewApproval(rec, req, f.ctx, id, f.adminRepo, f.supplierRepo)
		return rec.Code, rec.Body.String()
	}

	// Approve a proposal via the inbox → product created.
	f.seedProposal(t, "pa-gen-1")
	code, body := review("pa-gen-1", `{"status":"approved"}`)
	if code != http.StatusOK {
		t.Fatalf("inbox approve = %d %s", code, body)
	}
	var approved map[string]interface{}
	if err := json.Unmarshal([]byte(body), &approved); err != nil || approved["productId"] == "" || approved["productId"] == nil {
		t.Fatalf("inbox approve body = %s (err %v), want productId", body, err)
	}
	if n := f.productCount(t); n != 1 {
		t.Fatalf("products after inbox approve = %d, want 1", n)
	}
	if status, _ := f.approvalRow(t, "pa-gen-1"); status != "approved" {
		t.Fatalf("approval status = %q, want approved", status)
	}

	// Second review (the other route racing) → conflict, still ONE product.
	code, _ = review("pa-gen-1", `{"status":"approved"}`)
	if code != http.StatusConflict {
		t.Fatalf("second inbox approve = %d, want 409", code)
	}
	if n := f.productCount(t); n != 1 {
		t.Fatalf("products after second approve = %d, want exactly 1", n)
	}

	// Reject a proposal via the inbox → rejected + reason, no product.
	f.seedProposal(t, "pa-gen-2")
	code, body = review("pa-gen-2", `{"status":"rejected","rejectionReason":"توضیح ناقص"}`)
	if code != http.StatusOK || !strings.Contains(body, `"status":"rejected"`) {
		t.Fatalf("inbox reject = %d %s", code, body)
	}
	if status, reason := f.approvalRow(t, "pa-gen-2"); status != "rejected" || !reason.Valid || reason.String != "توضیح ناقص" {
		t.Fatalf("inbox-rejected row = (%q, %+v)", status, reason)
	}
	if n := f.productCount(t); n != 1 {
		t.Fatalf("products after inbox reject = %d, want still 1", n)
	}

	// A warranty_property review keeps the generic behaviour exactly:
	// property row created, approval flipped, NO product involved.
	warrantyData := `{"proposed_name":"ویژگی آزمایشی","description":"توضیح کامل ویژگی آزمایشی","warranty_request_id":"wreq-1","supplier_id":"sup-1"}`
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO warranty_property_requests (id, supplier_id, proposed_name, description, status)
		VALUES ('wreq-1', 'sup-1', 'ویژگی آزمایشی', 'توضیح کامل ویژگی آزمایشی', 'pending')`); err != nil {
		t.Fatalf("seed warranty request: %v", err)
	}
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO pending_approvals (id, entity_type, target_id, title, requested_data, requested_by, status)
		VALUES ('pa-w-1', 'warranty_property', 'pa-w-1', 'ویژگی آزمایشی', $1, 'sup-1', 'pending')`, warrantyData); err != nil {
		t.Fatalf("seed warranty approval: %v", err)
	}
	code, body = review("pa-w-1", `{"status":"approved"}`)
	if code != http.StatusOK {
		t.Fatalf("warranty review = %d %s", code, body)
	}
	var propStatus string
	if err := f.db.QueryRowContext(f.ctx, `
		SELECT status FROM warranty_properties WHERE id = 'WPROP-pa-w-1'`).Scan(&propStatus); err != nil {
		t.Fatalf("warranty property row missing: %v", err)
	}
	if propStatus != "active" {
		t.Fatalf("warranty property status = %q, want active", propStatus)
	}
	var reqStatus, reqProp string
	if err := f.db.QueryRowContext(f.ctx, `
		SELECT status, COALESCE(approved_property_id, '') FROM warranty_property_requests WHERE id = 'wreq-1'`).Scan(&reqStatus, &reqProp); err != nil {
		t.Fatalf("read warranty request: %v", err)
	}
	if reqStatus != "approved" || reqProp != "WPROP-pa-w-1" {
		t.Fatalf("warranty request = (%q, %q), want (approved, WPROP-pa-w-1)", reqStatus, reqProp)
	}
	if status, _ := f.approvalRow(t, "pa-w-1"); status != "approved" {
		t.Fatalf("warranty approval status = %q, want approved", status)
	}
	if n := f.productCount(t); n != 1 {
		t.Fatalf("products after warranty review = %d, want still 1 (no product path)", n)
	}
}
