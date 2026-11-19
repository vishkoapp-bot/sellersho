package repository

import (
	"context"
	"database/sql"
	"errors"
)

// ErrInboundOutsideStaffScope masks missing and out-of-scope shipments alike.
var ErrInboundOutsideStaffScope = errors.New("inbound shipment not found")

// Only the shipment's recorded warehouse ID can establish its city.
const inboundStaffCityMatch = `site.id = inbound_shipments.warehouse_id AND site.id <> ''
	  AND staff.is_active IS TRUE AND staff.warehouse_city_confirmed IS TRUE
	  AND NULLIF(BTRIM(staff.warehouse_city), '') COLLATE "C" = BTRIM(site.city_name) COLLATE "C"`

const inboundStaffCityPredicate = `EXISTS (
	SELECT 1 FROM warehouses site JOIN users staff ON staff.id = $1
	WHERE ` + inboundStaffCityMatch + `
)`

func requireInboundStaffCityTx(ctx context.Context, tx *sql.Tx, inboundID string, lock bool) error {
	staffID := WarehouseStaffID(ctx)
	if staffID == "" {
		return nil
	}
	query := `SELECT 1 FROM inbound_shipments WHERE id = $2 AND ` + inboundStaffCityPredicate
	if lock {
		// Share locks prevent reassignment, deactivation, and site city edits
		// until the receiving mutation commits or rolls back.
		query = `SELECT 1 FROM inbound_shipments
			JOIN warehouses site ON site.id = inbound_shipments.warehouse_id
			JOIN users staff ON staff.id = $1
			WHERE inbound_shipments.id = $2 AND ` + inboundStaffCityMatch + `
			FOR UPDATE OF inbound_shipments FOR SHARE OF staff, site`
	}
	var found int
	err := tx.QueryRowContext(ctx, query, staffID, inboundID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInboundOutsideStaffScope
	}
	return err
}
