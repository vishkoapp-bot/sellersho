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
CREATE TABLE stores (
	id VARCHAR(50) PRIMARY KEY,
	owner_user_id UUID
,
	-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	name VARCHAR(255) NOT NULL DEFAULT '',
	slug VARCHAR(255) NOT NULL DEFAULT '',
	owner_name VARCHAR(255) NOT NULL DEFAULT '',
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	active_categories text[] NOT NULL DEFAULT '{}'::text[],
	active_plan_id character varying(20) NOT NULL DEFAULT 'trial'::character varying,
	addon_section_title character varying(200),
	contract_signature_code text,
	custom_domain character varying(150),
	custom_store_margin_override numeric(5,2),
	description text,
	is_margin_locked_by_admin boolean NOT NULL DEFAULT false,
	logo text NOT NULL DEFAULT ''::text,
	owner_phone text NOT NULL DEFAULT ''::text,
	owner_type character varying(20) NOT NULL DEFAULT 'dropshipper'::character varying,
	panel_config jsonb NOT NULL DEFAULT '{}'::jsonb,
	plan_expires_at timestamp with time zone,
	profit_margin_percent numeric(5,2) NOT NULL DEFAULT 12.00,
	referral_code character varying(30),
	referrer_seller_id character varying(50),
	rejection_reason text,
	related_section_title character varying(200),
	seller_deals_widget_config jsonb,
	signed_contract_version integer NOT NULL DEFAULT 1,
	status character varying(20) NOT NULL DEFAULT 'active'::character varying,
	tagline text,
	template_config jsonb NOT NULL DEFAULT '{}'::jsonb,
	template_id character varying(50),
	total_sales bigint NOT NULL DEFAULT 0,
	updated_at timestamp with time zone NOT NULL DEFAULT now());
CREATE TABLE platform_settings (
	id INT  DEFAULT 1 PRIMARY KEY,
	supplier_contract_version INT
 DEFAULT 2 ,
	-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	early_withdrawal_fee_pct NUMERIC,
	early_withdrawal_minimum_toman BIGINT NOT NULL DEFAULT 1000000 CONSTRAINT platform_settings_early_withdrawal_minimum_toman_check CHECK (early_withdrawal_minimum_toman BETWEEN 1 AND 9007199254740991),
	platform_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
	settlement_day INTEGER,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	attribution_model character varying(20) NOT NULL DEFAULT 'last_touch'::character varying,
	base_fee_f bigint NOT NULL DEFAULT 100000,
	central_campaign_budget bigint NOT NULL DEFAULT 0,
	central_profit_pct double precision NOT NULL DEFAULT 10,
	central_store_markup_percent numeric(5,2) NOT NULL DEFAULT 10,
	cookie_days integer NOT NULL DEFAULT 30,
	default_item_weight_grams integer NOT NULL DEFAULT 500,
	default_margin numeric(5,2) NOT NULL DEFAULT 20.00,
	default_markup_type character varying(10) NOT NULL DEFAULT 'percent'::character varying,
	default_markup_value numeric(10,2) NOT NULL DEFAULT 30,
	delivery_followup_trigger_days integer NOT NULL DEFAULT 10,
	delivery_hold_period_days integer NOT NULL DEFAULT 20,
	increase_cap_pct double precision NOT NULL DEFAULT 10,
	increase_locked boolean NOT NULL DEFAULT true,
	ippanel_api_key text NOT NULL DEFAULT ''::text,
	ippanel_base_url text NOT NULL DEFAULT 'https://edge.ippanel.com/v1'::text,
	ippanel_enabled boolean NOT NULL DEFAULT false,
	ippanel_pattern_code text NOT NULL DEFAULT ''::text,
	ippanel_sender text NOT NULL DEFAULT '+983000505'::text,
	max_dropshipper_margin_percent numeric(5,2) NOT NULL DEFAULT 100,
	max_profit_margin numeric(5,2) NOT NULL DEFAULT 35.00,
	min_profit_margin numeric(5,2) NOT NULL DEFAULT 5.00,
	packaging_tariffs jsonb,
	product_fee_overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
	referral_commission_percent numeric(5,2) NOT NULL DEFAULT 2.00,
	referral_is_enabled boolean NOT NULL DEFAULT true,
	referral_subject_to_delivery_lock boolean NOT NULL DEFAULT true,
	return_window_days integer NOT NULL DEFAULT 7,
	seller_contract_text text NOT NULL DEFAULT ''::text,
	seller_contract_version integer NOT NULL DEFAULT 2,
	shipping_base_rate bigint NOT NULL DEFAULT 0,
	shipping_per_kg bigint NOT NULL DEFAULT 0,
	storage_fee_enabled boolean NOT NULL DEFAULT false,
	storage_fee_free_days integer NOT NULL DEFAULT 30,
	storage_fee_per_kg_day bigint NOT NULL DEFAULT 2000,
	storage_fee_per_m3_day bigint NOT NULL DEFAULT 8000,
	stuck_order_expiry_enabled boolean NOT NULL DEFAULT false,
	supplier_contract_text text NOT NULL DEFAULT ''::text,
	supplier_fee_overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
	supplier_return_postal_charge bigint,
	tapin_default_box_id text NOT NULL DEFAULT '4'::text,
	tapin_platform_shop_id text,
	tapin_platform_token text,
	tapin_tariff_url text,
	trial_catalog_quota integer NOT NULL DEFAULT 25,
	updated_at timestamp with time zone NOT NULL DEFAULT now(),
	volumetric_divisor integer NOT NULL DEFAULT 5000,
	wallet_min_withdrawal bigint NOT NULL DEFAULT 0,
	wallet_spend_enabled boolean NOT NULL DEFAULT true,
	wallet_withdraw_enabled boolean NOT NULL DEFAULT true,
	warehouse_tariffs jsonb NOT NULL DEFAULT '{}'::jsonb);
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
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	revoked_at timestamp with time zone);
-- TEST-02 round 5: re-seed the migration-canonical suppliers (sup-ali from
-- 000011_direct_dropship; sup-khanoumi / sup-technolife / sup-filimo from
-- 000002_seed_data). This fixture DROP TABLEs suppliers, deleting those rows
-- for every later test in the shared disposable DB; tests that lock those
-- accounts then fail at the row guard ("no rows in result set"). ON
-- CONFLICT DO NOTHING keeps it safe if a row already exists. The fixture's
-- own suppliers use different ids.
INSERT INTO suppliers (id, owner_user_id, name) VALUES ('sup-ali',gen_random_uuid(),''),('sup-filimo',gen_random_uuid(),''),('sup-khanoumi',gen_random_uuid(),''),('sup-technolife',gen_random_uuid(),'') ON CONFLICT (id) DO NOTHING;
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
	if supplier == nil || supplier.ApprovalStatus != "approved" || !user.IsActive {
		t.Fatal("registration must return an approved supplier and active owner")
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
