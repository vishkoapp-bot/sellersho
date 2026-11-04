-- Backfill stores.owner_phone for rows where onboarding left it empty (Fix 11,
-- data-loss audit 20261018). The suspended-store 410 page needs owner_phone
-- for its call button; without it the button vanishes. Each store's owner
-- authenticated with their mobile, so users.mobile of the store owner is the
-- correct fallback. New reads also coalesce at query time (store_repo), this
-- migration heals the existing rows durably.
UPDATE stores s
SET owner_phone = u.mobile, updated_at = NOW()
FROM users u
WHERE s.owner_user_id = u.id
  AND (s.owner_phone IS NULL OR TRIM(s.owner_phone) = '')
  AND u.mobile IS NOT NULL AND TRIM(u.mobile) <> '';
