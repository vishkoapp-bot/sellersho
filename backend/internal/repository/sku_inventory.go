package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

type skuAllocation struct {
	BalanceID string `json:"balanceId"`
	Quantity  int    `json:"quantity"`
}

func hasSKUInventoryTx(ctx context.Context, tx *sql.Tx) (bool, error) {
	var enabled bool
	err := tx.QueryRowContext(ctx, `SELECT to_regclass('public.order_inventory_captures') IS NOT NULL`).Scan(&enabled)
	return enabled, err
}

// SeedTrackedSKUInventoryTx creates only the exact declared V2 SKU balances.
// Existing NULL-variant historical balances are never allocated or divided.
func SeedTrackedSKUInventoryTx(ctx context.Context, tx *sql.Tx, productID, supplierID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id::text,stock FROM product_variants WHERE product_id=$1 AND inventory_mode='tracked' ORDER BY id`, productID)
	if err != nil {
		return err
	}
	type seed struct {
		id  string
		qty int
	}
	var seeds []seed
	for rows.Next() {
		var s seed
		if err := rows.Scan(&s.id, &s.qty); err != nil {
			rows.Close()
			return err
		}
		seeds = append(seeds, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, s := range seeds {
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled)
		 VALUES($1,$2,NULLIF($3,''),$4,'supplier','supplier',$5,'sellable',$6,TRUE)
		 ON CONFLICT(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL DO NOTHING`, "INV-"+uuid.NewString(), productID, supplierID, s.id, "supplier:"+supplierID, s.qty); err != nil {
			return err
		}
	}
	return syncProductStockTx(ctx, tx, productID)
}

// creditSKUInventoryTx records an idempotent physical receipt/restore and updates
// its exact SKU balance. The ledger trigger maintains SKU/product projections.
func creditSKUInventoryTx(ctx context.Context, tx *sql.Tx, productID, supplierID, variantID string, quantity int, status, locationType, location, referenceType, referenceID, key, actor string) error {
	if quantity <= 0 {
		return nil
	}
	if err := lockSKUProductTx(ctx, tx, productID, variantID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata)
	 VALUES($1,$2,NULLIF($3,''),NULLIF($4,'')::uuid,$5,'external',$6,'external',$7,$8,$9,$10,NULLIF($11,''),'{}') ON CONFLICT(idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), productID, supplierID, variantID, quantity, status, location, referenceType, referenceID, key, actor)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled)
	 VALUES($1,$2,NULLIF($3,''),$4,'supplier',$5,$6,$7,$8,TRUE)
	 ON CONFLICT(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL
	 DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`, "INV-"+uuid.NewString(), productID, supplierID, variantID, locationType, location, status, quantity)
	return err
}

// ReceiveTrackedSKUShipmentItemTx handles the SKU path; false leaves a legacy
// item to its existing receipt implementation. Cumulative received counts may
// increase, but cannot be rewritten after they have entered the physical ledger.
func ReceiveTrackedSKUShipmentItemTx(ctx context.Context, tx *sql.Tx, shipmentID, variantID string, receivedQty int, qcStatus string) (bool, error) {
	var productID, supplierID, mode string
	if err := tx.QueryRowContext(ctx, `SELECT pv.product_id,COALESCE(p.supplier_id,''),COALESCE(to_jsonb(pv)->>'inventory_mode','legacy') FROM product_variants pv JOIN products p ON p.id=pv.product_id WHERE pv.id=$1`, variantID).Scan(&productID, &supplierID, &mode); err != nil {
		return false, err
	}
	if mode != "tracked" {
		return false, nil
	}
	if err := lockSKUProductTx(ctx, tx, productID, variantID); err != nil {
		return true, err
	}
	var lineCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM shipment_items WHERE shipment_id=$1 AND variant_id=$2`, shipmentID, variantID).Scan(&lineCount); err != nil {
		return true, err
	}
	if lineCount != 1 {
		return true, fmt.Errorf("shipment must contain exactly one line per tracked SKU")
	}
	var expected, accounted int
	var oldQC string
	if err := tx.QueryRowContext(ctx, `SELECT expected_qty,inventory_received_qty,qc_status FROM shipment_items WHERE shipment_id=$1 AND variant_id=$2 FOR UPDATE`, shipmentID, variantID).Scan(&expected, &accounted, &oldQC); err != nil {
		return true, err
	}
	if receivedQty < accounted || receivedQty > expected || (accounted > 0 && qcStatus != oldQC) {
		return true, fmt.Errorf("received SKU quantity/QC cannot rewrite accounted inventory")
	}
	if qcStatus != "passed" && qcStatus != "failed" {
		return true, fmt.Errorf("invalid QC status")
	}
	delta := receivedQty - accounted
	if delta > 0 {
		// Transfer already-declared supplier units first. Additional physically
		// received units are new stock, never taken from another SKU/legacy pool.
		var source string
		var available int
		err := tx.QueryRowContext(ctx, `SELECT id,quantity FROM inventory_balances WHERE product_id=$1 AND variant_id=$2 AND location_type='supplier' AND location_key=$3 AND status='sellable' FOR UPDATE`, productID, variantID, "supplier:"+supplierID).Scan(&source, &available)
		if err != nil && err != sql.ErrNoRows {
			return true, err
		}
		debit := delta
		if debit > available {
			debit = available
		}
		// Accepted receipts remain physical staging until the complete
		// putaway task places this exact SKU into a bin. Payment must never
		// consume units promised to an outstanding putaway task.
		status := "physical"
		if qcStatus == "failed" {
			status = "quarantine"
		}
		key := fmt.Sprintf("sku-receipt:%s:%s:%d", shipmentID, variantID, receivedQty)
		if debit > 0 {
			if err := moveSKUInventoryTx(ctx, tx, productID, supplierID, variantID, debit, "sellable", status, "supplier:"+supplierID, "warehouse", "shipment:"+shipmentID, "sku_receipt", shipmentID, key+":transfer", ""); err != nil {
				return true, err
			}
		}
		if delta > debit {
			if err := creditSKUInventoryTx(ctx, tx, productID, supplierID, variantID, delta-debit, status, "warehouse", "shipment:"+shipmentID, "sku_receipt", shipmentID, key+":new", ""); err != nil {
				return true, err
			}
		}
		if qcStatus == "passed" {
			_, err := tx.ExecContext(ctx, `INSERT INTO putaway_tasks(id,product_id,variant_id,inventory_source_location_key,quantity,status,created_at) VALUES($1,$2,$3,$4,$5,'pending',NOW())`, "PT-"+uuid.NewString(), productID, variantID, "shipment:"+shipmentID, delta)
			if err != nil {
				return true, err
			}
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE shipment_items SET received_qty=$3,inventory_received_qty=$3,qc_status=$4 WHERE shipment_id=$1 AND variant_id=$2`, shipmentID, variantID, receivedQty, qcStatus)
	return true, err
}

func putawayTrackedSKUInventoryTx(ctx context.Context, tx *sql.Tx, productID, supplierID, variantID string, quantity int, sourceLocation, binCode, taskID, actor string) error {
	return moveSKUInventoryTx(ctx, tx, productID, supplierID, variantID, quantity, "physical", "sellable", sourceLocation, "bin", "bin:"+binCode, "putaway_confirmed", taskID, "putaway:"+taskID, actor)
}

func moveSKUInventoryTx(ctx context.Context, tx *sql.Tx, productID, supplierID, variantID string, qty int, fromStatus, toStatus, source, locationType, destination, referenceType, referenceID, key, actor string) error {
	if qty <= 0 {
		return nil
	}
	if err := lockSKUProductTx(ctx, tx, productID, variantID); err != nil {
		return err
	}
	var balanceID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM inventory_balances WHERE product_id=$1 AND variant_id=$2 AND location_key=$3 AND status=$4 FOR UPDATE`, productID, variantID, source, fromStatus).Scan(&balanceID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,''),'{}') ON CONFLICT(idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), productID, supplierID, variantID, qty, fromStatus, toStatus, source, destination, referenceType, referenceID, key, actor)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	res, err = tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$2,updated_at=NOW() WHERE id=$1 AND quantity >=$2`, balanceID, qty)
	if err != nil {
		return err
	}
	n, err = res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInsufficientInventory
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled,bin_code) VALUES($1,$2,NULLIF($3,''),$4,'supplier',$5::text,$6::text,$7,$8,TRUE,CASE WHEN $5::text='bin' THEN substring($6::text FROM 5) END) ON CONFLICT(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`, "INV-"+uuid.NewString(), productID, supplierID, variantID, locationType, destination, toStatus, qty)
	return err
}

func lockSKUProductTx(ctx context.Context, tx *sql.Tx, productID, variantID string) error {
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM products WHERE id=$1 FOR UPDATE`, productID).Scan(&locked); err != nil {
		return err
	}
	return tx.QueryRowContext(ctx, `SELECT id::text FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, variantID, productID).Scan(&locked)
}

// restoreCapturedReturnTx accounts at most the quantity actually captured for
// the order line. A return lot has exact SKU identity without inventing its old
// location; historical orders stay explicitly unallocated in the legacy path.
func restoreCapturedReturnTx(ctx context.Context, tx *sql.Tx, returnItemID, orderItemID, productID, supplierID string, qty int, qcStatus, destination, actor string) (bool, error) {
	enabled, err := hasSKUInventoryTx(ctx, tx)
	if err != nil || !enabled {
		return enabled, err
	}
	var variantID, mode string
	var captured, restored, vqty int
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(variant_id::text,''),inventory_mode,quantity,restored_quantity,variant_quantity FROM order_inventory_captures WHERE order_item_id=$1 FOR UPDATE`, orderItemID).Scan(&variantID, &mode, &captured, &restored, &vqty)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_inventory_returns WHERE return_item_id=$1)`, returnItemID).Scan(&exists); err != nil {
		return true, err
	}
	if exists {
		return true, nil
	}
	if qty <= 0 || qty > captured-restored {
		return true, fmt.Errorf("return quantity exceeds captured inventory")
	}
	if mode != "tracked" { // The existing product-only return path remains authoritative.
		if _, err := tx.ExecContext(ctx, `INSERT INTO order_inventory_returns(return_item_id,order_item_id,quantity) VALUES($1,$2,$3)`, returnItemID, orderItemID, qty); err != nil {
			return true, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE order_inventory_captures SET restored_quantity=restored_quantity+$2 WHERE order_item_id=$1`, orderItemID, qty); err != nil {
			return true, err
		}
		// Restore only SKU units this new capture actually debited. Old
		// untracked zero-stock SKUs must never gain units from a return.
		if qcStatus == "ok" && vqty > 0 && variantID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET stock=stock+$2 WHERE id=$1`, variantID, qty); err != nil {
				return true, err
			}
		}
		return false, nil
	}
	status := "sellable"
	if qcStatus == "defective" {
		status = "quarantine"
	}
	if destination == "" {
		destination = "customer-return"
	}
	if err := creditSKUInventoryTx(ctx, tx, productID, supplierID, variantID, qty, status, "warehouse", destination+":return:"+returnItemID, "customer_return", returnItemID, "sku-return:"+returnItemID, actor); err != nil {
		return true, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO order_inventory_returns(return_item_id,order_item_id,quantity) VALUES($1,$2,$3)`, returnItemID, orderItemID, qty); err != nil {
		return true, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE order_inventory_captures SET restored_quantity=restored_quantity+$2 WHERE order_item_id=$1`, orderItemID, qty)
	return true, err
}

type inventoryCaptureItem struct {
	ID, ProductID, VariantID string
	Quantity                 int
}

// captureOrderInventoryTx locks products/SKUs in stable order and records exact
// allocations for every newly paid line. Returning false is a schema-only
// compatibility path for old test/restore databases, never a V2 stock fallback.
func captureOrderInventoryTx(ctx context.Context, tx *sql.Tx, orderID string) (bool, error) {
	enabled, err := hasSKUInventoryTx(ctx, tx)
	if err != nil || !enabled {
		return enabled, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,product_id,COALESCE(variant_id::text,''),quantity FROM order_items WHERE order_id=$1 ORDER BY product_id,variant_id,id`, orderID)
	if err != nil {
		return true, err
	}
	var items []inventoryCaptureItem
	for rows.Next() {
		var it inventoryCaptureItem
		if err := rows.Scan(&it.ID, &it.ProductID, &it.VariantID, &it.Quantity); err != nil {
			rows.Close()
			return true, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return true, err
	}
	rows.Close()
	// Validate cumulative SKU limits, including separate addon lines.
	totals := map[string]int{}
	for _, it := range items {
		key := it.ProductID + "/" + it.VariantID
		if it.Quantity <= 0 || totals[key] > int(^uint(0)>>1)-it.Quantity {
			return true, ErrInsufficientInventory
		}
		totals[key] += it.Quantity
	}
	validated := map[string]bool{}
	for _, it := range items {
		var stock, version int
		var typ, supplierID string
		if err := tx.QueryRowContext(ctx, `SELECT stock,schema_version,product_type,COALESCE(supplier_id,'') FROM products WHERE id=$1 FOR UPDATE`, it.ProductID).Scan(&stock, &version, &typ, &supplierID); err != nil {
			return true, err
		}
		if err := ValidateProductSKUSelection(version, typ, it.VariantID); err != nil {
			return true, err
		}
		mode := "legacy"
		variantStock, minimum := 0, 1
		var maximum sql.NullInt32
		if it.VariantID != "" {
			var active bool
			if err := tx.QueryRowContext(ctx, `SELECT inventory_mode,is_active,stock,COALESCE(min_cart_qty,1),max_cart_qty FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, it.VariantID, it.ProductID).Scan(&mode, &active, &variantStock, &minimum, &maximum); err != nil {
				return true, err
			}
			key := it.ProductID + "/" + it.VariantID
			if version >= 2 && mode != "tracked" {
				return true, fmt.Errorf("typed products require tracked SKU inventory")
			}
			if !validated[key] {
				var max *int
				if maximum.Valid {
					n := int(maximum.Int32)
					max = &n
				}
				if err := ValidateSKUQuantity(mode, active, variantStock, minimum, max, totals[key]); err != nil {
					return true, err
				}
				validated[key] = true
			}
		}
		allocations, err := consumeInventoryBalancesTx(ctx, tx, it, mode)
		if err != nil {
			return true, err
		}
		variantQty := 0
		if mode == "tracked" {
			variantQty = it.Quantity
		} else {
			// Legacy aggregate inventory is never guessed into SKU balances.
			if len(allocations) == 0 {
				res, err := tx.ExecContext(ctx, `UPDATE products SET stock=stock-$2 WHERE id=$1 AND stock >=$2`, it.ProductID, it.Quantity)
				if err != nil {
					return true, err
				}
				n, _ := res.RowsAffected()
				if n != 1 {
					return true, ErrInsufficientInventory
				}
			}
			if it.VariantID != "" && variantStock > 0 {
				res, err := tx.ExecContext(ctx, `UPDATE product_variants SET stock=stock-$2 WHERE id=$1 AND stock >=$2`, it.VariantID, it.Quantity)
				if err != nil {
					return true, err
				}
				n, _ := res.RowsAffected()
				if n != 1 {
					return true, ErrInsufficientInventory
				}
				variantQty = it.Quantity
			}
		}
		encoded, _ := json.Marshal(allocations)
		if _, err := tx.ExecContext(ctx, `INSERT INTO order_inventory_captures(order_item_id,order_id,product_id,variant_id,quantity,variant_quantity,inventory_mode,allocations) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8)`, it.ID, orderID, it.ProductID, it.VariantID, it.Quantity, variantQty, mode, string(encoded)); err != nil {
			return true, err
		}
	}
	return true, nil
}

func consumeInventoryBalancesTx(ctx context.Context, tx *sql.Tx, it inventoryCaptureItem, mode string) ([]skuAllocation, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,quantity FROM inventory_balances WHERE product_id=$1 AND status='sellable' AND (($3='tracked' AND variant_id=NULLIF($2,'')::uuid) OR ($3<>'tracked' AND variant_id IS NULL)) ORDER BY id FOR UPDATE`, it.ProductID, it.VariantID, mode)
	if err != nil {
		return nil, err
	}
	var available []skuAllocation
	total := 0
	for rows.Next() {
		var a skuAllocation
		if err := rows.Scan(&a.BalanceID, &a.Quantity); err != nil {
			rows.Close()
			return nil, err
		}
		available = append(available, a)
		total += a.Quantity
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(available) == 0 && mode != "tracked" {
		return []skuAllocation{}, nil
	}
	if total < it.Quantity {
		return nil, ErrInsufficientInventory
	}
	remaining := it.Quantity
	var taken []skuAllocation
	for _, a := range available {
		if remaining == 0 {
			break
		}
		if a.Quantity > remaining {
			a.Quantity = remaining
		}
		if a.Quantity == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$2,updated_at=NOW() WHERE id=$1`, a.BalanceID, a.Quantity); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,metadata)
		 SELECT $1,product_id,supplier_id,variant_id,$2,'sellable','sold',location_key,'customer','order_capture',$3,$4,'{}' FROM inventory_balances WHERE id=$5`, "IM-"+uuid.NewString(), a.Quantity, it.ID, "sku-capture:"+it.ID+":"+a.BalanceID, a.BalanceID); err != nil {
			return nil, err
		}
		taken = append(taken, a)
		remaining -= a.Quantity
	}
	return taken, nil
}

func restoreOrderInventoryTx(ctx context.Context, tx *sql.Tx, orderID string) (bool, error) {
	enabled, err := hasSKUInventoryTx(ctx, tx)
	if err != nil || !enabled {
		return enabled, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT order_item_id,product_id,COALESCE(variant_id::text,''),quantity,variant_quantity,restored_quantity,inventory_mode,allocations FROM order_inventory_captures WHERE order_id=$1 ORDER BY product_id,variant_id,order_item_id FOR UPDATE`, orderID)
	if err != nil {
		return true, err
	}
	type captured struct {
		id, pid, vid, mode  string
		qty, vqty, restored int
		raw                 []byte
	}
	var captures []captured
	for rows.Next() {
		var c captured
		if err := rows.Scan(&c.id, &c.pid, &c.vid, &c.qty, &c.vqty, &c.restored, &c.mode, &c.raw); err != nil {
			rows.Close()
			return true, err
		}
		captures = append(captures, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return true, err
	}
	rows.Close()
	// Historical captures did not record successful SKU allocation. Keep their
	// established aggregate restore, but do not manufacture a SKU allocation.
	if len(captures) == 0 {
		return false, nil
	}
	for _, c := range captures {
		if c.restored == c.qty {
			continue
		}
		if c.restored != 0 {
			return true, fmt.Errorf("partially returned order cannot be cancelled")
		}
		var allocations []skuAllocation
		if err := json.Unmarshal(c.raw, &allocations); err != nil {
			return true, err
		}
		sort.Slice(allocations, func(i, j int) bool { return allocations[i].BalanceID < allocations[j].BalanceID })
		if _, err := tx.ExecContext(ctx, `SELECT id FROM products WHERE id=$1 FOR UPDATE`, c.pid); err != nil {
			return true, err
		}
		if len(allocations) == 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE products SET stock=stock+$2 WHERE id=$1`, c.pid, c.qty); err != nil {
				return true, err
			}
		}
		for _, a := range allocations {
			movement, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,metadata)
			 SELECT $1,product_id,supplier_id,variant_id,$2,'sold','sellable','customer',location_key,'order_cancel',$3,$4,'{}' FROM inventory_balances WHERE id=$5 ON CONFLICT(idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), a.Quantity, c.id, "sku-cancel:"+c.id+":"+a.BalanceID, a.BalanceID)
			if err != nil {
				return true, err
			}
			n, err := movement.RowsAffected()
			if err != nil {
				return true, err
			}
			if n != 1 {
				return true, fmt.Errorf("recorded inventory allocation is missing or already restored")
			}
			res, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity+$2,updated_at=NOW() WHERE id=$1`, a.BalanceID, a.Quantity)
			if err != nil {
				return true, err
			}
			n, err = res.RowsAffected()
			if err != nil {
				return true, err
			}
			if n != 1 {
				return true, fmt.Errorf("recorded inventory allocation is missing")
			}
		}
		if c.mode != "tracked" && c.vqty > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET stock=stock+$2 WHERE id=$1`, c.vid, c.vqty); err != nil {
				return true, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE order_inventory_captures SET restored_quantity=quantity WHERE order_item_id=$1`, c.id); err != nil {
			return true, err
		}
	}
	return true, nil
}
