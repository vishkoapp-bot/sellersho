package repository

import (
	"context"
	"database/sql"
	"encoding/json"
)

// ListScopedWarehouseShippingLegs exposes only platform warehouse legs with
// a persisted physical site. Supplier origins and legacy labels are not sites.
func ListScopedWarehouseShippingLegs(ctx context.Context, db *sql.DB, limit int) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	staffID := WarehouseStaffID(ctx)
	if staffID == "" {
		return out, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT row_to_json(leg)
		FROM order_shipping_legs leg
		JOIN warehouses site ON site.id = leg.warehouse_id AND site.id <> ''
		JOIN users staff ON staff.id = $1
		WHERE leg.supplier_id IS NULL AND leg.supplier_warehouse_id IS NULL
		  AND staff.is_active AND staff.warehouse_city_confirmed
		  AND NULLIF(BTRIM(staff.warehouse_city), '') COLLATE "C" = BTRIM(site.city_name) COLLATE "C"
		ORDER BY leg.id ASC LIMIT $2`, staffID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(raw))
	}
	return out, rows.Err()
}

const scopedCartonSiteJoin = `
	JOIN warehouses site ON site.id = s.warehouse_id AND site.id <> ''
	JOIN users staff ON staff.id = $2
`

const scopedCartonCityMatch = `
	AND staff.is_active AND staff.warehouse_city_confirmed
	AND NULLIF(BTRIM(staff.warehouse_city), '') COLLATE "C" = BTRIM(site.city_name) COLLATE "C"
`

// Shipment items have no independent site, but receipt provenance does.
// A carton with conflicting destinations has no unambiguous scope.
const scopedCartonDestinationConsistent = `
	AND NOT EXISTS (
		SELECT 1 FROM inbound_inventory_lines l
		WHERE l.shipment_id = s.id::text AND l.warehouse_id IS DISTINCT FROM s.warehouse_id
	)
`

// lockScopedCartonReceipt rechecks access after any existing ordered inventory
// locks have been acquired. Keep the actor, site and shipment stable until the
// receipt commits, including the historical acknowledgment path.
func lockScopedCartonReceipt(ctx context.Context, tx *sql.Tx, shipmentID, staffID, code string, status *string) error {
	return tx.QueryRowContext(ctx, `SELECT s.status FROM shipments s`+scopedCartonSiteJoin+`
		WHERE s.id = $1 AND UPPER(s.shipment_code) = UPPER($3)`+scopedCartonCityMatch+scopedCartonDestinationConsistent+`
		FOR UPDATE OF s FOR SHARE OF site, staff`, shipmentID, staffID, code).Scan(status)
}
