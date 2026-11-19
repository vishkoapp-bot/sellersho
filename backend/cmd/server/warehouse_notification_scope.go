package main

import (
	"context"
	"net/http"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

type warehouseInboxKey struct{}

// Shared inboxes use refreshed claims too, so a staff user cannot bypass
// warehouse scope by opening the same outbox through a second capability.
func notificationContext(r *http.Request) context.Context {
	ctx := r.Context()
	claims, _ := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if repository.WarehouseStaffID(ctx) == "" && claims.HasCapability("warehouse_staff") {
		return repository.WithWarehouseStaffID(ctx, claims.UserID)
	}
	return ctx
}

// Every query using this predicate binds the authenticated recipient to $1.
// Match persisted work IDs, never a city, site, or supplier claimed by payload.
// Supplier returns in transit have no recorded central destination to verify.
func warehouseNotificationFilter(ctx context.Context, recipient string) string {
	staff := repository.WarehouseStaffID(ctx)
	if staff == "" {
		return ""
	}
	if staff != recipient {
		return " AND FALSE"
	}
	const cityMatch = `site.id <> '' AND staff.is_active AND staff.warehouse_city_confirmed
		AND NULLIF(BTRIM(staff.warehouse_city), '') COLLATE "C" = BTRIM(site.city_name) COLLATE "C"`
	filter := `CASE
		WHEN kind IN ('` + repository.NotifKindInboundCreated + `', '` + repository.NotifKindInboundDispatched + `') THEN EXISTS (
			SELECT 1 FROM inbound_shipments work
			JOIN warehouses site ON site.id = work.warehouse_id
			JOIN users staff ON staff.id = $1::uuid
			WHERE jsonb_typeof(notification_outbox.payload->'inbound_id') = 'string'
			  AND work.id = notification_outbox.payload->>'inbound_id' AND ` + cityMatch + `)
		WHEN kind IN ('` + repository.NotifKindReturnRequested + `', '` + repository.NotifKindReturnShipped + `') THEN EXISTS (
			SELECT 1 FROM customer_returns work
			JOIN warehouses site ON site.id = work.warehouse_id
			JOIN users staff ON staff.id = $1::uuid
			WHERE jsonb_typeof(notification_outbox.payload->'return_id') = 'string'
			  AND work.id::text = notification_outbox.payload->>'return_id'
			  AND NULLIF(work.destination_supplier_warehouse_id, '') IS NULL AND ` + cityMatch + `)
		WHEN kind = '` + repository.NotifKindSupplierReturnTransit + `' THEN FALSE`
	if ctx.Value(warehouseInboxKey{}) == true {
		return " AND (" + filter + " ELSE FALSE END)"
	}
	return " AND (" + filter + " ELSE TRUE END)"
}
