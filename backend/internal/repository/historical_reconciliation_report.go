package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// QUAL-01 G4 bounded contracts for reconciliation review evidence and the
// full reconciliation report.
//
// Review evidence (G4a): a review POST selects at most
// MaxHistoricalReviewEvidence evidence references, each at most
// MaxHistoricalEvidenceEntryLength bytes. Oversized selections are rejected
// (ErrHistoricalEvidenceTooLarge, HTTP 400) — never silently truncated —
// because the evidence list is part of the durable audit record.
//
// Full report (G4b, tightened by PERF-02 D1): GET reconciliation-report
// is cursor-paged with explicit limit caps and deterministic ordering by
// product ID, and reports the total row count. DefaultHistoricalReportLimit
// is the limit for callers that pass no limit (including the existing
// frontend report fetch). Limit values above MaxHistoricalReportLimit are
// clamped to the max; the effective limit is echoed in the response.
// OFFSET pagination is gone: the cursor is the last product ID of the
// previous page (opaque base64), so concurrent product inserts can neither
// duplicate nor skip rows. The total is read in the same repeatable-read
// transaction as the page, so it agrees with the page snapshot.
//
// Nested evidence (G4a/G4b): each nested evidence list inside one report row
// (locations, reservations, movements, measurements) is capped at
// MaxHistoricalNestedEvidence entries in deterministic SQL order. The row
// reports EvidenceTotal (pre-truncation count) and EvidenceTruncated so a
// caller can tell the row's selectable evidence was bounded, and the
// reviewable evidenceIds selection is capped at the same size.
const (
	MaxHistoricalReviewEvidence      = 50
	MaxHistoricalEvidenceEntryLength = 256
	DefaultHistoricalReportLimit     = 50
	MaxHistoricalReportLimit         = 100
	MaxHistoricalNestedEvidence      = 50
)

type HistoricalInventoryLocationDetail struct {
	LocationKey   string `json:"locationKey"`
	LocationType  string `json:"locationType"`
	WarehouseName string `json:"warehouseName,omitempty"`
	BinCode       string `json:"binCode,omitempty"`
	Status        string `json:"status"`
	Quantity      int    `json:"quantity"`
	Reconciled    bool   `json:"reconciled"`
}

type HistoricalInventoryReservationDetail struct {
	ID          string `json:"id"`
	LocationKey string `json:"locationKey"`
	Quantity    int    `json:"quantity"`
	ReferenceID string `json:"referenceId"`
	Status      string `json:"status"`
}

type HistoricalInventoryMovementDetail struct {
	ID              string    `json:"id"`
	Quantity        int       `json:"quantity"`
	FromStatus      string    `json:"fromStatus,omitempty"`
	ToStatus        string    `json:"toStatus,omitempty"`
	FromLocationKey string    `json:"fromLocationKey,omitempty"`
	ToLocationKey   string    `json:"toLocationKey,omitempty"`
	ReferenceType   string    `json:"referenceType"`
	ReferenceID     string    `json:"referenceId"`
	CreatedAt       time.Time `json:"createdAt"`
}

type HistoricalMeasurementEvidence struct {
	ID             string    `json:"id"`
	NewLengthMM    int       `json:"newLengthMm"`
	NewWidthMM     int       `json:"newWidthMm"`
	NewHeightMM    int       `json:"newHeightMm"`
	NewWeightGrams int       `json:"newWeightGrams"`
	Reason         string    `json:"reason"`
	ActorUserID    string    `json:"actorUserId"`
	CreatedAt      time.Time `json:"createdAt"`
}

// HistoricalInventoryReportRow is intentionally read-only. It never assigns
// ownership, location, quantity, or measurements to a legacy record.
type HistoricalInventoryReportRow struct {
	ProductID                 string                                 `json:"productId"`
	ProductTitle              string                                 `json:"productTitle"`
	SupplierID                string                                 `json:"supplierId,omitempty"`
	LegacyStock               int                                    `json:"legacyStock"`
	CanonicalQuantity         int                                    `json:"canonicalQuantity"`
	PhysicalQuantity          int                                    `json:"physicalQuantity"`
	CanonicalSellableQuantity int                                    `json:"canonicalSellableQuantity"`
	CanonicalRows             int                                    `json:"canonicalRows"`
	UnreconciledRows          int                                    `json:"unreconciledRows"`
	MovementQuantity          int                                    `json:"movementQuantity"`
	MeasurementStatus         string                                 `json:"measurementStatus"`
	HasMeasurement            bool                                   `json:"hasMeasurement"`
	HasInboundEvidence        bool                                   `json:"hasInboundEvidence"`
	HasQCEvidence             bool                                   `json:"hasQCEvidence"`
	HasPutawayEvidence        bool                                   `json:"hasPutawayEvidence"`
	HasLedgerEvidence         bool                                   `json:"hasLedgerEvidence"`
	Classification            string                                 `json:"classification"`
	Classifications           []string                               `json:"classifications"`
	EvidenceInsufficient      bool                                   `json:"evidenceInsufficient"`
	Unresolved                bool                                   `json:"unresolved"`
	SuggestedAction           string                                 `json:"suggestedAction"`
	Difference                int                                    `json:"difference"`
	PhysicalInventory         bool                                   `json:"physicalInventory"`
	MeasurementClassification string                                 `json:"measurementClassification"`
	LocationBreakdown         []HistoricalInventoryLocationDetail    `json:"locationBreakdown"`
	ActiveReservations        []HistoricalInventoryReservationDetail `json:"activeReservations"`
	Movements                 []HistoricalInventoryMovementDetail    `json:"movements"`
	MeasurementEvidence       []HistoricalMeasurementEvidence        `json:"measurementEvidence"`
	EvidenceIDs               []string                               `json:"evidenceIds"`
	EvidenceTotal             int                                    `json:"evidenceTotal"`
	EvidenceTruncated         bool                                   `json:"evidenceTruncated"`
	ReviewStatus              string                                 `json:"reviewStatus"`
	ReviewID                  string                                 `json:"reviewId,omitempty"`
}

// historicalInventoryReportSQL is the deterministic read-only comparison.
// Callers append a product filter (and, for the cursor pages, a keyset
// bound + LIMIT); ordering is by product ID. There is no OFFSET pagination
// on this report: the cursor pages key on product ID, and the single-row
// lookup filters to one product. (The LIMITâ¦OFFSET that used to live in
// queryHistoricalReport was dead code â its only caller passed offset 0 â
// and was deleted in the PERF-02 critic-conditions round, not documented
// away.)
//
// PERF-02 Step 2: has_ledger is a correlated EXISTS (movement -> ledger)
// instead of `related IN (SELECT DISTINCT reference_id ...)`, which
// re-scanned the whole ledger per product (35.7 ms/product in situ,
// ~7.1 s per 200-row page; 0.06-0.08 ms correlated). Semantics are
// identical: a ledger entry exists whose related_order_or_dispute equals
// any movement reference_id of this product (NULLs match nothing either way).
// PERF-02 Step 8c: each nested jsonb_agg reads from a LIMIT-50 subquery, so
// the DB never builds unbounded JSONB for a dense product (a 5,005-movement
// product built ~1.46 MB JSONB that Go then truncated to 50); the
// SUM/COUNT aggregates still cover the full set. The four trailing count
// columns are the TRUE per-list totals: the Go-side EvidenceTotal must come
// from them, not from the capped list lengths (which would report 100 for
// that same 5,005-movement product instead of 10006).
const historicalInventoryReportSQL = `
		SELECT p.id,p.title,COALESCE(p.supplier_id,''),p.stock,
		       COALESCE(c.canonical_quantity,0),COALESCE(c.sellable_quantity,0),COALESCE(c.physical_quantity,0),COALESCE(c.canonical_rows,0),COALESCE(c.unreconciled_rows,0),
		       COALESCE(m.movement_quantity,0),COALESCE(p.measurement_status,'unverified'),
		       COALESCE(p.length_mm,0)>0 AND COALESCE(p.width_mm,0)>0 AND COALESCE(p.height_mm,0)>0 AND COALESCE(p.weight_grams,0)>0,
		       COALESCE(m.has_inbound,FALSE),COALESCE(m.has_qc,FALSE),COALESCE(m.has_putaway,FALSE),COALESCE(m.has_ledger,FALSE),
		       COALESCE(c.location_breakdown,'[]'::jsonb),COALESCE(res.active_reservations,'[]'::jsonb),
		       COALESCE(m.movement_details,'[]'::jsonb),COALESCE(me.measurement_details,'[]'::jsonb),
		       COALESCE(rv.review_id,''),COALESCE(rv.review_status,'needs_review'),
		       COALESCE(c.canonical_rows,0),COALESCE(res.reservation_count,0),COALESCE(m.movement_count,0),COALESCE(me.measurement_count,0)
		FROM products p
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(quantity),0)::int AS canonical_quantity,
			       COALESCE(SUM(quantity) FILTER (WHERE status='sellable'),0)::int AS sellable_quantity,
			       COALESCE(SUM(quantity) FILTER (WHERE location_type IN ('warehouse','bin','in_transit') AND quantity > 0),0)::int AS physical_quantity,
			       COUNT(*)::int AS canonical_rows,
			       COUNT(*) FILTER (WHERE NOT reconciled OR owner_type='unreconciled' OR status='unreconciled')::int AS unreconciled_rows
			       ,COALESCE((SELECT jsonb_agg(jsonb_build_object('locationKey',location_key,'locationType',location_type,'warehouseName',COALESCE(warehouse_name,''),'binCode',COALESCE(bin_code,''),'status',status,'quantity',quantity,'reconciled',reconciled) ORDER BY location_key,status) FROM (SELECT location_key,location_type,warehouse_name,bin_code,status,quantity,reconciled FROM inventory_balances WHERE product_id=p.id ORDER BY location_key,status LIMIT 50) ibl),'[]'::jsonb) AS location_breakdown
			FROM inventory_balances ib WHERE ib.product_id=p.id
		) c ON TRUE
		LEFT JOIN LATERAL (
			SELECT COALESCE(jsonb_agg(jsonb_build_object('id',ir.id,'locationKey',ir.location_key,'quantity',ir.quantity,'referenceId',ir.reference_id,'status',ir.status) ORDER BY ir.created_at),'[]'::jsonb) AS active_reservations,
			       (SELECT COUNT(*)::int FROM inventory_reservations WHERE product_id=p.id AND status='active') AS reservation_count
			FROM (SELECT id,location_key,quantity,reference_id,status,created_at FROM inventory_reservations WHERE product_id=p.id AND status='active' ORDER BY created_at LIMIT 50) ir
		) res ON TRUE
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(im.quantity),0)::int AS movement_quantity,
			       BOOL_OR(im.reference_type='inbound_received') AS has_inbound,
			       BOOL_OR(im.reference_type IN ('qc_pass','qc_failed')) AS has_qc,
			       BOOL_OR(im.reference_type='putaway_confirmed') AS has_putaway,
		       COUNT(*)::int AS movement_count,
			       EXISTS (SELECT 1 FROM inventory_movements im2 WHERE im2.product_id=p.id AND EXISTS (SELECT 1 FROM ledger_entries le WHERE le.related_order_or_dispute=im2.reference_id)) AS has_ledger,
			       COALESCE((SELECT jsonb_agg(jsonb_build_object('id',id,'quantity',quantity,'fromStatus',from_status,'toStatus',to_status,'fromLocationKey',from_location_key,'toLocationKey',to_location_key,'referenceType',reference_type,'referenceId',reference_id,'createdAt',created_at) ORDER BY created_at) FROM (SELECT id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,created_at FROM inventory_movements WHERE product_id=p.id ORDER BY created_at LIMIT 50) iml),'[]'::jsonb) AS movement_details
			FROM inventory_movements im WHERE im.product_id=p.id
		) m ON TRUE
		LEFT JOIN LATERAL (
			SELECT COALESCE(jsonb_agg(jsonb_build_object('id',pm.id,'newLengthMm',pm.new_length_mm,'newWidthMm',pm.new_width_mm,'newHeightMm',pm.new_height_mm,'newWeightGrams',pm.new_weight_grams,'reason',pm.reason,'actorUserId',pm.actor_user_id,'createdAt',pm.created_at) ORDER BY pm.created_at),'[]'::jsonb) AS measurement_details,
			       (SELECT COUNT(*)::int FROM product_measurement_events WHERE product_id=p.id) AS measurement_count
			FROM (SELECT id,new_length_mm,new_width_mm,new_height_mm,new_weight_grams,reason,actor_user_id,created_at FROM product_measurement_events WHERE product_id=p.id ORDER BY created_at LIMIT 50) pm
		) me ON TRUE
		LEFT JOIN LATERAL (
			SELECT id AS review_id,review_status FROM historical_reconciliation_reviews WHERE product_id=p.id
		) rv ON TRUE
`

// historicalInventoryReportSummarySQL is the summary-only twin of
// historicalInventoryReportSQL (PERF-02 Step 8d): same rows, same
// deterministic ordering, same scalar aggregates and flags — but NO nested
// jsonb_agg evidence at all. The list view needs counts and
// classifications, not evidence payloads; per-product evidence comes from
// GetHistoricalInventoryReportRow. The four trailing columns are the
// pre-truncation per-list evidence counts (balances, active reservations,
// movements, measurements) so the row can still report EvidenceTotal and
// EvidenceTruncated without building any JSONB.
const historicalInventoryReportSummarySQL = `
		SELECT p.id,p.title,COALESCE(p.supplier_id,''),p.stock,
		       COALESCE(c.canonical_quantity,0),COALESCE(c.sellable_quantity,0),COALESCE(c.physical_quantity,0),COALESCE(c.canonical_rows,0),COALESCE(c.unreconciled_rows,0),
		       COALESCE(m.movement_quantity,0),COALESCE(p.measurement_status,'unverified'),
		       COALESCE(p.length_mm,0)>0 AND COALESCE(p.width_mm,0)>0 AND COALESCE(p.height_mm,0)>0 AND COALESCE(p.weight_grams,0)>0,
		       COALESCE(m.has_inbound,FALSE),COALESCE(m.has_qc,FALSE),COALESCE(m.has_putaway,FALSE),COALESCE(m.has_ledger,FALSE),
		       '[]'::jsonb,'[]'::jsonb,
		       '[]'::jsonb,'[]'::jsonb,
		       COALESCE(rv.review_id,''),COALESCE(rv.review_status,'needs_review'),
		       COALESCE(c.canonical_rows,0),COALESCE(res.reservation_count,0),COALESCE(m.movement_count,0),COALESCE(me.measurement_count,0)
		FROM products p
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(quantity),0)::int AS canonical_quantity,
			       COALESCE(SUM(quantity) FILTER (WHERE status='sellable'),0)::int AS sellable_quantity,
			       COALESCE(SUM(quantity) FILTER (WHERE location_type IN ('warehouse','bin','in_transit') AND quantity > 0),0)::int AS physical_quantity,
			       COUNT(*)::int AS canonical_rows,
			       COUNT(*) FILTER (WHERE NOT reconciled OR owner_type='unreconciled' OR status='unreconciled')::int AS unreconciled_rows
			FROM inventory_balances ib WHERE ib.product_id=p.id
		) c ON TRUE
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS reservation_count
			FROM inventory_reservations ir WHERE ir.product_id=p.id AND ir.status='active'
		) res ON TRUE
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(im.quantity),0)::int AS movement_quantity,
			       BOOL_OR(im.reference_type='inbound_received') AS has_inbound,
			       BOOL_OR(im.reference_type IN ('qc_pass','qc_failed')) AS has_qc,
			       BOOL_OR(im.reference_type='putaway_confirmed') AS has_putaway,
		       COUNT(*)::int AS movement_count,
			       EXISTS (SELECT 1 FROM inventory_movements im2 WHERE im2.product_id=p.id AND EXISTS (SELECT 1 FROM ledger_entries le WHERE le.related_order_or_dispute=im2.reference_id)) AS has_ledger
			FROM inventory_movements im WHERE im.product_id=p.id
		) m ON TRUE
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS measurement_count
			FROM product_measurement_events pm WHERE pm.product_id=p.id
		) me ON TRUE
		LEFT JOIN LATERAL (
			SELECT id AS review_id,review_status FROM historical_reconciliation_reviews WHERE product_id=p.id
		) rv ON TRUE
`

// GetHistoricalInventoryReport returns the first bounded page of the
// deterministic read-only comparison. Use
// GetHistoricalInventoryReportCursorPage for explicit pagination.
func (r *SupplierRepository) GetHistoricalInventoryReport(ctx context.Context) ([]HistoricalInventoryReportRow, error) {
	rows, _, _, err := r.GetHistoricalInventoryReportCursorPage(ctx, MaxHistoricalReportLimit, "")
	return rows, err
}

// encodeHistoricalReportCursor builds the opaque page token from the last
// product ID of a page. The report orders by product ID ascending, so the
// next page is every product with a greater ID.
func encodeHistoricalReportCursor(productID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(productID))
}

// decodeHistoricalReportCursor parses a page token back into a product ID.
func decodeHistoricalReportCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", fmt.Errorf("invalid cursor: %w", err)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("invalid cursor")
	}
	return string(raw), nil
}

// GetHistoricalInventoryReportCursorPage returns one deterministic page of
// the read-only report (ordered by product ID ascending) plus the total
// product count and the cursor for the next page ("" when this is the last
// page). limit is clamped to MaxHistoricalReportLimit. The total and the
// page are read in one repeatable-read transaction, so they agree with
// each other even under concurrent product inserts; the cursor is stable
// under inserts (a new product sorts after the cursor only if its ID is
// greater, and is then seen on a later page, never duplicated).
func (r *SupplierRepository) GetHistoricalInventoryReportCursorPage(ctx context.Context, limit int, cursor string) ([]HistoricalInventoryReportRow, int, string, error) {
	if limit < 1 {
		return nil, 0, "", errors.New("invalid historical report limit")
	}
	if limit > MaxHistoricalReportLimit {
		limit = MaxHistoricalReportLimit
	}
	afterID, err := decodeHistoricalReportCursor(cursor)
	if err != nil {
		return nil, 0, "", err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, 0, "", err
	}
	defer tx.Rollback()
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products`).Scan(&total); err != nil {
		return nil, 0, "", err
	}
	query := historicalInventoryReportSQL
	args := []any{}
	if afterID != "" {
		query += ` WHERE p.id > $1`
		args = append(args, afterID)
	}
	query += ` ORDER BY p.id`
	if afterID != "" {
		query += ` LIMIT $2`
	} else {
		query += ` LIMIT $1`
	}
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, "", err
	}
	scanned, err := scanHistoricalReportRows(rows)
	if err != nil {
		return nil, 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, "", err
	}
	nextCursor := ""
	if len(scanned) == limit {
		nextCursor = encodeHistoricalReportCursor(scanned[len(scanned)-1].ProductID)
	}
	return scanned, total, nextCursor, nil
}

// GetHistoricalInventoryReportSummaryCursorPage returns one deterministic
// summary page of the read-only report (ordered by product ID ascending)
// plus the total product count and the cursor for the next page. Same
// contract as GetHistoricalInventoryReportCursorPage, but the rows carry
// counts and flags only — no nested evidence JSONB is built at all
// (PERF-02 Step 8d). Per-product evidence comes from
// GetHistoricalInventoryReportRow.
func (r *SupplierRepository) GetHistoricalInventoryReportSummaryCursorPage(ctx context.Context, limit int, cursor string) ([]HistoricalInventoryReportRow, int, string, error) {
	if limit < 1 {
		return nil, 0, "", errors.New("invalid historical report limit")
	}
	if limit > MaxHistoricalReportLimit {
		limit = MaxHistoricalReportLimit
	}
	afterID, err := decodeHistoricalReportCursor(cursor)
	if err != nil {
		return nil, 0, "", err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, 0, "", err
	}
	defer tx.Rollback()
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products`).Scan(&total); err != nil {
		return nil, 0, "", err
	}
	query := historicalInventoryReportSummarySQL
	args := []any{}
	if afterID != "" {
		query += ` WHERE p.id > $1`
		args = append(args, afterID)
	}
	query += ` ORDER BY p.id`
	if afterID != "" {
		query += ` LIMIT $2`
	} else {
		query += ` LIMIT $1`
	}
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, "", err
	}
	scanned, err := scanHistoricalReportRows(rows)
	if err != nil {
		return nil, 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, "", err
	}
	nextCursor := ""
	if len(scanned) == limit {
		nextCursor = encodeHistoricalReportCursor(scanned[len(scanned)-1].ProductID)
	}
	return scanned, total, nextCursor, nil
}

// GetHistoricalInventoryReportRow returns the bounded report row for one
// product. The review path uses this instead of the full report so a product
// past the first page is still reviewable.
func (r *SupplierRepository) GetHistoricalInventoryReportRow(ctx context.Context, productID string) (HistoricalInventoryReportRow, error) {
	rows, err := r.queryHistoricalReport(ctx, productID)
	if err != nil {
		return HistoricalInventoryReportRow{}, err
	}
	if len(rows) == 0 {
		return HistoricalInventoryReportRow{}, sql.ErrNoRows
	}
	return rows[0], nil
}

// queryHistoricalReport runs the deterministic read-only comparison for one
// product (the single-row review lookup). It carries no LIMIT/OFFSET
// pagination: the report pages by cursor (product ID keyset) and this
// helper filters to exactly one product.
func (r *SupplierRepository) queryHistoricalReport(ctx context.Context, productID string) ([]HistoricalInventoryReportRow, error) {
	query := historicalInventoryReportSQL + ` WHERE p.id = $1 ORDER BY p.id LIMIT 1`
	rows, err := r.db.QueryContext(ctx, query, productID)
	if err != nil {
		return nil, err
	}
	return scanHistoricalReportRows(rows)
}

func scanHistoricalReportRows(rows *sql.Rows) ([]HistoricalInventoryReportRow, error) {
	defer rows.Close()
	result := []HistoricalInventoryReportRow{}
	for rows.Next() {
		var row HistoricalInventoryReportRow
		var rawLocations, rawReservations, rawMovements, rawMeasurements []byte
		var balanceCount, reservationCount, movementCount, measurementCount int
		if err := rows.Scan(&row.ProductID, &row.ProductTitle, &row.SupplierID, &row.LegacyStock, &row.CanonicalQuantity, &row.CanonicalSellableQuantity, &row.PhysicalQuantity, &row.CanonicalRows, &row.UnreconciledRows, &row.MovementQuantity, &row.MeasurementStatus, &row.HasMeasurement, &row.HasInboundEvidence, &row.HasQCEvidence, &row.HasPutawayEvidence, &row.HasLedgerEvidence, &rawLocations, &rawReservations, &rawMovements, &rawMeasurements, &row.ReviewID, &row.ReviewStatus, &balanceCount, &reservationCount, &movementCount, &measurementCount); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rawLocations, &row.LocationBreakdown)
		_ = json.Unmarshal(rawReservations, &row.ActiveReservations)
		_ = json.Unmarshal(rawMovements, &row.Movements)
		_ = json.Unmarshal(rawMeasurements, &row.MeasurementEvidence)
		finishHistoricalReportRow(&row)
		// EvidenceTotal/Truncated come from the SQL-side per-list counts,
		// not the (SQL-capped) list lengths: the full query caps each nested
		// list at 50, so the lengths alone would under-report a dense
		// product (a 5,005-movement product reported 100 instead of 10006).
		row.EvidenceTotal = balanceCount + reservationCount + movementCount + measurementCount
		row.EvidenceTruncated = balanceCount > MaxHistoricalNestedEvidence || reservationCount > MaxHistoricalNestedEvidence || movementCount > MaxHistoricalNestedEvidence || measurementCount > MaxHistoricalNestedEvidence
		result = append(result, row)
	}
	return result, rows.Err()
}

// finishHistoricalReportRow derives the Go-side row fields (difference,
// classification, evidence bounds) shared by the full and summary scans.
func finishHistoricalReportRow(row *HistoricalInventoryReportRow) {
	row.PhysicalInventory = row.PhysicalQuantity > 0
	row.Difference = row.LegacyStock - row.CanonicalSellableQuantity
	row.MeasurementClassification = measurementClassification(*row)
	boundHistoricalReportEvidence(row)
	row.Classification, row.EvidenceInsufficient, row.SuggestedAction, row.Classifications = classifyHistoricalRow(*row)
	row.Unresolved = unresolvedHistoricalRow(*row)
}

// boundHistoricalReportEvidence caps each nested evidence list at
// MaxHistoricalNestedEvidence entries (deterministic SQL order) and records
// the pre-truncation total so callers can tell the row was bounded.
func boundHistoricalReportEvidence(row *HistoricalInventoryReportRow) {
	total := len(row.LocationBreakdown) + len(row.ActiveReservations) + len(row.Movements) + len(row.MeasurementEvidence)
	row.EvidenceTotal = total
	truncated := false
	if len(row.LocationBreakdown) > MaxHistoricalNestedEvidence {
		row.LocationBreakdown = row.LocationBreakdown[:MaxHistoricalNestedEvidence]
		truncated = true
	}
	if len(row.ActiveReservations) > MaxHistoricalNestedEvidence {
		row.ActiveReservations = row.ActiveReservations[:MaxHistoricalNestedEvidence]
		truncated = true
	}
	if len(row.Movements) > MaxHistoricalNestedEvidence {
		row.Movements = row.Movements[:MaxHistoricalNestedEvidence]
		truncated = true
	}
	if len(row.MeasurementEvidence) > MaxHistoricalNestedEvidence {
		row.MeasurementEvidence = row.MeasurementEvidence[:MaxHistoricalNestedEvidence]
		truncated = true
	}
	row.EvidenceIDs = evidenceIDs(*row)
	if len(row.EvidenceIDs) > MaxHistoricalNestedEvidence {
		row.EvidenceIDs = row.EvidenceIDs[:MaxHistoricalNestedEvidence]
		truncated = true
	}
	row.EvidenceTruncated = truncated
}

func classifyHistoricalRow(row HistoricalInventoryReportRow) (string, bool, string, []string) {
	issues := []string{}
	if row.CanonicalRows == 0 && row.LegacyStock > 0 {
		issues = append(issues, "LEGACY_ONLY")
	}
	if row.CanonicalRows > 0 && row.LegacyStock == 0 {
		issues = append(issues, "CANONICAL_ONLY")
	}
	if row.CanonicalRows > 0 && row.CanonicalSellableQuantity != row.LegacyStock {
		issues = append(issues, "QUANTITY_MISMATCH")
	}
	if row.UnreconciledRows > 0 {
		issues = append(issues, "EVIDENCE_INSUFFICIENT")
	}
	if row.MeasurementClassification == "PHYSICAL_MEASUREMENT_MISSING" {
		issues = append(issues, "MEASUREMENT_MISSING")
	} else if row.MeasurementClassification == "CATALOG_ONLY_MEASUREMENT_MISSING" {
		issues = append(issues, "CATALOG_ONLY_MEASUREMENT_MISSING")
	} else if row.MeasurementClassification == "MEASUREMENT_UNVERIFIED" {
		issues = append(issues, "MEASUREMENT_UNVERIFIED")
	} else if row.MeasurementClassification == "PHYSICAL_PRESENCE_UNCONFIRMED" {
		issues = append(issues, "PHYSICAL_PRESENCE_UNCONFIRMED")
	}
	if len(issues) == 0 {
		issues = append(issues, "MATCHED")
	}
	primary := issues[0]
	return primary, primary != "MATCHED", "manual review; no automatic mutation", issues
}

func measurementClassification(row HistoricalInventoryReportRow) string {
	if row.HasMeasurement {
		return "VERIFIED"
	}
	if row.PhysicalInventory && row.MeasurementStatus == "unverified" {
		return "PHYSICAL_MEASUREMENT_MISSING"
	}
	if row.PhysicalInventory && row.MeasurementStatus == "disputed" {
		return "MEASUREMENT_UNVERIFIED"
	}
	if row.LegacyStock > 0 || row.CanonicalRows > 0 || row.MovementQuantity > 0 {
		return "PHYSICAL_PRESENCE_UNCONFIRMED"
	}
	return "CATALOG_ONLY_MEASUREMENT_MISSING"
}

func unresolvedHistoricalRow(row HistoricalInventoryReportRow) bool {
	for _, classification := range row.Classifications {
		switch classification {
		case "LEGACY_ONLY", "CANONICAL_ONLY", "QUANTITY_MISMATCH", "EVIDENCE_INSUFFICIENT", "MEASUREMENT_MISSING", "MEASUREMENT_UNVERIFIED", "PHYSICAL_PRESENCE_UNCONFIRMED":
			return true
		}
	}
	return false
}

func evidenceIDs(row HistoricalInventoryReportRow) []string {
	ids := make([]string, 0, len(row.Movements)+len(row.MeasurementEvidence))
	for _, movement := range row.Movements {
		ids = append(ids, movement.ReferenceID)
	}
	for _, measurement := range row.MeasurementEvidence {
		ids = append(ids, measurement.ID)
	}
	return ids
}

type HistoricalReview struct {
	ID              string                       `json:"id"`
	ProductID       string                       `json:"productId"`
	ReviewStatus    string                       `json:"reviewStatus"`
	Classifications []string                     `json:"classifications"`
	ReportSnapshot  HistoricalInventoryReportRow `json:"reportSnapshot"`
	EvidenceUsed    []string                     `json:"evidenceUsed"`
	DecisionReason  string                       `json:"decisionReason"`
	ReviewerUserID  string                       `json:"reviewerUserId,omitempty"`
	ReviewedAt      *time.Time                   `json:"reviewedAt,omitempty"`
	UpdatedAt       time.Time                    `json:"updatedAt"`
}

// ErrInvalidHistoricalReview indicates an inadmissible review decision.
var ErrInvalidHistoricalReview = errors.New("invalid historical review")

// ErrHistoricalEvidenceTooLarge indicates a review evidence selection that
// exceeds MaxHistoricalReviewEvidence entries or holds an entry longer than
// MaxHistoricalEvidenceEntryLength bytes. The handler maps it to HTTP 400.
var ErrHistoricalEvidenceTooLarge = errors.New("historical review evidence exceeds the bounded selection")

// historicalReviewDecisionKey identifies one decision durably: status,
// reason, reviewer and the canonical JSON of the evidence selection. The
// key is computed from Go-marshalled evidence, never by comparing raw JSONB
// text, so JSONB normalization cannot turn one decision into two.
func historicalReviewDecisionKey(status, reason, reviewer string, evidenceJSON []byte) string {
	sum := sha256.Sum256([]byte(status + "\x00" + reason + "\x00" + reviewer + "\x00" + string(evidenceJSON)))
	return hex.EncodeToString(sum[:])
}

// Durable replay semantics (QUAL-01 G4c), consistent with the wallet
// withdrawal idempotency contract in this tree (same submission replays the
// persisted outcome; a submission under the same scope never silently
// overwrites it):
//   - An exact resubmission of the CURRENT decision replays the persisted
//     review row with no new audit event.
//   - A resubmission of a SUPERSEDED decision replays that decision's own
//     durable entry from historical_reconciliation_review_decisions, with
//     its original timestamps, and leaves the current row and audit log
//     untouched. A retry after a different decision therefore never flips
//     the current outcome back.
//   - A genuinely NEW decision is recorded as its own durable entry, then
//     becomes the current row (same stable review ID) with one audit event.
// Concurrent identical first writes converge on one current review, one
// history entry and one audit event via the product unique key.
func (r *SupplierRepository) ReviewHistoricalInventory(ctx context.Context, productID, status, reason, reviewer string, evidence []string) (*HistoricalReview, error) {
	allowed := map[string]bool{"needs_review": true, "in_review": true, "awaiting_evidence": true, "approved_for_correction": true, "rejected": true, "resolved": true, "indeterminate": true}
	if !allowed[status] || productID == "" || reviewer == "" {
		return nil, ErrInvalidHistoricalReview
	}
	if len(evidence) > MaxHistoricalReviewEvidence {
		return nil, ErrHistoricalEvidenceTooLarge
	}
	for _, entry := range evidence {
		if strings.TrimSpace(entry) == "" || len(entry) > MaxHistoricalEvidenceEntryLength {
			return nil, ErrHistoricalEvidenceTooLarge
		}
	}
	// Single-row lookup: the reviewable snapshot must not depend on which
	// report page the product falls on.
	snapshot, err := r.GetHistoricalInventoryReportRow(ctx, productID)
	if err != nil {
		return nil, err
	}
	classifications, _ := json.Marshal(snapshot.Classifications)
	reportJSON, _ := json.Marshal(snapshot)
	evidenceJSON, _ := json.Marshal(evidence)
	decisionKey := historicalReviewDecisionKey(status, reason, reviewer, evidenceJSON)
	review := &HistoricalReview{ID: "HREV-" + uuid.NewString(), ProductID: productID, ReviewStatus: status, Classifications: snapshot.Classifications, ReportSnapshot: snapshot, EvidenceUsed: evidence, DecisionReason: reason, ReviewerUserID: reviewer}
	err = func() error {
		tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return err
		}
		defer tx.Rollback()
		previousStatus := "needs_review"
		currentExists := false
		var currentID, currentStatus, currentReason, currentReviewer string
		var currentEvidence, currentClassifications, currentReport []byte
		var currentReviewedAt *time.Time
		var currentUpdatedAt time.Time
		cerr := tx.QueryRowContext(ctx, `SELECT id,review_status,decision_reason,reviewer_user_id,evidence_used,classification_snapshot,report_snapshot,reviewed_at,updated_at FROM historical_reconciliation_reviews WHERE product_id=$1 FOR UPDATE`, productID).Scan(&currentID, &currentStatus, &currentReason, &currentReviewer, &currentEvidence, &currentClassifications, &currentReport, &currentReviewedAt, &currentUpdatedAt)
		if cerr == nil {
			currentExists = true
			previousStatus = currentStatus
			var currentEvidenceList []string
			_ = json.Unmarshal(currentEvidence, &currentEvidenceList)
			currentEvidenceJSON, _ := json.Marshal(currentEvidenceList)
			if historicalReviewDecisionKey(currentStatus, currentReason, currentReviewer, currentEvidenceJSON) == decisionKey {
				// Identical replay of the current decision: no write, no audit.
				review.ID = currentID
				if err := json.Unmarshal(currentClassifications, &review.Classifications); err != nil {
					return err
				}
				review.ReportSnapshot = HistoricalInventoryReportRow{}
				if err := json.Unmarshal(currentReport, &review.ReportSnapshot); err != nil {
					return err
				}
				review.ReviewedAt, review.UpdatedAt = currentReviewedAt, currentUpdatedAt
				return tx.Commit()
			}
		} else if cerr != sql.ErrNoRows {
			return cerr
		}
		// Durable replay of a superseded decision: return its own recorded
		// entry and change nothing.
		var historyID string
		var historyClassifications, historyReport, historyEvidence []byte
		var historyReviewedAt *time.Time
		var historyUpdatedAt time.Time
		herr := tx.QueryRowContext(ctx, `SELECT id,classification_snapshot,report_snapshot,evidence_used,reviewed_at,updated_at FROM historical_reconciliation_review_decisions WHERE product_id=$1 AND decision_key=$2`, productID, decisionKey).Scan(&historyID, &historyClassifications, &historyReport, &historyEvidence, &historyReviewedAt, &historyUpdatedAt)
		if herr == nil {
			var historyEvidenceList []string
			_ = json.Unmarshal(historyEvidence, &historyEvidenceList)
			if err := json.Unmarshal(historyClassifications, &review.Classifications); err != nil {
				return err
			}
			review.ID = historyID
			review.ReportSnapshot = HistoricalInventoryReportRow{}
			if err := json.Unmarshal(historyReport, &review.ReportSnapshot); err != nil {
				return err
			}
			review.EvidenceUsed = historyEvidenceList
			review.ReviewedAt, review.UpdatedAt = historyReviewedAt, historyUpdatedAt
			return tx.Commit()
		} else if herr != sql.ErrNoRows {
			return herr
		}
		// Genuinely new decision: record it as its own durable entry first.
		historyID = "HREV-D-" + uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO historical_reconciliation_review_decisions(id,product_id,decision_key,review_status,classification_snapshot,report_snapshot,evidence_used,decision_reason,reviewer_user_id,reviewed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW()) ON CONFLICT (product_id, decision_key) DO NOTHING`, historyID, productID, decisionKey, status, classifications, reportJSON, evidenceJSON, reason, reviewer); err != nil {
			return err
		}
		if currentExists {
			review.ID = currentID
			if err := tx.QueryRowContext(ctx, `UPDATE historical_reconciliation_reviews SET review_status=$1,classification_snapshot=$2,report_snapshot=$3,evidence_used=$4,decision_reason=$5,reviewer_user_id=$6,reviewed_at=NOW(),updated_at=NOW() WHERE id=$7 RETURNING reviewed_at,updated_at`, status, classifications, reportJSON, evidenceJSON, reason, reviewer, review.ID).Scan(&review.ReviewedAt, &review.UpdatedAt); err != nil {
				return err
			}
		} else {
			// The unique product key arbitrates simultaneous first writes.
			err := tx.QueryRowContext(ctx, `INSERT INTO historical_reconciliation_reviews(id,product_id,review_status,classification_snapshot,report_snapshot,evidence_used,decision_reason,reviewer_user_id,reviewed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NOW()) ON CONFLICT (product_id) DO NOTHING RETURNING reviewed_at,updated_at`, review.ID, productID, status, classifications, reportJSON, evidenceJSON, reason, reviewer).Scan(&review.ReviewedAt, &review.UpdatedAt)
			if err == sql.ErrNoRows {
				// A concurrent identical write won. Re-read under lock: if it
				// recorded this same decision, replay it with no extra audit;
				// if it recorded a different one, apply ours as an update.
				var winnerEvidence []byte
				var winnerClassifications, winnerReport []byte
				if err := tx.QueryRowContext(ctx, `SELECT id,review_status,decision_reason,reviewer_user_id,evidence_used,classification_snapshot,report_snapshot,reviewed_at,updated_at FROM historical_reconciliation_reviews WHERE product_id=$1 FOR UPDATE`, productID).Scan(&review.ID, &currentStatus, &currentReason, &currentReviewer, &winnerEvidence, &winnerClassifications, &winnerReport, &review.ReviewedAt, &review.UpdatedAt); err != nil {
					return err
				}
				var winnerEvidenceList []string
				_ = json.Unmarshal(winnerEvidence, &winnerEvidenceList)
				winnerEvidenceJSON, _ := json.Marshal(winnerEvidenceList)
				if historicalReviewDecisionKey(currentStatus, currentReason, currentReviewer, winnerEvidenceJSON) == decisionKey {
					if err := json.Unmarshal(winnerClassifications, &review.Classifications); err != nil {
						return err
					}
					review.ReportSnapshot = HistoricalInventoryReportRow{}
					if err := json.Unmarshal(winnerReport, &review.ReportSnapshot); err != nil {
						return err
					}
					return tx.Commit()
				}
				previousStatus = currentStatus
				if err := tx.QueryRowContext(ctx, `UPDATE historical_reconciliation_reviews SET review_status=$1,classification_snapshot=$2,report_snapshot=$3,evidence_used=$4,decision_reason=$5,reviewer_user_id=$6,reviewed_at=NOW(),updated_at=NOW() WHERE id=$7 RETURNING reviewed_at,updated_at`, status, classifications, reportJSON, evidenceJSON, reason, reviewer, review.ID).Scan(&review.ReviewedAt, &review.UpdatedAt); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		details, _ := json.Marshal(map[string]interface{}{"productId": productID, "reviewId": review.ID, "previousStatus": previousStatus, "newStatus": status, "reason": reason, "evidence": evidence, "reportSnapshot": snapshot, "historicalMutation": false})
		if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(id,operator,action,category,details) VALUES($1,$2,'historical_reconciliation_review','warehouse',$3)`, "HREV-LOG-"+uuid.NewString(), reviewer, string(details)); err != nil {
			return err
		}
		return tx.Commit()
	}()
	if err != nil {
		return nil, err
	}
	return review, nil
}

var _ *sql.DB
