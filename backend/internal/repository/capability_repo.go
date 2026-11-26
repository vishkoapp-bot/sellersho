package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/drup/platform/internal/accesspolicy"
	"github.com/lib/pq"
)

// CapabilityRepository owns explicit identity capability membership. All
// authorization reads are limited to active memberships.
type CapabilityRepository struct {
	db *sql.DB
}

func NewCapabilityRepository(db *sql.DB) *CapabilityRepository {
	return &CapabilityRepository{db: db}
}

func (r *CapabilityRepository) ListActive(ctx context.Context, userID string) ([]string, error) {
	// TEST-02 round 4: the join compares ids as text. In production
	// both sides are UUID so this is a no-op, but test fixtures
	// rebuild users with VARCHAR/TEXT ids while user_capabilities
	// keeps UUID — a plain `u.id = uc.user_id` then fails with
	// 42883 (operator does not exist: character varying = uuid) and
	// the auth middleware answers 503.
	rows, err := r.db.QueryContext(ctx, `
		SELECT uc.capability, u.mobile
		FROM user_capabilities uc
		JOIN users u ON u.id::text = uc.user_id::text
		WHERE uc.user_id = $1 AND uc.status = 'active'
		ORDER BY uc.capability
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list active capabilities: %w", err)
	}
	defer rows.Close()

	capabilities := make([]string, 0)
	for rows.Next() {
		var capability, mobile string
		if err := rows.Scan(&capability, &mobile); err != nil {
			return nil, err
		}
		if capability == accesspolicy.SuperAdminCapability && !accesspolicy.IsSuperAdminMobile(mobile) {
			continue
		}
		capabilities = append(capabilities, capability)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return capabilities, nil
}

// LoadActiveState loads the whole per-request authorization state for
// one identity in ONE indexed query (PERF-03 G1): whether a users row
// exists, whether it is active, and its active capabilities. It
// replaces the two sequential seq-scanning queries (ListActive +
// IsUserActive) the auth middleware used to run per authenticated
// request. It keeps the live per-request read: no TTL, no cache — a
// block or capability revocation is visible on the very next request,
// exactly as with the two-query path. Results are identical to
// ListActive + IsUserActive combined: an unknown user yields
// found=false with no capabilities (IsUserActive's ErrNoRows -> false
// behaviour), and the super_admin capability is still withheld unless
// the mobile is a super-admin mobile.
// A non-UUID user id, or a database whose users.id is TEXT rather
// than UUID (the TEST-02 fixture shape), cannot run the indexed join —
// those inputs fall back to the legacy two queries, which carry the
// ::text casts that tolerate them, plus an EXISTS check so Found stays
// exact on that path too. The fallback never runs in production,
// where user ids are always UUIDs and users.id is UUID.
func (r *CapabilityRepository) LoadActiveState(ctx context.Context, userID string) (found bool, active bool, capabilities []string, err error) {
	found, active, capabilities, err = r.loadActiveStateIndexed(ctx, userID)
	if err == nil {
		return found, active, capabilities, nil
	}
	if !isAuthTypeMismatch(err) {
		return false, false, nil, err
	}
	var exists bool
	if qerr := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id::text = $1)`, userID).Scan(&exists); qerr != nil {
		return false, false, nil, qerr
	}
	if !exists {
		return false, false, []string{}, nil
	}
	active, aerr := r.IsUserActive(ctx, userID)
	if aerr != nil {
		return false, false, nil, aerr
	}
	caps, cerr := r.ListActive(ctx, userID)
	if cerr != nil {
		return false, false, nil, cerr
	}
	return true, active, caps, nil
}

// loadActiveStateIndexed is the single-query hot path of
// LoadActiveState: one PK lookup on users plus the active-capability
// join, no ::text casts. Unknown user -> zero rows -> found=false.
func (r *CapabilityRepository) loadActiveStateIndexed(ctx context.Context, userID string) (found bool, active bool, capabilities []string, err error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.is_active, uc.capability, u.mobile
		FROM users u
		LEFT JOIN user_capabilities uc ON uc.user_id = u.id AND uc.status = 'active'
		WHERE u.id = $1
		ORDER BY uc.capability
	`, userID)
	if err != nil {
		return false, false, nil, fmt.Errorf("failed to load active state: %w", err)
	}
	defer rows.Close()
	capabilities = []string{}
	for rows.Next() {
		var isActive bool
		var capability sql.NullString
		var mobile string
		if err := rows.Scan(&isActive, &capability, &mobile); err != nil {
			return false, false, nil, err
		}
		found = true
		active = isActive
		if !capability.Valid {
			continue
		}
		if capability.String == accesspolicy.SuperAdminCapability && !accesspolicy.IsSuperAdminMobile(mobile) {
			continue
		}
		capabilities = append(capabilities, capability.String)
	}
	if err := rows.Err(); err != nil {
		return false, false, nil, err
	}
	return found, active, capabilities, nil
}

// isAuthTypeMismatch reports whether err is a PostgreSQL type error
// of the kind the indexed auth join cannot survive (operator does not
// exist, datatype mismatch, invalid UUID text) — the trigger for the
// legacy-query fallback in LoadActiveState. Any other error (connect
// failure, permission, ...) is real and must surface, never fall back.
func isAuthTypeMismatch(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	switch string(pqErr.Code) {
	case "42883", "42804", "22P02":
		return true
	}
	return false
}

func (r *CapabilityRepository) HasActive(ctx context.Context, userID, capability string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM user_capabilities uc
			JOIN users u ON u.id::text = uc.user_id::text
			WHERE uc.user_id = $1 AND uc.capability = $2 AND uc.status = 'active'
			  AND (uc.capability <> $3 OR u.mobile IN ('09933070870', '09127504005'))
		)
	`, userID, capability, accesspolicy.SuperAdminCapability).Scan(&exists)
	return exists, err
}

// IsUserActive reports users.is_active for one identity (D57, live symbol
// CapabilityRepository.IsUserActive). An unknown user id reports
// inactive (false, nil): a token naming no users row must not pass a
// status gate. This reads only the existing is_active flag; no new
// status value is introduced (U53 gap: whether live also consulted
// another signal is unverified).
func (r *CapabilityRepository) IsUserActive(ctx context.Context, userID string) (bool, error) {
	var active bool
	err := r.db.QueryRowContext(ctx, `SELECT is_active FROM users WHERE id::text = $1`, userID).Scan(&active)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check user active status: %w", err)
	}
	return active, nil
}

// Grant activates an existing membership or creates it. The unique key makes
// repeated ownership/backfill events idempotent.
func (r *CapabilityRepository) Grant(ctx context.Context, userID, capability, sourceType, sourceID string, grantedBy *string) error {
	capability = strings.TrimSpace(capability)
	if userID == "" || capability == "" {
		return fmt.Errorf("user ID and capability are required")
	}
	if capability == accesspolicy.SuperAdminCapability {
		return fmt.Errorf("super admin requires OTP verification")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_capabilities
			(user_id, capability, status, source_type, source_id, granted_by, granted_at, revoked_at, updated_at)
		VALUES ($1, $2, 'active', $3, NULLIF($4, ''), $5, NOW(), NULL, NOW())
		ON CONFLICT (user_id, capability) DO UPDATE SET
			status = 'active',
			source_type = EXCLUDED.source_type,
			source_id = EXCLUDED.source_id,
			granted_by = EXCLUDED.granted_by,
			granted_at = COALESCE(user_capabilities.granted_at, EXCLUDED.granted_at),
			revoked_at = NULL,
			updated_at = NOW()
	`, userID, capability, sourceType, sourceID, grantedBy)
	return err
}

// GrantCustomerAfterOTP provisions first-time shopping access while preserving
// any existing membership decision and its provenance.
func (r *CapabilityRepository) GrantCustomerAfterOTP(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_capabilities (user_id, capability, status, source_type)
		VALUES ($1, 'customer', 'active', 'otp_customer_login')
		ON CONFLICT (user_id, capability) DO NOTHING
	`, userID)
	return err
}

// GrantSuperAdminAfterOTP is the only grant path for super_admin. It is
// insert-only so a revoked membership cannot be restored by ordinary login.
func (r *CapabilityRepository) GrantSuperAdminAfterOTP(ctx context.Context, userID, mobile string) error {
	if !accesspolicy.IsSuperAdminMobile(mobile) {
		return fmt.Errorf("mobile is not eligible for super admin")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_capabilities
			(user_id, capability, status, source_type, source_id, granted_at, updated_at)
		VALUES ($1, $2, 'active', 'otp_verified_super_admin', $3, NOW(), NOW())
		ON CONFLICT (user_id, capability) DO NOTHING
	`, userID, accesspolicy.SuperAdminCapability, strings.TrimSpace(mobile))
	return err
}

func (r *CapabilityRepository) Revoke(ctx context.Context, userID, capability string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE user_capabilities
		SET status = 'revoked', revoked_at = NOW(), updated_at = NOW()
		WHERE user_id = $1 AND capability = $2
	`, userID, capability)
	return err
}

// TEST-02 round 4: the id selects cast to uuid. The values are
// UUIDs, so the cast is a no-op in production; it keeps the startup
// bootstrap working when the standing users/stores/suppliers/member
// rebuilds store the id as TEXT/VARCHAR while user_capabilities.
// user_id is UUID (42804 column is of type uuid but expression is
// of type character varying). Rows whose id is not a UUID at all
// (fixture placeholders such as gap13's 'owner-direct') are skipped:
// they have no users row to grant a capability to, and the cast
// would fail the whole bootstrap (22P02) on them.
// BackfillOwnershipCapabilities repairs capability rows for ownership created
// after the migration and is safe to call on startup or after onboarding.
func (r *CapabilityRepository) BackfillOwnershipCapabilities(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
		SELECT id::uuid,
		       CASE WHEN role = 'finance' THEN 'finance_specialist' ELSE 'finance' END,
		       'legacy_role_alias', id::text
		FROM users WHERE role IN ('finance', 'finance_specialist') AND id::text ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
		ON CONFLICT (user_id, capability) DO NOTHING
	`); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
		SELECT owner_user_id::uuid, 'seller', 'store_ownership', id
		FROM stores WHERE owner_user_id IS NOT NULL AND owner_user_id::text ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
		ON CONFLICT (user_id, capability) DO NOTHING
	`)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
		SELECT owner_user_id::uuid, 'supplier', 'supplier_ownership', id
		FROM suppliers WHERE owner_user_id IS NOT NULL AND owner_user_id::text ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
		ON CONFLICT (user_id, capability) DO NOTHING
	`)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
		SELECT sm.user_id::uuid, 'supplier', 'supplier_membership', sm.supplier_id
		FROM supplier_members sm WHERE sm.is_active = TRUE AND sm.user_id::text ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
		ON CONFLICT (user_id, capability) DO NOTHING
	`)
	return err
}
