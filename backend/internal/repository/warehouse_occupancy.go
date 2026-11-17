package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/drup/platform/internal/warehouseopt"
	"github.com/google/uuid"
)

var ErrOccupancyReconciliation = errors.New("physical bin occupancy requires counted reconciliation")

// InventoryAllocation is immutable departure evidence, never current sellable
// availability. Captures retain zero-quantity source rows after payment.
type InventoryAllocation struct {
	BalanceID   string `json:"balanceId"`
	ProductID   string `json:"productId"`
	VariantID   string `json:"variantId"`
	SupplierID  string `json:"supplierId"`
	WarehouseID string `json:"warehouseId"`
	BinCode     string `json:"binCode"`
	Quantity    int    `json:"quantity"`
	unresolved  bool
}

func lockReturnDeparturePhysicalTx(ctx context.Context, tx *sql.Tx, returnID, product, supplier string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var plan []TransactionRowLock
	queries := []struct {
		table, sql string
		args       []interface{}
	}{
		{"inventory_balances", `SELECT id FROM inventory_balances WHERE product_id=$1`, []interface{}{product}},
		{"inventory_reservations", `SELECT id FROM inventory_reservations WHERE reference_type='supplier_return' AND reference_id=$1`, []interface{}{returnID}},
	}
	binSource, err := returnHasBinSourceTx(ctx, tx, returnID)
	if err != nil {
		return err
	}
	if binSource {
		queries = append(queries,
			struct {
				table, sql string
				args       []interface{}
			}{"warehouse_bins", `SELECT bin_code FROM warehouse_bins WHERE bin_code IN(SELECT warehouse_bin_identity(location_key,to_jsonb(inventory_balances)->>'bin_code') FROM inventory_balances WHERE product_id=$1 AND location_type='bin') OR bin_code IN(SELECT bin_code FROM warehouse_placements WHERE product_id=$1)`, []interface{}{product}},
			struct {
				table, sql string
				args       []interface{}
			}{"warehouse_placements", `SELECT id FROM warehouse_placements WHERE product_id=$1`, []interface{}{product}},
		)
	}
	exists, err := storagePositionsExistTx(ctx, tx)
	if err != nil {
		return err
	}
	if exists {
		queries = append(queries, struct {
			table, sql string
			args       []interface{}
		}{"warehouse_storage_positions", `SELECT id FROM warehouse_storage_positions WHERE product_id=$1 AND supplier_id=$2 AND closed_at IS NULL`, []interface{}{product, supplier}})
	}
	for _, q := range queries {
		rows, err := tx.QueryContext(ctx, q.sql, q.args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			plan = append(plan, TransactionRowLock{Table: q.table, ID: id})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}

	return LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, plan...)
}

func returnHasBinSourceTx(ctx context.Context, tx *sql.Tx, returnID string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_reservations ir JOIN inventory_balances b ON b.product_id=ir.product_id AND b.supplier_id=ir.supplier_id AND b.location_key=ir.location_key AND COALESCE(to_jsonb(b)->>'variant_id','')=COALESCE(to_jsonb(ir)->>'variant_id','') WHERE ir.reference_type='supplier_return' AND ir.reference_id=$1 AND b.location_type='bin')`, returnID).Scan(&exists)
	return exists, err
}

func releaseSupplierReturnOccupancyTx(ctx context.Context, tx *sql.Tx, returnID, actor string) error {
	binSource, err := returnHasBinSourceTx(ctx, tx, returnID)
	if err != nil {
		return err
	}
	if !binSource {
		return nil
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT expected_warehouse_departure('supplier_return',$1)`, returnID).Scan(&raw); err != nil {
		return err
	}
	var allocations []InventoryAllocation
	if err := json.Unmarshal(raw, &allocations); err != nil {
		return err
	}
	return releasePlacementOccupancyTx(ctx, tx, returnID, "supplier_return", actor, allocations)
}

func loadOrderDepartureAllocationsTx(ctx context.Context, tx *sql.Tx, order string) ([]InventoryAllocation, error) {
	enabled, err := hasSKUInventoryTx(ctx, tx)
	if err != nil || !enabled {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.product_id,CASE WHEN c.inventory_mode='tracked' THEN COALESCE(c.variant_id::text,'') ELSE '' END,COALESCE(p.supplier_id,''),c.allocations,c.quantity,c.restored_quantity,oi.quantity FROM order_inventory_captures c JOIN order_items oi ON oi.id::text=c.order_item_id AND oi.order_id=c.order_id JOIN products p ON p.id=c.product_id WHERE c.order_id=$1 ORDER BY c.order_item_id COLLATE "C"`, order)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InventoryAllocation
	for rows.Next() {
		var product, variant, supplier string
		var raw []byte
		var captured, restored, ordered int
		if err = rows.Scan(&product, &variant, &supplier, &raw, &captured, &restored, &ordered); err != nil {
			return nil, err
		}
		if captured != ordered || restored != 0 {
			return nil, ErrOccupancyReconciliation
		}
		allocations, err := decodeCancellationAllocations(raw)
		if err != nil {
			return nil, err
		}
		for _, a := range allocations {
			out = append(out, InventoryAllocation{BalanceID: a.BalanceID, ProductID: product, VariantID: variant, SupplierID: supplier, Quantity: a.Quantity})
		}
	}
	return out, rows.Err()
}

func resolveDepartureAllocationsTx(ctx context.Context, tx *sql.Tx, allocations []InventoryAllocation) ([]InventoryAllocation, error) {
	totals := map[string]InventoryAllocation{}
	for _, a := range allocations {
		if a.BalanceID == "" || a.Quantity <= 0 {
			return nil, ErrOccupancyReconciliation
		}
		var product, variant, supplier, warehouse, kind, location, bin string
		if err := tx.QueryRowContext(ctx, `SELECT product_id,COALESCE(to_jsonb(b)->>'variant_id',''),COALESCE(supplier_id,''),COALESCE(to_jsonb(b)->>'warehouse_id',''),location_type,location_key,COALESCE(to_jsonb(b)->>'bin_code','') FROM inventory_balances b WHERE id=$1`, a.BalanceID).Scan(&product, &variant, &supplier, &warehouse, &kind, &location, &bin); err != nil {
			return nil, err
		}
		if (a.ProductID != "" && a.ProductID != product) || (a.VariantID != variant) || (a.SupplierID != "" && supplier != "" && a.SupplierID != supplier) {
			return nil, ErrOccupancyReconciliation
		}
		if kind != "bin" {
			continue
		} // Supplier and floor lots have no bin occupancy to release.
		if bin == "" {
			bin = location
		}
		bin = strings.TrimPrefix(bin, "bin:")
		location = strings.TrimPrefix(location, "bin:")
		a.unresolved = warehouse == "" || supplier == "" || bin == "" || (location != "" && location != bin)
		a.ProductID, a.VariantID, a.SupplierID, a.WarehouseID, a.BinCode = product, variant, supplier, warehouse, bin
		old := totals[a.BalanceID]
		if old.Quantity > 2147483647-a.Quantity {
			return nil, ErrOccupancyReconciliation
		}
		a.Quantity += old.Quantity
		totals[a.BalanceID] = a
	}
	out := make([]InventoryAllocation, 0, len(totals))
	for _, a := range totals {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BalanceID < out[j].BalanceID })
	return out, nil
}

// Caller preholds supplier/product/SKU/work and balance guards. Lock bins before
// placements and storage positions; replay verifies the entire normalized input.
func releasePlacementOccupancyTx(ctx context.Context, tx *sql.Tx, eventID, eventType, actor string, allocations []InventoryAllocation) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if eventID == "" || (eventType != "warehouse_shipment" && eventType != "supplier_return") {
		return ErrOccupancyReconciliation
	}
	allocations, err := resolveDepartureAllocationsTx(ctx, tx, allocations)
	if err != nil {
		return err
	}
	if len(allocations) == 0 {
		// Private historical DTO schemas may have no bin component. An existing
		// canonical nonempty event must still reject a changed empty replay.
		var installed bool
		if err = tx.QueryRowContext(ctx, `SELECT to_regclass('warehouse_departure_events') IS NOT NULL`).Scan(&installed); err != nil {
			return err
		}
		if !installed {
			return nil
		}
	}
	raw, err := json.Marshal(allocations)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(sum[:])
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM warehouse_departure_events WHERE event_type=$1 AND event_id=$2`, eventType, eventID).Scan(&existing)
	if err == nil {
		if existing != fingerprint {
			return ErrOccupancyReconciliation
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	if len(allocations) == 0 {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO warehouse_departure_events(event_type,event_id,payload_hash,actor_user_id,payload_json) VALUES($1,$2,$3,$4,$5)`, eventType, eventID, fingerprint, actor, string(raw)); err != nil {
		return err
	}
	var locks []TransactionRowLock
	knownBins := map[string]bool{}
	blocked := map[string]bool{}
	for _, a := range allocations {
		var actualWarehouse string
		e := tx.QueryRowContext(ctx, `SELECT COALESCE(to_jsonb(b)->>'warehouse_id','') FROM warehouse_bins b WHERE bin_code=$1`, a.BinCode).Scan(&actualWarehouse)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		knownBins[a.BinCode] = e == nil
		if a.unresolved || e == sql.ErrNoRows || actualWarehouse != a.WarehouseID {
			blocked[a.BinCode] = true
			if _, err = tx.ExecContext(ctx, `INSERT INTO warehouse_departure_unresolved_sources(event_type,event_id,balance_id,bin_code,reason) VALUES($1,$2,$3,$4,'unknown_physical_origin') ON CONFLICT DO NOTHING`, eventType, eventID, a.BalanceID, a.BinCode); err != nil {
				return err
			}
		}
		if e == nil {
			locks = append(locks, TransactionRowLock{Table: "warehouse_bins", ID: a.BinCode})
		}
	}
	if err = LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, locks...); err != nil {
		return err
	}
	// Discover all affected placement rows before FIFO consumption.
	ids := map[string]bool{}
	for _, a := range allocations {
		rows, e := tx.QueryContext(ctx, `SELECT id FROM warehouse_placements WHERE product_id=$1 AND variant_id=$2 AND supplier_id=$3 AND warehouse_id=$4 AND bin_code=$5 ORDER BY id COLLATE "C"`, a.ProductID, a.VariantID, a.SupplierID, a.WarehouseID, a.BinCode)
		if e != nil {
			return e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids[id] = true
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	locks = nil
	for id := range ids {
		locks = append(locks, TransactionRowLock{Table: "warehouse_placements", ID: id})
	}
	if err = LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, locks...); err != nil {
		return err
	}
	for bin, skip := range blocked {
		if skip && knownBins[bin] {
			if err = disputeOccupancyTx(ctx, tx, bin, eventID, eventType, "unknown_physical_origin"); err != nil {
				return err
			}
		}
	}
	physical, err := groupPhysicalDepartures(allocations)
	if err != nil {
		return err
	}
	// Decide coverage for every product/SKU in a shared bin before releasing any
	// of them. One uncertain source makes the whole bin conservative.
	for _, a := range physical {
		if blocked[a.BinCode] {
			continue
		}
		var available int64
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(p.remaining_quantity),0) FROM warehouse_remaining_placements p JOIN inventory_measurement_revisions m ON m.id=p.measurement_revision_id AND m.verified WHERE p.product_id=$1 AND p.variant_id=$2 AND p.supplier_id=$3 AND p.warehouse_id=$4 AND p.bin_code=$5`, a.ProductID, a.VariantID, a.SupplierID, a.WarehouseID, a.BinCode).Scan(&available); err != nil {
			return err
		}
		if available < int64(a.Quantity) {
			blocked[a.BinCode] = true
			if err = disputeOccupancyTx(ctx, tx, a.BinCode, eventID, eventType, "departure_lacks_measured_placement_coverage"); err != nil {
				return err
			}
		}
	}
	for _, a := range physical {
		if _, seen := blocked[a.BinCode]; seen {
			continue
		}
		var status string
		var q, v, w, knownQ, knownV, knownW int64
		var unresolved bool
		if err = tx.QueryRowContext(ctx, `SELECT b.capacity_status,b.quantity,b.occupied_volume_cm3,b.occupied_weight_grams,COALESCE(SUM(p.remaining_quantity),0),COALESCE(SUM(p.remaining_volume_cm3),0),COALESCE(SUM(p.remaining_weight_grams),0),EXISTS(SELECT 1 FROM warehouse_occupancy_exceptions x WHERE x.bin_code=b.bin_code) FROM warehouse_bins b LEFT JOIN warehouse_remaining_placements p ON p.bin_code=b.bin_code WHERE b.bin_code=$1 GROUP BY b.bin_code`, a.BinCode).Scan(&status, &q, &v, &w, &knownQ, &knownV, &knownW, &unresolved); err != nil {
			return err
		}
		blocked[a.BinCode] = unresolved || status != "verified" || q != knownQ || v != knownV || w != knownW || q < 0 || v < 0 || w < 0
		if blocked[a.BinCode] {
			if err = disputeOccupancyTx(ctx, tx, a.BinCode, eventID, eventType, "physical_counter_requires_count"); err != nil {
				return err
			}
		}
	}
	for _, a := range physical {
		if blocked[a.BinCode] {
			continue
		}
		if err = releaseAllocatedPlacementTx(ctx, tx, eventID, eventType, a); err != nil {
			return err
		}
	}
	return nil
}

func groupPhysicalDepartures(allocations []InventoryAllocation) ([]InventoryAllocation, error) {
	groups := map[string]InventoryAllocation{}
	for _, a := range allocations {
		key, err := json.Marshal([]string{a.BinCode, a.ProductID, a.VariantID, a.SupplierID, a.WarehouseID})
		if err != nil {
			return nil, err
		}
		old := groups[string(key)]
		if old.Quantity > 2147483647-a.Quantity {
			return nil, ErrOccupancyReconciliation
		}
		a.Quantity += old.Quantity
		groups[string(key)] = a
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]InventoryAllocation, 0, len(keys))
	for _, key := range keys {
		out = append(out, groups[key])
	}
	return out, nil
}

func disputeOccupancyTx(ctx context.Context, tx *sql.Tx, bin, eventID, eventType, reason string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO warehouse_occupancy_exceptions(bin_code,reason,event_type,event_id) VALUES($1,$2,$3,$4) ON CONFLICT(bin_code) DO NOTHING`, bin, reason, eventType, eventID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE warehouse_bins SET capacity_status='disputed' WHERE bin_code=$1`, bin)
	return err
}

type departurePlacement struct {
	id                                               string
	quantity, remaining                              int64
	volume, weight, remainingVolume, remainingWeight int64
	l, w, h, g                                       int64
}

func releaseAllocatedPlacementTx(ctx context.Context, tx *sql.Tx, eventID, eventType string, a InventoryAllocation) error {
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.quantity,p.remaining_quantity,p.volume_cm3,p.weight_grams,p.remaining_volume_cm3,p.remaining_weight_grams,m.length_mm,m.width_mm,m.height_mm,m.weight_grams FROM warehouse_remaining_placements p JOIN inventory_measurement_revisions m ON m.id=p.measurement_revision_id WHERE p.product_id=$1 AND p.variant_id=$2 AND p.supplier_id=$3 AND p.warehouse_id=$4 AND p.bin_code=$5 AND p.remaining_quantity>0 ORDER BY p.created_at,p.id COLLATE "C"`, a.ProductID, a.VariantID, a.SupplierID, a.WarehouseID, a.BinCode)
	if err != nil {
		return err
	}
	var placements []departurePlacement
	var available int64
	for rows.Next() {
		var p departurePlacement
		if err = rows.Scan(&p.id, &p.quantity, &p.remaining, &p.volume, &p.weight, &p.remainingVolume, &p.remainingWeight, &p.l, &p.w, &p.h, &p.g); err != nil {
			rows.Close()
			return err
		}
		placements = append(placements, p)
		available += p.remaining
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if available < int64(a.Quantity) {
		return disputeOccupancyTx(ctx, tx, a.BinCode, eventID, eventType, "departure_lacks_measured_placement_coverage")
	}
	left := int64(a.Quantity)
	for _, p := range placements {
		if left == 0 {
			break
		}
		take := left
		if take > p.remaining {
			take = p.remaining
		}
		beforeVolume, beforeWeight, e := warehouseopt.Capacity(p.l, p.w, p.h, p.g, p.remaining)
		if e != nil || beforeVolume != p.remainingVolume || beforeWeight != p.remainingWeight {
			return ErrOccupancyReconciliation
		}
		var afterVolume, afterWeight int64
		if p.remaining > take {
			afterVolume, afterWeight, e = warehouseopt.Capacity(p.l, p.w, p.h, p.g, p.remaining-take)
			if e != nil {
				return e
			}
		}
		volume, weight := beforeVolume-afterVolume, beforeWeight-afterWeight
		if _, err = tx.ExecContext(ctx, `INSERT INTO warehouse_occupancy_releases(id,event_type,event_id,placement_id,quantity,volume_cm3,weight_grams) VALUES($1,$2,$3,$4,$5,$6,$7)`, "OCC-"+uuid.NewString(), eventType, eventID, p.id, take, volume, weight); err != nil {
			return err
		}
		left -= take
	}
	return nil
}
