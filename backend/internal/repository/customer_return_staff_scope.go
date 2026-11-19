package repository

// customerReturnCentralStaffCityPredicate assumes the outer row is from
// customer_returns. Supplier destinations never fall back to a central site.
func customerReturnCentralStaffCityPredicate(staffParam string) string {
	return ` AND NULLIF(customer_returns.destination_supplier_warehouse_id, '') IS NULL
		AND EXISTS (
			SELECT 1 FROM warehouses site JOIN users staff ON staff.id = ` + staffParam + `
			WHERE site.id = NULLIF(customer_returns.warehouse_id, '')
			  AND staff.is_active IS TRUE AND staff.warehouse_city_confirmed IS TRUE
			  AND NULLIF(BTRIM(staff.warehouse_city), '') COLLATE "C" = BTRIM(site.city_name) COLLATE "C"
		)`
}
