-- 202610250005: rename supplier_shipping_methods.supplier_id to owner_user_id.
--
-- The column never held a supplier PK: it stores the supplier OWNER's
-- users.id (UUID text) — the keying is deliberate (migration 202610170012,
-- audit M5: admin tooling joins this table directly with the users list)
-- but the name invited exactly the wrong INSERT (a supplier id silently
-- matched nothing — night-audit P3 #47 / A-9, independently hit live by
-- the shipping front). Rename the column so the name can no longer lie;
-- the owner-UUID keying and every behaviour are unchanged.
--
-- Also drops orphan rows that resolve to no user at all (they can never be
-- joined to a supplier or an admin decision).
ALTER TABLE supplier_shipping_methods RENAME COLUMN supplier_id TO owner_user_id;
ALTER INDEX IF EXISTS idx_ssm_supplier RENAME TO idx_ssm_owner_user;
DELETE FROM supplier_shipping_methods ssm
WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id::text = ssm.owner_user_id);
