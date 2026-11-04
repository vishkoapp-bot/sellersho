-- 202610280006: numeric public identifiers for the new product URL scheme
-- (Oct 28, 2026 — product-URL restructure, Option A).
--
--   products.spn_number          BIGINT UNIQUE NOT NULL — the <number> in
--                                /product/spn-<number>/<persian-title-slug>/
--   product_variants.spnc_number INT UNIQUE NULL        — the <number> in
--                                ?spnc=<number>
--
-- The INTERNAL codes are untouched and keep their existing meaning:
--   products.spn ("PC-" + 7 random digits, 202610160006) and
--   product_variants.spnc (8-char uppercase hex) stay exactly as they are —
--   GenerateSPN/GenerateSPNC in cmd/server/sku.go are not touched, and
--   neither code is ever exposed on a public payload.
--
-- NUMBERING (deterministic, so the deploy-time result is reproducible):
--   spn_number  from 10000001, products ORDER BY created_at ASC, id ASC
--   spnc_number from 20000001, variants ORDER BY product created_at ASC,
--               variant created_at ASC, variant id ASC
-- The full prod mapping produced by this exact ordering is recorded in
-- ~/workspace/product-urls-20261028/build/backend/backfill-mapping.json
-- (118 products, 10 variants — captured read-only from prod before deploy).
-- Regenerate it from prod right after deploy; anything created on prod
-- after that capture sorts into the same order and shifts later numbers.
--
-- AUTO-ASSIGNMENT: one sequence per column + a nextval() DEFAULT, so rows
-- inserted after this migration (proposals, admin creates, supplier
-- variants) get the next number automatically with no code change. The
-- sequences are advanced past the backfilled maxima below.
--
-- IDEMPOTENCY: re-running UP on an already-migrated DB is a no-op for the
-- DDL (IF NOT EXISTS) and re-derives the same numbers (the backfill is a
-- pure function of created_at/id, and the UNIQUE constraints reject any
-- drift). UP is applied once by runAutoMigrations; DOWN drops the columns
-- and sequences (the mapping JSON is the only record of the numbers).

CREATE SEQUENCE IF NOT EXISTS products_spn_number_seq START WITH 10000001;
CREATE SEQUENCE IF NOT EXISTS product_variants_spnc_number_seq START WITH 20000001;

ALTER TABLE products ADD COLUMN IF NOT EXISTS spn_number BIGINT;
ALTER TABLE product_variants ADD COLUMN IF NOT EXISTS spnc_number INT;

-- Backfill products: every row, including inactive ones.
WITH numbered AS (
    SELECT id, 10000000 + ROW_NUMBER() OVER (ORDER BY created_at ASC, id ASC) AS n
    FROM products
)
UPDATE products p SET spn_number = numbered.n
FROM numbered WHERE p.id = numbered.id;

-- Backfill variants: every row (default rows included — they are the
-- inventory anchor and may gain a public number even though the PDP only
-- lists non-default variants).
WITH numbered AS (
    SELECT v.id, 20000000 + ROW_NUMBER() OVER (
        ORDER BY p.created_at ASC, v.created_at ASC, v.id ASC) AS n
    FROM product_variants v
    JOIN products p ON p.id = v.product_id
)
UPDATE product_variants v SET spnc_number = numbered.n
FROM numbered WHERE v.id = numbered.id;

-- Advance the sequences past the backfilled maxima (empty table = start
-- value, so the first auto-assigned number is 10000001 / 20000001).
SELECT setval('products_spn_number_seq',
    COALESCE((SELECT MAX(spn_number) FROM products), 10000000), true);
SELECT setval('product_variants_spnc_number_seq',
    COALESCE((SELECT MAX(spnc_number) FROM product_variants), 20000000), true);

-- Defaults + uniqueness. spn_number is NOT NULL: every product has a
-- public number after the backfill. spnc_number stays NULL-able by design
-- (a variant row inserted with an explicit NULL keeps NULL); the DEFAULT
-- still auto-assigns for ordinary inserts that omit the column.
ALTER TABLE products ALTER COLUMN spn_number SET DEFAULT nextval('products_spn_number_seq');
ALTER TABLE products ALTER COLUMN spn_number SET NOT NULL;
ALTER TABLE products ADD CONSTRAINT products_spn_number_unique UNIQUE (spn_number);
ALTER TABLE product_variants ALTER COLUMN spnc_number SET DEFAULT nextval('product_variants_spnc_number_seq');
ALTER TABLE product_variants ADD CONSTRAINT product_variants_spnc_number_unique UNIQUE (spnc_number);

COMMENT ON COLUMN products.spn_number IS
    'Public numeric product number for /product/spn-<n>/... (migration 202610280006). Distinct from the internal warehouse code products.spn ("PC-...").';
COMMENT ON COLUMN product_variants.spnc_number IS
    'Public numeric variant number for ?spnc=<n> (migration 202610280006). Distinct from the internal warehouse code product_variants.spnc (8-char hex).';
