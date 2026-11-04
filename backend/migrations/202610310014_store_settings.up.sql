-- 202610310014 (Cluster D, fieldsweep follow-up, 2026-11-01):
-- F-09: the dist's saveStoreSettings sends {tagline, logo} and
-- {tagline, shortDescription, addonSectionTitle}. addon_section_title
-- already exists (202610250403); NO stores logo column existed, so
-- this adds stores.logo (the dist reads/writes the store's `logo`
-- field as a plain string, e.g. an image URL). The handler also
-- keeps logo/addonSectionTitle inside panel_config JSONB, so a
-- pre-0014 database still round-trips through panel_config; this
-- column is the dedicated, queryable home for logo.
-- F-16: disputes had no financial_decision column; the dist's
-- resolveDispute sends financial_decision alongside the core fields.
ALTER TABLE stores ADD COLUMN IF NOT EXISTS logo TEXT NOT NULL DEFAULT '';
ALTER TABLE disputes ADD COLUMN IF NOT EXISTS financial_decision TEXT;
