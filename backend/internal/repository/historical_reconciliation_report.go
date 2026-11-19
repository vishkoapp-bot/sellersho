package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
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
// Full report (G4b): GET reconciliation-report is paginated with explicit
// page/limit caps and deterministic ordering by product ID, and reports the
// total row count. DefaultHistoricalReportLimit is the limit for callers
// that pass no limit (including the existing frontend report fetch).
// Limit values above MaxHistoricalReportLimit are clamped to the max; the
// effective limit is echoed in the response.
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
	DefaultHistoricalReportLimit     = 200
	MaxHistoricalReportLimit         = 200
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
// Callers append a product filter and/or LIMIT/OFFSET; ordering by product
// ID is applied by queryHistoricalReport.
const historicalInventoryReportSQL = `
		SELECT p.id,p.title,COALESCE(p.supplier_id,''),p.stock,
		       COALESCE(c.canonical_quantity,0),COALESCE(c.sellable_quantity,0),COALESCE(c.physical_quantity,0),COALESCE(c.canonical_rows,0),COALESCE(c.unreconciled_rows,0),
		       COALESCE(m.movement_quantity,0),COALESCE(p.measurement_status,'unverified'),
		       COALESCE(p.length_mm,0)>0 AND COALESCE(p.width_mm,0)>0 AND COALESCE(p.height_mm,0)>0 AND COALESCE(p.weight_grams,0)>0,
		       COALESCE(m.has_inbound,FALSE),COALESCE(m.has_qc,FALSE),COALESCE(m.has_putaway,FALSE),COALESCE(m.has_ledger,FALSE),
		       COALESCE(c.location_breakdown,'[]'::jsonb),COALESCE(res.active_reservations,'[]'::jsonb),
		       COALESCE(m.movement_details,'[]'::jsonb),COALESCE(me.measurement_details,'[]'::jsonb),
		       COALESCE(rv.review_id,''),COALESCE(rv.review_status,'needs_review')
		FROM products p
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(quantity),0)::int AS canonical_quantity,
			       COALESCE(SUM(quantity) FILTER (WHERE status='sellable'),0)::int AS sellable_quantity,
			       COALESCE(SUM(quantity) FILTER (WHERE location_type IN ('warehouse','bin','in_transit') AND quantity > 0),0)::int AS physical_quantity,
			       COUNT(*)::int AS canonical_rows,
			       COUNT(*) FILTER (WHERE NOT reconciled OR owner_type='unreconciled' OR status='unreconciled')::int AS unreconciled_rows
			       ,COALESCE(jsonb_agg(jsonb_build_object('locationKey',location_key,'locationType',location_type,'warehouseName',COALESCE(warehouse_name,''),'binCode',COALESCE(bin_code,''),'status',status,'quantity',quantity,'reconciled',reconciled) ORDER BY location_key,status),'[]'::jsonb) AS location_breakdown
			FROM inventory_balances ib WHERE ib.product_id=p.id
		) c ON TRUE
		LEFT JOIN LATERAL (
			SELECT COALESCE(jsonb_agg(jsonb_build_object('id',ir.id,'locationKey',ir.location_key,'quantity',ir.quantity,'referenceId',ir.reference_id,'status',ir.status) ORDER BY ir.created_at),'[]'::jsonb) AS active_reservations
			FROM inventory_reservations ir WHERE ir.product_id=p.id AND ir.status='active'
		) res ON TRUE
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(im.quantity),0)::int AS movement_quantity,
			       BOOL_OR(im.reference_type='inbound_received') AS has_inbound,
			       BOOL_OR(im.reference_type IN ('qc_pass','qc_failed')) AS has_qc,
			       BOOL_OR(im.reference_type='putaway_confirmed') AS has_putaway,
			       EXISTS (SELECT 1 FROM ledger_entries le WHERE le.related_order_or_dispute IN (SELECT DISTINCT im2.reference_id FROM inventory_movements im2 WHERE im2.product_id=p.id)) AS has_ledger,
			       COALESCE(jsonb_agg(jsonb_build_object('id',im.id,'quantity',im.quantity,'fromStatus',im.from_status,'toStatus',im.to_status,'fromLocationKey',im.from_location_key,'toLocationKey',im.to_location_key,'referenceType',im.reference_type,'referenceId',im.reference_id,'createdAt',im.created_at) ORDER BY im.created_at),'[]'::jsonb) AS movement_details
			FROM inventory_movements im WHERE im.product_id=p.id
		) m ON TRUE
		LEFT JOIN LATERAL (
			SELECT COALESCE(jsonb_agg(jsonb_build_object('id',pm.id,'newLengthMm',pm.new_length_mm,'newWidthMm',pm.new_width_mm,'newHeightMm',pm.new_height_mm,'newWeightGrams',pm.new_weight_grams,'reason',pm.reason,'actorUserId',pm.actor_user_id,'createdAt',pm.created_at) ORDER BY pm.created_at),'[]'::jsonb) AS measurement_details
			FROM product_measurement_events pm WHERE pm.product_id=p.id
		) me ON TRUE
		LEFT JOIN LATERAL (
			SELECT id AS review_id,review_status FROM historical_reconciliation_reviews WHERE product_id=p.id
		) rv ON TRUE
`

// GetHistoricalInventoryReport returns the first bounded page of the
// deterministic read-only comparison. Use GetHistoricalInventoryReportPage
// for explicit pagination.
func (r *SupplierRepository) GetHistoricalInventoryReport(ctx context.Context) ([]HistoricalInventoryReportRow, error) {
	rows, _, err := r.GetHistoricalInventoryReportPage(ctx, 1, MaxHistoricalReportLimit)
	return rows, err
}

// GetHistoricalInventoryReportPage returns one deterministic page of the
// read-only report (ordered by product ID) plus the total product count.
// page is 1-based; limit is clamped to MaxHistoricalReportLimit.
func (r *SupplierRepository) GetHistoricalInventoryReportPage(ctx context.Context, page, limit int) ([]HistoricalInventoryReportRow, int, error) {
	if page < 1 || limit < 1 {
		return nil, 0, errors.New("invalid historical report page")
	}
	if limit > MaxHistoricalReportLimit {
		limit = MaxHistoricalReportLimit
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM products`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.queryHistoricalReport(ctx, "", limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// GetHistoricalInventoryReportRow returns the bounded report row for one
// product. The review path uses this instead of the full report so a product
// past the first page is still reviewable.
func (r *SupplierRepository) GetHistoricalInventoryReportRow(ctx context.Context, productID string) (HistoricalInventoryReportRow, error) {
	rows, err := r.queryHistoricalReport(ctx, productID, 1, 0)
	if err != nil {
		return HistoricalInventoryReportRow{}, err
	}
	if len(rows) == 0 {
		return HistoricalInventoryReportRow{}, sql.ErrNoRows
	}
	return rows[0], nil
}

// queryHistoricalReport runs the deterministic read-only comparison.
// An empty productID selects all products; otherwise one product.
func (r *SupplierRepository) queryHistoricalReport(ctx context.Context, productID string, limit, offset int) ([]HistoricalInventoryReportRow, error) {
	query := historicalInventoryReportSQL
	args := []any{}
	if productID != "" {
		query += ` WHERE p.id = $1`
		args = append(args, productID)
	}
	query += ` ORDER BY p.id`
	if limit > 0 {
		if productID != "" {
			query += ` LIMIT $2 OFFSET $3`
		} else {
			query += ` LIMIT $1 OFFSET $2`
		}
		args = append(args, limit, offset)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
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
		if err := rows.Scan(&row.ProductID, &row.ProductTitle, &row.SupplierID, &row.LegacyStock, &row.CanonicalQuantity, &row.CanonicalSellableQuantity, &row.PhysicalQuantity, &row.CanonicalRows, &row.UnreconciledRows, &row.MovementQuantity, &row.MeasurementStatus, &row.HasMeasurement, &row.HasInboundEvidence, &row.HasQCEvidence, &row.HasPutawayEvidence, &row.HasLedgerEvidence, &rawLocations, &rawReservations, &rawMovements, &rawMeasurements, &row.ReviewID, &row.ReviewStatus); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rawLocations, &row.LocationBreakdown)
		_ = json.Unmarshal(rawReservations, &row.ActiveReservations)
		_ = json.Unmarshal(rawMovements, &row.Movements)
		_ = json.Unmarshal(rawMeasurements, &row.MeasurementEvidence)
		row.PhysicalInventory = row.PhysicalQuantity > 0
		row.Difference = row.LegacyStock - row.CanonicalSellableQuantity
		row.MeasurementClassification = measurementClassification(row)
		boundHistoricalReportEvidence(&row)
		row.Classification, row.EvidenceInsufficient, row.SuggestedAction, row.Classifications = classifyHistoricalRow(row)
		row.Unresolved = unresolvedHistoricalRow(row)
		result = append(result, row)
	}
	return result, rows.Err()
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
