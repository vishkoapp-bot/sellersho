-- FIN-012: version new snapshots without recomputing historical paid money.
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS pricing_policy_version TEXT
    NOT NULL DEFAULT 'legacy-unversioned';
COMMENT ON COLUMN order_items.pricing_policy_version IS
    'Policy used at purchase. Historical rows stay legacy-unversioned; never recalculate paid snapshots.';
