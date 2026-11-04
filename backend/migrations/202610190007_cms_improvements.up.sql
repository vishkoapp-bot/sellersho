-- CMS improvements: scheduling + history for homepage sections.
-- 1. Scheduling columns: sections can appear/disappear at set times (campaigns, promos).
ALTER TABLE homepage_sections
    ADD COLUMN IF NOT EXISTS starts_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS ends_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS notes TEXT;

-- 2. History table: snapshot of every change (who, when, what). Enables rollback.
CREATE TABLE IF NOT EXISTS homepage_section_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    section_id UUID NOT NULL,
    action VARCHAR(20) NOT NULL CHECK (action IN ('created','updated','deleted','restored')),
    section_type VARCHAR(50),
    title VARCHAR(200),
    config JSONB,
    sort_order INT,
    is_active BOOLEAN,
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    changed_by VARCHAR(200),
    changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_section_history_section
    ON homepage_section_history(section_id, changed_at DESC);
