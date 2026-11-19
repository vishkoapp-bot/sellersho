package repository

import (
	"database/sql"
	"testing"
)

// createEffectiveMeasurementsView re-creates the
// inventory_effective_measurements view after a fixture that DROP TABLEs
// products (or product_variants) CASCADE.
//
// TEST-02 round 4: the destructive fixtures in this package drop products
// CASCADE, which also drops this view (migration 202611140010 defines it
// over products + product_variants + inventory_measurement_revisions).
// The first such fixture (catalog_team04, alphabetically) drops the view
// for the rest of the package, so the 81 FIN005 / audit / storage tests
// that query it through the repo (sku_measurement.go, warehouse_repo.go,
// storage_receipt_history.go, customer_return_putaway.go) fail with 42P01
// "relation inventory_effective_measurements does not exist". Every
// fixture that rebuilds products must call this afterwards — the view
// binds to the public tables under the fixtures' search_path.
//
// The DDL is copied verbatim from
// migrations/202611140010_sku_measurement_revision.up.sql (lines 50-66).
// CREATE OR REPLACE keeps it idempotent if the view already exists. The
// trigger functions that reference the view survive the CASCADE drop
// (plpgsql bodies are opaque text, not dependencies), so they work again
// once the view is back — no trigger re-creation is needed.
// re-create the inventory_measurement_revisions table FIRST (TEST-02
// round 4, second half): the finance fixtures run in an exclusive
// scratch schema (openPrivateFinanceFixture, search_path with NO public
// fallback), which never contains this table — the view DDL below
// references it, and the repo queries it directly
// (warehouse_occupancy.go JOINs it), so without the table both the
// view creation AND 94 FIN005 / finance / gap6 tests fail with 42P01
// "relation inventory_measurement_revisions does not exist". The DDL
// is copied verbatim from
// migrations/202611140010_sku_measurement_revision.up.sql (lines 8-23);
// CREATE TABLE IF NOT EXISTS keeps it a no-op on the shared DB, where
// the migrated table already exists. The migration's INSERT snapshot
// and guard triggers are deliberately NOT recreated: the scratch
// fixtures install neither, and the tests seed revisions explicitly.
func createEffectiveMeasurementsView(t *testing.T, db *sql.DB) {
	t.Helper()
	const revisions = `CREATE TABLE IF NOT EXISTS inventory_measurement_revisions (
 id TEXT PRIMARY KEY,
 product_id TEXT NOT NULL,
 variant_id TEXT NOT NULL DEFAULT '',
 scope TEXT NOT NULL CHECK(scope IN ('product','sku')),
 length_mm INT NOT NULL CHECK(length_mm BETWEEN 1 AND 1000000),
 width_mm INT NOT NULL CHECK(width_mm BETWEEN 1 AND 1000000),
 height_mm INT NOT NULL CHECK(height_mm BETWEEN 1 AND 1000000),
 weight_grams INT NOT NULL CHECK(weight_grams BETWEEN 1 AND 1000000000),
 verified BOOLEAN NOT NULL,
 reason TEXT NOT NULL CHECK(reason<>''),
 actor_user_id TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK((scope='product' AND variant_id='') OR (scope='sku' AND variant_id<>'')))`
	if _, err := db.Exec(revisions); err != nil {
		t.Fatalf("re-create inventory_measurement_revisions table: %v", err)
	}
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

// TEST-02 round 5: the warehouse_occupancy_audit view (migration
// 202611150001) is CASCADE-dropped whenever a fixture DROP TABLEs one
// of its base tables (order_inventory_captures, inventory_balances,
// warehouse_placements, ...). The INV06 read-only audit test queries
// it through scripts/audit-warehouse-occupancy.sql and fails with
// 42P01 "relation warehouse_occupancy_audit does not exist" in the
// full run (it passes in isolation, where no earlier fixture dropped
// the view). Recreate the three views verbatim from the migration,
// plus the two occupancy tables if a fixture dropped them (CREATE
// TABLE IF NOT EXISTS is a no-op on the shared DB). The releases
// table is recreated WITHOUT its migration FKs: the view only SELECTs
// from it, and the FK targets may themselves be fixture-rebuilt in a
// shape the FK would reject. Mirrors createEffectiveMeasurementsView
// / createReturnBinSourcesView above.
func createWarehouseOccupancyAuditViews(t *testing.T, db *sql.DB) {
	t.Helper()
	// TEST-02 round 5: DROP first, then CREATE. CREATE OR REPLACE
	// fails with 42P16 here because the view selects p.* from
	// warehouse_placements, and the fixture-rebuilt placements table
	// has a different column set/order than the migrated one — the
	// replacement query expands to a different column list than the
	// existing view. Dropping first (CASCADE removes the dependent
	// audit/orphan views, which are recreated below) avoids the
	// mismatch. Trigger functions that reference these views survive
	// (plpgsql bodies are opaque text).
	for _, q := range []string{
		`DROP VIEW IF EXISTS warehouse_occupancy_audit CASCADE`,
		`DROP VIEW IF EXISTS warehouse_occupancy_orphan_allocations CASCADE`,
		`DROP VIEW IF EXISTS warehouse_remaining_placements CASCADE`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("drop warehouse occupancy view: %v", err)
		}
	}
	const releases = `CREATE TABLE IF NOT EXISTS warehouse_occupancy_releases (
 id TEXT PRIMARY KEY,
 event_type TEXT NOT NULL,
 event_id TEXT NOT NULL,
 placement_id TEXT NOT NULL,
 quantity INT NOT NULL CHECK(quantity>0),
 volume_cm3 BIGINT NOT NULL CHECK(volume_cm3>=0),
 weight_grams BIGINT NOT NULL CHECK(weight_grams>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(event_type,event_id,placement_id))`
	if _, err := db.Exec(releases); err != nil {
		t.Fatalf("re-create warehouse_occupancy_releases table: %v", err)
	}
	const exceptions = `CREATE TABLE IF NOT EXISTS warehouse_occupancy_exceptions (
 bin_code TEXT PRIMARY KEY,
 reason TEXT NOT NULL,
 event_type TEXT NOT NULL,
 event_id TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`
	if _, err := db.Exec(exceptions); err != nil {
		t.Fatalf("re-create warehouse_occupancy_exceptions table: %v", err)
	}
	const remaining = `CREATE OR REPLACE VIEW warehouse_remaining_placements AS
 SELECT p.*,p.quantity-COALESCE(r.quantity,0) AS remaining_quantity,
 p.volume_cm3-COALESCE(r.volume_cm3,0) AS remaining_volume_cm3,
 p.weight_grams-COALESCE(r.weight_grams,0) AS remaining_weight_grams
 FROM warehouse_placements p LEFT JOIN (
 SELECT placement_id,SUM(quantity) AS quantity,SUM(volume_cm3) AS volume_cm3,SUM(weight_grams) AS weight_grams
 FROM warehouse_occupancy_releases GROUP BY placement_id) r ON r.placement_id=p.id`
	if _, err := db.Exec(remaining); err != nil {
		t.Fatalf("re-create warehouse_remaining_placements view: %v", err)
	}
	const audit = `CREATE OR REPLACE VIEW warehouse_occupancy_audit AS
 WITH measured AS (
  SELECT bin_code,SUM(remaining_quantity) AS quantity,SUM(remaining_volume_cm3) AS volume,SUM(remaining_weight_grams) AS weight FROM warehouse_remaining_placements GROUP BY bin_code
 ), held AS (
  SELECT warehouse_bin_identity(location_key,to_jsonb(inventory_balances)->>'bin_code') AS bin_code,SUM(quantity) AS quantity FROM inventory_balances WHERE location_type='bin' AND status IN ('sellable','return_requested','quarantine','qc_accepted','allocated','reserved') GROUP BY 1
 ), captured AS (
  SELECT warehouse_bin_identity(b.location_key,to_jsonb(b)->>'bin_code') AS bin_code,(a->>'quantity')::numeric AS quantity,c.restored_quantity,c.quantity AS captured_quantity,o.payment_status,o.status,o.shipped_at,c.order_id
  FROM order_inventory_captures c CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a JOIN inventory_balances b ON b.id=a->>'balanceId' JOIN orders o ON o.id=c.order_id WHERE b.location_type='bin'
 ), capture_totals AS (
  SELECT bin_code,COALESCE(SUM(quantity) FILTER(WHERE payment_status='paid' AND shipped_at IS NULL AND status NOT IN ('cancelled','shipped','delivered','delivered_unconfirmed','delivered_confirmed','delivery_disputed') AND restored_quantity=0),0) AS pending_quantity,
   COUNT(*) FILTER(WHERE restored_quantity>0 AND restored_quantity<captured_quantity) AS partial_restore_count,
   COUNT(*) FILTER(WHERE (shipped_at IS NOT NULL OR status IN ('shipped','delivered','delivered_unconfirmed','delivered_confirmed','delivery_disputed')) AND NOT EXISTS(SELECT 1 FROM warehouse_departure_events e WHERE e.event_type='warehouse_shipment' AND e.event_id=captured.order_id)) AS historical_departures
  FROM captured GROUP BY bin_code
 ), return_history AS (
  SELECT warehouse_bin_identity(b.location_key,to_jsonb(b)->>'bin_code') AS bin_code,COUNT(*) AS historical_handovers FROM inventory_reservations ir JOIN inventory_balances b ON b.product_id=ir.product_id AND b.supplier_id=ir.supplier_id AND b.location_key=ir.location_key AND COALESCE(to_jsonb(b)->>'variant_id','')=COALESCE(to_jsonb(ir)->>'variant_id','') AND b.status='return_requested' AND b.location_type='bin' WHERE ir.reference_type='supplier_return' AND ir.status='consumed' AND NOT EXISTS(SELECT 1 FROM warehouse_departure_events e WHERE e.event_type='supplier_return' AND e.event_id=ir.reference_id) GROUP BY 1
 ), event_units AS (
  SELECT e.event_type,e.event_id,a->>'binCode' AS bin_code,SUM((a->>'quantity')::numeric) AS quantity FROM warehouse_departure_events e CROSS JOIN LATERAL jsonb_array_elements(e.payload_json::jsonb) a GROUP BY 1,2,3
 ), released_units AS (
  SELECT r.event_type,r.event_id,p.bin_code,SUM(r.quantity) AS quantity FROM warehouse_occupancy_releases r JOIN warehouse_placements p ON p.id=r.placement_id GROUP BY 1,2,3
 ), incomplete AS (
  SELECT e.bin_code,COUNT(*) AS incomplete_departures FROM event_units e LEFT JOIN released_units r USING(event_type,event_id,bin_code) WHERE e.quantity<>COALESCE(r.quantity,0) GROUP BY e.bin_code
 ), evidence AS (
  SELECT b.bin_code,b.warehouse_id,b.capacity_status,b.quantity AS recorded_quantity,b.occupied_volume_cm3 AS recorded_volume_cm3,b.occupied_weight_grams AS recorded_weight_grams,
   COALESCE(m.quantity,0) AS measured_quantity,COALESCE(m.volume,0) AS measured_volume_cm3,COALESCE(m.weight,0) AS measured_weight_grams,
   COALESCE(h.quantity,0) AS held_balance_quantity,COALESCE(c.pending_quantity,0) AS paid_unshipped_quantity,
   COALESCE(c.partial_restore_count,0) AS partial_restore_count,COALESCE(c.historical_departures,0) AS historical_departures,
   COALESCE(r.historical_handovers,0) AS historical_handovers,COALESCE(i.incomplete_departures,0) AS incomplete_departures,
   COALESCE(x.reason,'') AS persisted_exception
  FROM warehouse_bins b LEFT JOIN measured m USING(bin_code) LEFT JOIN held h USING(bin_code) LEFT JOIN capture_totals c USING(bin_code) LEFT JOIN return_history r USING(bin_code) LEFT JOIN incomplete i USING(bin_code) LEFT JOIN warehouse_occupancy_exceptions x USING(bin_code)
 )
 SELECT *,recorded_quantity<>measured_quantity OR recorded_volume_cm3<>measured_volume_cm3 OR recorded_weight_grams<>measured_weight_grams OR measured_quantity<>held_balance_quantity+paid_unshipped_quantity OR partial_restore_count>0 OR historical_departures>0 OR historical_handovers>0 OR incomplete_departures>0 OR persisted_exception<>'' AS requires_reconciliation FROM evidence`
	if _, err := db.Exec(audit); err != nil {
		t.Fatalf("re-create warehouse_occupancy_audit view: %v", err)
	}
	const orphans = `CREATE OR REPLACE VIEW warehouse_occupancy_orphan_allocations AS
 SELECT c.order_id,c.order_item_id,c.product_id,a->>'balanceId' AS missing_balance_id,a->>'quantity' AS recorded_quantity
 FROM order_inventory_captures c CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a LEFT JOIN inventory_balances b ON b.id=a->>'balanceId' WHERE b.id IS NULL`
	if _, err := db.Exec(orphans); err != nil {
		t.Fatalf("re-create warehouse_occupancy_orphan_allocations view: %v", err)
	}
}

// TEST-02 round 4: the warehouse_customer_return_bin_sources view
// (migration 202611150002) is CASCADE-dropped whenever a fixture
// DROP TABLEs one of its base tables (products, order_items,
// customer_returns, ...). The BEFORE trigger on
// warehouse_return_bin_entries references the view, so a later
// putaway fails with 42P01 (raw error) instead of the intended
// source-conflict outcome. Recreate it verbatim from the migration.
// The trigger function survives the CASCADE drop (plpgsql bodies are
// opaque text), so no trigger re-creation is needed. Mirrors
// createReturnBinSourcesView in cmd/server/test02_view_fixture_test.go.
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
