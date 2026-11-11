package repository

import (
	"context"
	"fmt"
)

func (r *SupplierRepository) UpdateProductStockForVariant(ctx context.Context, supplierID, productID, variantID string, newStock int, reason, actor string) error {
	if variantID == "" {
		return r.UpdateProductStock(ctx, supplierID, productID, newStock, reason)
	}
	if newStock < 0 {
		return fmt.Errorf("stock cannot be negative")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = SetTrackedSKUStockTx(ctx, tx, productID, supplierID, variantID, newStock, reason, actor); err != nil {
		return err
	}
	if err = insertSupplierAuditTx(ctx, tx, supplierID, "supplier_sku_stock_update", map[string]interface{}{"product_id": productID, "variant_id": variantID, "new_stock": newStock, "reason": reason}); err != nil {
		return err
	}
	return tx.Commit()
}
