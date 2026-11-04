-- 202610310004 DOWN: reverses only what 0004 itself added, and only on a
-- table in the legacy shape (customer_id present). On a new-shape table
-- (created by 0002) this is a no-op: dropping user_id/comment there
-- would destroy live review data, so it is guarded out.
--
-- NOT NULL restoration on customer_id / order_id / order_item_id is
-- attempted ONLY when no NULLs exist in that column (rows written by
-- the new code have NULLs there); otherwise the column is left relaxed
-- and a NOTICE is raised, rather than failing the rollback.
-- WARNING: dropping user_id/comment loses the new-code linkage of any
-- reviews written after 0004. Use this down only for a rollback before
-- new reviews were written, or accept that loss explicitly.
-- ROLLBACK DOES NOT REVERSE THE 0004 RE-KEYING: this down does NOT
-- un-hide the deduped rows (their status='hidden' and appended note
-- persist) and does NOT restore the re-keyed user_id values to their
-- originals — dropping the derived user_id column removes those
-- values, it does not restore them. The original user linkage is
-- still recoverable from the untouched customer_id column. The
-- customer_display_name DEFAULT '' added by the up is dropped here.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'product_reviews' AND column_name = 'customer_id'
    ) THEN
        ALTER TABLE product_reviews
            DROP CONSTRAINT IF EXISTS product_reviews_product_user_unique;
        ALTER TABLE product_reviews DROP COLUMN IF EXISTS user_id;
        ALTER TABLE product_reviews DROP COLUMN IF EXISTS comment;
        ALTER TABLE product_reviews ALTER COLUMN body DROP DEFAULT;
        ALTER TABLE product_reviews ALTER COLUMN customer_display_name DROP DEFAULT;

        IF NOT EXISTS (SELECT 1 FROM product_reviews WHERE customer_id IS NULL) THEN
            ALTER TABLE product_reviews ALTER COLUMN customer_id SET NOT NULL;
        ELSE
            RAISE NOTICE '0004 down: customer_id has NULL rows; NOT NULL not restored';
        END IF;
        IF NOT EXISTS (SELECT 1 FROM product_reviews WHERE order_id IS NULL) THEN
            ALTER TABLE product_reviews ALTER COLUMN order_id SET NOT NULL;
        ELSE
            RAISE NOTICE '0004 down: order_id has NULL rows; NOT NULL not restored';
        END IF;
        IF NOT EXISTS (SELECT 1 FROM product_reviews WHERE order_item_id IS NULL) THEN
            ALTER TABLE product_reviews ALTER COLUMN order_item_id SET NOT NULL;
        ELSE
            RAISE NOTICE '0004 down: order_item_id has NULL rows; NOT NULL not restored';
        END IF;
    ELSE
        RAISE NOTICE '0004 down: product_reviews not in legacy shape; nothing to reverse';
    END IF;
END $$;
