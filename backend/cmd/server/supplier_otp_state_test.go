package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
	"github.com/drup/platform/internal/testredis"

	_ "github.com/lib/pq"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/phone"
	"github.com/drup/platform/internal/repository"
)

// GAP 19b, OTP path: the supplier pending/rejected login states must
// surface on /auth/otp/verify. State is revealed ONLY
// after the code verifies; a wrong code gets the ordinary OTP
// failure with no state leak. Codes are seeded directly into the
// disposable Redis -- the same key GenerateAndStoreOTP writes -- so
// the OTP send path is never exercised and no SMS is ever sent.
//
// DB-gated like the other suites in this package
// (TEAM04_TEST_DATABASE_URL); Redis requires its own explicit disposable URL
// and marker. Only absent opt-in skips; unsafe/unavailable explicit targets fail.
// The DDL is the minimal faithful subset of the real
// schema the exercised handler and repo functions touch.

const supplierOTPStateDDL = `
DROP TABLE IF EXISTS user_capabilities, supplier_members, suppliers, users CASCADE;
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
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	warehouse_city character varying(50) NOT NULL DEFAULT 'تهران'::character varying);
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
	signed_contract_version INT DEFAULT 1 ,
	tapin_shop_id VARCHAR(50),
	warehouse_city_confirmed BOOLEAN DEFAULT FALSE,
	warehouse_city_source VARCHAR(30),
	default_markup_type VARCHAR(20),
	default_markup_value DECIMAL(10,2),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	suspended_at timestamp with time zone,
	suspended_by text,
	suspension_reason text,
	tapin_token text);
CREATE TABLE supplier_members (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	supplier_id VARCHAR(50) NOT NULL,
	user_id UUID NOT NULL,
	is_active BOOLEAN NOT NULL DEFAULT TRUE
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	created_at timestamp with time zone NOT NULL DEFAULT now(),
	invited_by uuid,
	permissions jsonb NOT NULL DEFAULT '[]'::jsonb,
	role_key character varying(40),
	updated_at timestamp with time zone NOT NULL DEFAULT now());
CREATE TABLE user_capabilities (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	capability VARCHAR(50) NOT NULL,
	status VARCHAR(20) NOT NULL DEFAULT 'active',
	source_type VARCHAR(40) NOT NULL DEFAULT 'manual',
	source_id VARCHAR(100),
	granted_by UUID REFERENCES users(id) ON DELETE SET NULL,
	granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	revoked_at TIMESTAMPTZ,
	metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (user_id, capability)
);
`

type supplierOTPFixture struct {
	db             *sql.DB
	ctx            context.Context
	userRepo       *repository.UserRepository
	supplierRepo   *repository.SupplierRepository
	capabilityRepo *repository.CapabilityRepository
	jwtService     *auth.JWTService
	otpService     *auth.OTPService
	redisClient    *repository.RedisClient
}

func supplierOTPSetup(t *testing.T) *supplierOTPFixture {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEAM04B_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set TEAM04_TEST_DATABASE_URL to run the supplier OTP-state handler tests")
	}
	redisClient := &repository.RedisClient{Client: testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, supplierOTPStateDDL); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	return &supplierOTPFixture{
		db:             db,
		ctx:            ctx,
		userRepo:       repository.NewUserRepository(db),
		supplierRepo:   repository.NewSupplierRepository(db),
		capabilityRepo: repository.NewCapabilityRepository(db),
		jwtService:     auth.NewJWTService("supplier-otp-state-test-secret"),
		redisClient:    redisClient,
		// env "test": VerifyOTP only consults Redis; nothing here
		// can reach the SMS send path.
		otpService: auth.NewOTPService(redisClient, "test"),
	}
}

func (f *supplierOTPFixture) seedUser(t *testing.T, idSuffix, mobile, role string, isActive bool) string {
	t.Helper()
	userID := "33333333-3333-3333-3333-333333" + idSuffix
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO users (id, mobile, full_name, role, is_active, username, password_hash)
		VALUES ($1, $2, $3, $4, $5, '', '')`,
		userID, mobile, "Owner "+mobile, role, isActive); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return userID
}

func (f *supplierOTPFixture) seedSupplier(t *testing.T, idSuffix, userID, approvalStatus string) {
	t.Helper()
	var rejectionReason interface{}
	if approvalStatus == "rejected" {
		rejectionReason = "مدارک ناقص است"
	}
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO suppliers (id, owner_user_id, name, warehouse_city, approval_status, rejection_reason)
		VALUES ($1, $2, $3, 'قم', $4, $5)`,
		"sup-"+idSuffix, userID, "شرکت تست "+idSuffix, approvalStatus, rejectionReason); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
}

// seedOTPCode writes the code exactly where GenerateAndStoreOTP
// stores it (otp:<normalized mobile>), without the send path.
func (f *supplierOTPFixture) seedOTPCode(t *testing.T, mobile, code string) {
	t.Helper()
	normalized := phone.Normalize(mobile)
	key := "otp:" + normalized
	if err := f.redisClient.Client.Set(f.ctx, key, code, 2*time.Minute).Err(); err != nil {
		t.Fatalf("seed otp code: %v", err)
	}
	t.Cleanup(func() {
		_ = f.redisClient.Client.Del(context.Background(), key, "otp:attempts:"+normalized)
	})
}

func (f *supplierOTPFixture) verifyOTP(mobile, code, role string) (int, string) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/otp/verify",
		strings.NewReader(`{"mobile":"`+mobile+`","code":"`+code+`","role":"`+role+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handleVerifyOTP(rec, req, f.ctx, f.otpService, f.userRepo, f.jwtService, f.supplierRepo, f.capabilityRepo, nil)
	return rec.Code, rec.Body.String()
}

func TestVerifyOTPSupplierStates(t *testing.T) {
	f := supplierOTPSetup(t)

	pendingMobile := "09127000001"
	pendingUID := f.seedUser(t, "000001", pendingMobile, "supplier", false)
	f.seedSupplier(t, "000001", pendingUID, "pending_approval")
	f.seedOTPCode(t, pendingMobile, "1234")

	rejectedMobile := "09127000002"
	rejectedUID := f.seedUser(t, "000002", rejectedMobile, "supplier", false)
	f.seedSupplier(t, "000002", rejectedUID, "rejected")
	f.seedOTPCode(t, rejectedMobile, "1234")

	blockedMobile := "09127000003"
	blockedUID := f.seedUser(t, "000003", blockedMobile, "supplier", false)
	f.seedSupplier(t, "000003", blockedUID, "approved")
	f.seedOTPCode(t, blockedMobile, "1234")

	activeMobile := "09127000004"
	activeUID := f.seedUser(t, "000004", activeMobile, "supplier", true)
	f.seedSupplier(t, "000004", activeUID, "approved")
	if _, err := f.db.ExecContext(f.ctx, `
		INSERT INTO user_capabilities (user_id, capability, status, source_type, source_id)
		VALUES ($1, 'supplier', 'active', 'supplier_ownership', 'sup-000004')`, activeUID); err != nil {
		t.Fatalf("seed capability: %v", err)
	}
	f.seedOTPCode(t, activeMobile, "1234")

	customerMobile := "09127000005"
	f.seedUser(t, "000005", customerMobile, "customer", false)
	f.seedOTPCode(t, customerMobile, "1234")

	// Pending supplier + WRONG code -> the ordinary OTP failure, and
	// nothing about the account state leaks.
	code, body := f.verifyOTP(pendingMobile, "9999", "supplier")
	if code != http.StatusUnauthorized {
		t.Fatalf("wrong-code status = %d, body %s", code, body)
	}
	if !strings.Contains(body, "کد یکبارمصرف نادرست یا منقضی شده است") {
		t.Fatalf("wrong-code body = %s, want the ordinary OTP error", body)
	}
	if strings.Contains(body, "supplier_pending_approval") || strings.Contains(body, "supplierName") ||
		strings.Contains(body, "در انتظار تأیید") {
		t.Fatalf("wrong code leaked account state: %s", body)
	}

	// Pending supplier + correct code -> the password path's exact
	// 403 state body (code + message + name + city).
	code, body = f.verifyOTP(pendingMobile, "1234", "supplier")
	if code != http.StatusForbidden {
		t.Fatalf("pending verify status = %d, body %s", code, body)
	}
	var pending map[string]interface{}
	if err := json.Unmarshal([]byte(body), &pending); err != nil {
		t.Fatalf("pending body not JSON: %v (%s)", err, body)
	}
	if pending["code"] != "supplier_pending_approval" ||
		pending["error"] != "حساب تأمین‌کننده شما در انتظار تأیید مدیریت است" ||
		pending["supplierName"] != "شرکت تست 000001" ||
		pending["warehouseCity"] != "قم" {
		t.Fatalf("pending body = %v", pending)
	}

	// Rejected supplier + correct code -> 403 with code + reason.
	code, body = f.verifyOTP(rejectedMobile, "1234", "supplier")
	if code != http.StatusForbidden {
		t.Fatalf("rejected verify status = %d, body %s", code, body)
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

	// Approved but blocked -> the pre-existing generic disabled
	// behaviour, unchanged, with no state code attached.
	code, body = f.verifyOTP(blockedMobile, "1234", "supplier")
	if code != http.StatusForbidden || !strings.Contains(body, "Account is disabled") {
		t.Fatalf("blocked-approved = %d %s, want 403 Account is disabled", code, body)
	}
	if strings.Contains(body, "supplier_pending_approval") || strings.Contains(body, "supplier_rejected") {
		t.Fatalf("blocked-approved leaked a state code: %s", body)
	}

	// Plain inactive customer (no supplier record) -> generic
	// disabled message, byte-identical to before this change.
	code, body = f.verifyOTP(customerMobile, "1234", "")
	if code != http.StatusForbidden || !strings.Contains(body, "Account is disabled") {
		t.Fatalf("inactive customer = %d %s, want 403 Account is disabled", code, body)
	}

	// Approved + active supplier -> login proceeds exactly as
	// before: 200 with a token.
	code, body = f.verifyOTP(activeMobile, "1234", "supplier")
	if code != http.StatusOK || !strings.Contains(body, `"token"`) {
		t.Fatalf("active supplier verify = %d %s, want 200 with token", code, body)
	}
}

func TestVerifyOTPPreservesCustomerRevocation(t *testing.T) {
	f := supplierOTPSetup(t)
	mobile := "09127000006"
	userID := f.seedUser(t, "000006", mobile, "seller", true)
	if err := f.capabilityRepo.Grant(f.ctx, userID, "customer", "admin_grant", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := f.capabilityRepo.Revoke(f.ctx, userID, "customer"); err != nil {
		t.Fatal(err)
	}
	if err := f.capabilityRepo.Grant(f.ctx, userID, "support_specialist", "admin_grant", "", nil); err != nil {
		t.Fatal(err)
	}
	f.seedOTPCode(t, mobile, "1234")
	status, body := f.verifyOTP(mobile, "1234", "")
	assertFixtureSession(t, f.jwtService, status, body, "support_specialist", []string{"support_specialist"})
	var membershipStatus, source string
	if err := f.db.QueryRow(`SELECT status, source_type FROM user_capabilities WHERE user_id=$1 AND capability='customer'`, userID).Scan(&membershipStatus, &source); err != nil {
		t.Fatal(err)
	}
	if membershipStatus != "revoked" || source != "admin_grant" {
		t.Fatalf("customer membership = %q (%q), want revoked admin_grant", membershipStatus, source)
	}
}

func assertFixtureSession(t *testing.T, jwtService *auth.JWTService, status int, body string, role string, capabilities []string) {
	t.Helper()
	if status != http.StatusOK {
		t.Fatalf("login status = %d, want 200", status)
	}
	var response struct {
		Token string
		User  struct {
			Role         string
			Capabilities []string
		}
	}
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatal("invalid login response")
	}
	if response.User.Role != role || !slices.Equal(response.User.Capabilities, capabilities) {
		t.Fatalf("login role/capabilities = %q/%v, want %q/%v", response.User.Role, response.User.Capabilities, role, capabilities)
	}
	claims, err := jwtService.ValidateToken(response.Token)
	if err != nil {
		t.Fatal("invalid login token")
	}
	if claims.Role != role || !slices.Equal(claims.Capabilities, capabilities) {
		t.Fatalf("token role/capabilities = %q/%v, want %q/%v", claims.Role, claims.Capabilities, role, capabilities)
	}
}
