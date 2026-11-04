-- Canonicalize legacy Iranian mobile representations without merging identities.
-- Rows that would collide are intentionally left for manual review.
WITH normalized AS (
    SELECT id,
           CASE
             WHEN regexp_replace(mobile, '[^0-9]', '', 'g') LIKE '98%' THEN '0' || substring(regexp_replace(mobile, '[^0-9]', '', 'g') FROM 3)
             WHEN regexp_replace(mobile, '[^0-9]', '', 'g') LIKE '9%' AND length(regexp_replace(mobile, '[^0-9]', '', 'g')) = 10 THEN '0' || regexp_replace(mobile, '[^0-9]', '', 'g')
             ELSE regexp_replace(mobile, '[^0-9]', '', 'g')
           END AS canonical_mobile
    FROM users
), unique_targets AS (
    SELECT canonical_mobile
    FROM normalized
    GROUP BY canonical_mobile
    HAVING COUNT(*) = 1
)
UPDATE users u
SET mobile = n.canonical_mobile
FROM normalized n
JOIN unique_targets t ON t.canonical_mobile = n.canonical_mobile
WHERE u.id = n.id
  AND u.mobile <> n.canonical_mobile;