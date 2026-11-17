package repository

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
)

// These fixtures record explicit physical stock and checkout provenance, using
// the current migrated constraints rather than products.stock as inventory.
func seedCanonicalWarehouseStock(t *testing.T, db *sql.DB, product, supplier string, quantity int) string {
	t.Helper()
	warehouse := "fixture-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'Test province','Test city')`, warehouse); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,warehouse_id) VALUES($1,$2,$3,'supplier','warehouse',$4,'sellable',$5,$6)`, uuid.NewString(), product, supplier, "warehouse:"+warehouse, quantity, warehouse); err != nil {
		t.Fatal(err)
	}
	return warehouse
}

func seedCanonicalWarehouseOrderOrigins(t *testing.T, db *sql.DB, order, warehouse string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,warehouse_id,origin_label,quantity)
		SELECT 'fixture-origin-'||oi.id,oi.order_id,oi.id,0,'warehouse',p.supplier_id,$2,'Test warehouse',oi.quantity
		FROM order_items oi JOIN products p ON p.id=oi.product_id WHERE oi.order_id=$1`, order, warehouse); err != nil {
		t.Fatal(err)
	}
}
