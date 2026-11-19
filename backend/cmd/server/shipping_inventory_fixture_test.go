package main

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
)

// Minimal shipping DTO fixtures retain the canonical inventory ledger and its
// triggers. Supply the product fields those triggers require before seeding lots.
func prepareShippingInventoryFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`ALTER TABLE products ADD COLUMN IF NOT EXISTS schema_version INT DEFAULT 0, ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW()`,
		`ALTER TABLE product_variants ADD COLUMN IF NOT EXISTS inventory_mode TEXT DEFAULT 'legacy', ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT TRUE`,
		`ALTER TABLE order_items ADD COLUMN IF NOT EXISTS pricing_policy_version TEXT DEFAULT 'legacy-unversioned'`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

func seedShippingInventoryFixtureLot(t *testing.T, db *sql.DB, product, supplier, warehouse, supplierWarehouse string, quantity int) {
	t.Helper()
	if (warehouse == "") == (supplierWarehouse == "") || quantity <= 0 {
		t.Fatal("shipping fixture requires one exact positive physical lot")
	}
	kind, key := "warehouse", "warehouse:"+warehouse
	if supplierWarehouse != "" {
		kind, key = "supplier", "supplier-warehouse:"+supplierWarehouse
	}
	// TEST-02 round 4: the seed is NOT idempotent — gap13Setup runs once
	// per test against the shared disposable DB, and inventory_balances is
	// NOT in its DROP list, so the second caller re-inserts the same lot
	// and hits the partial unique index
	// inventory_balances_unallocated_unique
	// (product_id, owner_type, location_type, location_key, status)
	// WHERE variant_id IS NULL (migration 202611110003). Resetting the
	// lot to the seeded quantity restores the fresh-world-per-test
	// semantics the fixture promises.
	if _, err := db.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,warehouse_id,supplier_warehouse_id)
		VALUES($1,$2,$3,'supplier',$4,$5,'sellable',$6,NULLIF($7,''),NULLIF($8,'')) ON CONFLICT (product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL DO UPDATE SET quantity=EXCLUDED.quantity`, uuid.NewString(), product, supplier, kind, key, quantity, warehouse, supplierWarehouse); err != nil {
		t.Fatal(err)
	}
}
