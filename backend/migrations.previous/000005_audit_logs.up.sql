-- Drup Platform - Audit Logs Migration (000005)

CREATE TABLE IF NOT EXISTS audit_logs (
    id VARCHAR(50) PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    operator VARCHAR(100) NOT NULL,
    action VARCHAR(255) NOT NULL,
    category VARCHAR(50) NOT NULL CHECK (category IN (
        'commission', 'catalog', 'store_status', 'warehouse',
        'finance', 'subscription', 'dispute', 'supplier', 'auth'
    )),
    details TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
