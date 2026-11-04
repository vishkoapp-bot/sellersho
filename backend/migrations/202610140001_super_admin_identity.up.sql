-- Provision the single allowlisted super-admin identity.
--
-- Why this exists:
--   * The transactional purge (cmd/seed -action purge) removes the users table
--     content, which would otherwise leave nobody able to administer the
--     platform (the OTP handler refuses a requested role for a non-existent
--     user, so the account must exist before first login).
--   * Migration 202610130002 only ran an UPDATE, so it silently did nothing on
--     a fresh database. This migration is INSERT-driven and idempotent.
--
-- Security posture:
--   * Authority is granted exclusively through the backend allowlist
--     (internal/accesspolicy). This migration provisions the identity only; it
--     deliberately does NOT set a password hash, because any hash committed to
--     the repository is effectively public. The operator authenticates this
--     account with phone OTP, and may set a password hash out-of-band if the
--     password login path is required.
--
-- The id is fixed so repeated runs and environment rebuilds stay deterministic.

INSERT INTO users (id, mobile, full_name, role, username, password_hash, is_active, created_at, updated_at)
VALUES (
    'f21bdcd1-3e7c-4ff2-a247-1516f04d407b',
    '09933070870',
    'مدیر ارشد پلتفرم',
    'admin',
    '09933070870',
    '',
    TRUE,
    NOW(),
    NOW()
)
ON CONFLICT (mobile) DO UPDATE SET
    role = 'admin',
    username = '09933070870',
    full_name = COALESCE(NULLIF(users.full_name, ''), EXCLUDED.full_name),
    is_active = TRUE,
    updated_at = NOW();

-- Purge any repository-visible hash that earlier migrations may have applied.
UPDATE users
SET password_hash = '', updated_at = NOW()
WHERE mobile = '09933070870' AND COALESCE(password_hash, '') <> '';

CREATE UNIQUE INDEX IF NOT EXISTS users_super_admin_username_unique
ON users (LOWER(username))
WHERE username = '09933070870';

-- Grant the super-admin capability to the allowlisted identity only.
INSERT INTO user_capabilities (user_id, capability, status, source_type, source_id, granted_at, updated_at)
SELECT id, 'super_admin', 'active', 'static_allowlist', mobile, NOW(), NOW()
FROM users
WHERE mobile = '09933070870'
ON CONFLICT (user_id, capability) DO UPDATE SET
    status = 'active',
    revoked_at = NULL,
    updated_at = NOW();
