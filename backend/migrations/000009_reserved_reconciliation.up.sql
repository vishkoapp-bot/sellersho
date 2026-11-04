-- Migration 000009: Reserved slot for ledger reconciliation and double-entry consistency checks
-- This maintains contiguous sequential migration numbering (000001 - 000011)

CREATE TABLE IF NOT EXISTS reconciliation_records (
    id VARCHAR(50) PRIMARY KEY,
    audit_date DATE NOT NULL DEFAULT CURRENT_DATE,
    account_type VARCHAR(50) NOT NULL,
    calculated_balance BIGINT NOT NULL DEFAULT 0,
    actual_balance BIGINT NOT NULL DEFAULT 0,
    discrepancy_amount BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(30) NOT NULL DEFAULT 'verified',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
