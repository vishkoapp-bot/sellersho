-- Per-variant supplier cost (Oct 28, 2026 — user order: a variant must
-- carry a price, e.g. the black color costs more than the white one).
--
-- product_variants.supplier_base_cost is the supplier cost C (Toman,
-- integer — the same unit and type as products.supplier_base_cost) for
-- THIS variant. NULL means "no variant-specific cost — inherit from
-- products.supplier_base_cost" (COALESCE at every read site), so existing
-- variants behave exactly as today and no data is fabricated.
-- The effective C flows through the 3-tier pricing unchanged
-- (variant C -> base B = C + F -> central x(1+central%) -> dropshipper
-- margin), and is snapshotted into order_items at order time; the
-- ledger, returns settlement and store_variant_overrides read those
-- snapshots, so they need no change. The cost is confidential: it must
-- NEVER enter the public variant DTO (domain.ProductVariant /
-- GetPublicVariants) — only the internal server-side loader reads it.
ALTER TABLE product_variants
    ADD COLUMN IF NOT EXISTS supplier_base_cost BIGINT,
    ADD CONSTRAINT product_variants_supplier_base_cost_positive
        CHECK (supplier_base_cost IS NULL OR supplier_base_cost > 0);
COMMENT ON COLUMN product_variants.supplier_base_cost IS
    'Supplier cost C for this variant, Toman (integer). NULL = inherit products.supplier_base_cost.';
