-- Onboarding persistence: store the niche selections (step 2) and the owner's
-- phone (step 1) collected in the dropshipper onboarding modal so they are
-- no longer silently dropped at registration time.
ALTER TABLE stores ADD COLUMN IF NOT EXISTS active_categories TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE stores ADD COLUMN IF NOT EXISTS owner_phone TEXT NOT NULL DEFAULT '';
