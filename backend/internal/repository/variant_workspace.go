package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var ErrVariantWorkspaceConflict = errors.New("variant_workspace_conflict")

type SupplierWorkspaceVariant struct {
	ID           string `json:"id"`
	ProductID    string `json:"productId"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	SPNC         string `json:"spnc"`
	Sellable     int    `json:"sellable"`
	Reserved     int    `json:"reserved"`
	Cost         *int64 `json:"cost"`
	ProductCost  int64  `json:"productCost"`
	PendingCost  *int64 `json:"pendingCost,omitempty"`
	PendingClear bool   `json:"pendingClear,omitempty"`
	Active       bool   `json:"active"`
	CanEditStock bool   `json:"canEditStock"`
	CanEditCost  bool   `json:"canEditCost"`
}

func (r *SupplierRepository) ListWorkspaceVariants(ctx context.Context, supplierID string, canStock, canPrice bool) ([]SupplierWorkspaceVariant, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH quantity AS (
			SELECT product_id, variant_id,
			       COALESCE(SUM(quantity) FILTER (WHERE status='sellable'),0) AS sellable,
			       COALESCE(SUM(quantity) FILTER (WHERE status='sales_reserved'),0) AS reserved
			FROM inventory_balances WHERE supplier_id=$1 AND variant_id IS NOT NULL AND status IN ('sellable','sales_reserved')
			GROUP BY product_id, variant_id
		)
		SELECT v.id,p.id,p.title,v.variant_name,v.spnc,
		       COALESCE(q.sellable,0),COALESCE(q.reserved,0),v.supplier_base_cost,p.supplier_base_cost,
		       pi.new_reference,COALESCE(pi.clear_on_approve,FALSE),
		       p.is_active AND v.is_active,
		       $2 AND v.inventory_mode='tracked' AND (p.fulfillment_method IS DISTINCT FROM 'supplier_direct' OR COALESCE(origin.is_active,FALSE))
		FROM products p JOIN product_variants v ON v.product_id=p.id
		LEFT JOIN supplier_warehouses origin ON origin.id=p.origin_supplier_warehouse_id AND origin.supplier_id=p.supplier_id
		LEFT JOIN quantity q ON q.product_id=p.id AND q.variant_id=v.id
		LEFT JOIN LATERAL (
		    SELECT new_reference,clear_on_approve FROM supplier_price_increases
		    WHERE product_id=p.id AND variant_id=v.id::text AND supplier_id=$1 AND status='pending'
		    ORDER BY created_at DESC LIMIT 1
		) pi ON TRUE
		WHERE p.supplier_id=$1 ORDER BY p.title, p.id, v.created_at, v.id`, supplierID, canStock)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SupplierWorkspaceVariant{}
	for rows.Next() {
		var row SupplierWorkspaceVariant
		if err := rows.Scan(&row.ID, &row.ProductID, &row.Title, &row.Name, &row.SPNC,
			&row.Sellable, &row.Reserved, &row.Cost, &row.ProductCost,
			&row.PendingCost, &row.PendingClear, &row.Active, &row.CanEditStock); err != nil {
			return nil, err
		}
		row.CanEditCost = canPrice
		items = append(items, row)
	}
	return items, rows.Err()
}

func (r *StoreRepository) HiddenWorkspaceVariants(ctx context.Context, storeID string, ids []string) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT variant_id FROM store_variant_visibility WHERE store_id=$1 AND product_id=ANY($2) AND is_visible=FALSE
	`, storeID, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

type VariantOverridePrecondition struct {
	Value *float64 `json:"value"`
}

func (r *PricingRepository) SetWorkspaceVariantMargin(ctx context.Context, store domain.Store, override pricing.StoreVariantOverride, desired *float64, expected VariantOverridePrecondition) error {
	if _, err := uuid.Parse(override.VariantID); err != nil {
		return ErrVariantWorkspaceConflict
	}
	var eligible bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM store_products sp JOIN product_variants v ON v.product_id=sp.product_id
		JOIN products p ON p.id=sp.product_id WHERE sp.store_id=$1 AND sp.product_id=$2 AND v.id=$3
		AND v.is_active AND p.is_active)`, override.StoreID, override.ProductID, override.VariantID).Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return ErrVariantWorkspaceConflict
	}
	catalog := NewCatalogRepository(r.db)
	products, err := catalog.GetProductsByIDs(ctx, []string{override.ProductID})
	if err != nil {
		return err
	}
	if len(products) != 1 {
		return ErrVariantWorkspaceConflict
	}
	locks, err := r.ListStoreMarginLocks(ctx, override.StoreID)
	if err != nil {
		return err
	}
	settings, err := r.GetPlatformPricingSettings(ctx)
	if err != nil {
		return err
	}
	if pricing.ResolveReferencePricingForStoreProductVariantWithCost(products[0], products[0].SupplierBaseCost, override.VariantID, store, settings, locks, nil).AdminLocked {
		return ErrVariantWorkspaceConflict
	}
	if desired != nil {
		if _, err := pricing.ParsePercent(*desired); err != nil || *desired < math.Max(0, settings.MinProfitMargin) ||
			*desired > pricing.EffectiveMarginCeiling(override.ProductID, settings) {
			return ErrVariantWorkspaceConflict
		}
	}
	args := []any{override.StoreID, override.ProductID, override.VariantID}
	var write sql.Result
	switch {
	case desired == nil && expected.Value == nil:
		// Verify the inherited baseline without modifying a historical fixed row.
	case desired == nil:
		write, err = r.db.ExecContext(ctx, `DELETE FROM store_variant_overrides
			WHERE store_id=$1 AND product_id=$2 AND variant_id=$3 AND override_value=$4 AND mode='percent'`, append(args, *expected.Value)...)
	case expected.Value == nil:
		// A historical fixed override resolves like an unset percent override.
		write, err = r.db.ExecContext(ctx, `INSERT INTO store_variant_overrides(store_id,product_id,variant_id,override_value,mode)
			VALUES($1,$2,$3,$4,'percent') ON CONFLICT(store_id,product_id,variant_id)
			DO UPDATE SET override_value=EXCLUDED.override_value,mode=EXCLUDED.mode,updated_at=NOW()
			WHERE store_variant_overrides.mode='fixed'`, append(args, *desired)...)
	default:
		write, err = r.db.ExecContext(ctx, `UPDATE store_variant_overrides SET override_value=$4,updated_at=NOW()
			WHERE store_id=$1 AND product_id=$2 AND variant_id=$3 AND override_value=$5 AND mode='percent'`, append(args, *desired, *expected.Value)...)
	}
	if err != nil {
		return err
	}
	if write != nil {
		count, err := write.RowsAffected()
		if err != nil || count != 0 {
			return err
		}
	}
	current, err := r.GetStoreVariantOverride(ctx, override.StoreID, override.ProductID, override.VariantID)
	if err != nil {
		return err
	}
	if (desired == nil && (current == nil || current.Mode == "fixed")) ||
		(desired != nil && current != nil && current.Mode == "percent" && *desired == current.Value) {
		return nil
	}
	return ErrVariantWorkspaceConflict
}
