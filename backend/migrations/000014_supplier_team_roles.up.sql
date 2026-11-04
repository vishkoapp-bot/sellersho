-- Supplier team members and scoped permissions.
CREATE TABLE IF NOT EXISTS supplier_members (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    supplier_id VARCHAR(50) NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    role_key VARCHAR(40) NOT NULL,
    permissions JSONB NOT NULL DEFAULT '[]'::jsonb,
    invited_by UUID REFERENCES users(id) ON DELETE SET NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (supplier_id, user_id)
);

CREATE INDEX IF NOT EXISTS supplier_members_supplier_idx ON supplier_members (supplier_id, is_active);