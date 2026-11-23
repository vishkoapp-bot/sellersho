package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type skuAllocation struct {
	OriginID  string   `json:"originId,omitempty"`
	BalanceID string   `json:"balanceId"`
	Quantity  int      `json:"quantity"`
	UnitCodes []string `json:"unit_codes"`
}

func hasSKUInventoryTx(ctx context.Context, tx *sql.Tx) (bool, error) {
	var enabled bool
	err := tx.QueryRowContext(ctx, `SELECT to_regclass('order_inventory_captures') IS NOT NULL`).Scan(&enabled)
	return enabled, err
}

// SeedTrackedSKUInventoryTx creates only the exact declared V2 SKU balances.
// Existing NULL-variant historical balances are never allocated or divided.
func SeedTrackedSKUInventoryTx(ctx context.Context, tx *sql.Tx, productID, supplierID string, approvalID ...string) error {
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
	items := make([]inventoryCaptureItem, 0, len(seeds))
	for _, s := range seeds {
		items = append(items, inventoryCaptureItem{ProductID: productID, VariantID: s.id})
	}
	if err = lockInventoryParentsTx(ctx, tx, items); err != nil {
		return err
	}
	for _, s := range seeds {
		var lot string
		err := tx.QueryRowContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled)
		 VALUES($1,$2,NULLIF($3,''),$4,'supplier','supplier',$5,'sellable',0,TRUE)
		 ON CONFLICT(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL DO NOTHING RETURNING id`, "INV-"+uuid.NewString(), productID, supplierID, s.id, location).Scan(&lot)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if s.qty == 0 {
			continue
		}
		ref := productID
		if len(approvalID) > 0 {
			ref = approvalID[0]
		}
		key := "approval:" + ref + ":" + s.id
		if err = mintSKUStockTx(ctx, tx, productID, supplierID, s.id, lot, s.qty, "sellable", location, "approval", ref, key, ""); err != nil {
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
	var lot string
	err := tx.QueryRowContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled)
	 VALUES($1,$2,NULLIF($3,''),$4,'supplier',$5,$6,$7,0,TRUE)
	 ON CONFLICT(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL
	 DO UPDATE SET updated_at=inventory_balances.updated_at RETURNING id`, "INV-"+uuid.NewString(), productID, supplierID, variantID, locationType, location, status).Scan(&lot)
	if err != nil {
		return err
	}
	return mintSKUStockTx(ctx, tx, productID, supplierID, variantID, lot, quantity, status, location, referenceType, referenceID, key, actor)
}

func mintSKUStockTx(ctx context.Context, tx *sql.Tx, product, supplier, variant, lot string, quantity int, status, location, refType, refID, key, actor string) error {
	res, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata)
	 VALUES($1,$2,NULLIF($3,''),$4,$5,'external',$6,'external',$7,$8,$9,$10,NULLIF($11,''),'{}') ON CONFLICT(idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), product, supplier, variant, quantity, status, location, refType, refID, key, actor)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	_, err = mintUnitsTx(ctx, tx, variant, product, lot, quantity, unitStatusForLot(status, location), refType, refID, actor, key)
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
	return moveSKUInventoryFromLotTx(ctx, tx, productID, supplierID, variantID, balanceID, qty, fromStatus, toStatus, source, locationType, destination, referenceType, referenceID, key, actor)
}

func moveSKUInventoryFromLotTx(ctx context.Context, tx *sql.Tx, productID, supplierID, variantID, balanceID string, qty int, fromStatus, toStatus, source, locationType, destination, referenceType, referenceID, key, actor string) error {
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
	var target string
	err = tx.QueryRowContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled,bin_code,warehouse_id,supplier_warehouse_id)
	SELECT $1,$2,NULLIF($3,''),$4,'supplier',$5::text,$6::text,$7,$8,TRUE,CASE WHEN $5::text='bin' THEN substring($6::text FROM 5) END,
		CASE WHEN location_key=$6 AND location_type=$5 THEN warehouse_id END,
		CASE WHEN location_key=$6 AND location_type=$5 THEN supplier_warehouse_id END
	FROM inventory_balances WHERE id=$9
	ON CONFLICT(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW() RETURNING id`, "INV-"+uuid.NewString(), productID, supplierID, variantID, locationType, destination, toStatus, qty, balanceID).Scan(&target)
	if err != nil {
		return err
	}
	operation := "inbound"
	if referenceType == "supplier_return" {
		operation = "supplier_return"
	}
	_, err = moveLotUnitsTx(ctx, tx, productID, variantID, balanceID, target, unitStatusForLot(fromStatus, source), unitStatusForLot(toStatus, destination), qty, operation, referenceType, referenceID, actor)
	return err
}

func lockSKUProductTx(ctx context.Context, tx *sql.Tx, productID, variantID string) error {
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM products WHERE id=$1 FOR UPDATE`, productID).Scan(&locked); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, variantID, productID).Scan(&locked); err != nil {
		return err
	}
	return lockExistingUnitCounterTx(ctx, tx, variantID)
}

// restoreCapturedReturnTx accounts at most the quantity actually captured for
// the order line. A return lot has exact SKU identity without inventing its old
// location; historical orders stay explicitly unallocated in the legacy path.
func restoreCapturedReturnTx(ctx context.Context, tx *sql.Tx, returnItemID, orderItemID, productID, supplierID string, qty int, qcStatus, destination, actor string) (bool, error) {
	enabled, err := hasSKUInventoryTx(ctx, tx)
	if err != nil || !enabled {
		return enabled, err
	}
	var variant, order, returnID string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(c.variant_id::text,oi.variant_id::text,''),oi.order_id,ri.return_id::text
		FROM order_items oi JOIN return_items ri ON ri.order_item_id::text=oi.id::text
		LEFT JOIN order_inventory_captures c ON c.order_item_id=oi.id::text WHERE oi.id=$1 AND ri.id=$2`, orderItemID, returnItemID).Scan(&variant, &order, &returnID); err != nil {
		return true, err
	}
	items := []inventoryCaptureItem{{ProductID: productID, VariantID: variant}}
	if err := lockInventoryParentsTx(ctx, tx, items, true); err != nil {
		return true, err
	}
	variant = items[0].VariantID
	var captured, restored int
	var captureMode string
	err = tx.QueryRowContext(ctx, `SELECT quantity,restored_quantity,inventory_mode FROM order_inventory_captures WHERE order_item_id=$1`, orderItemID).Scan(&captured, &restored, &captureMode)
	if err != nil && err != sql.ErrNoRows {
		return true, err
	}
	hasCapture := err == nil
	if qty <= 0 {
		return true, fmt.Errorf("invalid return quantity")
	}
	if hasCapture {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_inventory_returns WHERE return_item_id=$1)`, returnItemID).Scan(&exists); err != nil {
			return true, err
		}
		if exists {
			return true, nil
		}
		if qty > captured-restored {
			return true, fmt.Errorf("return quantity exceeds captured inventory")
		}
	}
	// Claim the receipt before touching the lots, including historical receipts
	// which have no capture record for order_inventory_returns to reference.
	receiptKey := "spncx-return-receipt:" + returnItemID
	holdingLocation := "return:" + returnID
	res, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id)
		VALUES($1,$2,NULLIF($3,''),NULLIF($4,'')::uuid,$5,'sold','quarantine','customer',$6,'customer_return_receipt',$7,$8,NULLIF($9,'')) ON CONFLICT(idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), productID, supplierID, variant, qty, holdingLocation, returnID, receiptKey, actor)
	if err != nil {
		return true, err
	}
	affected, err := res.RowsAffected()
	if err != nil || affected == 0 {
		return true, err
	}
	if err = ensureCanonicalCoverageTx(ctx, tx, productID, supplierID); err != nil {
		return true, err
	}
	holding, err := creditReturnLotTx(ctx, tx, productID, supplierID, variant, "quarantine", holdingLocation, destination, qty)
	if err != nil {
		return true, err
	}
	units, err := capturedUnitsTx(ctx, tx, orderItemID, nil, []string{"sold"}, qty)
	if err != nil {
		return true, err
	}
	if len(units) < qty {
		if spncxStrict && hasCapture {
			var serialized bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_item_units WHERE order_item_id=$1)`, orderItemID).Scan(&serialized); err != nil {
				return true, err
			}
			if serialized {
				return true, ErrUnitCoverageGap
			}
		}
		if err = auditUnitGapTx(ctx, tx, productID, variant, "restock", order, qty, len(units)); err != nil {
			return true, err
		}
	}
	if err = debitUnitSourceLotsTx(ctx, tx, units); err != nil {
		return true, err
	}
	if err = transitionUnitsTx(ctx, tx, units, "returned", holding, "returned", "customer_return", returnID, actor); err != nil {
		return true, err
	}
	// A recorded quantity-only capture is still restockable before backfill.
	// Only pre-cutover orders without a capture stay in the holding lot (§4.3).
	if hasCapture {
		toStatus, status, event := "sellable", "in_stock", "restored"
		switch qcStatus {
		case "defective":
			toStatus, status, event = "quarantine", "quarantined", "quarantined"
		case "destroyed":
			toStatus, status, event = "damaged", "damaged", "damaged"
		case "ok":
		default:
			return true, ErrInvalidUnitTransition
		}
		location := destination + ":return:" + returnItemID
		if destination == "" {
			location = "custreturn:" + returnID + ":" + returnItemID
		}
		lotVariant, referenceID, movementKey := variant, returnItemID, "sku-return:"+returnItemID
		if captureMode != "tracked" {
			lotVariant, location = "", "custreturn:"+returnID
			referenceID, movementKey = returnID, "crstk:"+returnID+":"+returnItemID+":"+toStatus
		}
		target, err := creditReturnLotTx(ctx, tx, productID, supplierID, lotVariant, toStatus, location, destination, qty)
		if err != nil {
			return true, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$2,updated_at=NOW() WHERE id=$1`, holding, qty); err != nil {
			return true, err
		}
		if err = transitionUnitsTx(ctx, tx, units, status, target, event, "customer_return", returnItemID, actor); err != nil {
			return true, err
		}
		if err = refreshUnitTrackedLotTx(ctx, tx, target); err != nil {
			return true, err
		}
		if status == "in_stock" {
			if err = markUnitsRestoredTx(ctx, tx, orderItemID, units); err != nil {
				return true, err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata)
			VALUES($1,$2,NULLIF($3,''),NULLIF($4,'')::uuid,$5,'quarantine',$6,$7,$8,'customer_return',$9,$10,NULLIF($11,''),jsonb_build_object('qc',$12::text,'destination',$13::text))`, "IM-"+uuid.NewString(), productID, supplierID, lotVariant, qty, toStatus, holdingLocation, location, referenceID, movementKey, actor, qcStatus, destination); err != nil {
			return true, err
		}
	}
	if err = refreshUnitTrackedLotTx(ctx, tx, holding); err != nil {
		return true, err
	}
	if !hasCapture {
		return true, nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO order_inventory_returns(return_item_id,order_item_id,quantity) VALUES($1,$2,$3)`, returnItemID, orderItemID, qty); err != nil {
		return true, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE order_inventory_captures SET restored_quantity=restored_quantity+$2 WHERE order_item_id=$1`, orderItemID, qty)
	return true, err
}

type inventoryCaptureItem struct {
	ID, ProductID, VariantID, OrderID string
	Quantity                          int
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
		it.OrderID = orderID
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return true, err
	}
	rows.Close()
	if err := lockInventoryParentsTx(ctx, tx, items, true); err != nil {
		return true, err
	}
	// Captures are immutable business events; replay cannot debit again.
	var capturedCount int
	var hasUnits bool
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM order_inventory_captures WHERE order_id=$1), EXISTS(SELECT 1 FROM order_item_units WHERE order_id=$1)`, orderID).Scan(&capturedCount, &hasUnits); err != nil {
		return true, err
	}
	if capturedCount > 0 || hasUnits {
		if capturedCount != len(items) {
			return true, fmt.Errorf("incomplete inventory capture")
		}
		return true, nil
	}
	for _, it := range items {
		if it.VariantID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE order_items SET variant_id=$2 WHERE id=$1 AND variant_id IS NULL`, it.ID, it.VariantID); err != nil {
				return true, err
			}
		}
		var supplier string
		var legacy bool
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(p.supplier_id,''),COALESCE(v.inventory_mode,'legacy')='legacy' FROM products p LEFT JOIN product_variants v ON v.id=NULLIF($2,'')::uuid AND v.product_id=p.id WHERE p.id=$1`, it.ProductID, it.VariantID).Scan(&supplier, &legacy); err != nil {
			return true, err
		}
		if legacy {
			if err := ensureCanonicalCoverageTx(ctx, tx, it.ProductID, supplier); err != nil {
				return true, err
			}
		}
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
		var version int
		var typ string
		if err := tx.QueryRowContext(ctx, `SELECT schema_version,product_type FROM products WHERE id=$1 FOR UPDATE`, it.ProductID).Scan(&version, &typ); err != nil {
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
			// Tracked SKU stock excludes staging and sibling variants. A zero
			// projection cannot support a quantity-only capture for this SKU.
			if mode == "tracked" && variantStock == 0 {
				return true, ErrInsufficientInventory
			}
			if !validated[key] {
				var max *int
				if maximum.Valid {
					n := int(maximum.Int32)
					max = &n
				}
				// The locked lots decide stock admission, including the cutover gap.
				if err := ValidateSKUQuantity("legacy", active, 0, minimum, max, totals[key]); err != nil {
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
			for _, a := range allocations {
				variantQty += a.Quantity
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
	rows, err := tx.QueryContext(ctx, `SELECT id,quantity FROM inventory_balances WHERE product_id=$1 AND status='sellable' AND (($3='tracked' AND variant_id=NULLIF($2,'')::uuid) OR ($3<>'tracked' AND variant_id IS NULL)) AND ((variant_id IS NULL AND location_key='warehouse:legacy_migration' AND reconciled AND $3<>'tracked') OR ($4='warehouse' AND warehouse_id=$5 AND supplier_warehouse_id IS NULL AND location_type IN ('warehouse','bin')) OR ($4='supplier' AND supplier_id=$6 AND supplier_warehouse_id=$7 AND warehouse_id IS NULL AND location_type='supplier')) ORDER BY id FOR UPDATE`, it.ProductID, it.VariantID, mode, origin.Kind, origin.WarehouseID, origin.SupplierID, origin.SupplierWarehouseID)
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

	if total == 0 {
		var stock int
		var staged bool
		if err := tx.QueryRowContext(ctx, `SELECT stock,EXISTS(
			SELECT 1 FROM inventory_balances b WHERE b.product_id=$1 AND b.warehouse_id=$2
			  AND ($2<>'' AND $3='warehouse') AND b.quantity>0
			  AND b.status IN ('quarantine','qc_accepted','physical')
			  AND (($4='tracked' AND b.variant_id=NULLIF($5,'')::uuid) OR ($4<>'tracked' AND b.variant_id IS NULL))
		) FROM products WHERE id=$1`, it.ProductID, origin.WarehouseID, origin.Kind, mode, it.VariantID).Scan(&stock, &staged); err != nil {
			return nil, err
		}
		if stock < it.Quantity || staged {
			return nil, ErrInsufficientInventory
		}
	}
	if total < it.Quantity && spncxStrict {
		return nil, ErrUnitCoverageGap
	}
	take := min(total, it.Quantity)
	unitsByBalance, err := selectCaptureUnitsTx(ctx, tx, available, take)
	if err != nil {
		return nil, err
	}
	serialized := 0
	for _, units := range unitsByBalance {
		serialized += len(units)
	}
	if serialized < take && spncxStrict {
		return nil, ErrUnitCoverageGap
	}
	if err := auditUnitGapTx(ctx, tx, it.ProductID, it.VariantID, "capture", it.OrderID, it.Quantity, serialized); err != nil {
		return nil, err
	}
	remaining := take - serialized
	taken := make([]skuAllocation, 0, len(available))
	for _, a := range available {
		units := unitsByBalance[a.BalanceID]
		quantityOnly := min(remaining, a.Quantity-len(units))
		a.Quantity = len(units) + quantityOnly
		remaining -= quantityOnly
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
		a.UnitCodes, err = allocateUnitsForCaptureTx(ctx, tx, it, a.BalanceID, units)
		if err != nil {
			return nil, err
		}
		taken = append(taken, a)
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
	if err := lockInventoryParentsTx(ctx, tx, items, true); err != nil {
		return true, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT order_item_id,product_id,COALESCE(variant_id::text,''),quantity,variant_quantity,restored_quantity,inventory_mode,allocations FROM order_inventory_captures WHERE order_id=$1 ORDER BY order_item_id::text COLLATE "C"`, orderID)
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
	if len(captures) == 0 {
		return true, restorePreCutoverCancelTx(ctx, tx, orderID)
	}
	// INV-12: the old EXISTS … jsonb_array_elements guard forced a full
	// sequential scan of inventory_balances on every paid cancellation
	// (6.6 ms at 20k rows here, linear in table size). Parse the (few)
	// allocation arrays in Go first and lock exactly those balance IDs
	// in sorted order — the same lock-order discipline as
	// lockInventoryParentsTx / LockTransactionRowsTx. The per-allocation
	// validation below still runs; this guard only pre-locks the rows.
	balanceIDs := map[string]bool{}
	for _, c := range captures {
		var allocations []skuAllocation
		if err := json.Unmarshal(c.raw, &allocations); err != nil {
			return true, err
		}
		for _, a := range allocations {
			if a.BalanceID != "" {
				balanceIDs[a.BalanceID] = true
			}
		}
	}
	ids := make([]string, 0, len(balanceIDs))
	for id := range balanceIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > 0 {
		balanceGuards, err := tx.QueryContext(ctx, `SELECT id FROM inventory_balances WHERE id = ANY($1) ORDER BY id COLLATE "C" FOR UPDATE`, pq.Array(ids))
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
		sort.Slice(allocations, func(i, j int) bool { return allocations[i].BalanceID < allocations[j].BalanceID })
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
			if err := restoreUnitsTx(ctx, tx, c.id, a); err != nil {
				return true, err
			}
			if err := refreshUnitTrackedLotTx(ctx, tx, a.BalanceID); err != nil {
				return true, err
			}
		}

		if _, err := tx.ExecContext(ctx, `UPDATE order_inventory_captures SET restored_quantity=quantity WHERE order_item_id=$1`, c.id); err != nil {
			return true, err
		}
	}
	return true, nil
}

// Remove after the pre-cutover cancellation/return window closes. Review 2026-12-23.
func restorePreCutoverCancelTx(ctx context.Context, tx *sql.Tx, orderID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id::text,product_id,COALESCE(variant_id::text,''),quantity
		FROM order_items WHERE order_id=$1 ORDER BY product_id,variant_id,id`, orderID)
	if err != nil {
		return err
	}
	var items []inventoryCaptureItem
	for rows.Next() {
		var it inventoryCaptureItem
		if err = rows.Scan(&it.ID, &it.ProductID, &it.VariantID, &it.Quantity); err != nil {
			rows.Close()
			return err
		}
		items = append(items, it)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = lockInventoryParentsTx(ctx, tx, items, true); err != nil {
		return err
	}
	for _, it := range items {
		var supplier string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(supplier_id,'') FROM products WHERE id=$1`, it.ProductID).Scan(&supplier); err != nil {
			return err
		}
		// Preserve existing sellable projection when this is the first ledger write.
		if err = ensureCanonicalCoverageTx(ctx, tx, it.ProductID, supplier); err != nil {
			return err
		}
		if _, err = creditReturnLotTx(ctx, tx, it.ProductID, supplier, it.VariantID, "unreconciled", "legacy-cancel:"+orderID, "", it.Quantity); err != nil {
			return err
		}
		if err = auditUnitGapTx(ctx, tx, it.ProductID, it.VariantID, "restock", orderID, it.Quantity, 0); err != nil {
			return err
		}
	}
	return nil
}

func creditReturnLotTx(ctx context.Context, tx *sql.Tx, product, supplier, variant, status, location, destination string, quantity int) (string, error) {
	var id string
	locationType := "warehouse"
	warehouse := strings.TrimPrefix(destination, "warehouse:")
	supplierWarehouse := ""
	if strings.HasPrefix(destination, "supplier_warehouse:") {
		locationType = "supplier"
		supplierWarehouse = strings.TrimPrefix(destination, "supplier_warehouse:")
		warehouse = ""
	}
	conflict := "(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL"
	if variant == "" {
		conflict = "(product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL"
	}
	err := tx.QueryRowContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled,warehouse_id,supplier_warehouse_id)
		VALUES($1,$2,NULLIF($3,''),NULLIF($4,'')::uuid,'supplier',$5,$6,$7,$8,TRUE,NULLIF($9,''),NULLIF($10,''))
		ON CONFLICT `+conflict+` DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW() RETURNING id`, "INV-"+uuid.NewString(), product, supplier, variant, locationType, location, status, quantity, warehouse, supplierWarehouse).Scan(&id)
	return id, err
}
