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
	ProductID           string           `json:"product_id"`
	VariantID           string           `json:"variant_id"`
	PreviousStock       int              `json:"previous_stock"`
	Stock               int              `json:"stock"`
	Delta               int              `json:"delta"`
	RetiredCodes        []string         `json:"retired_codes"`
	RetiredPrintedCodes []string         `json:"retired_printed_codes"`
	CoverageGap         *UnitCoverageGap `json:"coverage_gap"`
	ProductStock        int              `json:"-"`
}

func (r *SupplierRepository) AdjustProductStock(ctx context.Context, supplier, product, variant string, total int, reason, actor string) (StockAdjustmentResult, error) {
	result := StockAdjustmentResult{ProductID: product, RetiredCodes: []string{}, RetiredPrintedCodes: []string{}}
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
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(supplier_id=$2,FALSE),stock FROM products WHERE id=$1`, product, supplier).Scan(&owned, &result.PreviousStock); err != nil {
		return result, err
	}
	if !owned {
		return result, ErrStockOwnership
	}
	action := "supplier_stock_update"
	mode := "legacy"
	if variant == "" {
		resolved, resolveErr := ResolveLegacyPurchaseVariant(ctx, tx, product, "")
		if resolveErr != nil {
			return result, resolveErr
		}
		variant = resolved
	}
	result.VariantID = variant
	if variant != "" {
		if err = tx.QueryRowContext(ctx, `SELECT inventory_mode FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, variant, product).Scan(&mode); err != nil {
			return result, err
		}
		if err = lockExistingUnitCounterTx(ctx, tx, variant); err != nil {
			return result, err
		}
	}
	if mode != "tracked" {
		result.Stock, err = setSupplierSellableTotalTx(ctx, tx, product, supplier, total, reason, actor, r.inventoryHolds)
		if err != nil {
			return result, err
		}
		result.ProductStock = result.Stock
		result.Delta = result.Stock - result.PreviousStock
		if result.Delta > 0 {
			result.CoverageGap = &UnitCoverageGap{Shortfall: result.Delta, Reason: "legacy_pre_backfill"}
			if err = auditUnitGapTx(ctx, tx, product, variant, "inbound", "", result.Delta, 0); err != nil {
				return result, err
			}
		} else if result.Delta < 0 {
			if spncxStrict {
				return result, ErrUnitCoverageGap
			}
			result.CoverageGap = &UnitCoverageGap{Shortfall: -result.Delta, Reason: "legacy_pre_backfill"}
			if err = auditUnitGapTx(ctx, tx, product, variant, "adjust_decrease", "", -result.Delta, 0); err != nil {
				return result, err
			}
		}
	} else {
		if err = setTrackedSKUStockTx(ctx, tx, product, supplier, variant, total, reason, actor, r.inventoryHolds, &result); err != nil {
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
