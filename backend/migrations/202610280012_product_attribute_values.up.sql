-- 202610280012: product ↔ Digikala attribute-value links
-- (price-filters mission, Oct 29, 2026).
--
-- WHY: attribute faceted filtering was impossible — the dk_* tables
-- (migration 202610280011) link attributes to CATEGORIES only; no
-- table linked a PRODUCT to the dk attribute values its supplier
-- picked in the proposal form (categoryAttributes in
-- pending_approvals.requested_data). This table is that link.
--
-- SHAPE (mission contract, adapted to the real schema):
--   - product_id is VARCHAR(50), NOT UUID: products.id is
--     VARCHAR(50) (migration 000001), and every other product link
--     table (product_niches, product_images, ...) uses VARCHAR(50).
--     A UUID column could not carry the FK to products(id).
--   - dk_attribute_id / dk_value_id are BIGINT per the contract
--     (the dk_* registries use INT; values fit, and the approval
--     write path validates picks against dk_attribute_values so no
--     junk pair can be stored — validation is the join, not an FK,
--     because a composite FK would force the INT types and the
--     contract fixes BIGINT here).
--   - PRIMARY KEY (product_id, dk_value_id) per the contract: a
--     product holds a given value at most once. (dk_value_id is
--     unique per attribute in the Digikala registry pairing, and
--     the (dk_attribute_id, dk_value_id) index below keeps the
--     attribute dimension queryable.)
--
-- INDEXES: the filter direction is (attribute, value) -> products,
-- so an index on (dk_attribute_id, dk_value_id) is created. An index
-- on product_id alone is NOT created: the PRIMARY KEY already starts
-- with product_id, so product lookups (DTO batch load, ON DELETE
-- CASCADE) use it — a second index would be redundant.
--
-- No backfill: existing products have no recorded picks (specs is
-- free-text and is NOT a reliable source), so they simply have no
-- rows and are correctly excluded from attribute-filtered results
-- until a re-approval / direct creation records picks.

CREATE TABLE IF NOT EXISTS product_attribute_values (
    product_id      VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    dk_attribute_id BIGINT NOT NULL,
    dk_value_id     BIGINT NOT NULL,
    PRIMARY KEY (product_id, dk_value_id)
);

CREATE INDEX IF NOT EXISTS idx_product_attribute_values_attr_value
    ON product_attribute_values (dk_attribute_id, dk_value_id);

COMMENT ON TABLE product_attribute_values IS
    'Product ↔ Digikala attribute-value picks recorded at approval (migration 202610280012). Validated against dk_attribute_values at write time.';
