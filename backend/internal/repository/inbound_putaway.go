package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// Caller preholds the receipt's supplier, product/SKU and complete balance plan.
// One positive acceptance delta creates one immutable task and isolated source;
// later receipts cannot resize that task or supply its missing units.
func createInboundPutawayTx(ctx context.Context, tx *sql.Tx, l inboundPersistedLine, disposition string, quantity int) error {
	task, source := "PUT-"+uuid.NewString(), "qc-accepted:"+disposition
	if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,warehouse_id,reconciled) VALUES($1,$2,$3,NULLIF($4,'')::uuid,'supplier','warehouse',$5,'qc_accepted',$6,$7,TRUE)`, "INV-"+uuid.NewString(), l.Product, l.Supplier, l.Variant, source, quantity, l.Warehouse); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO putaway_tasks(id,product_id,variant_id,inbound_id,warehouse_id,inventory_source_location_key,quantity,status,created_at) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,'pending',NOW())`, task, l.Product, l.Variant, l.Shipment, l.Warehouse, source, quantity); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO inbound_putaway_allocations(task_id,disposition_id,receipt_line_id,shipment_id,supplier_id,warehouse_id,product_id,variant_id,source_location_key,quantity) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, task, disposition, l.ID, l.Shipment, l.Supplier, l.Warehouse, l.Product, l.Variant, source, quantity)
	return err
}

// Read-only discovery followed by the global supplier/product/SKU/work prefix.
// Parent identity is re-read after acquisition before any task or bin mutation.
func lockPutawayParentsTx(ctx context.Context, tx *sql.Tx, task string) error {
	var product, supplier, variant string
	if err := tx.QueryRowContext(ctx, `SELECT pt.product_id,COALESCE(p.supplier_id,''),COALESCE(pt.variant_id::text,'') FROM putaway_tasks pt JOIN products p ON p.id=pt.product_id WHERE pt.id=$1`, task).Scan(&product, &supplier, &variant); err != nil {
		return err
	}
	locks := []TransactionRowLock{{Table: "products", ID: product}, {Table: "putaway_tasks", ID: task}}
	if supplier != "" {
		locks = append(locks, TransactionRowLock{Table: "suppliers", ID: supplier})
	}
	if variant != "" {
		locks = append(locks, TransactionRowLock{Table: "product_variants", ID: variant})
	}
	var shipment string
	var asn, carton bool
	err := tx.QueryRowContext(ctx, `SELECT a.shipment_id,EXISTS(SELECT 1 FROM inbound_shipments i WHERE i.id=a.shipment_id),EXISTS(SELECT 1 FROM shipments s WHERE s.id::text=a.shipment_id) FROM inbound_putaway_allocations a WHERE a.task_id=$1`, task).Scan(&shipment, &asn, &carton)
	if err == nil {
		if asn == carton {
			return ErrInboundProvenance
		}
		table := "shipments"
		if asn {
			table = "inbound_shipments"
		}
		locks = append(locks, TransactionRowLock{Table: table, ID: shipment})
	} else if err != sql.ErrNoRows {
		return err
	}
	if err := LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, locks...); err != nil {
		return err
	}
	var currentProduct, currentSupplier, currentVariant string
	if err := tx.QueryRowContext(ctx, `SELECT pt.product_id,COALESCE(p.supplier_id,''),COALESCE(pt.variant_id::text,'') FROM putaway_tasks pt JOIN products p ON p.id=pt.product_id WHERE pt.id=$1`, task).Scan(&currentProduct, &currentSupplier, &currentVariant); err != nil {
		return err
	}
	if currentProduct != product || currentSupplier != supplier || currentVariant != variant {
		return ErrInboundProvenance
	}
	return nil
}

type exactPutaway struct {
	product, supplier, variant, warehouse, inbound, source string
	quantity                                               int
}

func loadExactPutawayTx(ctx context.Context, tx *sql.Tx, task string) (exactPutaway, error) {
	var a exactPutaway
	err := tx.QueryRowContext(ctx, `SELECT a.product_id,a.supplier_id,a.variant_id,a.warehouse_id,a.shipment_id,a.source_location_key,a.quantity FROM inbound_putaway_allocations a JOIN putaway_tasks pt ON pt.id=a.task_id JOIN products p ON p.id=a.product_id AND p.supplier_id=a.supplier_id WHERE a.task_id=$1 AND pt.product_id=a.product_id AND COALESCE(pt.variant_id::text,'')=a.variant_id AND pt.inbound_id=a.shipment_id AND pt.warehouse_id=a.warehouse_id AND pt.inventory_source_location_key=a.source_location_key AND pt.quantity=a.quantity`, task).Scan(&a.product, &a.supplier, &a.variant, &a.warehouse, &a.inbound, &a.source, &a.quantity)
	if err == sql.ErrNoRows {
		return a, ErrInboundProvenance
	}
	return a, err
}

// Parents and task are already held. Balances precede bin, capacity reservation
// and storage positions, including zero source rows needed for replay checks.
func lockPutawayPhysicalTx(ctx context.Context, tx *sql.Tx, task, bin string, a exactPutaway) error {
	var locks []TransactionRowLock
	for _, query := range []struct {
		table, sql string
		args       []interface{}
	}{
		{"inventory_balances", `SELECT id FROM inventory_balances WHERE product_id=$1`, []interface{}{a.product}},
		{"putaway_capacity_reservations", `SELECT id FROM putaway_capacity_reservations WHERE task_id=$1`, []interface{}{task}},
		{"warehouse_storage_positions", `SELECT id FROM warehouse_storage_positions WHERE product_id=$1 AND supplier_id=$2 AND warehouse_id=$3 AND closed_at IS NULL`, []interface{}{a.product, a.supplier, a.warehouse}},
	} {
		rows, err := tx.QueryContext(ctx, query.sql, query.args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			locks = append(locks, TransactionRowLock{Table: query.table, ID: id})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	locks = append(locks, TransactionRowLock{Table: "warehouse_bins", ID: bin})
	return LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, locks...)
}

func consumeExactPutawayTx(ctx context.Context, tx *sql.Tx, task, bin, actor string, a exactPutaway) error {
	var warehouse string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(warehouse_id,'') FROM warehouse_bins WHERE bin_code=$1`, bin).Scan(&warehouse); err != nil {
		return err
	}
	if warehouse != a.warehouse {
		return ErrInboundProvenance
	}
	var conflictOrigin bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_balances WHERE product_id=$1 AND COALESCE(variant_id::text,'')=$2 AND location_key=$3 AND status='sellable' AND (supplier_id IS DISTINCT FROM $4 OR warehouse_id IS DISTINCT FROM $5 OR location_type<>'bin' OR owner_type<>'supplier'))`, a.product, a.variant, "bin:"+bin, a.supplier, a.warehouse).Scan(&conflictOrigin); err != nil {
		return err
	}
	if conflictOrigin {
		return ErrInboundProvenance
	}
	res, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=0,updated_at=NOW() WHERE product_id=$1 AND supplier_id=$2 AND COALESCE(variant_id::text,'')=$3 AND warehouse_id=$4 AND location_key=$5 AND status='qc_accepted' AND owner_type='supplier' AND location_type='warehouse' AND quantity=$6`, a.product, a.supplier, a.variant, a.warehouse, a.source, a.quantity)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInboundProvenance
	}
	conflict := `(product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL`
	if a.variant != "" {
		conflict = `(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL`
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,warehouse_id,reconciled) VALUES($1,$2,$3,NULLIF($4,'')::uuid,'supplier','bin',$5,'sellable',$6,$7,TRUE) ON CONFLICT `+conflict+` DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`, "INV-"+uuid.NewString(), a.product, a.supplier, a.variant, "bin:"+bin, a.quantity, a.warehouse); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,'qc_accepted','sellable',$6,$7,'putaway_confirmed',$8,$9,NULLIF($10,''),'{}')`, "IM-"+uuid.NewString(), a.product, a.supplier, a.variant, a.quantity, a.source, "bin:"+bin, task, "putaway:"+task, actor); err != nil {
		return fmt.Errorf("exact putaway movement: %w", err)
	}
	// Storage's legacy inbound_id FK names only ASN headers. Carton identity is
	// retained by the immutable task allocation; never insert a carton UUID as ASN.
	var storageInbound string
	err = tx.QueryRowContext(ctx, `SELECT id FROM inbound_shipments WHERE id=$1`, a.inbound).Scan(&storageInbound)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err = creditStoragePositionTx(ctx, tx, a.product, a.supplier, a.warehouse, storageInbound, a.quantity); err != nil {
		return err
	}
	return syncProductStockTx(ctx, tx, a.product)
}
