package main

import (
	"database/sql"
	"testing"
)

func seedAndVerifyHotSKU(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
INSERT INTO products(id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,length_mm,width_mm,height_mm,weight_grams,measurement_status,fulfillment_method,origin_supplier_warehouse_id)
VALUES('cap-hot-product','Capacity hot SKU','cap-hot-product','cap-category','cap-supplier-100',120000,100000,10000,100,100,100,100,'verified','supplier_direct','cap-swh-100');
INSERT INTO store_products(store_id,product_id) VALUES('cap-store-100','cap-hot-product');
INSERT INTO product_variants(id,product_id,spnc,variant_name,stock,is_default) VALUES(md5('cap-hot-sku')::uuid,'cap-hot-product','CAP-HOT-SKU','cap-hot-sku',5,true);
INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled,supplier_warehouse_id)
VALUES('cap-hot-balance','cap-hot-product','cap-supplier-100','supplier','supplier','supplier-warehouse:cap-swh-100','sellable',5,true,'cap-swh-100');
INSERT INTO inventory_movements(id,product_id,supplier_id,quantity,to_status,to_location_key,reference_type,reference_id,idempotency_key,metadata)
VALUES('cap-hot-receipt','cap-hot-product','cap-supplier-100',5,'sellable','supplier-warehouse:cap-swh-100','capacity_hot_receipt','cap-hot-receipt','cap-hot-receipt','{"synthetic":true}');
`); err != nil {
		t.Fatal(err)
	}
	var stock int
	if err := db.QueryRow(`SELECT COALESCE(sum(quantity),0) FROM inventory_balances WHERE product_id='cap-hot-product' AND status='sellable'`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock != 5 {
		t.Fatalf("hot SKU fixture stock=%d want5", stock)
	}
}
