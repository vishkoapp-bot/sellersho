package repository

// RejectProposal notification coverage (proposal_rejected outbox row).
// Runs ONLY when REJECT_NOTIFY_TEST_DATABASE_URL points at a DISPOSABLE
// local Postgres (never the live database); otherwise skips, like the
// other DB-gated suites in this package. The DDL is the minimal
// faithful subset of the real schema the reject + notify path touches:
// users (outbox FK target), suppliers (the columns GetSupplierByID
// selects), pending_approvals, notification_outbox.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"


	_ "github.com/lib/pq"
)

const rejectNotifyDDL = `
DROP TABLE IF EXISTS notification_outbox, pending_approvals, suppliers, users CASCADE;
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
	logo_url TEXT,
	rating DECIMAL(3,2) DEFAULT 0,
	total_products INT DEFAULT 0,
	delivery_score INT DEFAULT 100,
	warehouse_city VARCHAR(100) DEFAULT '',
	description TEXT,
	badge VARCHAR(50) DEFAULT '',
	supports_direct_shipping BOOLEAN DEFAULT FALSE,
	default_markup_type VARCHAR(20),
	default_markup_value DECIMAL(10,2),
	signed_contract_version INT DEFAULT 1,
	approval_status VARCHAR(20) DEFAULT 'approved',
	approved_by VARCHAR(50),
	approved_at TIMESTAMPTZ,
	rejection_reason TEXT,
	tapin_shop_id VARCHAR(50),
	tapin_token TEXT,
	warehouse_city_confirmed BOOLEAN DEFAULT FALSE,
	warehouse_city_source VARCHAR(30),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	suspended_at timestamp with time zone,
	suspended_by text,
	suspension_reason text);
CREATE TABLE pending_approvals (
	id VARCHAR(50) PRIMARY KEY,
	entity_type VARCHAR(50) NOT NULL,
	target_id VARCHAR(50) NOT NULL DEFAULT '',
	title VARCHAR(200) NOT NULL DEFAULT '',
	requested_data JSONB NOT NULL DEFAULT '{}'::jsonb,
	requested_by VARCHAR(100) NOT NULL DEFAULT '',
	status VARCHAR(50) NOT NULL DEFAULT 'pending',
	rejection_reason TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	reviewed_at TIMESTAMPTZ
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
-- TEST-02 round 5: re-seed the migration-canonical suppliers (sup-ali from
-- 000011_direct_dropship; sup-khanoumi / sup-technolife / sup-filimo from
-- 000002_seed_data). This fixture DROP TABLEs suppliers, deleting those rows
-- for every later test in the shared disposable DB; tests that lock those
-- accounts then fail at the row guard ("no rows in result set"). ON
-- CONFLICT DO NOTHING keeps it safe if a row already exists. The fixture's
-- own suppliers use different ids.
INSERT INTO suppliers (id, name) VALUES ('sup-ali',''),('sup-filimo',''),('sup-khanoumi',''),('sup-technolife','') ON CONFLICT (id) DO NOTHING;
`

func rejectNotifyTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("REJECT_NOTIFY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("REJECT_NOTIFY_TEST_DATABASE_URL not set; skipping disposable-DB integration test")
	}
	// The reduced fixture drops and recreates canonical table names.
	db := openPrivateFinanceFixture(t, dsn)
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	if _, err := db.Exec(rejectNotifyDDL); err != nil {
		t.Fatalf("seed DDL: %v", err)
	}
	return db
}

// seedRejectNotifySupplier creates one owner user + one supplier owned
// by them and returns (ownerUserID, supplierID). ownerID may be "" to
// seed a supplier with a NULL owner_user_id.
func seedRejectNotifySupplier(t *testing.T, db *sql.DB, ownerMobile, supplierID string, withOwner bool) string {
	t.Helper()
	var ownerID string
	if withOwner {
		err := db.QueryRow(`INSERT INTO users (mobile, full_name, role) VALUES ($1, 'Test Owner', 'supplier') RETURNING id`, ownerMobile).Scan(&ownerID)
		if err != nil {
			t.Fatalf("seed owner user: %v", err)
		}
		_, err = db.Exec(`INSERT INTO suppliers (id, owner_user_id, name) VALUES ($1, $2, 'Test Supplier')`, supplierID, ownerID)
		if err != nil {
			t.Fatalf("seed supplier: %v", err)
		}
		return ownerID
	}
	if _, err := db.Exec(`INSERT INTO suppliers (id, owner_user_id, name) VALUES ($1, NULL, 'Ownerless Supplier')`, supplierID); err != nil {
		t.Fatalf("seed ownerless supplier: %v", err)
	}
	return ""
}

func seedRejectNotifyProposal(t *testing.T, db *sql.DB, proposalID, supplierID, title string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO pending_approvals (id, entity_type, target_id, title, requested_data, requested_by, status)
		VALUES ($1, 'proposed_product', $1, $2, '{}'::jsonb, $3, 'pending')`, proposalID, title, supplierID); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
}

func countOutbox(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&n); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return n
}

func TestRejectProposalNotifiesSupplierOwner(t *testing.T) {
	db := rejectNotifyTestDB(t)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	ownerID := seedRejectNotifySupplier(t, db, "09120000001", "sup-1", true)
	seedRejectNotifyProposal(t, db, "prop-1", "sup-1", "تیشرت زنانه نخی")

	if err := repo.RejectProposal(ctx, "prop-1", "کیفیت عکس‌ها کافی نیست"); err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}

	var status string
	var reason sql.NullString
	if err := db.QueryRow(`SELECT status, rejection_reason FROM pending_approvals WHERE id = 'prop-1'`).Scan(&status, &reason); err != nil {
		t.Fatalf("read proposal: %v", err)
	}
	if status != "rejected" || !reason.Valid || reason.String != "کیفیت عکس‌ها کافی نیست" {
		t.Fatalf("proposal row = status %q reason %v, want rejected + reason stored", status, reason)
	}

	if n := countOutbox(t, db); n != 1 {
		t.Fatalf("outbox rows = %d, want exactly 1", n)
	}
	var recipient, kind, title, body, payloadRaw, rowStatus string
	if err := db.QueryRow(`SELECT recipient_user_id, kind, title, body, payload, status FROM notification_outbox`).Scan(&recipient, &kind, &title, &body, &payloadRaw, &rowStatus); err != nil {
		t.Fatalf("read outbox row: %v", err)
	}
	if recipient != ownerID {
		t.Fatalf("recipient = %s, want owner %s", recipient, ownerID)
	}
	if kind != "proposal_rejected" {
		t.Fatalf("kind = %q, want proposal_rejected", kind)
	}
	if rowStatus != "pending" {
		t.Fatalf("status = %q, want pending", rowStatus)
	}
	if !strings.Contains(title, "تیشرت زنانه نخی") || !strings.Contains(title, "رد شد") {
		t.Fatalf("title = %q, want product title + رد شد", title)
	}
	if !strings.Contains(body, "تیشرت زنانه نخی") || !strings.Contains(body, "کیفیت عکس‌ها کافی نیست") {
		t.Fatalf("body = %q, want title + reason", body)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	if payload["proposal_id"] != "prop-1" || payload["event"] != "proposal_rejected" ||
		payload["entity_type"] != "proposed_product" || payload["product_title"] != "تیشرت زنانه نخی" {
		t.Fatalf("payload = %v, want proposal_id/event/entity_type/product_title", payload)
	}
}

func TestRejectProposalSecondRejectIs409AndNoDuplicateRow(t *testing.T) {
	db := rejectNotifyTestDB(t)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	seedRejectNotifySupplier(t, db, "09120000002", "sup-2", true)
	seedRejectNotifyProposal(t, db, "prop-2", "sup-2", "کفش چرم")

	if err := repo.RejectProposal(ctx, "prop-2", "دلیل اول"); err != nil {
		t.Fatalf("first RejectProposal: %v", err)
	}
	if err := repo.RejectProposal(ctx, "prop-2", "دلیل دوم"); !errors.Is(err, ErrProposalNotPending) {
		t.Fatalf("second RejectProposal err = %v, want ErrProposalNotPending", err)
	}
	if n := countOutbox(t, db); n != 1 {
		t.Fatalf("outbox rows = %d after double reject, want exactly 1", n)
	}
}

func TestRejectProposalEmptyReasonUsesFallbackBody(t *testing.T) {
	db := rejectNotifyTestDB(t)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	seedRejectNotifySupplier(t, db, "09120000003", "sup-3", true)
	seedRejectNotifyProposal(t, db, "prop-3", "sup-3", "کیف پول")

	if err := repo.RejectProposal(ctx, "prop-3", ""); err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}
	var reason sql.NullString
	if err := db.QueryRow(`SELECT rejection_reason FROM pending_approvals WHERE id = 'prop-3'`).Scan(&reason); err != nil {
		t.Fatalf("read proposal: %v", err)
	}
	if reason.Valid {
		t.Fatalf("rejection_reason = %q, want NULL for empty reason", reason.String)
	}
	var body string
	if err := db.QueryRow(`SELECT body FROM notification_outbox`).Scan(&body); err != nil {
		t.Fatalf("read outbox row: %v", err)
	}
	if !strings.Contains(body, "دلیلی ثبت نشده است") || !strings.Contains(body, "کیف پول") {
		t.Fatalf("body = %q, want fallback body with product title", body)
	}
	if strings.Contains(body, "علت عدم تأیید:") {
		t.Fatalf("body = %q, fallback must not render a fabricated reason", body)
	}
}

func TestRejectProposalMissingSupplierStillRejects(t *testing.T) {
	db := rejectNotifyTestDB(t)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	// Proposal whose requested_by matches no supplier row at all.
	seedRejectNotifyProposal(t, db, "prop-4", "sup-gone", "ساعت مچی")

	if err := repo.RejectProposal(ctx, "prop-4", "دلیل"); err != nil {
		t.Fatalf("RejectProposal with missing supplier: %v (reject must succeed)", err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM pending_approvals WHERE id = 'prop-4'`).Scan(&status); err != nil {
		t.Fatalf("read proposal: %v", err)
	}
	if status != "rejected" {
		t.Fatalf("status = %q, want rejected", status)
	}
	if n := countOutbox(t, db); n != 0 {
		t.Fatalf("outbox rows = %d, want 0 (no recipient to notify)", n)
	}
}

func TestRejectProposalNullOwnerStillRejects(t *testing.T) {
	db := rejectNotifyTestDB(t)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	seedRejectNotifySupplier(t, db, "", "sup-5", false)
	seedRejectNotifyProposal(t, db, "prop-5", "sup-5", "عینک آفتابی")

	if err := repo.RejectProposal(ctx, "prop-5", "دلیل"); err != nil {
		t.Fatalf("RejectProposal with NULL owner_user_id: %v (reject must succeed)", err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM pending_approvals WHERE id = 'prop-5'`).Scan(&status); err != nil {
		t.Fatalf("read proposal: %v", err)
	}
	if status != "rejected" {
		t.Fatalf("status = %q, want rejected", status)
	}
	if n := countOutbox(t, db); n != 0 {
		t.Fatalf("outbox rows = %d, want 0 (NULL owner has no recipient)", n)
	}
}
