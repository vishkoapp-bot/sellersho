-- Migration 000010: Reserved slot for automated Paya settlements queue metadata
-- This maintains contiguous sequential migration numbering (000001 - 000011)

CREATE TABLE IF NOT EXISTS paya_batch_metadata (
    id VARCHAR(50) PRIMARY KEY,
    batch_reference VARCHAR(100) UNIQUE NOT NULL,
    total_amount BIGINT NOT NULL DEFAULT 0,
    item_count INT NOT NULL DEFAULT 0,
    status VARCHAR(30) NOT NULL DEFAULT 'completed',
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
