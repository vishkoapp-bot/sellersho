package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var ErrReturnBinConflict = errors.New("موجودی مرجوعی، اندازه‌گیری یا ظرفیت قفسه نیاز به بررسی دارد؛ اطلاعات را تازه کنید")
var ErrReturnBinInput = errors.New("قلم مرجوعی، قفسه، تعداد صحیح مثبت، دلیل و کلید درخواست الزامی است")

type ReturnBinPlacementResult struct {
	ID                  string    `json:"id"`
	ReturnID            string    `json:"returnId"`
	ReturnItemID        string    `json:"returnItemId"`
	BinCode             string    `json:"binCode"`
	Quantity            int       `json:"quantity"`
	VolumeCM3           int64     `json:"volumeCm3"`
	WeightGrams         int64     `json:"weightGrams"`
	MeasurementRevision string    `json:"measurementRevision"`
	ReceivedAt          time.Time `json:"receivedAt"`
}

type ReturnBinSource struct {
	ReturnItemID      string               `json:"returnItemId"`
	ProductID         string               `json:"productId"`
	VariantID         string               `json:"variantId"`
	ProductTitle      string               `json:"productTitle"`
	VariantName       string               `json:"variantName"`
	VariantSPNC       string               `json:"variantSpnc"`
	WarehouseID       string               `json:"warehouseId"`
	QCStatus          string               `json:"qcStatus"`
	Quantity          int                  `json:"quantity"`
	AvailableQuantity int64                `json:"availableQuantity"`
	SourceProven      bool                 `json:"sourceProven"`
	Physical          SKUPhysical          `json:"physical"`
	Bins              []ReturnBinCandidate `json:"bins"`
}

type ReturnBinCandidate struct {
	BinCode              string `json:"binCode"`
	RemainingVolumeCM3   int64  `json:"remainingVolumeCm3"`
	RemainingWeightGrams int64  `json:"remainingWeightGrams"`
}

// Source availability is advisory. The atomic SQL command revalidates the
// receipt, exact surviving balance, measurement and capacity under parent locks.
func (r *WarehouseRepository) ListCustomerReturnBinSources(ctx context.Context, returnID string) ([]ReturnBinSource, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := uuid.Parse(returnID); err != nil {
		return nil, ErrReturnBinInput
	}
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM customer_returns WHERE id=$1)`, returnID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, sql.ErrNoRows
	}
	rows, err := r.db.QueryContext(ctx, `SELECT s.return_item_id::text,s.product_id,s.variant_id,s.product_title,s.variant_name,s.variant_spnc,COALESCE(s.warehouse_id,''),s.qc_status,s.qty,CASE WHEN s.source_proven THEN GREATEST(s.available_quantity,0) ELSE 0 END,s.source_proven,COALESCE(m.length_mm,0),COALESCE(m.width_mm,0),COALESCE(m.height_mm,0),COALESCE(m.weight_grams,0),COALESCE(m.verified,FALSE),COALESCE(m.revision,''),COALESCE(m.source,'') FROM warehouse_customer_return_bin_sources s LEFT JOIN inventory_effective_measurements m ON m.product_id=s.product_id AND m.variant_id=s.variant_id WHERE s.return_id=$1 ORDER BY s.return_item_id`, returnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReturnBinSource{}
	for rows.Next() {
		var s ReturnBinSource
		if err = rows.Scan(&s.ReturnItemID, &s.ProductID, &s.VariantID, &s.ProductTitle, &s.VariantName, &s.VariantSPNC, &s.WarehouseID, &s.QCStatus, &s.Quantity, &s.AvailableQuantity, &s.SourceProven, &s.Physical.LengthMM, &s.Physical.WidthMM, &s.Physical.HeightMM, &s.Physical.WeightGrams, &s.Physical.Verified, &s.Physical.Revision, &s.Physical.Source); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		s := &out[i]
		s.Bins = []ReturnBinCandidate{}
		if !s.SourceProven || !s.Physical.Verified || s.AvailableQuantity <= 0 {
			continue
		}
		candidates, e := r.db.QueryContext(ctx, `SELECT b.bin_code,GREATEST(b.capacity_volume_cm3-b.occupied_volume_cm3-COALESCE(r.v,0),0),GREATEST(b.max_weight_grams-b.occupied_weight_grams-COALESCE(r.w,0),0) FROM warehouse_bins b LEFT JOIN (SELECT bin_code,SUM(volume_cm3) v,SUM(weight_grams) w FROM putaway_capacity_reservations WHERE status='active' GROUP BY bin_code) r ON r.bin_code=b.bin_code LEFT JOIN (SELECT bin_code,SUM(remaining_quantity) q,SUM(remaining_volume_cm3) v,SUM(remaining_weight_grams) w FROM warehouse_remaining_placements GROUP BY bin_code) p ON p.bin_code=b.bin_code WHERE b.warehouse_id=$1 AND b.capacity_status='verified' AND b.capacity_volume_cm3 IS NOT NULL AND b.max_weight_grams IS NOT NULL AND NOT EXISTS(SELECT 1 FROM warehouse_occupancy_exceptions x WHERE x.bin_code=b.bin_code) AND (b.quantity::bigint,b.occupied_volume_cm3,b.occupied_weight_grams)=(COALESCE(p.q,0),COALESCE(p.v,0),COALESCE(p.w,0)) ORDER BY b.bin_code`, s.WarehouseID)
		if e != nil {
			return nil, e
		}
		for candidates.Next() {
			var c ReturnBinCandidate
			if e = candidates.Scan(&c.BinCode, &c.RemainingVolumeCM3, &c.RemainingWeightGrams); e != nil {
				candidates.Close()
				return nil, e
			}
			s.Bins = append(s.Bins, c)
		}
		e = candidates.Err()
		candidates.Close()
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}

func (r *WarehouseRepository) PlaceCustomerReturnItemInBin(ctx context.Context, returnID, returnItemID, bin string, quantity int, reason, actor, key string) (ReturnBinPlacementResult, error) {
	var out ReturnBinPlacementResult
	reason = strings.TrimSpace(reason)
	bin = strings.TrimSpace(bin)
	key = strings.TrimSpace(key)
	returnUUID, err := uuid.Parse(returnID)
	if err != nil {
		return out, ErrReturnBinInput
	}
	itemUUID, err := uuid.Parse(returnItemID)
	if err != nil {
		return out, ErrReturnBinInput
	}
	returnID, returnItemID = returnUUID.String(), itemUUID.String()
	if quantity <= 0 || int64(quantity) > 2147483647 || reason == "" || len(reason) > 2000 || bin == "" || len(bin) > 200 || strings.TrimSpace(actor) == "" || key == "" || len(key) > 200 {
		return out, ErrReturnBinInput
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Resolve immutable replay first: a later measurement edit or exhausted source
	// cannot make a successful request appear to fail after a lost response.
	replay := func() (bool, error) {
		var oldReason string
		err := r.db.QueryRowContext(ctx, `SELECT id,return_id::text,return_item_id::text,bin_code,quantity,volume_cm3,weight_grams,measurement_revision_id,received_at,reason FROM warehouse_return_bin_entries WHERE actor_user_id=$1 AND idempotency_key=$2`, actor, key).Scan(&out.ID, &out.ReturnID, &out.ReturnItemID, &out.BinCode, &out.Quantity, &out.VolumeCM3, &out.WeightGrams, &out.MeasurementRevision, &out.ReceivedAt, &oldReason)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if out.ReturnID != returnID || out.ReturnItemID != returnItemID || out.BinCode != bin || out.Quantity != quantity || oldReason != reason {
			return false, ErrReturnBinConflict
		}
		return true, nil
	}
	if found, err := replay(); found || err != nil {
		return out, err
	}
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM return_items WHERE id=$1 AND return_id=$2)`, returnItemID, returnID).Scan(&exists); err != nil {
		return out, err
	}
	if !exists {
		return out, sql.ErrNoRows
	}
	// BEFORE derives every source/physical field. AFTER applies debit/credit,
	// immutable transfer/placement, bin counters and receipt-dated storage together.
	_, err = r.db.ExecContext(ctx, `INSERT INTO warehouse_return_bin_entries(id,return_id,return_item_id,bin_code,quantity,reason,actor_user_id,idempotency_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, "RBP-"+uuid.NewString(), returnID, returnItemID, bin, quantity, reason, actor, key)
	if err != nil {
		var pg *pq.Error
		if errors.As(err, &pg) {
			// A concurrent successful command can exhaust the source inside the
			// BEFORE guard before uniqueness is checked. Resolve its immutable
			// result after either conflict; never make a lost response ambiguous.
			if pg.Code == "23505" || pg.Code == "P0001" || pg.Code == "23514" || pg.Code == "22003" {
				if found, replayErr := replay(); found || replayErr != nil {
					return out, replayErr
				}
			}
			if pg.Code == "P0001" || pg.Code == "23514" || pg.Code == "22003" {
				return out, ErrReturnBinConflict
			}
		}
		return out, err
	}
	if _, err = replay(); err != nil {
		return out, err
	}
	return out, nil
}
