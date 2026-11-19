package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrSKUPhysicalUnverified = errors.New("ابعاد و وزن همین تنوع کالا باید در انبار تأیید شود")

type SKUPhysical struct {
	LengthMM    int64  `json:"lengthMm"`
	WidthMM     int64  `json:"widthMm"`
	HeightMM    int64  `json:"heightMm"`
	WeightGrams int64  `json:"weightGrams"`
	Verified    bool   `json:"verified"`
	Revision    string `json:"revision"`
	Source      string `json:"source"`
}

// Read-only resolver. Callers making reservations/placements prehold product
// and SKU guards; advisory suggestions may observe a snapshot and revalidate.
func loadEffectiveSKUPhysicalTx(ctx context.Context, tx *sql.Tx, product, variant string) (SKUPhysical, error) {
	var out SKUPhysical
	err := tx.QueryRowContext(ctx, `SELECT length_mm,width_mm,height_mm,weight_grams,verified,revision,source FROM inventory_effective_measurements WHERE product_id=$1 AND variant_id=$2`, product, variant).Scan(&out.LengthMM, &out.WidthMM, &out.HeightMM, &out.WeightGrams, &out.Verified, &out.Revision, &out.Source)
	return out, err
}

// Parent guards serialize task discovery with assignment/confirmation. Only
// pending work is reset; completed placement/occupancy snapshots never change.
func invalidatePendingPhysicalReservationsTx(ctx context.Context, tx *sql.Tx, product, variant string) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('putaway_tasks') IS NOT NULL AND to_regclass('putaway_capacity_reservations') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT pt.id,COALESCE(r.id,''),COALESCE(r.bin_code,'') FROM putaway_tasks pt LEFT JOIN putaway_capacity_reservations r ON r.task_id=pt.id AND r.status='active' WHERE pt.product_id=$1 AND pt.status IN ('pending','assigned') AND (($2::text<>'' AND pt.variant_id::text=$2) OR ($2::text='' AND NOT EXISTS(SELECT 1 FROM product_variants v WHERE v.id::text=pt.variant_id::text AND (v.length_mm IS NOT NULL OR v.width_mm IS NOT NULL OR v.height_mm IS NOT NULL OR v.weight_grams IS NOT NULL))))`, product, variant)
	if err != nil {
		return err
	}
	var tasks []string
	var locks []TransactionRowLock
	for rows.Next() {
		var task, reservation, bin string
		if err = rows.Scan(&task, &reservation, &bin); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, task)
		locks = append(locks, TransactionRowLock{Table: "putaway_tasks", ID: task})
		if reservation != "" {
			locks = append(locks, TransactionRowLock{Table: "warehouse_bins", ID: bin}, TransactionRowLock{Table: "putaway_capacity_reservations", ID: reservation})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, locks...); err != nil {
		return err
	}
	for _, task := range tasks {
		if _, err = tx.ExecContext(ctx, `UPDATE putaway_capacity_reservations SET status='released',released_at=NOW() WHERE task_id=$1 AND status='active'`, task); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE putaway_tasks SET status='pending',bin_location=NULL,assigned_to=NULL WHERE id=$1 AND status='assigned'`, task); err != nil {
			return err
		}
	}
	return nil
}

func (r *SupplierRepository) RecordSKUPhysicalMeasurement(ctx context.Context, product, variant, actor string, l, w, h, g int, reason string) (SKUPhysical, error) {
	var out SKUPhysical
	if ValidateVariantPhysicalValues(&g, &l, &w, &h) != nil || strings.TrimSpace(reason) == "" || len(reason) > 2000 || strings.TrimSpace(actor) == "" {
		return out, ErrSKUPhysicalUnverified
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var supplier string
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(supplier_id,'') FROM products WHERE id=$1`, product).Scan(&supplier); err != nil {
		return out, err
	}
	locks := []TransactionRowLock{{Table: "products", ID: product}}
	if supplier != "" {
		locks = append(locks, TransactionRowLock{Table: "suppliers", ID: supplier})
	}
	if variant != "" {
		locks = append(locks, TransactionRowLock{Table: "product_variants", ID: variant})
	}
	if err = LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, locks...); err != nil {
		return out, err
	}
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(supplier_id,'') FROM products WHERE id=$1`, product).Scan(&current); err != nil {
		return out, err
	}
	if current != supplier {
		return out, ErrStockOwnership
	}
	var oldL, oldW, oldH, oldG sql.NullInt64
	if variant == "" {
		err = tx.QueryRowContext(ctx, `SELECT length_mm,width_mm,height_mm,weight_grams FROM products WHERE id=$1`, product).Scan(&oldL, &oldW, &oldH, &oldG)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT length_mm,width_mm,height_mm,weight_grams FROM product_variants WHERE id=$1 AND product_id=$2`, variant, product).Scan(&oldL, &oldW, &oldH, &oldG)
	}
	if err != nil {
		return out, err
	}
	if err = invalidatePendingPhysicalReservationsTx(ctx, tx, product, variant); err != nil {
		return out, err
	}
	scope := "product"
	if variant != "" {
		scope = "sku"
	}
	revision := "MEAS-" + uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO inventory_measurement_revisions(id,product_id,variant_id,scope,length_mm,width_mm,height_mm,weight_grams,verified,reason,actor_user_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,TRUE,$9,$10)`, revision, product, variant, scope, l, w, h, g, strings.TrimSpace(reason), actor); err != nil {
		return out, err
	}
	if variant == "" {
		_, err = tx.ExecContext(ctx, `UPDATE products SET length_mm=$2,width_mm=$3,height_mm=$4,weight_grams=$5,measurement_status='verified',measurement_revision_id=$6,measurement_source='warehouse_measured',measurement_version=measurement_version+1,updated_at=NOW() WHERE id=$1`, product, l, w, h, g, revision)
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO product_measurement_events(id,product_id,old_length_mm,old_width_mm,old_height_mm,old_weight_grams,new_length_mm,new_width_mm,new_height_mm,new_weight_grams,reason,actor_user_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, revision, product, nullInt(oldL), nullInt(oldW), nullInt(oldH), nullInt(oldG), l, w, h, g, reason, actor)
		}
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE product_variants SET length_mm=$3,width_mm=$4,height_mm=$5,weight_grams=$6,measurement_status='verified',measurement_revision_id=$7,measurement_source='warehouse_measured',measurement_version=measurement_version+1 WHERE id=$1 AND product_id=$2`, variant, product, l, w, h, g, revision)
	}
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return SKUPhysical{LengthMM: int64(l), WidthMM: int64(w), HeightMM: int64(h), WeightGrams: int64(g), Verified: true, Revision: revision, Source: scope}, nil
}
