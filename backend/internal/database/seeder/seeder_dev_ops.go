package seeder

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Seeder) seedDevOperations(ctx context.Context, tx *sql.Tx) error {
	// 4. Dev Products & Niches (Upsert on unique business key slug)
	queryProduct := `
		INSERT INTO products (
			id, title, slug, category_slug, supplier_id, original_price,
			supplier_base_cost, platform_margin, is_original, stock, image_url, description
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (slug) DO UPDATE SET
			title = EXCLUDED.title,
			supplier_base_cost = EXCLUDED.supplier_base_cost,
			platform_margin = EXCLUDED.platform_margin,
			stock = EXCLUDED.stock,
			updated_at = NOW()
	`
	for _, p := range GetDevProducts() {
		if _, err := tx.ExecContext(ctx, queryProduct,
			p.ID, p.Title, p.Slug, p.CategorySlug, p.SupplierID, p.OriginalPrice,
			p.SupplierBaseCost, p.PlatformMargin, p.IsOriginal, p.Stock, p.ImageURL, p.Description,
		); err != nil {
			return fmt.Errorf("failed to seed product %s: %w", p.ID, err)
		}

		for _, nID := range p.NicheIDs {
			_, _ = tx.ExecContext(ctx, `INSERT INTO product_niches (product_id, niche_id) VALUES ($1, $2) ON CONFLICT (product_id, niche_id) DO NOTHING`, p.ID, nID)
		}
	}

	// 5. Dev Store Overrides
	queryOverride := `
		INSERT INTO store_product_overrides (store_id, product_id, override_value, mode)
		VALUES ('store-bibi', 'prod-1', 250000.00, 'fixed')
		ON CONFLICT (store_id, product_id) DO UPDATE SET override_value = EXCLUDED.override_value
	`
	if _, err := tx.ExecContext(ctx, queryOverride); err != nil {
		return fmt.Errorf("failed to seed store product override: %w", err)
	}

	// 6. Dev WMS Bins
	queryBin := `
		INSERT INTO warehouse_bins (bin_code, hall_name, aisle, shelf, capacity_percent, product_id, quantity)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (bin_code) DO UPDATE SET
			capacity_percent = EXCLUDED.capacity_percent,
			quantity = EXCLUDED.quantity
	`
	for _, b := range GetDevBins() {
		if _, err := tx.ExecContext(ctx, queryBin,
			b.BinCode, b.HallName, b.Aisle, b.Shelf, b.CapacityPercent, b.ProductID, b.Quantity,
		); err != nil {
			return fmt.Errorf("failed to seed warehouse bin %s: %w", b.BinCode, err)
		}
	}

	// 7. Dev Inbound Shipments
	queryInbound := `
		INSERT INTO inbound_shipments (id, supplier_id, supplier_name, items_count, cartons_count, carrier, truck_plate, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status
	`
	for _, inb := range GetDevInbounds() {
		if _, err := tx.ExecContext(ctx, queryInbound,
			inb.ID, inb.SupplierID, inb.SupplierName, inb.ItemsCount, inb.CartonsCount,
			inb.Carrier, inb.TruckPlate, inb.Status,
		); err != nil {
			return fmt.Errorf("failed to seed inbound shipment %s: %w", inb.ID, err)
		}
	}

	// 8. Dev Manifests
	queryManifest := `
		INSERT INTO shipping_manifests (id, carrier, driver_name, packages_count, status)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status
	`
	for _, m := range GetDevManifests() {
		if _, err := tx.ExecContext(ctx, queryManifest, m.ID, m.Carrier, m.DriverName, m.PackagesCount, m.Status); err != nil {
			return fmt.Errorf("failed to seed manifest %s: %w", m.ID, err)
		}
	}

	// 9. Dev Ledger Entries
	queryLedger := `
		INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
		VALUES ($1, NOW(), $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING
	`
	for _, l := range GetDevLedger() {
		if _, err := tx.ExecContext(ctx, queryLedger,
			l.ID, l.TargetAccount, l.TargetType, l.Type, l.Amount, l.Reason, l.Operator, l.RelatedOrderOrDispute,
		); err != nil {
			return fmt.Errorf("failed to seed ledger entry %s: %w", l.ID, err)
		}
	}

	return tx.Commit()
}
