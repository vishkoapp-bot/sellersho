package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/drup/platform/internal/warehouseopt"
	"github.com/google/uuid"
	"sort"
	"time"
)

type BinSuggestion struct {
	BinCode         string  `json:"binCode"`
	Distance        float64 `json:"distanceMeters"`
	RemainingVolume int64   `json:"remainingVolumeCm3"`
	RemainingWeight int64   `json:"remainingWeightGrams"`
}

func (r *WarehouseRepository) SuggestBins(ctx context.Context, taskID string, origin warehouseopt.Point) ([]BinSuggestion, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var product, variant, warehouse, warehouseID string
	var quantity int64
	err = tx.QueryRowContext(ctx, `SELECT pt.product_id,COALESCE(pt.variant_id::text,''),pt.quantity,COALESCE(wh.name,i.warehouse_name,p.warehouse_name,''),COALESCE(a.warehouse_id,'') FROM putaway_tasks pt JOIN products p ON p.id=pt.product_id LEFT JOIN inbound_shipments i ON i.id=pt.inbound_id LEFT JOIN inbound_putaway_allocations a ON a.task_id=pt.id LEFT JOIN warehouses wh ON wh.id=a.warehouse_id WHERE pt.id=$1 AND pt.status IN ('pending','assigned')`, taskID).Scan(&product, &variant, &quantity, &warehouse, &warehouseID)
	if err != nil {
		return nil, err
	}
	physical, err := loadEffectiveSKUPhysicalTx(ctx, tx, product, variant)
	if err != nil {
		return nil, err
	}
	if !physical.Verified || !origin.Valid() {
		return nil, ErrSKUPhysicalUnverified
	}
	volume, weight, err := warehouseopt.Capacity(physical.LengthMM, physical.WidthMM, physical.HeightMM, physical.WeightGrams, quantity)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT b.bin_code,b.position_x,b.position_y,b.position_z,
 b.capacity_volume_cm3::numeric-b.occupied_volume_cm3::numeric-COALESCE(SUM(r.volume_cm3),0),b.max_weight_grams::numeric-b.occupied_weight_grams::numeric-COALESCE(SUM(r.weight_grams),0)
 FROM warehouse_bins b LEFT JOIN putaway_capacity_reservations r ON r.bin_code=b.bin_code AND r.status='active' AND r.task_id<>$1
 WHERE b.capacity_status='verified' AND b.occupied_volume_cm3>=0 AND b.occupied_weight_grams>=0 AND (($5::text<>'' AND b.warehouse_id=$5) OR ($5::text='' AND b.warehouse_name=$2)) AND b.position_x IS NOT NULL AND b.position_y IS NOT NULL AND b.position_z IS NOT NULL AND b.max_weight_grams IS NOT NULL
 GROUP BY b.bin_code HAVING b.capacity_volume_cm3::numeric-b.occupied_volume_cm3::numeric-COALESCE(SUM(r.volume_cm3),0)>=$3 AND b.max_weight_grams::numeric-b.occupied_weight_grams::numeric-COALESCE(SUM(r.weight_grams),0)>=$4`, taskID, warehouse, volume, weight, warehouseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BinSuggestion{}
	for rows.Next() {
		var b BinSuggestion
		var p warehouseopt.Point
		if err = rows.Scan(&b.BinCode, &p.X, &p.Y, &p.Z, &b.RemainingVolume, &b.RemainingWeight); err != nil {
			return nil, err
		}
		b.Distance = warehouseopt.Distance(origin, p)
		out = append(out, b)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Distance != out[j].Distance {
			return out[i].Distance < out[j].Distance
		}
		return out[i].BinCode < out[j].BinCode
	})
	if len(out) > 10 {
		out = out[:10]
	}
	rows.Close()
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

type PickingRoute struct {
	warehouseopt.Route
	MissingBins []string `json:"missingBins"`
	Warehouse   string   `json:"warehouse"`
}

func (r *WarehouseRepository) OptimizePicking(ctx context.Context, ids []string, warehouse string, start warehouseopt.Point) (PickingRoute, error) {
	out := PickingRoute{MissingBins: []string{}, Warehouse: warehouse}
	if len(ids) > 50 || warehouse == "" || !start.Valid() {
		return out, fmt.Errorf("حداکثر ۵۰ سفارش، نام انبار و مختصات معتبر لازم است")
	}
	staffID := WarehouseStaffID(ctx)
	var siteID string
	if staffID != "" {
		err := r.db.QueryRowContext(ctx, `SELECT site.id FROM warehouses site JOIN users staff ON staff.id=$1
			WHERE site.id <> '' AND site.name COLLATE "C"=$2 AND staff.is_active AND staff.warehouse_city_confirmed
			AND (SELECT COUNT(*) FROM warehouses named WHERE named.name COLLATE "C"=$2)=1
			AND NULLIF(BTRIM(staff.warehouse_city),'') COLLATE "C" = BTRIM(site.city_name) COLLATE "C"`, staffID, warehouse).Scan(&siteID)
		if err == sql.ErrNoRows {
			return out, fmt.Errorf("انبار در دسترس نیست")
		}
		if err != nil {
			return out, err
		}
	}
	orders, err := r.GetOrdersReadyToPick(ctx)
	if err != nil {
		return out, err
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	seen := map[string]bool{}
	stops := map[string]*warehouseopt.Stop{}
	missing := map[string]bool{}
	allocated := map[string]int{}
	for _, o := range orders {
		if len(wanted) > 0 && !wanted[o.ID] {
			continue
		}
		if staffID == "" {
			seen[o.ID] = true
		}
		var items []struct {
			OrderItemID string `json:"orderItemId"`
			ProductID   string `json:"productId"`
			VariantID   string `json:"variantId"`
			VariantName string `json:"variantName"`
			VariantSPNC string `json:"variantSpnc"`
			Quantity    int    `json:"quantity"`
			Bin         string `json:"binLocation"`
		}
		if err = json.Unmarshal(o.Items, &items); err != nil {
			return out, err
		}
		for _, it := range items {
			if staffID != "" {
				// The queue carries a city's total warehouse quantity. Only the
				// persisted legs for this route's exact site may be planned here.
				err = r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(origin.quantity),0)
					FROM order_item_origins origin JOIN warehouses site ON site.id=origin.warehouse_id
					JOIN users staff ON staff.id=$4
					WHERE origin.order_id=$1 AND origin.order_item_id::text=$2 AND site.id=$3
					AND origin.origin_type='warehouse' AND origin.quantity>0 AND staff.is_active AND staff.warehouse_city_confirmed
					AND NULLIF(BTRIM(staff.warehouse_city),'') COLLATE "C" = BTRIM(site.city_name) COLLATE "C"`, o.ID, it.OrderItemID, siteID, staffID).Scan(&it.Quantity)
				if err != nil {
					return out, err
				}
				if it.Quantity == 0 {
					continue
				}
				seen[o.ID] = true
			}
			// Allocate the actual quantity across canonical lots, never just the
			// last bin label. Shared stock cannot be planned twice in one batch.
			query := `WITH captured AS (
			 SELECT allocations FROM order_inventory_captures WHERE order_item_id=$4 AND product_id=$1
			 AND jsonb_typeof(allocations)='array' AND jsonb_array_length(allocations)>0
			), lots AS (
			 SELECT COALESCE(ib.bin_code,CASE WHEN ib.location_type='bin' THEN substring(ib.location_key FROM 5) END) bin_code,SUM((a->>'quantity')::int)::int quantity
			 FROM captured c CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a JOIN inventory_balances ib ON ib.id=a->>'balanceId'
			 GROUP BY 1
			 UNION ALL
			 SELECT bin_code,SUM(quantity)::int FROM inventory_balances WHERE product_id=$1 AND variant_id IS NULL AND status='sellable' AND quantity>0
			 AND NOT EXISTS(SELECT 1 FROM captured)
			 AND ($3='' OR EXISTS(SELECT 1 FROM product_variants WHERE id=NULLIF($3,'')::uuid AND inventory_mode='legacy')) GROUP BY bin_code
			)
			 SELECT b.bin_code,b.position_x,b.position_y,b.position_z,COALESCE(l.quantity,
			 CASE WHEN b.product_id=$1 AND NOT EXISTS(SELECT 1 FROM captured) AND NOT EXISTS(SELECT 1 FROM inventory_balances WHERE product_id=$1)
			 AND ($3='' OR EXISTS(SELECT 1 FROM product_variants WHERE id=NULLIF($3,'')::uuid AND inventory_mode='legacy')) THEN b.quantity ELSE 0 END),EXISTS(SELECT 1 FROM captured)
			 FROM warehouse_bins b LEFT JOIN lots l ON l.bin_code=b.bin_code WHERE b.warehouse_name=$2 AND b.position_x IS NOT NULL AND b.position_y IS NOT NULL AND b.position_z IS NOT NULL AND (l.quantity>0 OR b.product_id=$1)`
			args := []any{it.ProductID, warehouse, it.VariantID, it.OrderItemID}
			if staffID != "" {
				query = `WITH site_scope AS (
					SELECT site.id FROM warehouses site JOIN users staff ON staff.id=$5
					WHERE site.id=$2 AND staff.is_active AND staff.warehouse_city_confirmed
					AND NULLIF(BTRIM(staff.warehouse_city),'') COLLATE "C" = BTRIM(site.city_name) COLLATE "C"
				), captured AS (
					SELECT allocations FROM order_inventory_captures WHERE order_item_id=$4 AND product_id=$1
					AND jsonb_typeof(allocations)='array' AND jsonb_array_length(allocations)>0
				), lots AS (
					SELECT substring(ib.location_key FROM 5) AS bin_code, SUM((a->>'quantity')::int)::int AS quantity
					FROM captured c CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a
					JOIN inventory_balances ib ON ib.id=a->>'balanceId' AND ib.product_id=$1
					JOIN site_scope s ON s.id=ib.warehouse_id
					WHERE ib.location_type='bin' AND ib.supplier_warehouse_id IS NULL
					AND ib.location_key LIKE 'bin:%' AND (ib.bin_code IS NULL OR ib.bin_code=substring(ib.location_key FROM 5))
					GROUP BY 1
					UNION ALL
					SELECT substring(ib.location_key FROM 5), SUM(ib.quantity)::int
					FROM inventory_balances ib JOIN site_scope s ON s.id=ib.warehouse_id
					WHERE ib.product_id=$1 AND ib.variant_id IS NULL AND ib.status='sellable' AND ib.quantity>0
					AND ib.location_type='bin' AND ib.supplier_warehouse_id IS NULL
					AND ib.location_key LIKE 'bin:%' AND (ib.bin_code IS NULL OR ib.bin_code=substring(ib.location_key FROM 5))
					AND NOT EXISTS(SELECT 1 FROM captured)
					AND ($3='' OR EXISTS(SELECT 1 FROM product_variants WHERE id=NULLIF($3,'')::uuid AND inventory_mode='legacy')) GROUP BY 1
				)
				SELECT b.bin_code,b.position_x,b.position_y,b.position_z,l.quantity,EXISTS(SELECT 1 FROM captured)
				FROM warehouse_bins b JOIN site_scope s ON s.id=b.warehouse_id JOIN lots l ON l.bin_code=b.bin_code
				WHERE l.quantity>0 AND b.position_x IS NOT NULL AND b.position_y IS NOT NULL AND b.position_z IS NOT NULL`
				args = []any{it.ProductID, siteID, it.VariantID, it.OrderItemID, staffID}
			}
			rows, e := r.db.QueryContext(ctx, query, args...)
			if e != nil {
				return out, e
			}
			type lot struct {
				bin   string
				p     warehouseopt.Point
				q     int
				exact bool
			}
			lots := []lot{}
			for rows.Next() {
				var l lot
				if e = rows.Scan(&l.bin, &l.p.X, &l.p.Y, &l.p.Z, &l.q, &l.exact); e != nil {
					rows.Close()
					return out, e
				}
				lots = append(lots, l)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return out, e
			}
			sort.Slice(lots, func(i, j int) bool {
				a, b := warehouseopt.Distance(start, lots[i].p), warehouseopt.Distance(start, lots[j].p)
				if a != b {
					return a < b
				}
				return lots[i].bin < lots[j].bin
			})
			remaining := it.Quantity
			for _, l := range lots {
				key := it.ProductID + "/" + it.VariantID + "/" + l.bin
				if l.exact {
					key = it.OrderItemID + "/" + l.bin
				}
				available := l.q - allocated[key]
				if available <= 0 {
					continue
				}
				take := remaining
				if take > available {
					take = available
				}
				if take <= 0 {
					break
				}
				allocated[key] += take
				remaining -= take
				if _, ok := stops[l.bin]; !ok {
					stops[l.bin] = &warehouseopt.Stop{ID: l.bin, Point: l.p, Items: []string{}}
				}
				label := it.ProductID
				if it.VariantName != "" {
					label += " / " + it.VariantName
				}
				if it.VariantSPNC != "" {
					label += " [" + it.VariantSPNC + "]"
				}
				stops[l.bin].Items = append(stops[l.bin].Items, fmt.Sprintf("%s / %s × %d", o.OrderNumber, label, take))
				if remaining == 0 {
					break
				}
			}
			if remaining > 0 {
				missing[fmt.Sprintf("%s / %s: %d واحد بدون جایگاه قابل برداشت", o.OrderNumber, it.ProductID, remaining)] = true
			}
		}
	}
	for id := range wanted {
		if !seen[id] {
			return out, fmt.Errorf("سفارش %s در صف پرداخت‌شدهٔ قابل جمع‌آوری نیست", id)
		}
	}
	for b := range missing {
		out.MissingBins = append(out.MissingBins, b)
	}
	sort.Strings(out.MissingBins)
	nodes := []warehouseopt.Stop{}
	for _, s := range stops {
		nodes = append(nodes, *s)
	}
	out.Route, err = warehouseopt.Optimize(start, nodes)
	return out, err
}
func (r *WarehouseRepository) SetBinPosition(ctx context.Context, bin string, p warehouseopt.Point, actor string) error {
	if !p.Valid() {
		return fmt.Errorf("مختصات معتبر نیست")
	}
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	res, e := tx.ExecContext(ctx, `UPDATE warehouse_bins SET position_x=$2,position_y=$3,position_z=$4,updated_at=NOW() WHERE bin_code=$1`, bin, p.X, p.Y, p.Z)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO audit_logs(id,operator,action,category,details) VALUES($1,$2,'bin_position','warehouse',$3)`, "pos-"+uuid.NewString(), actor, fmt.Sprintf("%s: %.3f, %.3f, %.3f", bin, p.X, p.Y, p.Z))
	if e != nil {
		return e
	}
	return tx.Commit()
}
