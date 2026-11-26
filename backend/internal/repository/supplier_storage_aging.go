package repository

import (
	"context"
	"time"
)

const supplierStoragePageSize = 25
const supplierStorageWarningDays = 7

type SupplierStorageAgingRow struct {
	ID            string `json:"id"`
	ProductTitle  string `json:"product_title"`
	VariantName   string `json:"variant_name"`
	SPNC          string `json:"spnc"`
	WarehouseName string `json:"warehouse_name"`
	Quantity      *int   `json:"quantity"`
	ReceivedOn    string `json:"received_on"`
	AgeDays       int    `json:"age_days"`
	FeeStartsOn   string `json:"fee_starts_on,omitempty"`
}

type SupplierStorageAging struct {
	FeesEnabled     bool                      `json:"fees_enabled"`
	AsOf            string                    `json:"as_of"`
	WarningCount    int                       `json:"warning_count"`
	WarningDays     int                       `json:"warning_days"`
	UnknownAgeCount int                       `json:"unknown_age_count"`
	UnratedCount    int                       `json:"unrated_count"`
	TotalRows       int                       `json:"total_rows"`
	HasMore         bool                      `json:"has_more"`
	Rows            []SupplierStorageAgingRow `json:"rows"`
}

func (r *SupplierRepository) GetSupplierStorageAging(ctx context.Context, supplierID string, page int) (SupplierStorageAging, error) {
	return r.getSupplierStorageAging(ctx, supplierID, page, time.Now())
}

func (r *SupplierRepository) getSupplierStorageAging(ctx context.Context, supplierID string, page int, now time.Time) (SupplierStorageAging, error) {
	out := SupplierStorageAging{
		AsOf: tehranDay(now).Format(time.DateOnly), WarningDays: supplierStorageWarningDays,
		Rows: []SupplierStorageAgingRow{},
	}
	// Calendar dates mirror storage assessment's max(held-day, billing-day
	// plus frozen free days). A fee-start date conveys eligibility, not that
	// any particular daily charge has been assessed or posted.
	rows, err := r.db.QueryContext(ctx, `
		WITH policy AS (
			SELECT COALESCE((SELECT storage_fee_enabled FROM platform_settings WHERE id=1),FALSE) AS enabled
		), active_lots AS MATERIALIZED (
			SELECT p.id,p.product_id,p.warehouse_id,p.qty,l.variant_id,l.history_status,
				l.physical_received_at,l.held_from,l.billing_start_at,l.free_days,l.unit_day_toman,
				CASE WHEN l.id IS NOT NULL THEN storage_receipt_is_blocked(l.id) ELSE FALSE END AS blocked
			FROM warehouse_storage_positions p
			LEFT JOIN warehouse_storage_receipt_lots l ON l.id=p.id
				AND (l.supplier_id,l.product_id,l.warehouse_id)=(p.supplier_id,p.product_id,p.warehouse_id)
			WHERE p.supplier_id=$1 AND p.closed_at IS NULL AND p.qty>0
		), aging AS MATERIALIZED (
			SELECT l.id,l.product_id,l.warehouse_id,l.variant_id,
				CASE WHEN NOT l.blocked THEN l.qty END AS qty,
				(l.physical_received_at AT TIME ZONE 'Asia/Tehran')::date AS received_on,
				CASE WHEN cfg.enabled AND l.history_status='verified' AND l.unit_day_toman>0 AND NOT l.blocked
					THEN GREATEST((l.held_from AT TIME ZONE 'Asia/Tehran')::date,
						(l.billing_start_at AT TIME ZONE 'Asia/Tehran')::date+l.free_days)
				END AS fee_starts_on,
				cfg.enabled AND l.history_status IS NOT NULL AND (l.history_status<>'verified' OR l.blocked) AS fee_unresolved
			FROM active_lots l CROSS JOIN policy cfg
		), summary AS (
			SELECT COUNT(*) FILTER(WHERE received_on IS NULL OR received_on>$2::date) AS unknown_ages,
				COUNT(*) FILTER(WHERE fee_unresolved) AS unresolved_fees,
				COUNT(*) FILTER(WHERE received_on<=$2::date AND fee_starts_on<=$2::date+$3::int) AS warnings,
				COUNT(*) FILTER(WHERE received_on<=$2::date) AS known_ages
			FROM aging
		), page AS (
			SELECT * FROM aging WHERE received_on<=$2::date
			ORDER BY fee_starts_on ASC NULLS LAST,received_on,id OFFSET $4 LIMIT $5
		)
		SELECT cfg.enabled,s.unknown_ages,s.unresolved_fees,s.warnings,s.known_ages,
			COALESCE(a.id,''),COALESCE(p.title,''),COALESCE(v.variant_name,''),COALESCE(v.spnc,''),COALESCE(w.name,''),
			a.qty,COALESCE(to_char(a.received_on,'YYYY-MM-DD'),''),COALESCE($2::date-a.received_on,0),
			COALESCE(to_char(a.fee_starts_on,'YYYY-MM-DD'),'')
		FROM policy cfg CROSS JOIN summary s LEFT JOIN page a ON TRUE
		LEFT JOIN products p ON p.id=a.product_id AND p.supplier_id=$1
		LEFT JOIN product_variants v ON v.id::text=a.variant_id AND v.product_id=p.id
		LEFT JOIN warehouses w ON w.id=a.warehouse_id
		ORDER BY a.fee_starts_on ASC NULLS LAST,a.received_on,a.id`,
		supplierID, out.AsOf, supplierStorageWarningDays, page*supplierStoragePageSize, supplierStoragePageSize)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var row SupplierStorageAgingRow
		if err := rows.Scan(&out.FeesEnabled, &out.UnknownAgeCount, &out.UnratedCount, &out.WarningCount, &out.TotalRows,
			&row.ID, &row.ProductTitle, &row.VariantName, &row.SPNC, &row.WarehouseName,
			&row.Quantity, &row.ReceivedOn, &row.AgeDays, &row.FeeStartsOn); err != nil {
			return out, err
		}
		if row.ID != "" {
			out.Rows = append(out.Rows, row)
		}
	}
	out.HasMore = (page+1)*supplierStoragePageSize < out.TotalRows
	return out, rows.Err()
}
