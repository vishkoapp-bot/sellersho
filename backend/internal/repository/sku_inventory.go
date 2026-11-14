package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type skuAllocation struct {
	OriginID  string `json:"originId,omitempty"`
	BalanceID string `json:"balanceId"`
	Quantity  int    `json:"quantity"`
}

func hasSKUInventoryTx(ctx context.Context, tx *sql.Tx) (bool, error) {
	var enabled bool
	err := tx.QueryRowContext(ctx, `SELECT to_regclass('order_inventory_captures') IS NOT NULL`).Scan(&enabled)
	return enabled, err
}

// SeedTrackedSKUInventoryTx creates only the exact declared V2 SKU balances.
// Existing NULL-variant historical balances are never allocated or divided.
func SeedTrackedSKUInventoryTx(ctx context.Context, tx *sql.Tx, productID, supplierID string) error {
	location, err := supplierDeclaredLocationTx(ctx, tx, productID, supplierID)
	if err != nil {
		return err
	}
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
		 ON CONFLICT(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL DO NOTHING`, "INV-"+uuid.NewString(), productID, supplierID, s.id, location, s.qty); err != nil {
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
	var line string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM inbound_inventory_lines WHERE shipment_id=$1 AND variant_id=$2`, shipmentID, variantID).Scan(&line); err != nil {
		return true, ErrInboundProvenance
	}
	if qcStatus != "passed" && qcStatus != "failed" {
		return true, ErrInboundProvenance
	}
	if err := receiveInboundLineTx(ctx, tx, line, receivedQty, ""); err != nil {
		return true, err
	}
	_, err := tx.ExecContext(ctx, `UPDATE shipment_items SET received_qty=$3,inventory_received_qty=$3,qc_status=$4 WHERE shipment_id=$1 AND variant_id=$2`, shipmentID, variantID, receivedQty, qcStatus)
	return true, err
}

func moveSKUInventoryTx(ctx context.Context, tx *sql.Tx, productID, supplierID, variantID string, qty int, fromStatus, toStatus, source, locationType, destination, referenceType, referenceID, key, actor string) error {
	if qty <= 0 {
		return nil
	}
	if err := lockSKUProductTx(ctx, tx, productID, variantID); err != nil {
		return err
	}
	if locationType == "bin" {
		var matches bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_balances s JOIN warehouse_bins b ON b.warehouse_id=s.warehouse_id WHERE s.product_id=$1 AND s.variant_id=$2 AND s.location_key=$3 AND s.status=$4 AND b.bin_code=substring($5 FROM 5) AND s.warehouse_id IS NOT NULL)`, productID, variantID, source, fromStatus, destination).Scan(&matches); err != nil {
			return err
		}
		if !matches {
			return fmt.Errorf("putaway requires matching immutable warehouse identities")
		}
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
	var returnVariant string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(variant_id::text,'') FROM order_inventory_captures WHERE order_item_id=$1`, orderItemID).Scan(&returnVariant)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if err := lockInventoryParentsTx(ctx, tx, []inventoryCaptureItem{{ProductID: productID, VariantID: returnVariant}}); err != nil {
		return true, err
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
	locationType := "warehouse"
	if strings.HasPrefix(destination, "supplier_warehouse:") {
		locationType = "supplier"
	}
	if err := creditSKUInventoryTx(ctx, tx, productID, supplierID, variantID, qty, status, locationType, destination+":return:"+returnItemID, "customer_return", returnItemID, "sku-return:"+returnItemID, actor); err != nil {
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
	if err := lockInventoryParentsTx(ctx, tx, items); err != nil {
		return true, err
	}
	// Captures are immutable business events; replay cannot debit again.
	var capturedCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_inventory_captures WHERE order_id=$1`, orderID).Scan(&capturedCount); err != nil {
		return true, err
	}
	if capturedCount > 0 {
		if capturedCount != len(items) {
			return true, fmt.Errorf("incomplete inventory capture")
		}
		return true, nil
	}
	// Validate cumulative SKU limits, including separate addon lines.
	totals := map[string]int{}
	for _, it := range items {
		key := it.ProductID + "/" + it.VariantID
		if it.Quantity <= 0 || totals[key] > int(^uint(0)>>1)-it.Quantity {
			return true, ErrInsufficientInventory
		}
		totals[key] += it.Quantity
	}
	guards, err := tx.QueryContext(ctx, `SELECT b.id FROM inventory_balances b WHERE b.status='sellable' AND EXISTS(SELECT 1 FROM order_items oi WHERE oi.order_id=$1 AND oi.product_id=b.product_id) ORDER BY b.id COLLATE "C" FOR UPDATE`, orderID)
	if err != nil {
		return true, err
	}
	for guards.Next() {
		var id string
		if err := guards.Scan(&id); err != nil {
			guards.Close()
			return true, err
		}
	}
	err = guards.Err()
	guards.Close()
	if err != nil {
		return true, err
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
		allocations, err := consumeInventoryForOriginTx(ctx, tx, it, mode)
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

func consumeInventoryBalancesAtOriginTx(ctx context.Context, tx *sql.Tx, it inventoryCaptureItem, mode string, origin inventoryOrigin) ([]skuAllocation, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,quantity FROM inventory_balances WHERE product_id=$1 AND status='sellable' AND (($3='tracked' AND variant_id=NULLIF($2,'')::uuid) OR ($3<>'tracked' AND variant_id IS NULL)) AND (($4='warehouse' AND warehouse_id=$5 AND supplier_warehouse_id IS NULL AND location_type IN ('warehouse','bin')) OR ($4='supplier' AND supplier_id=$6 AND supplier_warehouse_id=$7 AND warehouse_id IS NULL AND location_type='supplier')) ORDER BY id FOR UPDATE`, it.ProductID, it.VariantID, mode, origin.Kind, origin.WarehouseID, origin.SupplierID, origin.SupplierWarehouseID)
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
		 SELECT $1,product_id,supplier_id,variant_id,$2,'sellable','sold',location_key,'customer','order_capture',$3,$4,'{}' FROM inventory_balances WHERE id=$5`, "IM-"+uuid.NewString(), a.Quantity, it.ID, "sku-capture:"+it.ID+":"+origin.ID+":"+a.BalanceID, a.BalanceID); err != nil {
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
	parents, err := tx.QueryContext(ctx, `SELECT product_id,COALESCE(variant_id::text,'') FROM order_inventory_captures WHERE order_id=$1`, orderID)
	if err != nil {
		return true, err
	}
	var items []inventoryCaptureItem
	for parents.Next() {
		var it inventoryCaptureItem
		if err := parents.Scan(&it.ProductID, &it.VariantID); err != nil {
			parents.Close()
			return true, err
		}
		items = append(items, it)
	}
	err = parents.Err()
	parents.Close()
	if err != nil {
		return true, err
	}
	if err := lockInventoryParentsTx(ctx, tx, items); err != nil {
		return true, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT order_item_id,product_id,COALESCE(variant_id::text,''),quantity,variant_quantity,restored_quantity,inventory_mode,allocations FROM order_inventory_captures WHERE order_id=$1 ORDER BY order_item_id COLLATE "C" FOR UPDATE`, orderID)
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
	balanceGuards, err := tx.QueryContext(ctx, `SELECT b.id FROM inventory_balances b WHERE EXISTS(SELECT 1 FROM order_inventory_captures c CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a WHERE c.order_id=$1 AND a->>'balanceId'=b.id) ORDER BY b.id COLLATE "C" FOR UPDATE`, orderID)
	if err != nil {
		return true, err
	}
	for balanceGuards.Next() {
		var id string
		if err := balanceGuards.Scan(&id); err != nil {
			balanceGuards.Close()
			return true, err
		}
	}
	err = balanceGuards.Err()
	balanceGuards.Close()
	if err != nil {
		return true, err
	}
	for _, c := range captures {
		if c.restored == c.qty {
			continue
		}
		if c.restored != 0 {
			return true, fmt.Errorf("%w: partially returned order", ErrOrderCancellationState)
		}
		var allocations []skuAllocation
		if err := json.Unmarshal(c.raw, &allocations); err != nil {
			return true, err
		}
		allocated := 0
		for _, a := range allocations {
			if a.Quantity <= 0 || a.Quantity > c.qty-allocated {
				return true, fmt.Errorf("invalid recorded inventory allocation")
			}
			allocated += a.Quantity
		}
		if len(allocations) > 0 && allocated != c.qty {
			return true, fmt.Errorf("incomplete recorded inventory allocation")
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
			 SELECT $1,product_id,supplier_id,variant_id,$2,'sold','sellable','customer',location_key,'order_cancel',$3,$4,'{}' FROM inventory_balances WHERE id=$5 ON CONFLICT(idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), a.Quantity, c.id, "sku-cancel:"+c.id+":"+a.OriginID+":"+a.BalanceID, a.BalanceID)
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
