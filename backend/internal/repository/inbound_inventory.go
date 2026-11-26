package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var ErrInboundProvenance = errors.New("inbound provenance conflict or reconciliation required")
var ErrInboundLifecycleClosed = errors.New("use cumulative receipt-line QC disposition instead of the obsolete fail-only operation")
var ErrCartonNotCancellable = errors.New("carton_not_cancellable")

type InventoryOrigin struct{ Type, SupplierID, WarehouseID, SupplierWarehouseID string }
type InboundTransferLine struct {
	ProductID, VariantID, SourceKind, ExternalReference, Reason string
	Quantity                                                    int
	Source                                                      InventoryOrigin
}
type inboundPersistedLine struct {
	ID, Shipment, Supplier, Warehouse, Product, Variant, Kind, Source, External, Reason string
	Quantity, Dispatched, Received, Cancelled                                           int
	Allocations                                                                         []skuAllocation
}

// Caller holds the complete supplier/product/SKU/work/balance plan through
// lockInboundShipmentTx. Dispositions are append-only subordinate event metadata,
// serialized by the existing shipment guard, never late independent row guards.
// Counts are cumulative: (line,total disposed) is a durable business identity.
func dispositionInboundLineTx(ctx context.Context, tx *sql.Tx, l inboundPersistedLine, actor string, passed, failed, shortage int, reason string, selection dispositionUnitSelection) error {
	total := passed + failed // bounded by the public caller before arithmetic
	if total <= 0 || total > l.Received {
		return ErrInboundProvenance
	}
	var oldPassed, oldFailed, oldShortage int
	var oldReason string
	err := tx.QueryRowContext(ctx, `SELECT quantity_passed,quantity_failed,quantity_shortage,reason FROM inbound_qc_dispositions WHERE receipt_line_id=$1 AND cumulative_disposed=$2`, l.ID, total).Scan(&oldPassed, &oldFailed, &oldShortage, &oldReason)
	if err == nil {
		// RecordQCPass has no reason field; an opposite-endpoint all-pass
		// replay acknowledges the original event without changing its details.
		if oldPassed != passed || oldFailed != failed || oldShortage != shortage || (reason != "" && oldReason != "" && reason != oldReason) {
			return ErrInboundProvenance
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	var priorTotal, priorPassed, priorFailed, priorShortage int
	err = tx.QueryRowContext(ctx, `SELECT cumulative_disposed,quantity_passed,quantity_failed,quantity_shortage FROM inbound_qc_dispositions WHERE receipt_line_id=$1 ORDER BY cumulative_disposed DESC LIMIT 1`, l.ID).Scan(&priorTotal, &priorPassed, &priorFailed, &priorShortage)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if total <= priorTotal || passed < priorPassed || failed < priorFailed || shortage > l.Quantity-l.Received {
		return ErrInboundProvenance
	}
	// Historical QC evidence cannot be mapped to this new allocation by
	// guessing. No legacy damage-based subtraction or automatic repair.
	var historical bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qc_results q WHERE q.inbound_id=$1 AND q.product_id=$2 AND NOT EXISTS(SELECT 1 FROM inbound_qc_dispositions d WHERE d.id=q.id))`, l.Shipment, l.Product).Scan(&historical); err != nil {
		return err
	}
	if historical {
		return ErrInboundProvenance
	}
	deltaPassed, deltaFailed := passed-priorPassed, failed-priorFailed
	// Verify every existing state row belongs to the exact persisted origin.
	// All these rows have already been locked globally, before this read.
	var invalid bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_balances WHERE product_id=$1 AND COALESCE(variant_id::text,'')=$2 AND location_key=$3 AND (supplier_id IS DISTINCT FROM $4 OR warehouse_id IS DISTINCT FROM $5 OR owner_type<>'supplier' OR location_type<>'warehouse'))`, l.Product, l.Variant, "inbound-line:"+l.ID, l.Supplier, l.Warehouse).Scan(&invalid); err != nil {
		return err
	}
	if invalid {
		return ErrInboundProvenance
	}
	var undisposed int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE product_id=$1 AND COALESCE(variant_id::text,'')=$2 AND location_key=$3 AND status='quarantine'`, l.Product, l.Variant, "inbound-line:"+l.ID).Scan(&undisposed); err != nil {
		return err
	}
	if undisposed != l.Received-priorTotal {
		return ErrInboundProvenance
	}
	sourceLocation := "inbound-line:" + l.ID
	var sourceLot string
	if deltaPassed > 0 || deltaFailed > 0 {
		sourceLot, err = inventoryLotIDTx(ctx, tx, l.Product, l.Variant, sourceLocation, "quarantine")
		if err != nil {
			return err
		}
	}
	resolved, err := ResolveLegacyPurchaseVariant(ctx, tx, l.Product, l.Variant)
	if err != nil {
		return err
	}
	qcID := "QC-" + uuid.NewString()
	moves := []struct {
		status   string
		quantity int
		units    []lockedProductUnit
	}{{"qc_accepted", deltaPassed, selection.passedUnits}, {"damaged", deltaFailed, selection.failedUnits}}
	if len(selection.FailedCodes) > 0 {
		// Remove the explicitly failed units before FIFO selects passed units.
		moves[0], moves[1] = moves[1], moves[0]
	}
	for _, move := range moves {
		if move.quantity == 0 {
			continue
		}
		res, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$4,updated_at=NOW() WHERE product_id=$1 AND COALESCE(variant_id::text,'')=$2 AND location_key=$3 AND status='quarantine' AND quantity >=$4`, l.Product, l.Variant, "inbound-line:"+l.ID, move.quantity)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrInboundProvenance
		}
		destination := sourceLocation
		targetLine := l
		targetLine.Variant = resolved
		legacyMint := l.Variant == "" && resolved != ""
		targetQty := move.quantity
		if legacyMint {
			targetQty = 0
		}
		if move.status == "qc_accepted" {
			if err := createInboundPutawayTx(ctx, tx, targetLine, qcID, targetQty, move.quantity); err != nil {
				return err
			}
			destination = "qc-accepted:" + qcID
		} else if legacyMint {
			if _, err = creditReturnLotTx(ctx, tx, l.Product, l.Supplier, resolved, "damaged", destination, "warehouse:"+l.Warehouse, 0); err != nil {
				return err
			}
		} else if err := inboundCreditTx(ctx, tx, l.ID, l.Supplier, l.Warehouse, l.Product, l.Variant, move.status, move.quantity); err != nil {
			return err
		}
		target, err := inventoryLotIDTx(ctx, tx, l.Product, resolved, destination, move.status)
		if err != nil {
			return err
		}
		if legacyMint && move.status == "damaged" {
			stage, err := creditReturnLotTx(ctx, tx, l.Product, l.Supplier, resolved, "quarantine", "inbound-qc-mint:"+qcID, "warehouse:"+l.Warehouse, 0)
			if err != nil {
				return err
			}
			if _, err = mintUnitsTx(ctx, tx, resolved, l.Product, stage, move.quantity, "quarantined", "inbound_qc_disposition", qcID, actor); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=CASE WHEN id=$1 THEN quantity-$3 ELSE quantity+$3 END WHERE id IN ($1,$2)`, stage, target, move.quantity); err != nil {
				return err
			}
			if _, err = moveLotUnitsTx(ctx, tx, l.Product, resolved, stage, target, "quarantined", "damaged", move.quantity, "inbound", "inbound_qc_disposition", qcID, actor); err != nil {
				return err
			}
		} else {
			var movedCodes []string
			switch {
			case legacyMint:
				units, err := mintUnitsTx(ctx, tx, resolved, l.Product, target, move.quantity, "defined", "inbound_qc_disposition", qcID, actor)
				if err != nil {
					return err
				}
				for _, unit := range units {
					movedCodes = append(movedCodes, unit.Code)
				}
			case move.units != nil:
				if err := moveSelectedInboundUnitsTx(ctx, tx, move.units, sourceLot, target, unitStatusForLot(move.status, destination), "inbound_qc_disposition", qcID, actor); err != nil {
					return err
				}
				movedCodes = unitCodes(move.units)
			default:
				movedCodes, err = moveLotUnitsTx(ctx, tx, l.Product, l.Variant, sourceLot, target, "quarantined", unitStatusForLot(move.status, destination), move.quantity, "inbound", "inbound_qc_disposition", qcID, actor)
				if err != nil {
					return err
				}
			}
			if move.status == "qc_accepted" && selection.passedEcho != nil {
				*selection.passedEcho = movedCodes
			}
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,'quarantine',$6,$7,$11,'inbound_qc_disposition',$8,$9,NULLIF($10,''),'{}')`, "IM-"+uuid.NewString(), l.Product, l.Supplier, resolved, move.quantity, move.status, sourceLocation, qcID, fmt.Sprintf("inbound-qc:%s:%d:%s", l.ID, total, move.status), actor, destination); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO qc_results(id,product_id,inbound_id,inspector_user_id,status,defect_reason,quantity_passed,quantity_failed,created_at) VALUES($1,$2,$3,NULLIF($4,''),'completed',$5,$6,$7,NOW())`, qcID, l.Product, l.Shipment, actor, reason, deltaPassed, deltaFailed); err != nil {
		return err
	}
	if shortage > priorShortage {
		if _, err := tx.ExecContext(ctx, `INSERT INTO qc_discrepancies(id,inbound_id,supplier_id,product_id,expected_quantity,received_quantity,defect_count,reason,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'open',NOW())`, "DISP-QC-"+uuid.NewString(), l.Shipment, l.Supplier, l.Product, l.Received+shortage, l.Received, deltaFailed, reason); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inbound_qc_dispositions(id,receipt_line_id,cumulative_disposed,received_quantity,quantity_passed,quantity_failed,quantity_shortage,passed_delta,failed_delta,reason,actor_user_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,clock_timestamp())`, qcID, l.ID, total, l.Received, passed, failed, shortage, deltaPassed, deltaFailed, reason, actor)
	if err != nil {
		return err
	}
	if deltaPassed > 0 {
		return creditQCStorageReceiptTx(ctx, tx, qcID)
	}
	return nil
}

// Caller owns the transaction and preholds supplier, all products/SKUs and the
// shipment work guard. Append-only provenance is subordinate to that guard, not
// an independently acquired work-row lock. All balance rows are locked by ID.
func reserveInboundTransferTx(ctx context.Context, tx *sql.Tx, shipmentID, supplierID, warehouseID string, lines []InboundTransferLine) error {
	if shipmentID == "" || supplierID == "" || warehouseID == "" || len(lines) == 0 {
		return ErrInboundProvenance
	}
	sort.Slice(lines, func(i, j int) bool {
		return lines[i].ProductID+lines[i].VariantID < lines[j].ProductID+lines[j].VariantID
	})
	products := make([]string, 0, len(lines))
	for _, line := range lines {
		products = append(products, line.ProductID)
	}
	if err := lockInboundBalancesTx(ctx, tx, products); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, l := range lines {
		identity := l.ProductID + ":" + l.VariantID
		if seen[identity] {
			return ErrInboundProvenance
		}
		seen[identity] = true
		if l.SourceKind == "" {
			l.SourceKind = "transfer"
		}
		if l.ProductID == "" || l.Quantity <= 0 || int64(l.Quantity) > 2147483647 {
			return ErrInboundProvenance
		}
		if l.SourceKind == "transfer" {
			if l.Source.Type != "supplier" || l.Source.SupplierID != supplierID || l.Source.SupplierWarehouseID == "" || l.Source.WarehouseID != "" || l.ExternalReference != "" {
				return ErrInboundProvenance
			}
		} else if l.SourceKind != "external_replenishment" || l.ExternalReference == "" || l.Reason == "" || l.Source.SupplierWarehouseID != "" || l.Source.WarehouseID != "" {
			return ErrInboundProvenance
		}
		var owned bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM products p WHERE p.id=$1 AND p.supplier_id=$2 AND ($3='' OR EXISTS(SELECT 1 FROM product_variants v WHERE v.id::text=$3 AND v.product_id=p.id))) AND EXISTS(SELECT 1 FROM warehouses WHERE id=$4 AND is_active)`, l.ProductID, supplierID, l.VariantID, warehouseID).Scan(&owned); err != nil {
			return err
		}
		if !owned {
			return ErrInboundProvenance
		}
		if l.SourceKind == "transfer" {
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM supplier_warehouses WHERE id=$1 AND supplier_id=$2 AND is_active)`, l.Source.SupplierWarehouseID, supplierID).Scan(&owned); err != nil {
				return err
			}
			if !owned {
				return ErrInboundProvenance
			}
		}
		var old inboundPersistedLine
		err := tx.QueryRowContext(ctx, `SELECT source_kind,source_warehouse_id,warehouse_id,external_reference,reason,quantity FROM inbound_inventory_lines WHERE shipment_id=$1 AND product_id=$2 AND variant_id=$3`, shipmentID, l.ProductID, l.VariantID).Scan(&old.Kind, &old.Source, &old.Warehouse, &old.External, &old.Reason, &old.Quantity)
		if err == nil {
			if old.Kind != l.SourceKind || old.Source != l.Source.SupplierWarehouseID || old.Warehouse != warehouseID || old.External != l.ExternalReference || old.Reason != l.Reason || old.Quantity != l.Quantity {
				return ErrInboundProvenance
			}
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		allocations := []skuAllocation{}
		if l.SourceKind == "transfer" {
			rows, err := tx.QueryContext(ctx, `SELECT id,quantity FROM inventory_balances WHERE product_id=$1 AND COALESCE(variant_id::text,'')=$2 AND supplier_id=$3 AND supplier_warehouse_id=$4 AND owner_type='supplier' AND location_type='supplier' AND status='sellable' ORDER BY id COLLATE "C"`, l.ProductID, l.VariantID, supplierID, l.Source.SupplierWarehouseID)
			if err != nil {
				return err
			}
			remaining := l.Quantity
			for rows.Next() {
				var id string
				var n int
				if err := rows.Scan(&id, &n); err != nil {
					rows.Close()
					return err
				}
				if n > remaining {
					n = remaining
				}
				if n > 0 {
					allocations = append(allocations, skuAllocation{BalanceID: id, Quantity: n})
					remaining -= n
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if remaining > 0 {
				return ErrInsufficientInventory
			}
		}
		id := "IL-" + uuid.NewString()
		var raw []byte
		for _, a := range allocations {
			res, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$2,updated_at=NOW() WHERE id=$1 AND quantity >=$2`, a.BalanceID, a.Quantity)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return ErrInsufficientInventory
			}
		}
		if l.SourceKind == "transfer" {
			if err := inboundCreditTx(ctx, tx, id, supplierID, warehouseID, l.ProductID, l.VariantID, "inbound_reserved", l.Quantity); err != nil {
				return err
			}
			target, err := inventoryLotIDTx(ctx, tx, l.ProductID, l.VariantID, "inbound-line:"+id, "inbound_reserved")
			if err != nil {
				return err
			}
			for i := range allocations {
				a := &allocations[i]
				a.UnitCodes, err = moveLotUnitsTx(ctx, tx, l.ProductID, l.VariantID, a.BalanceID, target, "in_stock", "reserved", a.Quantity, "inbound", "inbound_booking", id, "")
				if err != nil {
					return err
				}
			}
			raw, err = json.Marshal(allocations)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,metadata) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,'sellable','inbound_reserved',$6,$7,'inbound_booking',$8,$9,$10)`, "IM-"+uuid.NewString(), l.ProductID, supplierID, l.VariantID, l.Quantity, "supplier-warehouse:"+l.Source.SupplierWarehouseID, "inbound-line:"+id, id, "inbound-booking:"+id, raw); err != nil {
				return err
			}

		}
		// Persist the complete provenance once, including the reserved unit codes.
		// The identity trigger intentionally forbids updating allocations later.
		raw, err = json.Marshal(allocations)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO inbound_inventory_lines(id,shipment_id,supplier_id,warehouse_id,product_id,variant_id,source_kind,source_warehouse_id,external_reference,reason,quantity,allocations) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, id, shipmentID, supplierID, warehouseID, l.ProductID, l.VariantID, l.SourceKind, l.Source.SupplierWarehouseID, l.ExternalReference, l.Reason, l.Quantity, raw); err != nil {
			var state interface{ SQLState() string }
			if errors.As(err, &state) && state.SQLState() == "23505" {
				return fmt.Errorf("%w: business reference already claimed", ErrInboundProvenance)
			}
			return err
		}
	}
	return nil
}

func inboundCreditTx(ctx context.Context, tx *sql.Tx, line, supplier, warehouse, product, variant, status string, quantity int) error {
	if quantity == 0 {
		return nil
	}
	conflict := `(product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL`
	if variant != "" {
		conflict = `(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL`
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled,warehouse_id) VALUES($1,$2,$3,NULLIF($4,'')::uuid,'supplier','warehouse',$5,$6,$7,TRUE,$8) ON CONFLICT `+conflict+` DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`, "INV-"+uuid.NewString(), product, supplier, variant, "inbound-line:"+line, status, quantity, warehouse)
	return err
}
func inboundLinesTx(ctx context.Context, tx *sql.Tx, shipment string) ([]inboundPersistedLine, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,shipment_id,supplier_id,warehouse_id,product_id,variant_id,source_kind,source_warehouse_id,external_reference,reason,quantity,dispatched_quantity,received_quantity,cancelled_quantity,allocations FROM inbound_inventory_lines WHERE shipment_id=$1 ORDER BY id COLLATE "C"`, shipment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []inboundPersistedLine
	for rows.Next() {
		var l inboundPersistedLine
		var raw []byte
		if err := rows.Scan(&l.ID, &l.Shipment, &l.Supplier, &l.Warehouse, &l.Product, &l.Variant, &l.Kind, &l.Source, &l.External, &l.Reason, &l.Quantity, &l.Dispatched, &l.Received, &l.Cancelled, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &l.Allocations); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func inboundMoveTx(ctx context.Context, tx *sql.Tx, l inboundPersistedLine, from, to string, n int, actor string, selected ...lockedProductUnit) error {
	if n == 0 {
		return nil
	}
	location := "inbound-line:" + l.ID
	key := fmt.Sprintf("inbound:%s:%s:%d", l.ID, to, l.Received+n)
	if from == "external" && l.Variant != "" {
		target, err := creditReturnLotTx(ctx, tx, l.Product, l.Supplier, l.Variant, to, location, "warehouse:"+l.Warehouse, 0)
		if err != nil {
			return err
		}
		return mintSKUStockTx(ctx, tx, l.Product, l.Supplier, l.Variant, target, n, to, location, "inbound_receive", l.ID, key, actor)
	}
	var source string
	if from != "external" {
		err := tx.QueryRowContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$4,updated_at=NOW() WHERE product_id=$1 AND COALESCE(variant_id::text,'')=$2 AND location_key=$3 AND quantity >=$4 AND status=$5 RETURNING id`, l.Product, l.Variant, location, n, from).Scan(&source)
		if err == sql.ErrNoRows {
			return ErrInboundProvenance
		}
		if err != nil {
			return err
		}
	}
	if err := inboundCreditTx(ctx, tx, l.ID, l.Supplier, l.Warehouse, l.Product, l.Variant, to, n); err != nil {
		return err
	}
	if source != "" {
		target, err := inventoryLotIDTx(ctx, tx, l.Product, l.Variant, location, to)
		if err != nil {
			return err
		}
		if selected != nil {
			if err := moveSelectedInboundUnitsTx(ctx, tx, selected, source, target, unitStatusForLot(to, location), "inbound_transfer", l.ID, actor); err != nil {
				return err
			}
		} else if _, err = moveLotUnitsTx(ctx, tx, l.Product, l.Variant, source, target, unitStatusForLot(from, location), unitStatusForLot(to, location), n, "inbound", "inbound_transfer", l.ID, actor); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$8,'inbound_transfer',$9,$10,NULLIF($11,''),'{}')`, "IM-"+uuid.NewString(), l.Product, l.Supplier, l.Variant, n, from, to, location, l.ID, key, actor)
	return err
}

func dispatchInboundTransferTx(ctx context.Context, tx *sql.Tx, shipment string) error {
	lines, err := inboundLinesTx(ctx, tx, shipment)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return ErrInboundProvenance
	}
	for _, l := range lines {
		n := l.Quantity - l.Cancelled - l.Dispatched
		if l.Cancelled > 0 {
			return ErrInboundProvenance
		}
		if n == 0 {
			continue
		}
		if l.Cancelled > 0 {
			return ErrInboundProvenance
		}
		if l.Kind == "transfer" {
			if err := inboundMoveTx(ctx, tx, l, "inbound_reserved", "in_transit", n, ""); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE inbound_inventory_lines SET dispatched_quantity=dispatched_quantity+$2 WHERE id=$1`, l.ID, n); err != nil {
			return err
		}
	}
	return nil
}
func receiveInboundLineTx(ctx context.Context, tx *sql.Tx, lineID string, cumulativeReceived int, actor string) error {
	var shipment string
	if err := tx.QueryRowContext(ctx, `SELECT shipment_id FROM inbound_inventory_lines WHERE id=$1`, lineID).Scan(&shipment); err != nil {
		return ErrInboundProvenance
	}
	lines, err := inboundLinesTx(ctx, tx, shipment)
	if err != nil {
		return err
	}
	for _, l := range lines {
		if l.ID != lineID {
			continue
		}
		return applyInboundLineReceiptTx(ctx, tx, l, cumulativeReceived, nil, actor)
	}
	return ErrInboundProvenance
}
func cancelInboundTransferTx(ctx context.Context, tx *sql.Tx, shipment string) error {
	lines, err := inboundLinesTx(ctx, tx, shipment)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return ErrInboundProvenance
	}
	for _, l := range lines {
		n := l.Quantity - l.Dispatched - l.Cancelled
		if n == 0 {
			continue
		}
		if l.Kind == "transfer" {
			res, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$2 WHERE location_key=$1 AND status='inbound_reserved' AND quantity >=$2`, "inbound-line:"+l.ID, n)
			if err != nil {
				return err
			}
			count, _ := res.RowsAffected()
			if count != 1 {
				return ErrInboundProvenance
			}
			reserved, err := inventoryLotIDTx(ctx, tx, l.Product, l.Variant, "inbound-line:"+l.ID, "inbound_reserved")
			if err != nil {
				return err
			}
			remaining := n
			for i := len(l.Allocations) - 1; i >= 0 && remaining > 0; i-- {
				a := l.Allocations[i]
				if a.Quantity > remaining {
					a.Quantity = remaining
				}
				remaining -= a.Quantity
				res, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity+$2 WHERE id=$1 AND product_id=$3 AND COALESCE(variant_id::text,'')=$4 AND supplier_id=$5 AND supplier_warehouse_id=$6 AND status='sellable'`, a.BalanceID, a.Quantity, l.Product, l.Variant, l.Supplier, l.Source)
				if err != nil {
					return err
				}
				count, _ := res.RowsAffected()
				if count != 1 {
					return ErrInboundProvenance
				}
				if err = restoreInboundReservationUnitsTx(ctx, tx, l, reserved, a); err != nil {
					return err
				}
			}
		}
		if l.Kind == "transfer" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,metadata) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,'inbound_reserved','sellable',$6,$7,'inbound_cancel',$8,$9,'{}')`, "IM-"+uuid.NewString(), l.Product, l.Supplier, l.Variant, n, "inbound-line:"+l.ID, "supplier-warehouse:"+l.Source, l.ID, "inbound-cancel:"+l.ID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE inbound_inventory_lines SET cancelled_quantity=cancelled_quantity+$2 WHERE id=$1`, l.ID, n); err != nil {
			return err
		}
	}
	return nil
}

func lockInboundShipmentTx(ctx context.Context, tx *sql.Tx, shipment string) error {
	lines, err := inboundLinesTx(ctx, tx, shipment)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return ErrInboundProvenance
	}
	locks := []TransactionRowLock{}
	for _, l := range lines {
		locks = append(locks, TransactionRowLock{Table: "suppliers", ID: l.Supplier}, TransactionRowLock{Table: "products", ID: l.Product})
		if l.Variant != "" {
			locks = append(locks, TransactionRowLock{Table: "product_variants", ID: l.Variant})
		}
	}
	table := "shipments"
	if len(shipment) > 4 && shipment[:4] == "INB-" {
		table = "inbound_shipments"
	}
	locks = append(locks, TransactionRowLock{Table: table, ID: shipment})
	if table == "shipments" {
		rows, err := tx.QueryContext(ctx, `SELECT id::text FROM shipment_items WHERE shipment_id=$1`, shipment)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			locks = append(locks, TransactionRowLock{Table: "shipment_items", ID: id})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	if err := LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, locks...); err != nil {
		return err
	}
	parents := make([]inventoryCaptureItem, 0, len(lines))
	products := make([]string, 0, len(lines))
	for _, line := range lines {
		products = append(products, line.Product)
		parents = append(parents, inventoryCaptureItem{ProductID: line.Product, VariantID: line.Variant})
	}
	if err := lockInventoryParentsTx(ctx, tx, parents, true); err != nil {
		return err
	}
	return lockInboundBalancesTx(ctx, tx, products)
}
func (r *SupplierRepository) ChangeCartonDispatch(ctx context.Context, supplier, shipment string, cancel bool) error {
	ctx, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockInboundShipmentTx(ctx, tx, shipment); err != nil {
		return err
	}
	var owner string
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id FROM shipments WHERE id=$1`, shipment).Scan(&owner); err != nil {
		return err
	}
	if owner != supplier {
		return ErrInboundProvenance
	}
	var pending, cancelled, dispatched int
	if err := tx.QueryRowContext(ctx, `SELECT SUM(quantity-dispatched_quantity-cancelled_quantity),
		SUM(cancelled_quantity),SUM(dispatched_quantity) FROM inbound_inventory_lines WHERE shipment_id=$1`,
		shipment).Scan(&pending, &cancelled, &dispatched); err != nil {
		return err
	}
	if !cancel && cancelled > 0 {
		return ErrInboundProvenance
	}
	if pending == 0 {
		if cancel && cancelled == 0 {
			return ErrCartonNotCancellable
		}
		return tx.Commit()
	}
	if cancel {
		err = cancelInboundTransferTx(ctx, tx, shipment)
	} else {
		err = dispatchInboundTransferTx(ctx, tx, shipment)
	}
	if err != nil {
		return err
	}
	status := "in_transit"
	if cancel {
		if dispatched > 0 {
			if _, err := syncCartonReceiptStatusTx(ctx, tx, shipment); err != nil {
				return err
			}
			return tx.Commit()
		}
		status = "cancelled"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shipments SET status=$2 WHERE id=$1`, shipment, status); err != nil {
		return err
	}
	return tx.Commit()
}

// Products/SKUs and work records are preheld. Discover the union of existing
// physical rows and acquire it in one bytewise balance-ID order, across all
// lines/products. Allocation selection below never acquires another row lock.
func lockInboundBalancesTx(ctx context.Context, tx *sql.Tx, products []string) error {
	ids := map[string]bool{}
	for _, id := range products {
		if id == "" {
			return ErrInboundProvenance
		}
		ids[id] = true
	}
	keys := make([]string, 0, len(ids))
	for id := range ids {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	args := make([]interface{}, len(keys))
	placeholders := ""
	for i, id := range keys {
		if i > 0 {
			placeholders += ","
		}
		placeholders += fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	if len(keys) == 0 {
		return ErrInboundProvenance
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM inventory_balances WHERE product_id IN (`+placeholders+`) ORDER BY id COLLATE "C" FOR UPDATE`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func restoreInboundReservationUnitsTx(ctx context.Context, tx *sql.Tx, l inboundPersistedLine, reserved string, a skuAllocation) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,code,status,balance_id,printed_at IS NOT NULL FROM product_units
		WHERE balance_id=$1 AND status='reserved' AND code=ANY($2) ORDER BY sequence DESC LIMIT $3 FOR UPDATE`, reserved, pq.Array(a.UnitCodes), a.Quantity)
	if err != nil {
		return err
	}
	units, err := scanLockedUnits(rows)
	rows.Close()
	if err != nil {
		return err
	}
	if len(units) < a.Quantity {
		if spncxStrict {
			return ErrUnitCoverageGap
		}
		if err = auditUnitGapTx(ctx, tx, l.Product, l.Variant, "inbound", "", a.Quantity, len(units)); err != nil {
			return err
		}
	}
	if err = transitionUnitsTx(ctx, tx, units, "in_stock", a.BalanceID, "released", "inbound_cancel", l.ID, ""); err != nil {
		return err
	}
	if err = refreshUnitTrackedLotTx(ctx, tx, reserved); err != nil {
		return err
	}
	return refreshUnitTrackedLotTx(ctx, tx, a.BalanceID)
}
