package repository

import "context"

type warehouseStaffContextKey struct{}

// WithWarehouseStaffID carries the authenticated staff principal to queries that
// apply city scope. Callers must never source this ID from request data.
func WithWarehouseStaffID(ctx context.Context, staffID string) context.Context {
	return context.WithValue(ctx, warehouseStaffContextKey{}, staffID)
}

// WarehouseStaffID returns the authenticated principal for a city-scoped
// warehouse operation, or an empty string for an unscoped internal caller.
func WarehouseStaffID(ctx context.Context) string {
	id, _ := ctx.Value(warehouseStaffContextKey{}).(string)
	return id
}
