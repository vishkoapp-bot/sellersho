-- Supplier approval workflow: new registrations start as pending_approval.
-- Admin must approve before the supplier account becomes active.

ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS approval_status VARCHAR(20) NOT NULL DEFAULT 'approved';
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS approved_by VARCHAR(50);
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ;
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS rejection_reason TEXT;

-- Existing suppliers are grandfathered as approved.
UPDATE suppliers SET approval_status = 'approved' WHERE approval_status IS NULL OR approval_status = '';

-- Index for the pending-approvals admin query.
CREATE INDEX IF NOT EXISTS idx_suppliers_approval_status ON suppliers(approval_status) WHERE approval_status = 'pending_approval';
