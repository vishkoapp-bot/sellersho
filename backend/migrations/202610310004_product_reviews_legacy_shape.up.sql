-- 202610310004: migrate a legacy (order-linked) product_reviews table to
-- the shape the new review code expects, WITHOUT losing any review data.
--
-- Background: migration 202610200003 created product_reviews in the old
-- shape (order_id, order_item_id, customer_id UUID, customer_display_name
-- VARCHAR(100) NOT NULL with no DEFAULT, title, body, status CHECK
-- published/hidden/deleted, moderation_*). Migration 202610310002
-- then used CREATE TABLE IF NOT EXISTS for the new shape (user_id TEXT,
-- comment TEXT, UNIQUE(product_id, user_id)), which silently no-opped on
-- databases where the old table already existed — so the new code's
-- INSERT/SELECT (user_id, comment, ON CONFLICT (product_id, user_id))
-- fails on those databases. This migration closes that gap.
--
-- Behaviour by database state (all branches are no-ops when they do not
-- apply, so the migration is safe to run on fresh databases too):
--   * Table already in the NEW shape (has user_id, no customer_id):
--     nothing to do; the whole body is skipped.
--   * Table in the OLD shape (has customer_id): migrated in place below.
--   * Table absent: skipped (0002 creates the new shape before this runs).
--
-- What the old-shape migration does, in order:
--   1. Adds user_id / comment columns (IF NOT EXISTS).
--   2. Backfills user_id = customer_id::text (UUID -> TEXT is lossless)
--      and comment = body (COALESCE to ''), so no old review text is lost.
--   3. Relaxes NOT NULL constraints the new code never sets and adds
--      the missing DEFAULTs. New-code inserts provide only
--      (id, product_id, user_id, rating, comment); on the old table,
--      customer_id / order_id / order_item_id are all NOT NULL with no
--      default, so without this step every new review insert would
--      fail. Relaxing (not dropping) those columns keeps every old row
--      intact. body keeps its NOT NULL but gains DEFAULT '' so omitted
--      bodies insert cleanly. customer_display_name keeps its NOT NULL
--      but gains DEFAULT '' for the same reason: it is VARCHAR(100)
--      NOT NULL with no DEFAULT in the old shape, and the new insert
--      never supplies it, so without this step every new review insert
--      would fail with 23502 (not-null violation). The column is NOT
--      dropped and old values are untouched.
--   4. Deduplicates for the unique constraint WITHOUT deleting anything.
--      CORRECTION to the previous version of this step: merely
--      re-marking extra rows status='hidden' does NOT satisfy a plain
--      UNIQUE(product_id, user_id) — PostgreSQL counts hidden rows
--      toward the constraint, so constraint creation failed with
--      23505 and the whole DO block rolled back. A partial unique
--      index was deliberately NOT used as a substitute: the new code's
--      predicate-less ON CONFLICT (product_id, user_id) cannot use a
--      partial index, so the key must be genuinely unique.
--      Instead, extra rows (all but the earliest per
--      (product_id, user_id), ordered by created_at, id) are BOTH
--      re-marked status='hidden' AND deterministically re-keyed:
--      user_id := user_id || '#0004-superseded-' || rn, where rn is
--      the row's 2, 3, ... position within its (product, user) group.
--      The earliest row per group is untouched and stays the visible
--      one. No row is deleted: review text, customer_id and order
--      linkage are all preserved. Only the derived backfill column
--      (user_id) is suffixed — the original user linkage survives
--      untouched in customer_id, so a re-keyed row's original user_id
--      is always recoverable as customer_id::text. The suffix can
--      only collide with a genuine user_id containing the literal
--      '#0004-superseded-' marker, which genuine UUID/app-generated
--      ids never contain. The count is RAISEd as NOTICE.
--   5. Adds UNIQUE(product_id, user_id) (named constraint, only if
--      absent) so the code's ON CONFLICT (product_id, user_id) upsert
--      has a matching constraint. Added AFTER re-keying, so it cannot
--      fail on duplicates.
--   6. Sets user_id / comment NOT NULL only if no NULLs remain after
--      backfill; otherwise leaves them nullable and RAISEs a NOTICE
--      flagging the rows for manual review (never fails the migration
--      over data it cannot prove safe).
-- Deliberately NOT changed: rating stays SMALLINT (old CHECK 1..5 is
-- equivalent to the new INT CHECK), status keeps its old CHECK and
-- 'published' default, and no old column is dropped.

DO $$
DECLARE
    rekeyed_count INT := 0;
BEGIN
    -- Only the legacy shape (customer_id present) needs migration.
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'product_reviews' AND column_name = 'customer_id'
    ) THEN
        ALTER TABLE product_reviews ADD COLUMN IF NOT EXISTS user_id TEXT;
        ALTER TABLE product_reviews ADD COLUMN IF NOT EXISTS comment TEXT;

        UPDATE product_reviews
           SET user_id = customer_id::text
         WHERE user_id IS NULL AND customer_id IS NOT NULL;

        UPDATE product_reviews
           SET comment = COALESCE(body, '')
         WHERE comment IS NULL;

        -- Relax NOT NULLs the new code never sets and add the missing
        -- DEFAULTs (see header, step 3).
        ALTER TABLE product_reviews ALTER COLUMN customer_id DROP NOT NULL;
        ALTER TABLE product_reviews ALTER COLUMN order_id DROP NOT NULL;
        ALTER TABLE product_reviews ALTER COLUMN order_item_id DROP NOT NULL;
        ALTER TABLE product_reviews ALTER COLUMN body SET DEFAULT '';
        ALTER TABLE product_reviews ALTER COLUMN customer_display_name SET DEFAULT '';

        -- Dedupe + re-key without deleting: hide extras AND suffix
        -- their derived user_id so the plain unique constraint can
        -- hold (see header, step 4). The earliest row per group keeps
        -- its original user_id and stays visible.
        WITH ranked AS (
            SELECT id,
                   ROW_NUMBER() OVER (
                       PARTITION BY product_id, user_id
                       ORDER BY created_at ASC, id ASC
                   ) AS rn
              FROM product_reviews
             WHERE user_id IS NOT NULL
        )
        UPDATE product_reviews r
           SET status = 'hidden',
               user_id = r.user_id || '#0004-superseded-' || ranked.rn::text,
               moderation_reason = COALESCE(r.moderation_reason, '')
                   || ' [0004 dedupe: duplicate of an earlier review for the same product/user; row kept (not deleted), hidden, and user_id re-keyed with #0004-superseded-<n> so the plain (product_id, user_id) unique constraint can hold; original user linkage preserved in customer_id]'
          FROM ranked
         WHERE r.id = ranked.id AND ranked.rn > 1;
        GET DIAGNOSTICS rekeyed_count = ROW_COUNT;
        IF rekeyed_count > 0 THEN
            RAISE NOTICE '0004: % duplicate product_reviews rows re-keyed and hidden (not deleted) for (product_id, user_id) uniqueness', rekeyed_count;
        END IF;

        -- Unique constraint for the new code's ON CONFLICT upsert (step 5).
        IF NOT EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE conname = 'product_reviews_product_user_unique'
        ) THEN
            ALTER TABLE product_reviews
                ADD CONSTRAINT product_reviews_product_user_unique
                UNIQUE (product_id, user_id);
        END IF;

        -- NOT NULL only where provably safe (step 6).
        IF NOT EXISTS (SELECT 1 FROM product_reviews WHERE user_id IS NULL) THEN
            ALTER TABLE product_reviews ALTER COLUMN user_id SET NOT NULL;
        ELSE
            RAISE NOTICE '0004: product_reviews still has NULL user_id rows; user_id left nullable for manual review';
        END IF;
        IF NOT EXISTS (SELECT 1 FROM product_reviews WHERE comment IS NULL) THEN
            ALTER TABLE product_reviews ALTER COLUMN comment SET NOT NULL;
        ELSE
            RAISE NOTICE '0004: product_reviews still has NULL comment rows; comment left nullable for manual review';
        END IF;
    ELSE
        RAISE NOTICE '0004: product_reviews already in new shape (or absent); nothing to migrate';
    END IF;
END $$;
