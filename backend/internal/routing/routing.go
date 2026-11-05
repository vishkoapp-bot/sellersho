// Package routing holds the ONE routing truth for supplier
// fulfilment (gap 13). Before this package existed, order creation
// routed by products.fulfillment_method alone while the warehouse
// queue re-derived routing from the supplier_shipping_methods
// toggles — the two diverged: a direct-only-enabled supplier's
// warehouse-flagged items routed direct in the queue but got no
// shipment at creation, falling between the two fulfilers.
//
// The canonical semantics are the queue's documented ones, now
// adopted as the platform rule and applied identically at order
// creation, at the standalone shipping-quote endpoint, and (via the
// persisted order_item_origins rows) in the warehouse queue:
//
//   - direct enabled, warehouse NOT enabled  -> supplier_direct,
//     whatever the product flag says;
//   - both enabled                           -> the product flag
//     decides;
//   - anything else (warehouse only, neither, no method rows at
//     all)                                   -> warehouse.
//
// The default is deliberately fail-safe: a supplier with no
// shipping-method rows routes to the warehouse, never direct.
package routing

// ResolveItemRouting resolves one order line's fulfilment routing
// from the product's fulfillment_method flag and the supplier's
// shipping-method status map (method -> status, e.g.
// {"direct": "enabled", "warehouse": "disabled"} — the shape
// SupplierRepository.ListShippingMethodsBySupplierIDs returns).
// A nil/empty map means "no methods enabled". The result is
// "supplier_direct" or "warehouse" — the same vocabulary as
// products.fulfillment_method and orders.fulfillment_method.
func ResolveItemRouting(productFlag string, methods map[string]string) string {
	direct := methods["direct"] == "enabled"
	warehouse := methods["warehouse"] == "enabled"
	switch {
	case direct && !warehouse:
		return "supplier_direct"
	case direct && warehouse:
		if productFlag == "supplier_direct" {
			return "supplier_direct"
		}
		return "warehouse"
	default:
		return "warehouse"
	}
}
