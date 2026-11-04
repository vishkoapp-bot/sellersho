package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/drup/platform/internal/accesspolicy"
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
	rows, err := r.db.QueryContext(ctx, `
		SELECT uc.capability, u.mobile
		FROM user_capabilities uc
		JOIN users u ON u.id = uc.user_id
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

func (r *CapabilityRepository) HasActive(ctx context.Context, userID, capability string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM user_capabilities uc
			JOIN users u ON u.id = uc.user_id
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

// BackfillOwnershipCapabilities repairs capability rows for ownership created
// after the migration and is safe to call on startup or after onboarding.
func (r *CapabilityRepository) BackfillOwnershipCapabilities(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
		SELECT id,
		       CASE WHEN role = 'finance' THEN 'finance_specialist' ELSE 'finance' END,
		       'legacy_role_alias', id::text
		FROM users WHERE role IN ('finance', 'finance_specialist')
		ON CONFLICT (user_id, capability) DO NOTHING
	`); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
		SELECT owner_user_id, 'seller', 'store_ownership', id
		FROM stores WHERE owner_user_id IS NOT NULL
		ON CONFLICT (user_id, capability) DO NOTHING
	`)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
		SELECT owner_user_id, 'supplier', 'supplier_ownership', id
		FROM suppliers WHERE owner_user_id IS NOT NULL
		ON CONFLICT (user_id, capability) DO NOTHING
	`)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
		SELECT sm.user_id, 'supplier', 'supplier_membership', sm.supplier_id
		FROM supplier_members sm WHERE sm.is_active = TRUE
		ON CONFLICT (user_id, capability) DO NOTHING
	`)
	return err
}
