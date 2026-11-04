-- Down for 202610200007 (homepage identity wave-B): restore the seed
-- composition from migration 202610160002 — trust_features back to the
-- last slot (sort_order 220) and suppliers_showcase active again.
-- Note: live prod had trust_features at sort_order 140 before the up
-- migration (admin reordering since the seed); this down restores the
-- original seed values, not the exact pre-up live values.
UPDATE homepage_sections
SET sort_order = 220,
    updated_at = NOW()
WHERE section_type = 'trust_features';

UPDATE homepage_sections
SET is_active = TRUE,
    updated_at = NOW()
WHERE section_type = 'suppliers_showcase';
