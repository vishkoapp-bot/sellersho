package main

import (
	"database/sql"
	"testing"
)

// TEST-02 round 4: the warehouse_customer_return_bin_sources view
// (migration 202611150002) is CASCADE-dropped whenever a fixture
// DROP TABLEs one of its base tables (products, order_items,
// customer_returns, ...). The BEFORE trigger on
// warehouse_return_bin_entries references the view, so a later
// putaway fails with 42P01 (raw error -> HTTP 500) instead of the
// intended 409 source conflict. Recreate it verbatim from the
// migration. The trigger function survives the CASCADE drop
// (plpgsql bodies are opaque text), so no trigger re-creation is
// needed. Mirrors createEffectiveMeasurementsView in
// internal/repository/test02_view_fixture_test.go.
func createReturnBinSourcesView(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE OR REPLACE VIEW warehouse_customer_return_bin_sources AS
 SELECT cr.id AS return_id,ri.id AS return_item_id,cr.order_id,oi.product_id,
  CASE WHEN c.inventory_mode='tracked' THEN c.variant_id::text ELSE '' END AS variant_id,
  p.title AS product_title,COALESCE(oi.variant_name,'') AS variant_name,COALESCE(oi.variant_spnc,'') AS variant_spnc,
  cr.warehouse_id,ri.qty,ri.qc_status,ri.received_at,cr.status AS return_status,
  b.id AS source_balance_id,b.supplier_id,b.location_key,
  LEAST(COALESCE(b.quantity,0),ri.qty-COALESCE(e.quantity,0))::bigint AS available_quantity,
  (cr.status IN ('received','refunded') AND ri.qc_status='ok' AND ri.received_at IS NOT NULL
   AND cr.warehouse_id IS NOT NULL AND cr.destination_supplier_warehouse_id IS NULL
   AND b.id IS NOT NULL AND b.owner_type='supplier' AND b.location_type='warehouse' AND b.status='sellable'
   AND b.supplier_id=p.supplier_id AND m.supplier_id=b.supplier_id AND m.quantity=ri.qty
   AND COALESCE(b.variant_id::text,'')=CASE WHEN c.inventory_mode='tracked' THEN c.variant_id::text ELSE '' END
   AND ((c.inventory_mode='tracked' AND b.warehouse_id=cr.warehouse_id)
    OR (COALESCE(c.inventory_mode,'legacy')<>'tracked' AND (b.warehouse_id IS NULL OR b.warehouse_id=cr.warehouse_id)
     AND m.metadata->>'destination'='warehouse:'||cr.warehouse_id
     AND (SELECT COUNT(*) FROM return_items sibling JOIN order_items soi ON soi.id::text=sibling.order_item_id WHERE sibling.return_id=ri.return_id AND soi.product_id=oi.product_id AND sibling.qc_status='ok')=1))) IS TRUE AS source_proven
 FROM customer_returns cr JOIN return_items ri ON ri.return_id=cr.id
 JOIN order_items oi ON oi.id::text=ri.order_item_id JOIN products p ON p.id=oi.product_id
 LEFT JOIN order_inventory_captures c ON c.order_item_id=ri.order_item_id
 LEFT JOIN inventory_movements m ON m.idempotency_key=CASE WHEN c.inventory_mode='tracked' THEN 'sku-return:'||ri.id::text ELSE 'crstk:'||cr.id::text||':'||ri.id::text||':sellable' END
  AND m.product_id=oi.product_id AND m.to_status='sellable' AND m.reference_type='customer_return'
  AND m.reference_id=CASE WHEN c.inventory_mode='tracked' THEN ri.id::text ELSE cr.id::text END
  AND m.to_location_key=CASE WHEN c.inventory_mode='tracked' THEN 'warehouse:'||cr.warehouse_id||':return:'||ri.id::text ELSE 'custreturn:'||cr.id::text END
  AND COALESCE(m.variant_id::text,'')=CASE WHEN c.inventory_mode='tracked' THEN c.variant_id::text ELSE '' END
 LEFT JOIN inventory_balances b ON b.product_id=oi.product_id AND b.location_key=m.to_location_key AND b.status='sellable' AND b.owner_type='supplier' AND b.location_type='warehouse' AND b.supplier_id=m.supplier_id AND COALESCE(b.variant_id::text,'')=COALESCE(m.variant_id::text,'')
 LEFT JOIN LATERAL (SELECT SUM(quantity) AS quantity FROM warehouse_return_bin_entries e WHERE e.return_item_id=ri.id) e ON TRUE`)
	if err != nil {
		t.Fatalf("recreate warehouse_customer_return_bin_sources view: %v", err)
	}
}

// TEST-02 round 4: the inventory_effective_measurements view
// (migration 202611140010) is CASCADE-dropped whenever a fixture
// DROP TABLEs products or product_variants. The customer-return-bin
// guard trigger reads it (verified measurement lookup), so a putaway
// fails with 42P01 instead of the intended 409 when it is missing.
// Same DDL as createEffectiveMeasurementsView in
// internal/repository/test02_view_fixture_test.go.
func createEffectiveMeasurementsView(t *testing.T, db *sql.DB) {
	t.Helper()
	const view = `CREATE OR REPLACE VIEW inventory_effective_measurements AS
 WITH effective AS (
  SELECT p.id::text AS product_id,''::text AS variant_id,COALESCE(p.length_mm,0) AS length_mm,COALESCE(p.width_mm,0) AS width_mm,COALESCE(p.height_mm,0) AS height_mm,COALESCE(p.weight_grams,0) AS weight_grams,
   'product'::text AS source,'product'::text AS scope,''::text AS revision_variant,p.measurement_status AS stored_status,COALESCE(p.measurement_revision_id,'') AS revision,TRUE AS complete
  FROM products p
  UNION ALL
  SELECT p.id::text,v.id::text,COALESCE(v.length_mm,p.length_mm,0),COALESCE(v.width_mm,p.width_mm,0),COALESCE(v.height_mm,p.height_mm,0),COALESCE(v.weight_grams,p.weight_grams,0),
   CASE WHEN s.overridden THEN 'sku' ELSE 'product' END,CASE WHEN s.overridden THEN 'sku' ELSE 'product' END,CASE WHEN s.overridden THEN v.id::text ELSE '' END,
   CASE WHEN s.overridden THEN v.measurement_status ELSE p.measurement_status END,COALESCE(CASE WHEN s.overridden THEN v.measurement_revision_id ELSE p.measurement_revision_id END,''),
   NOT s.overridden OR (v.length_mm IS NOT NULL AND v.width_mm IS NOT NULL AND v.height_mm IS NOT NULL AND v.weight_grams IS NOT NULL)
  FROM products p JOIN product_variants v ON v.product_id=p.id
  CROSS JOIN LATERAL (SELECT v.length_mm IS NOT NULL OR v.width_mm IS NOT NULL OR v.height_mm IS NOT NULL OR v.weight_grams IS NOT NULL AS overridden) s
 )
 SELECT e.product_id,e.variant_id,e.length_mm,e.width_mm,e.height_mm,e.weight_grams,e.source,e.revision,
  COALESCE(r.verified,FALSE) AND e.stored_status='verified' AND e.complete AS verified,
  CASE WHEN e.stored_status='disputed' THEN 'disputed' WHEN COALESCE(r.verified,FALSE) AND e.stored_status='verified' AND e.complete THEN 'verified' ELSE 'unverified' END AS measurement_status
 FROM effective e LEFT JOIN inventory_measurement_revisions r ON r.id=e.revision AND r.product_id=e.product_id AND r.variant_id=e.revision_variant AND r.scope=e.scope AND (r.length_mm,r.width_mm,r.height_mm,r.weight_grams)=(e.length_mm,e.width_mm,e.height_mm,e.weight_grams)`
	if _, err := db.Exec(view); err != nil {
		t.Fatalf("re-create inventory_effective_measurements view: %v", err)
	}
}
