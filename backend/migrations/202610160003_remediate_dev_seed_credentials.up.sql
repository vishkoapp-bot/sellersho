-- Remediate publicly-committed dev seed credentials and fabricated sales figures.
--
-- Context:
--   * Migrations 000008 and 000011 seeded users with REAL bcrypt hashes of
--     publicly-known test passwords (e.g. 'supplier123'). Any hash committed to
--     this repository is effectively public, so those accounts were compromised
--     by design on every database where the old migrations ran.
--   * Migration 000002 seeded stores with fabricated total_sales figures
--     (184500000 for store-central, etc.), skewing dashboards.
-- Editing the old migration files does NOT re-run them on already-migrated
-- databases, so this forward migration repairs the data in place.
--
-- This migration is surgical and idempotent:
--   * Password reset applies ONLY to rows still holding one of the four known
--     compromised hashes. Accounts whose hash was already rotated (or that were
--     purged) are untouched.
--   * total_sales is zeroed ONLY for the four seeded store ids AND only where
--     the value still exactly matches a known fabricated figure, so genuine
--     accumulated sales are never clobbered.
--   * The replacement hash marker '!UNUSABLE-...' is not a valid bcrypt hash,
--     so password login against it always fails (fail-closed).

-- 1. Neutralize the publicly-known dev seed password hashes.
UPDATE users
SET password_hash = '!UNUSABLE-DEV-SEED-ACCOUNT-DISABLED',
    updated_at = NOW()
WHERE password_hash IN (
    '$2a$10$boGlIBfo9lgFx7cwl0xKs.wnyHJe3St98d2feLRKC1bmYbFrFIfWK',
    '$2a$10$p6zuh4wx1l.S1.z3aScOaexWy1nQaaXIA8vbdH5HUA5Ilp1.Gx/YO',
    '$2a$10$bLIHIq/7H8LQ0qs6csDO9un3Yyw/6eA3CJGO6vQooXXDrRmh0MNZi',
    '$2a$10$5beGcbI84ITaLLG73XRBROptg/4.o7N1S795NU7Ljs/0dT0Gi546C'
);

-- 2. Zero the fabricated total_sales figures seeded by migration 000002.
UPDATE stores
SET total_sales = 0,
    updated_at = NOW()
WHERE id IN ('store-central', 'store-bibi', 'store-sepehr', 'store-parsian')
  AND total_sales IN (184500000, 44100000, 52000000, 38000000);
