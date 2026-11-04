-- Multi-capability identity membership. Legacy users.role remains the
-- compatibility/default capability until protected routes are migrated.

CREATE TABLE IF NOT EXISTS user_capabilities (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    capability VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'pending', 'revoked')),
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

CREATE INDEX IF NOT EXISTS user_capabilities_active_idx
    ON user_capabilities (user_id, capability)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS user_capabilities_source_idx
    ON user_capabilities (source_type, source_id);

-- Preserve existing authorization while making ownership-derived access
-- explicit. Ownership never grants administrative or staff capabilities.
INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
SELECT id, role, 'legacy_role', id::text
FROM users
WHERE role IN (
    'customer', 'seller', 'supplier', 'warehouse_staff', 'admin',
    'finance_specialist', 'finance', 'support_specialist', 'logistics_specialist'
)
ON CONFLICT (user_id, capability) DO NOTHING;

-- Finance was historically stored under both names; retain the existing
-- route equivalence as explicit capabilities during migration.
INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
SELECT id,
       CASE WHEN role = 'finance' THEN 'finance_specialist' ELSE 'finance' END,
       'legacy_role_alias',
       id::text
FROM users
WHERE role IN ('finance', 'finance_specialist')
ON CONFLICT (user_id, capability) DO NOTHING;

INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
SELECT owner_user_id, 'seller', 'store_ownership', id
FROM stores
WHERE owner_user_id IS NOT NULL
ON CONFLICT (user_id, capability) DO NOTHING;

INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
SELECT owner_user_id, 'supplier', 'supplier_ownership', id
FROM suppliers
WHERE owner_user_id IS NOT NULL
ON CONFLICT (user_id, capability) DO NOTHING;

-- Product rule: only legacy customer accounts receive the customer baseline
-- during migration. Other account types acquire customer access through an
-- ordinary OTP login, never by requesting a role.
INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
SELECT id, 'customer', 'legacy_customer_role', id::text
FROM users
WHERE is_active = TRUE AND role = 'customer'
ON CONFLICT (user_id, capability) DO NOTHING;

INSERT INTO user_capabilities (user_id, capability, source_type, source_id)
SELECT sm.user_id, 'supplier', 'supplier_membership', sm.supplier_id
FROM supplier_members sm
WHERE sm.is_active = TRUE
ON CONFLICT (user_id, capability) DO NOTHING;