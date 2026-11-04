-- Existing seller stores previously treated the catalog as globally available.
-- Preserve that behavior once checkout starts enforcing explicit publication.
INSERT INTO store_products (store_id, product_id, position)
SELECT s.id, p.id, ROW_NUMBER() OVER (PARTITION BY s.id ORDER BY p.id) - 1
FROM stores s
CROSS JOIN products p
WHERE s.owner_type = 'dropshipper'
  AND p.supplier_base_cost > 0
ON CONFLICT (store_id, product_id) DO NOTHING;