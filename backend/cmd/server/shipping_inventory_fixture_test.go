package main

import (
	"context"
	"database/sql"
	"github.com/drup/platform/internal/repository"
	"strings"
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
	var lot string
	if err := db.QueryRow(`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,warehouse_id,supplier_warehouse_id)
		VALUES($1,$2,$3,'supplier',$4,$5,'sellable',$6,NULLIF($7,''),NULLIF($8,'')) ON CONFLICT (product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL DO UPDATE SET quantity=EXCLUDED.quantity RETURNING id`, uuid.NewString(), product, supplier, kind, key, quantity, warehouse, supplierWarehouse).Scan(&lot); err != nil {
		t.Fatal(err)
	}
	var variant, spnc string
	err := db.QueryRow(`SELECT id::text,spnc FROM product_variants WHERE product_id=$1 AND is_default`, product).Scan(&variant, &spnc)
	if err == sql.ErrNoRows {
		variant = uuid.NewString()
		spnc = "T" + strings.ToUpper(strings.ReplaceAll(variant, "-", "")[:10])
		_, err = db.Exec(`INSERT INTO product_variants(id,product_id,spnc,variant_name,is_default,stock) VALUES($1,$2,$3,'پیش‌فرض',TRUE,$4)`, variant, product, spnc, quantity)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM product_units WHERE balance_id=$1`, lot); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO product_units(code,variant_id,product_id,sequence,status,balance_id)
		SELECT $1::text || '-X' || LPAD(i::text,4,'0'),$2,$3,i,'in_stock',$4 FROM generate_series(1,$5::int) i`, spnc, variant, product, lot, quantity); err != nil {
		t.Fatal(err)
	}
}

// seedCapacityCheckoutCodes adds the five sellable codes needed by the FIN HTTP
// tests without changing the benchmark dataset or their finance assertions.
func seedCapacityCheckoutCodes(t *testing.T, db *sql.DB, variant string) {
	t.Helper()
	const product, lot, origin = "cap-product-1", "cap-balance-1", "cap-swh-2"
	if _, err := db.Exec(`INSERT INTO product_units(code,variant_id,product_id,sequence,status,balance_id)
 SELECT v.spnc || '-X' || LPAD(i::text,4,'0'), v.id, v.product_id, i, 'in_stock', b.id
 FROM product_variants v JOIN inventory_balances b ON b.product_id=v.product_id
 CROSS JOIN generate_series(1,5) i
 WHERE v.id=$1 AND v.product_id=$2 AND v.is_active AND v.inventory_mode='legacy'
 AND b.id=$3 AND b.variant_id IS NULL AND b.status='sellable' AND b.quantity>=5
 AND b.supplier_warehouse_id=$4
 ON CONFLICT (variant_id,sequence) DO NOTHING`, variant, product, lot, origin); err != nil {
		t.Fatal(err)
	}
	routes, err := repository.ResolveInventoryOrigins(context.Background(), db, product, variant, "supplier_direct", 5)
	if err != nil || len(routes) != 1 || routes[0].SupplierWarehouseID != origin || routes[0].Quantity != 5 {
		t.Fatalf("capacity checkout fixture needs five eligible coded units at %s: origins=%+v err=%v", origin, routes, err)
	}
}
