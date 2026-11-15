package repository

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

type storagePhysicalSource struct {
	Balance, Product, Variant, Supplier, Warehouse, Bin, LocationType, Location string
	Quantity                                                                    int
}
type storagePhysicalTake struct {
	Position, Placement string
	Quantity            int
}

func hasKnownStorageProductTx(ctx context.Context, tx *sql.Tx, product string) (bool, error) {
	var known bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM warehouse_storage_receipt_lots WHERE product_id=$1)`, product).Scan(&known)
	return known, err
}

// Caller owns the actual custody-transfer transaction and its full ordered
// account/product/SKU/work/balance/bin/placement/storage plan. Allocation comes
// from captured balances or consumed reservations, not current catalog FIFO.
func consumeStorageBusinessEventTx(ctx context.Context, tx *sql.Tx, eventType, eventID, actor string) error {
	rows, err := tx.QueryContext(ctx, `SELECT balance_id,product_id,variant_id,supplier_id,warehouse_id,bin_code,location_type,location_key,quantity FROM expected_storage_departure_sources($1,$2) ORDER BY balance_id COLLATE "C"`, eventType, eventID)
	if err != nil {
		return err
	}
	var sources []storagePhysicalSource
	for rows.Next() {
		var s storagePhysicalSource
		if err = rows.Scan(&s.Balance, &s.Product, &s.Variant, &s.Supplier, &s.Warehouse, &s.Bin, &s.LocationType, &s.Location, &s.Quantity); err != nil {
			rows.Close()
			return err
		}
		sources = append(sources, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if eventType == "warehouse_shipment" && len(sources) == 0 {
		return ErrStorageReceiptSource
	}
	for _, s := range sources {
		var recorded int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT SUM(quantity) FROM warehouse_storage_lot_departures WHERE event_type=$1 AND event_id=$2 AND source_balance_id=$3),0)+COALESCE((SELECT quantity FROM warehouse_storage_history_exceptions WHERE event_type=$1 AND event_id=$2 AND source_balance_id=$3),0)`, eventType, eventID, s.Balance).Scan(&recorded); err != nil {
			return err
		}
		if recorded != 0 {
			if recorded != s.Quantity {
				return ErrStorageReceiptSource
			}
			continue
		}
		takes, complete, err := storageSourceTakesTx(ctx, tx, eventType, eventID, s)
		if err != nil {
			return err
		}
		if !complete {
			// Preserve historical custody transfer without guessing an old receipt age,
			// sibling SKU or warehouse. Affected known cohorts cannot accrue new debt
			// until separately counted reconciliation resolves the missing history.
			_, err = tx.ExecContext(ctx, `INSERT INTO warehouse_storage_history_exceptions(id,event_type,event_id,source_balance_id,product_id,variant_id,supplier_id,warehouse_id,bin_code,location_type,location_key,quantity,reason,actor_user_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'physical source lacks exact certified storage coverage',$13)`, "SHE-"+uuid.NewString(), eventType, eventID, s.Balance, s.Product, s.Variant, s.Supplier, s.Warehouse, s.Bin, s.LocationType, s.Location, s.Quantity, actor)
			if err != nil {
				return err
			}
			continue
		}
		var plan []TransactionRowLock
		for _, take := range takes {
			plan = append(plan, TransactionRowLock{Table: "warehouse_storage_positions", ID: take.Position})
		}
		if err = LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, plan...); err != nil {
			return err
		}
		for _, take := range takes {
			if _, err = tx.ExecContext(ctx, `INSERT INTO warehouse_storage_lot_departures(id,position_id,event_type,event_id,source_balance_id,placement_id,physical_variant_id,quantity,actor_user_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, "SHD-"+uuid.NewString(), take.Position, eventType, eventID, s.Balance, take.Placement, s.Variant, take.Quantity, actor); err != nil {
				return err
			}
		}
	}
	return nil
}

func storageSourceTakesTx(ctx context.Context, tx *sql.Tx, eventType, eventID string, s storagePhysicalSource) ([]storagePhysicalTake, bool, error) {
	var takes []storagePhysicalTake
	if s.Bin != "" {
		rows, err := tx.QueryContext(ctx, `SELECT r.placement_id,r.quantity FROM warehouse_occupancy_releases r JOIN warehouse_placements p ON p.id=r.placement_id WHERE r.event_type=$1 AND r.event_id=$2 AND p.product_id=$3 AND p.variant_id=$4 AND p.supplier_id=$5 AND p.bin_code=$6 AND ($7='' OR p.warehouse_id=$7) ORDER BY p.created_at,p.id COLLATE "C"`, eventType, eventID, s.Product, s.Variant, s.Supplier, s.Bin, s.Warehouse)
		if err != nil {
			return nil, false, err
		}
		type release struct {
			id       string
			quantity int
		}
		var releases []release
		released := 0
		for rows.Next() {
			var r release
			if err = rows.Scan(&r.id, &r.quantity); err != nil {
				rows.Close()
				return nil, false, err
			}
			released += r.quantity
			releases = append(releases, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, false, err
		}
		if released != s.Quantity {
			return nil, false, nil
		}
		for _, release := range releases {
			candidates, err := tx.QueryContext(ctx, `SELECT m.position_id,m.quantity-COALESCE((SELECT SUM(d.quantity) FROM warehouse_storage_lot_departures d WHERE d.position_id=m.position_id AND d.placement_id=m.placement_id),0) FROM warehouse_storage_placement_lots m JOIN warehouse_storage_receipt_lots l ON l.id=m.position_id WHERE m.placement_id=$1 AND NOT storage_receipt_is_blocked(l.id) ORDER BY l.physical_received_at,l.id COLLATE "C"`, release.id)
			if err != nil {
				return nil, false, err
			}
			left := release.quantity
			for candidates.Next() {
				var position string
				var available int
				if err = candidates.Scan(&position, &available); err != nil {
					candidates.Close()
					return nil, false, err
				}
				take := min(left, max(available, 0))
				if take > 0 {
					takes = append(takes, storagePhysicalTake{Position: position, Placement: release.id, Quantity: take})
					left -= take
				}
			}
			err = candidates.Err()
			candidates.Close()
			if err != nil {
				return nil, false, err
			}
			if left != 0 {
				return nil, false, nil
			}
		}
		return takes, true, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT l.id,l.initial_quantity-COALESCE((SELECT SUM(quantity) FROM warehouse_storage_placement_lots WHERE position_id=l.id),0)-COALESCE((SELECT SUM(quantity) FROM warehouse_storage_lot_departures WHERE position_id=l.id AND placement_id=''),0) FROM warehouse_storage_receipt_lots l JOIN inventory_balances original ON original.id=l.source_balance_id WHERE NOT storage_receipt_is_blocked(l.id) AND l.product_id=$1 AND l.variant_id=$2 AND l.supplier_id=$3 AND original.location_type=$4 AND original.location_key=$5 AND ($6='' OR l.warehouse_id=$6) ORDER BY l.physical_received_at,l.id COLLATE "C"`, s.Product, s.Variant, s.Supplier, s.LocationType, s.Location, s.Warehouse)
	if err != nil {
		return nil, false, err
	}
	left := s.Quantity
	for rows.Next() {
		var position string
		var available int
		if err = rows.Scan(&position, &available); err != nil {
			rows.Close()
			return nil, false, err
		}
		take := min(left, max(available, 0))
		if take > 0 {
			takes = append(takes, storagePhysicalTake{Position: position, Quantity: take})
			left -= take
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	return takes, left == 0, nil
}
