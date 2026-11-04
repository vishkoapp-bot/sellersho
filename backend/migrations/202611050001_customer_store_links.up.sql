-- 202611050001: buyer signup attribution (Fix83).
--
-- Buyers live in the central users table (one phone = one row, UNIQUE
-- mobile); this link table records WHICH stores a buyer signed up at,
-- without duplicating users or orders. A buyer may hold panels at
-- central, mone and other dropshippers, each attributed separately.
-- first_seen_at is set once: re-recording the same pair is a no-op
-- (ON CONFLICT DO NOTHING) and never overwrites it.
CREATE TABLE IF NOT EXISTS customer_store_links (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, store_id)
);

-- Backfill (idempotent, derived data only): attribute past customers
-- from their orders. The joins guard the FKs (NULL customer ids, empty
-- store ids and orphan store ids can never violate them), and only
-- customer-role users are attributed, matching the forward path.
-- ON CONFLICT keeps the original first_seen_at for pairs already recorded.
INSERT INTO customer_store_links (user_id, store_id)
SELECT DISTINCT o.customer_user_id, o.store_id
FROM orders o
JOIN users u ON u.id = o.customer_user_id AND u.role = 'customer'
JOIN stores s ON s.id = o.store_id
WHERE o.customer_user_id IS NOT NULL AND o.store_id IS NOT NULL AND o.store_id <> ''
ON CONFLICT (user_id, store_id) DO NOTHING;
