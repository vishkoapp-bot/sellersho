-- 202611050009: variant-level price-increase governance (critic fix F1).
--
-- Gap 11 governed PRODUCT price increases through
-- supplier_price_increases, but gap 10's variant pricing write path
-- (PUT /supplier/products/{id}/variants/{vid}/pricing) applied any
-- variant supplier_base_cost change immediately, bypassing the whole
-- recorded policy matrix (migrations 202610310003 + 202610310006).
-- Variant increases now queue in the SAME table and the SAME admin
-- review queue: variant_id names the product_variants row the request
-- targets. NULL = a product-level request (every pre-existing row).
-- old_reference for a variant row is the variant's EFFECTIVE cost at
-- filing time (its own supplier_base_cost, else the product's — the
-- inheritance the pricing resolver reads); review approval applies
-- new_reference to product_variants.supplier_base_cost and its stale
-- guard compares against the variant's current effective cost.
ALTER TABLE supplier_price_increases ADD COLUMN IF NOT EXISTS variant_id TEXT;
CREATE INDEX IF NOT EXISTS idx_supplier_price_increases_product_variant_status
  ON supplier_price_increases (product_id, variant_id, status);
