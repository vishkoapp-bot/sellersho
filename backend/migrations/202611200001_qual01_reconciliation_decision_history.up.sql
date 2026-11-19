-- Durable per-decision history for historical reconciliation reviews (QUAL-01 G4c).
-- The current review row (historical_reconciliation_reviews, one row per product)
-- keeps the latest decision. This table keeps every distinct decision as its own
-- durable entry, keyed by a digest of status + reason + reviewer + evidence, so a
-- retry of a superseded decision replays that decision's own record instead of
-- silently overwriting the current one. No existing rows are backfilled: decisions
-- recorded before this migration remain durable in audit_logs.
CREATE TABLE IF NOT EXISTS historical_reconciliation_review_decisions (
    id VARCHAR(120) PRIMARY KEY,
    product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    decision_key CHAR(64) NOT NULL,
    review_status VARCHAR(40) NOT NULL CHECK (review_status IN (
        'needs_review', 'in_review', 'awaiting_evidence', 'approved_for_correction',
        'rejected', 'resolved', 'indeterminate'
    )),
    classification_snapshot JSONB NOT NULL DEFAULT '[]'::jsonb,
    report_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    evidence_used JSONB NOT NULL DEFAULT '[]'::jsonb,
    decision_reason TEXT NOT NULL DEFAULT '',
    reviewer_user_id VARCHAR(80),
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (product_id, decision_key)
);

CREATE INDEX IF NOT EXISTS idx_historical_reconciliation_review_decisions_product
    ON historical_reconciliation_review_decisions(product_id, updated_at DESC);
