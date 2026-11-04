-- Per-field catalog review: reviewer approves/rejects each field individually.
-- Backward compatible: existing pending_approvals flow untouched.

CREATE TABLE IF NOT EXISTS proposal_field_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    proposal_id TEXT NOT NULL,
    field_key TEXT NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected', 'pending')),
    reason TEXT,
    reviewer_id TEXT,
    reviewed_at TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(proposal_id, field_key)
);
CREATE INDEX IF NOT EXISTS idx_pfr_proposal ON proposal_field_reviews(proposal_id);

-- Audit trail for the whole review lifecycle
CREATE TABLE IF NOT EXISTS proposal_review_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    proposal_id TEXT NOT NULL,
    action TEXT NOT NULL,
    field_key TEXT,
    reason TEXT,
    actor_id TEXT,
    actor_role TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_prh_proposal ON proposal_review_history(proposal_id);
