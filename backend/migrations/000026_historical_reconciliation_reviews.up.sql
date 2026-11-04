CREATE TABLE IF NOT EXISTS historical_reconciliation_reviews (
    id VARCHAR(120) PRIMARY KEY,
    product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    review_status VARCHAR(40) NOT NULL DEFAULT 'needs_review' CHECK (review_status IN (
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
    UNIQUE (product_id)
);

CREATE INDEX IF NOT EXISTS idx_historical_reconciliation_reviews_status
    ON historical_reconciliation_reviews(review_status, updated_at DESC);