package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (r *SupplierRepository) UpdateProductStockForVariant(ctx context.Context, supplierID, productID, variantID string, newStock int, reason, actor string) error {
	_, err := r.AdjustProductStock(ctx, supplierID, productID, variantID, newStock, reason, actor)
	return err
}

type StockAdjustmentResult struct {
	Stock        int `json:"stock"`
	ProductStock int `json:"productStock"`
}

func (r *SupplierRepository) AdjustProductStock(ctx context.Context, supplier, product, variant string, total int, reason, actor string) (StockAdjustmentResult, error) {
	var result StockAdjustmentResult
	if total < 0 || int64(total) > 2147483647 || strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return result, ErrStockAdjustmentInput
	}
	if variant != "" {
		parsed, err := uuid.Parse(variant)
		if err != nil {
			return result, ErrStockAdjustmentInput
		}
		variant = parsed.String()
	}
	reason = strings.TrimSpace(reason)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	locks := []TransactionRowLock{{Table: "suppliers", ID: supplier}, {Table: "products", ID: product}}
	if variant != "" {
		locks = append(locks, TransactionRowLock{Table: "product_variants", ID: variant})
	}
	if err = LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, locks...); err != nil {
		return result, err
	}
	var owned bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id=$1 AND supplier_id=$2)`, product, supplier).Scan(&owned); err != nil {
		return result, err
	}
	if !owned {
		return result, ErrStockOwnership
	}
	action := "supplier_stock_update"
	if variant == "" {
		result.Stock, err = setSupplierSellableTotalTx(ctx, tx, product, supplier, total, reason, actor)
		if err != nil {
			return result, err
		}
		result.ProductStock = result.Stock
	} else {
		if err = SetTrackedSKUStockTx(ctx, tx, product, supplier, variant, total, reason, actor); err != nil {
			return result, err
		}
		if err = tx.QueryRowContext(ctx, `SELECT pv.stock,p.stock FROM product_variants pv JOIN products p ON p.id=pv.product_id WHERE pv.id=$1 AND p.id=$2`, variant, product).Scan(&result.Stock, &result.ProductStock); err != nil {
			return result, err
		}
		action = "supplier_sku_stock_update"
	}
	if err = insertSupplierAuditTx(ctx, tx, supplier, action, map[string]interface{}{"product_id": product, "variant_id": variant, "new_stock": result.Stock, "product_stock": result.ProductStock, "reason": strings.TrimSpace(reason), "actor_user_id": actor}); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return StockAdjustmentResult{}, err
	}
	return result, nil
}
