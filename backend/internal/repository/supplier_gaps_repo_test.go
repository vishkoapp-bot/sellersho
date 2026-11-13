package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/drup/platform/internal/testdb"

	_ "github.com/lib/pq"
)

// Gaps 16 + 19a repository coverage, in the repo's DB-gated style:
// these tests run only when TEAM04_TEST_DATABASE_URL points at a
// disposable database, and skip otherwise (like the other repo
// suites). The DDL below is the minimal faithful subset of the real
// schema the exercised functions touch.

const supplierGapsRepoDDL = `
DROP TABLE IF EXISTS user_capabilities, supplier_members, stores, suppliers, platform_settings, users CASCADE;
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
CREATE TABLE stores (
	id VARCHAR(50) PRIMARY KEY,
	owner_user_id UUID
);
CREATE TABLE platform_settings (
	id INT PRIMARY KEY,
	supplier_contract_version INT
);
INSERT INTO platform_settings (id, supplier_contract_version) VALUES (1, 7);
CREATE TABLE supplier_members (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	supplier_id VARCHAR(50) NOT NULL,
	user_id UUID NOT NULL,
	role_key VARCHAR(30) NOT NULL,
	permissions JSONB NOT NULL DEFAULT '[]',
	invited_by TEXT,
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE user_capabilities (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	user_id UUID NOT NULL,
	capability VARCHAR(40) NOT NULL,
	status VARCHAR(20) NOT NULL DEFAULT 'active',
	source_type VARCHAR(40) DEFAULT '',
	source_id TEXT DEFAULT '',
	granted_by UUID,
	granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (user_id, capability)
);
`

func supplierGapsRepoSetup(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEAM04B_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set TEAM04_TEST_DATABASE_URL to run the supplier-gaps repository tests")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, supplierGapsRepoDDL); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	return db, ctx
}

// GAP 16: a member created via CreateMember must hold an ACTIVE
// 'supplier' capability row immediately — the same row shape the
// startup backfill writes — with no restart in between.
func TestCreateMemberGrantsSupplierCapabilityImmediately(t *testing.T) {
	db, ctx := supplierGapsRepoSetup(t)
	repo := NewSupplierRepository(db)

	ownerID := "11111111-1111-1111-1111-111111111111"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (id, mobile, full_name, role, is_active)
		VALUES ($1, '09120000001', 'Owner One', 'supplier', TRUE)`, ownerID); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO suppliers (id, owner_user_id, name, approval_status)
		VALUES ('sup-team', $1, 'Team Co', 'approved')`, ownerID); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}

	member, err := repo.CreateMember(ctx, "sup-team", ownerID, "Member One", "memberone", "09120000002", "password123", "orders")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if member.UserID == "" {
		t.Fatalf("CreateMember returned %+v", member)
	}

	// The exact row shape the backfill writes (gap 16 contract).
	var status, sourceType, sourceID string
	err = db.QueryRowContext(ctx, `
		SELECT status, source_type, source_id FROM user_capabilities
		WHERE user_id = $1 AND capability = 'supplier'`, member.UserID).
		Scan(&status, &sourceType, &sourceID)
	if err != nil {
		t.Fatalf("capability row missing right after CreateMember: %v", err)
	}
	if status != "active" || sourceType != "supplier_membership" || sourceID != "sup-team" {
		t.Fatalf("capability row = (%q, %q, %q), want (active, supplier_membership, sup-team)", status, sourceType, sourceID)
	}

	// And the lookup the auth capability refresh uses must return it.
	capRepo := NewCapabilityRepository(db)
	caps, err := capRepo.ListActive(ctx, member.UserID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	found := false
	for _, c := range caps {
		if c == "supplier" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListActive(%s) = %v, want it to contain 'supplier'", member.UserID, caps)
	}

	// The startup backfill stays untouched as the repair path and is
	// still a no-op for this already-granted member.
	if err := capRepo.BackfillOwnershipCapabilities(ctx); err != nil {
		t.Fatalf("BackfillOwnershipCapabilities: %v", err)
	}
	var rows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_capabilities WHERE user_id = $1`, member.UserID).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("capability rows after backfill = %d, want exactly 1", rows)
	}
}

// GAP 19a: the registration email is persisted on the user row and
// round-trips back through the returned user.
func TestRegisterSupplierPersistsEmail(t *testing.T) {
	db, ctx := supplierGapsRepoSetup(t)
	repo := NewSupplierRepository(db)

	user, supplier, err := repo.RegisterSupplier(ctx, "Register Co", "Reg Manager", "regsupplier",
		"password123", "09123334444", "  Hello@Example.com ", "قم")
	if err != nil {
		t.Fatalf("RegisterSupplier: %v", err)
	}
	if supplier == nil || supplier.ApprovalStatus != "pending_approval" {
		t.Fatalf("supplier = %+v, want pending_approval", supplier)
	}
	if user.Email != "Hello@Example.com" {
		t.Fatalf("returned user.Email = %q, want trimmed %q", user.Email, "Hello@Example.com")
	}
	var stored sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = $1`, user.ID).Scan(&stored); err != nil {
		t.Fatalf("read stored email: %v", err)
	}
	if !stored.Valid || stored.String != "Hello@Example.com" {
		t.Fatalf("stored email = %+v, want Hello@Example.com", stored)
	}

	// An empty email stays empty (NULL), never an error.
	user2, _, err := repo.RegisterSupplier(ctx, "NoMail Co", "No Mail", "nomailsupplier",
		"password123", "09125556666", "", "تهران")
	if err != nil {
		t.Fatalf("RegisterSupplier without email: %v", err)
	}
	var stored2 sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = $1`, user2.ID).Scan(&stored2); err != nil {
		t.Fatalf("read stored email 2: %v", err)
	}
	if stored2.Valid {
		t.Fatalf("empty registration email stored as %q, want NULL", stored2.String)
	}
	if user2.Email != "" {
		t.Fatalf("returned user2.Email = %q, want empty", user2.Email)
	}
}
