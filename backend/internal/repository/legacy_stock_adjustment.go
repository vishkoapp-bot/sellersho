package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var ErrStockAdjustmentInput = errors.New("موجودی معتبر و علت اصلاح را وارد کنید")
var ErrStockBelowHeld = errors.New("موجودی کل نباید از موجودی نگهداری‌شده در محل‌های دیگر کمتر باشد")
var ErrStockSKURequired = errors.New("برای اصلاح موجودی این کالا، تنوع دقیق را انتخاب کنید")
var ErrStockOriginRequired = errors.New("برای اعلام موجودی جدید، انبار فعال متعلق به تأمین‌کننده را مشخص کنید")
var ErrStockOwnership = errors.New("کالای متعلق به این تأمین‌کننده یافت نشد")
var ErrStockProjectionLimit = errors.New("مجموع موجودی تنوع‌های فعال از سقف مجاز بیشتر می‌شود؛ تعداد کمتری وارد کنید")

// Caller preholds supplier/product guards. Balance IDs are locked bytewise;
// only the named local legacy pool changes. Every other sellable lot is held.
func setSupplierSellableTotalTx(ctx context.Context, tx *sql.Tx, product, supplier string, total int, reason, actor string) (int, error) {
	if total < 0 || int64(total) > 2147483647 || strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return 0, ErrStockAdjustmentInput
	}
	var version, oldProjection int
	var owner string
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id,schema_version,stock FROM products WHERE id=$1 FOR UPDATE`, product).Scan(&owner, &version, &oldProjection); err != nil {
		return 0, err
	}
	if owner != supplier {
		return 0, ErrStockOwnership
	}
	if version >= 2 {
		return 0, ErrStockSKURequired
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM inventory_balances WHERE product_id=$1 ORDER BY id COLLATE "C" FOR UPDATE`, product)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	location, originErr := supplierDeclaredLocationTx(ctx, tx, product, supplier)
	if originErr != nil && !errors.Is(originErr, sql.ErrNoRows) {
		return 0, originErr
	}
	var held int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE product_id=$1 AND status='sellable' AND NOT(variant_id IS NULL AND owner_type='supplier' AND location_type='supplier' AND location_key=$2)`, product, location).Scan(&held); err != nil {
		return 0, err
	}
	if int64(total) < held {
		return 0, ErrStockBelowHeld
	}
	target := int64(total) - held
	if originErr != nil && target > 0 {
		return 0, ErrStockOriginRequired
	}
	if originErr == nil {
		var id, storedSupplier string
		var previous int
		err = tx.QueryRowContext(ctx, `SELECT id,COALESCE(supplier_id,''),quantity FROM inventory_balances WHERE product_id=$1 AND variant_id IS NULL AND owner_type='supplier' AND location_type='supplier' AND location_key=$2 AND status='sellable'`, product, location).Scan(&id, &storedSupplier, &previous)
		if err != nil && err != sql.ErrNoRows {
			return 0, err
		}
		if err == nil && storedSupplier != supplier {
			return 0, ErrStockOwnership
		}
		delta := target - int64(previous)
		if delta != 0 {
			if id == "" {
				_, err = tx.ExecContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES($1,$2,$3,'supplier','supplier',$4,'sellable',$5,FALSE)`, "INV-"+uuid.NewString(), product, supplier, location, target)
			} else {
				var result sql.Result
				result, err = tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=$2,updated_at=NOW() WHERE id=$1 AND quantity=$3`, id, target, previous)
				if err == nil {
					n, e := result.RowsAffected()
					if e != nil {
						return 0, e
					}
					if n != 1 {
						return 0, ErrInsufficientInventory
					}
				}
			}
			if err != nil {
				return 0, err
			}
			from, to, quantity := "adjustment", "sellable", delta
			if delta < 0 {
				from, to, quantity = "sellable", "adjustment", -delta
			}
			metadata, _ := json.Marshal(map[string]interface{}{"reason": strings.TrimSpace(reason), "requestedTotal": total, "heldSellable": held, "previousSupplierStock": previous, "supplierStock": target, "previousProjection": oldProjection})
			if _, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$7,'legacy_stock_adjustment',$2,$8,NULLIF($9,''),$10::jsonb)`, "IM-"+uuid.NewString(), product, supplier, quantity, from, to, location, "legacy-adjust:"+uuid.NewString(), actor, string(metadata)); err != nil {
				return 0, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) SELECT $2::varchar,$1::varchar,$3::varchar,'supplier','warehouse','warehouse:legacy_migration','sellable',0,TRUE WHERE NOT EXISTS(SELECT 1 FROM inventory_balances WHERE product_id=$1::varchar)`, product, "INV-"+uuid.NewString(), supplier); err != nil {
		return 0, err
	}
	if err = syncProductStockTx(ctx, tx, product); err != nil {
		return 0, err
	}
	var actual int
	if err = tx.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=$1`, product).Scan(&actual); err != nil {
		return 0, err
	}
	if actual != total {
		return 0, fmt.Errorf("%w: stock projection requires reconciliation", ErrInsufficientInventory)
	}
	return actual, nil
}
