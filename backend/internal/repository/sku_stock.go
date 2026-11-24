package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// SetTrackedSKUStockTx changes declared stock at the supplier only. Warehouse
// and return lots retain their physical quantities; an aggregate edit cannot
// silently remove units held elsewhere or change another SKU/legacy pool.
func SetTrackedSKUStockTx(ctx context.Context, tx *sql.Tx, productID, supplierID, variantID string, total int, reason, actor string) error {
	return setTrackedSKUStockTx(ctx, tx, productID, supplierID, variantID, total, reason, actor, nil, nil)
}

func setTrackedSKUStockTx(ctx context.Context, tx *sql.Tx, productID, supplierID, variantID string, total int, reason, actor string, holds *RedisClient, result *StockAdjustmentResult) error {
	if total < 0 || int64(total) > 2147483647 || strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return ErrStockAdjustmentInput
	}
	if err := lockSKUProductTx(ctx, tx, productID, variantID); err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT pv.is_active FROM product_variants pv JOIN products p ON p.id=pv.product_id WHERE pv.id=$1 AND p.id=$2 AND p.supplier_id=$3 AND pv.inventory_mode='tracked'`, variantID, productID, supplierID).Scan(&active); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return ErrStockOwnership
	}
	location, err := supplierDeclaredLocationTx(ctx, tx, productID, supplierID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrStockOriginRequired
		}
		return err
	}
	var held int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE product_id=$1 AND variant_id=$2 AND status='sellable' AND NOT(owner_type='supplier' AND location_type='supplier' AND location_key=$3)`, productID, variantID, location).Scan(&held); err != nil {
		return err
	}
	if int64(total) < held {
		return ErrStockBelowHeld
	}
	// The product's INT projection includes all active sellable SKU balances
	// (and the legacy pool only for an unmigrated product). The product guard
	// serializes this check with sibling updates and physical captures.
	if active {
		var other int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(ib.quantity),0) FROM products p JOIN inventory_balances ib ON ib.product_id=p.id LEFT JOIN product_variants pv ON pv.id=ib.variant_id WHERE p.id=$1 AND ib.status='sellable' AND ib.variant_id IS DISTINCT FROM $2::uuid AND ((ib.variant_id IS NULL AND p.schema_version<2) OR pv.is_active)`, productID, variantID).Scan(&other); err != nil {
			return err
		}
		if other < 0 || other > 2147483647-int64(total) {
			return ErrStockProjectionLimit
		}
	}
	target := total - int(held)
	var balanceID string
	var previous int
	err = tx.QueryRowContext(ctx, `SELECT id,quantity FROM inventory_balances WHERE product_id=$1 AND variant_id=$2 AND owner_type='supplier' AND location_type='supplier' AND location_key=$3 AND status='sellable' FOR UPDATE`, productID, variantID, location).Scan(&balanceID, &previous)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	delta := target - previous
	if result != nil {
		result.PreviousStock = previous + int(held)
		result.Delta = delta
	}
	if delta == 0 {
		return nil
	}
	key := "sku-adjust:" + uuid.NewString()
	metadata, _ := json.Marshal(map[string]interface{}{"reason": reason, "previousSupplierStock": previous, "supplierStock": target, "requestedTotal": total})
	if delta > 0 {
		if err := creditSKUInventoryTx(ctx, tx, productID, supplierID, variantID, delta, "sellable", "supplier", location, "sku_stock_adjustment", variantID, key, actor); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE inventory_movements SET metadata=$2::jsonb WHERE idempotency_key=$1`, key, string(metadata))
		return err
	}
	codes, printed, err := retireUnitsTx(ctx, tx, productID, variantID, balanceID, -delta, actor)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=$2,updated_at=NOW() WHERE id=$1 AND quantity=$3`, balanceID, target, previous)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInsufficientInventory
	}
	if err = refreshUnitTrackedLotTx(ctx, tx, balanceID); err != nil {
		return err
	}
	warehouse := strings.TrimPrefix(location, "supplier-warehouse:")
	if err = checkCodedStockHoldsTx(ctx, tx, holds, productID, variantID, warehouse); err != nil {
		return err
	}
	if result != nil {
		result.RetiredCodes = codes
		result.RetiredPrintedCodes = printed
		if len(codes) < -delta {
			result.CoverageGap = &UnitCoverageGap{Shortfall: -delta - len(codes), Reason: "legacy_pre_backfill"}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES($1,$2,$3,$4::uuid,$5,'sellable','adjustment',$6,$6,'sku_stock_adjustment',$4::uuid::text,$7,NULLIF($8,''),$9::jsonb)`, "IM-"+uuid.NewString(), productID, supplierID, variantID, -delta, location, key, actor, string(metadata))
	return err
}

// A mutation may leave enough coded stock in aggregate but remove the
// units needed at this exact supplier origin. Check the post-change SQL
// state while the caller's product guard still excludes new reservations.
func checkCodedStockHoldsTx(ctx context.Context, tx *sql.Tx, holds *RedisClient, product, variant, warehouse string) error {
	if holds == nil {
		return nil
	}
	aggregate := InventoryHoldRef{ProductID: product, VariantID: variant}
	origin := aggregate
	origin.SupplierWarehouseID = warehouse
	for _, ref := range []InventoryHoldRef{origin, aggregate} {
		held, err := holds.liveInventoryHeld(ctx, ref)
		if err != nil {
			return err
		}
		if held == 0 {
			continue
		}
		remaining, err := codedStockForReservation(ctx, tx, ref)
		if err != nil {
			return err
		}
		if remaining < held {
			return ErrStockBelowHeld
		}
	}
	return nil
}

// The hold index scores are expiry timestamps. Quantity keys outlive their
// scores, so a read of the maintained aggregate alone can overcount.
func (r *RedisClient) liveInventoryHeld(ctx context.Context, ref InventoryHoldRef) (int, error) {
	holds, _, prefix := holdKeys(ref)
	return r.Client.Eval(ctx, `
		local now = redis.call('TIME')
		local seconds = now[1] + math.floor(now[2] / 1000) / 1000
		local total = 0
		for _,order in ipairs(redis.call('ZRANGEBYSCORE', KEYS[1], string.format('(%0.3f', seconds), '+inf')) do
			total = total + (tonumber(redis.call('GET', ARGV[1] .. order)) or 0)
		end
		return total
	`, []string{holds}, prefix).Int()
}
