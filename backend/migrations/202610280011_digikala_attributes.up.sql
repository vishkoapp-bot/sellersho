-- 202610280011: Digikala per-category attributes (digikala-attributes-20261028).
--
-- WHY: the supplier proposal form must offer Digikala's per-category
-- variant axes (attributes + colors + brands) as pick-lists, keyed by
-- the picked category — NOT free text. Source: Digikala's public
-- category-search API, data.filters (attributes / color_palettes /
-- brands); products are NEVER imported, facets only.
--
-- DESIGN (research.md §4, faithful import):
--   - dk_attributes: ONE row per Digikala attribute id (global
--     definition: title + type). Probed types are all "checkbox"
--     (multi-select); the column keeps whatever type a full fetch
--     reports, so a future radio/range type is stored, not dropped.
--   - dk_attribute_values: (dk_attr_id, dk_value_id) unique — the
--     GLOBAL value registry. The same attribute id in two categories
--     (e.g. 2226 «سیستم عامل»: 6 values in mobile-phone, 12 in
--     notebook) shares the attribute row but NOT the value set.
--   - dk_category_attributes: the faithful own-category link — exactly
--     what Digikala returned for THAT category, nothing invented.
--   - dk_category_attribute_values: the per-(category, attribute)
--     value set. This is the table that makes the (category,
--     attribute) -> values mapping real; without it the API could
--     only serve the global value set, which is wrong (see above).
--   - dk_category_colors / dk_category_brands: the same category's
--     color_palettes / brands facets. Brands are REFERENCE-ONLY for a
--     suggestion dropdown: they are NOT merged into Selersho's brand
--     registry (products.brand stays a free-text column; the form's
--     brand field is unchanged).
--   - NO union rows are stored: a parent with 0 own attributes gets
--     its descendant union computed at query time (recursive CTE in
--     CatalogRepository.GetCategoryAttributesBySlug) and flagged
--     inherited:true. Storing the union would go stale on every
--     re-import and would lie about what Digikala returned.
--
-- INDEXES: every link table's PRIMARY KEY starts with category_id,
-- so category lookups use the PK index (no redundant second index on
-- the same leading column). dk_attribute_values' PK starts with
-- dk_attr_id for the same reason.
--
-- This file applies DDL only. import.sql (generated from raw/filters/)
-- only INSERTs and ASSERTs these tables exist; it never creates them.

CREATE TABLE IF NOT EXISTS dk_attributes (
    dk_attr_id INT PRIMARY KEY,
    title      TEXT NOT NULL,
    type       VARCHAR(50) NOT NULL DEFAULT 'checkbox'
);

CREATE TABLE IF NOT EXISTS dk_attribute_values (
    dk_attr_id  INT NOT NULL REFERENCES dk_attributes(dk_attr_id) ON DELETE CASCADE,
    dk_value_id INT NOT NULL,
    title_fa    TEXT NOT NULL,
    title_en    TEXT,
    PRIMARY KEY (dk_attr_id, dk_value_id)
);

CREATE TABLE IF NOT EXISTS dk_category_attributes (
    category_id VARCHAR(50) NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    dk_attr_id  INT NOT NULL REFERENCES dk_attributes(dk_attr_id) ON DELETE CASCADE,
    PRIMARY KEY (category_id, dk_attr_id)
);

CREATE TABLE IF NOT EXISTS dk_category_attribute_values (
    category_id VARCHAR(50) NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    dk_attr_id  INT NOT NULL,
    dk_value_id INT NOT NULL,
    PRIMARY KEY (category_id, dk_attr_id, dk_value_id),
    FOREIGN KEY (dk_attr_id, dk_value_id)
        REFERENCES dk_attribute_values(dk_attr_id, dk_value_id) ON DELETE CASCADE,
    FOREIGN KEY (category_id, dk_attr_id)
        REFERENCES dk_category_attributes(category_id, dk_attr_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS dk_category_colors (
    category_id VARCHAR(50) NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    dk_color_id INT NOT NULL,
    title       TEXT NOT NULL,
    hex_code    VARCHAR(20),
    PRIMARY KEY (category_id, dk_color_id)
);

CREATE TABLE IF NOT EXISTS dk_category_brands (
    category_id VARCHAR(50) NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    dk_brand_id INT NOT NULL,
    code        VARCHAR(100),
    title_fa    TEXT NOT NULL,
    title_en    TEXT,
    PRIMARY KEY (category_id, dk_brand_id)
);
