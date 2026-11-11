-- Additive identity metadata only. Existing UUID/SPNC/stock rows are untouched;
-- no legacy product stock is assigned to guessed combinations.
ALTER TABLE products
  ADD COLUMN IF NOT EXISTS product_type TEXT NOT NULL DEFAULT 'legacy',
  ADD COLUMN IF NOT EXISTS schema_version INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS attribute_schema JSONB;
ALTER TABLE products ADD CONSTRAINT products_product_model_v2_check CHECK (
  (schema_version = 0 AND product_type = 'legacy') OR
  (schema_version = 2 AND product_type IN ('simple','variable') AND attribute_schema IS NOT NULL)
);

ALTER TABLE product_variants
  ADD COLUMN IF NOT EXISTS selection JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS combination_key TEXT,
  ADD COLUMN IF NOT EXISTS inventory_mode TEXT NOT NULL DEFAULT 'legacy',
  ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE product_variants ADD CONSTRAINT product_variants_inventory_mode_check CHECK (inventory_mode IN ('legacy','tracked'));
ALTER TABLE product_variants ADD CONSTRAINT product_variants_selection_object_check CHECK (jsonb_typeof(selection) = 'object');
CREATE UNIQUE INDEX IF NOT EXISTS product_variants_combination_unique ON product_variants(product_id,combination_key) WHERE combination_key IS NOT NULL;

CREATE TABLE product_draft_sku_reservations (
  sku_id UUID PRIMARY KEY,
  proposal_id VARCHAR(50) NOT NULL REFERENCES pending_approvals(id) ON DELETE CASCADE
);
CREATE INDEX product_draft_sku_reservations_proposal ON product_draft_sku_reservations(proposal_id);

-- Requested definitions retain their stable draft IDs when published. Imported
-- DK/color identities remain in their original registries and are read as peers.
CREATE TABLE local_product_attributes (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  normalized_name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE local_product_attribute_values (
  attribute_id TEXT NOT NULL REFERENCES local_product_attributes(id),
  id TEXT NOT NULL,
  label TEXT NOT NULL,
  normalized_label TEXT NOT NULL,
  PRIMARY KEY(attribute_id,id),
  UNIQUE(attribute_id,normalized_label)
);
CREATE TABLE category_local_product_attributes (
  category_id VARCHAR(50) NOT NULL REFERENCES categories(id),
  attribute_id TEXT NOT NULL REFERENCES local_product_attributes(id),
  PRIMARY KEY(category_id,attribute_id)
);
CREATE TABLE category_local_product_attribute_values (
  category_id VARCHAR(50) NOT NULL,
  attribute_id TEXT NOT NULL,
  value_id TEXT NOT NULL,
  PRIMARY KEY(category_id,attribute_id,value_id),
  FOREIGN KEY(category_id,attribute_id) REFERENCES category_local_product_attributes(category_id,attribute_id),
  FOREIGN KEY(attribute_id,value_id) REFERENCES local_product_attribute_values(attribute_id,id)
);

-- New options for an imported color/DK definition keep the same attribute ID.
CREATE TABLE product_attribute_option_extensions (
  attribute_id TEXT NOT NULL,
  id TEXT NOT NULL,
  label TEXT NOT NULL,
  normalized_label TEXT NOT NULL,
  PRIMARY KEY(attribute_id,id),
  UNIQUE(attribute_id,normalized_label)
);
CREATE TABLE category_product_attribute_option_extensions (
  category_id VARCHAR(50) NOT NULL REFERENCES categories(id),
  attribute_id TEXT NOT NULL,
  value_id TEXT NOT NULL,
  PRIMARY KEY(category_id,attribute_id,value_id),
  FOREIGN KEY(attribute_id,value_id) REFERENCES product_attribute_option_extensions(attribute_id,id)
);
CREATE TABLE product_canonical_attribute_values (
  product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  attribute_id TEXT NOT NULL,
  option_id TEXT NOT NULL,
  PRIMARY KEY(product_id,attribute_id,option_id)
);
