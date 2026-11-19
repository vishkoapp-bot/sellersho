package repository

import (
	"context"
	"database/sql"
)

// A receipt's immutable site must still match its canonical header. Names,
// product assignments and other present-day routing defaults are not evidence.
const warehouseQCReceiptSite = `
	((EXISTS (SELECT 1 FROM inbound_shipments i WHERE i.id=l.shipment_id AND i.warehouse_id=l.warehouse_id)
	  AND NOT EXISTS (SELECT 1 FROM shipments s WHERE s.id::text=l.shipment_id))
	 OR (EXISTS (SELECT 1 FROM shipments s WHERE s.id::text=l.shipment_id AND s.warehouse_id=l.warehouse_id)
	  AND NOT EXISTS (SELECT 1 FROM inbound_shipments i WHERE i.id=l.shipment_id)))`

const warehouseStaffCityMatch = `staff.is_active AND staff.warehouse_city_confirmed AND NULLIF(BTRIM(staff.warehouse_city),'') COLLATE "C" = NULLIF(BTRIM(site.city_name),'') COLLATE "C"`

// The allocation snapshots the exact site; a task without an allocation is
// reconciliation history, not authority to reserve or move inventory.
const warehousePutawayExactSite = `a.task_id=pt.id AND a.warehouse_id=pt.warehouse_id
	AND a.shipment_id=pt.inbound_id AND a.product_id=pt.product_id AND a.variant_id=COALESCE(pt.variant_id::text,'')
	AND a.quantity=pt.quantity AND a.source_location_key=pt.inventory_source_location_key`

const warehousePutawayCurrentBin = `(
	(pt.status='pending' AND NULLIF(pt.bin_location,'') IS NULL)
	OR EXISTS (SELECT 1 FROM warehouse_bins b WHERE b.bin_code=pt.bin_location AND b.warehouse_id=a.warehouse_id))`

func warehouseScopeResult(err error) error {
	if err == sql.ErrNoRows {
		return ErrInboundProvenance
	}
	return err
}

// Call only after locking the shipment parents. Keep the staff/site rows held
// until commit, so a city edit cannot invalidate an in-flight QC write.
func lockWarehouseQCReceiptScopeTx(ctx context.Context, tx *sql.Tx, shipment, product, lineID, supplier string) error {
	staff := WarehouseStaffID(ctx)
	if staff == "" {
		return nil
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT l.id FROM inbound_inventory_lines l
		JOIN warehouses site ON site.id=l.warehouse_id JOIN users staff ON staff.id=$1
		WHERE l.shipment_id=$2 AND l.product_id=$3 AND ($4='' OR l.id=$4) AND ($5='' OR l.supplier_id=$5)
		AND `+warehouseQCReceiptSite+` AND `+warehouseStaffCityMatch+`
		LIMIT 1 FOR SHARE OF l,site,staff`, staff, shipment, product, lineID, supplier).Scan(&id)
	return warehouseScopeResult(err)
}

func lockWarehousePutawayScopeTx(ctx context.Context, tx *sql.Tx, task string) error {
	staff := WarehouseStaffID(ctx)
	if staff == "" {
		return nil
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT a.warehouse_id FROM putaway_tasks pt
		JOIN inbound_putaway_allocations a ON `+warehousePutawayExactSite+`
		JOIN warehouses site ON site.id=a.warehouse_id JOIN users staff ON staff.id=$1
		WHERE pt.id=$2 AND `+warehouseStaffCityMatch+` FOR SHARE OF site,staff`, staff, task).Scan(&id)
	return warehouseScopeResult(err)
}

func lockWarehousePutawayBinScopeTx(ctx context.Context, tx *sql.Tx, task, bin string) error {
	if WarehouseStaffID(ctx) == "" {
		return nil
	}
	var code string
	err := tx.QueryRowContext(ctx, `SELECT b.bin_code FROM warehouse_bins b
		JOIN inbound_putaway_allocations a ON a.warehouse_id=b.warehouse_id
		WHERE a.task_id=$1 AND b.bin_code=$2 FOR UPDATE OF b`, task, bin).Scan(&code)
	return warehouseScopeResult(err)
}

func lockWarehousePutawayAssignmentBinsTx(ctx context.Context, tx *sql.Tx, task, destination string) error {
	if WarehouseStaffID(ctx) == "" {
		return nil
	}
	var current, status, reserved string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(pt.bin_location,''),pt.status,
		COALESCE((SELECT bin_code FROM putaway_capacity_reservations WHERE task_id=pt.id AND status='active'),'')
		FROM putaway_tasks pt WHERE pt.id=$1`, task).Scan(&current, &status, &reserved); err != nil {
		return warehouseScopeResult(err)
	}
	if (status == "assigned" && (current == "" || reserved != current)) || (reserved != "" && reserved != current) {
		return ErrInboundProvenance
	}
	if current == "" || current == destination {
		return lockWarehousePutawayBinScopeTx(ctx, tx, task, destination)
	}
	if current > destination {
		current, destination = destination, current
	}
	if err := lockWarehousePutawayBinScopeTx(ctx, tx, task, current); err != nil {
		return err
	}
	return lockWarehousePutawayBinScopeTx(ctx, tx, task, destination)
}
