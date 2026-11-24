package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var (
	ErrPrintProductNotFound      = errors.New("product_not_found")
	ErrPrintVariantNotFound      = errors.New("variant_not_found")
	ErrPrintReceiptLineNotFound  = errors.New("receipt_line_not_found")
	ErrPrintInvalidStatus        = errors.New("invalid_status")
	ErrPrintInvalidLimit         = errors.New("invalid_limit")
	ErrPrintInvalidCursor        = errors.New("invalid_cursor")
	ErrPrintKeyRequired          = errors.New("idempotency_key_required")
	ErrPrintInvalidKey           = errors.New("invalid_idempotency_key")
	ErrPrintVariantRequired      = errors.New("variant_id_required")
	ErrPrintInvalidCount         = errors.New("invalid_count")
	ErrPrintSelectionRequired    = errors.New("unit_selection_required")
	ErrPrintDuplicateCodes       = errors.New("duplicate_unit_codes")
	ErrPrintVariantMismatch      = errors.New("unit_codes_variant_mismatch")
	ErrPrintConflictingSelection = errors.New("conflicting_unit_selection")
	ErrPrintIdempotencyMismatch  = errors.New("idempotency_key_mismatch")
	ErrPrintAlreadyPrinted       = errors.New("already_printed_in_job")
	ErrPrintNotPrintable         = errors.New("unit_not_printable")
)

type PrintUnitError struct {
	Code   string `json:"code"`
	Error  string `json:"error"`
	Status string `json:"status,omitempty"`
}

type PrintNotPrintableError struct {
	Units []PrintUnitError
}

func (e *PrintNotPrintableError) Error() string { return ErrPrintNotPrintable.Error() }
func (e *PrintNotPrintableError) Unwrap() error { return ErrPrintNotPrintable }

type PrintConflictError struct {
	Reason    error
	JobID     string
	UnitCodes []string
}

func (e *PrintConflictError) Error() string { return e.Reason.Error() }
func (e *PrintConflictError) Unwrap() error { return e.Reason }

type UnitLabelRow struct {
	Code          string     `json:"code"`
	SPNC          string     `json:"spnc"`
	VariantID     string     `json:"variant_id"`
	VariantName   string     `json:"variant_name"`
	ProductID     string     `json:"product_id"`
	SPN           string     `json:"spn"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	PrintedAt     *time.Time `json:"printed_at"`
	PrintCount    int        `json:"print_count"`
	LastPrintedAt *time.Time `json:"last_printed_at"`
}

type UnitLabelListOptions struct {
	VariantID     string
	Status        string
	UnprintedOnly bool
	Limit         int
	Cursor        string
}

type UnitLabelListResponse struct {
	SPN            string         `json:"spn"`
	ProductTitle   string         `json:"product_title"`
	Units          []UnitLabelRow `json:"units"`
	TotalForFilter int            `json:"total_for_filter"`
	NextCursor     *string        `json:"next_cursor"`
}

type unitLabelCursor struct {
	CreatedAt time.Time
	VariantID string
	Sequence  int
}

func validUnitLabelStatus(status string) bool {
	switch status {
	case "defined", "in_stock", "reserved", "sold", "returned", "quarantined", "damaged", "removed":
		return true
	}
	return false
}

func (r *SupplierRepository) ListProductUnitLabels(ctx context.Context, supplierID, productID string, opts UnitLabelListOptions) (UnitLabelListResponse, error) {
	out := UnitLabelListResponse{Units: []UnitLabelRow{}}
	// The owner, total and page come from the same snapshot.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(spn,''),COALESCE(title,'') FROM products WHERE id=$1 AND supplier_id=$2`,
		productID, supplierID).Scan(&out.SPN, &out.ProductTitle)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrPrintProductNotFound
	}
	if err != nil {
		return out, err
	}
	if opts.Status == "" {
		opts.Status = "in_stock"
	}
	if !validUnitLabelStatus(opts.Status) {
		return out, ErrPrintInvalidStatus
	}
	if opts.Limit < 0 {
		return out, ErrPrintInvalidLimit
	}
	if opts.Limit == 0 {
		opts.Limit = 100
	}
	opts.Limit = min(opts.Limit, 500)

	var variant any
	if opts.VariantID != "" {
		id, err := productPrintVariant(ctx, tx, productID, opts.VariantID)
		if err != nil {
			return out, err
		}
		variant = id
	}

	var cursorVariant any
	var cursor unitLabelCursor
	if opts.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(opts.Cursor)
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.CreatedAt.IsZero() ||
			cursor.Sequence < 1 || cursor.Sequence > unitSequenceCap {
			return out, ErrPrintInvalidCursor
		}
		id, err := uuid.Parse(cursor.VariantID)
		if err != nil {
			return out, ErrPrintInvalidCursor
		}
		cursorVariant = id
	}

	const filter = ` FROM product_units u JOIN product_variants v ON v.id=u.variant_id
		WHERE u.product_id=$1 AND u.status=$2 AND ($3::uuid IS NULL OR u.variant_id=$3)
		AND (NOT $4::boolean OR u.printed_at IS NULL)`
	args := []any{productID, opts.Status, variant, opts.UnprintedOnly}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)`+filter, args...).Scan(&out.TotalForFilter); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT u.code,COALESCE(v.spnc,''),v.id,COALESCE(v.variant_name,''),
		u.status,u.printed_at,u.print_count,u.last_printed_at,v.created_at,u.sequence`+filter+`
		AND ($5::uuid IS NULL OR (v.created_at,v.id,u.sequence)>($6,$5,$7))
		ORDER BY v.created_at,v.id,u.sequence LIMIT $8`,
		append(args, cursorVariant, cursor.CreatedAt, cursor.Sequence, opts.Limit+1)...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Units) == opts.Limit {
			data, err := json.Marshal(cursor)
			if err != nil {
				return out, err
			}
			next := base64.RawURLEncoding.EncodeToString(data)
			out.NextCursor = &next
			break
		}
		u := UnitLabelRow{ProductID: productID, SPN: out.SPN, Title: out.ProductTitle}
		if err := rows.Scan(&u.Code, &u.SPNC, &u.VariantID, &u.VariantName,
			&u.Status, &u.PrintedAt, &u.PrintCount, &u.LastPrintedAt, &cursor.CreatedAt, &cursor.Sequence); err != nil {
			return out, err
		}
		cursor.VariantID = u.VariantID
		out.Units = append(out.Units, u)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

type MarkUnitsPrintedRequest struct {
	SupplierID     string
	ProductID      string
	ActorUserID    string
	VariantID      string
	Count          *int
	UnitCodes      []string
	ReceiptLineID  *string
	UnprintedOnly  bool
	Reprint        bool
	IdempotencyKey string
}

type PrintLabelPayload struct {
	UnitLabelRow
	BarcodeValue string `json:"barcode_value"`
}

type printedUnitLabels struct {
	Units []PrintLabelPayload `json:"units"`
}

func (req *MarkUnitsPrintedRequest) validate() error {
	req.IdempotencyKey = strings.TrimPrefix(req.IdempotencyKey, "print:")
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return ErrPrintKeyRequired
	}
	if len(req.IdempotencyKey) != 36 || uuid.Validate(req.IdempotencyKey) != nil {
		return ErrPrintInvalidKey
	}
	if req.ReceiptLineID != nil && req.UnitCodes != nil {
		return ErrPrintConflictingSelection
	}
	if req.Count != nil {
		if req.VariantID == "" {
			return ErrPrintVariantRequired
		}
		if *req.Count <= 0 {
			return ErrPrintInvalidCount
		}
	}
	if len(req.UnitCodes) == 0 && req.Count == nil && req.ReceiptLineID == nil {
		return ErrPrintSelectionRequired
	}
	seen := make(map[string]bool, len(req.UnitCodes))
	for _, code := range req.UnitCodes {
		if seen[code] {
			return ErrPrintDuplicateCodes
		}
		seen[code] = true
	}
	return nil
}

func productPrintVariant(ctx context.Context, tx *sql.Tx, product, variant string) (string, error) {
	id, err := uuid.Parse(variant)
	if err != nil {
		return "", ErrPrintVariantNotFound
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM product_variants WHERE product_id=$1 AND id=$2`, product, id).Scan(&variant)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrPrintVariantNotFound
	}
	return variant, err
}

func (r *SupplierRepository) MarkUnitsPrinted(ctx context.Context, req MarkUnitsPrintedRequest) (json.RawMessage, error) {
	return MarkUnitsPrintedTx(ctx, r.db, req)
}

// MarkUnitsPrintedTx owns the transaction for both label-print endpoints.
func MarkUnitsPrintedTx(ctx context.Context, db *sql.DB, req MarkUnitsPrintedRequest) (json.RawMessage, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if req.SupplierID != "" {
		err = tx.QueryRowContext(ctx, `SELECT id FROM products WHERE id=$1 AND supplier_id=$2 FOR SHARE`,
			req.ProductID, req.SupplierID).Scan(&req.ProductID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPrintProductNotFound
		}
		if err != nil {
			return nil, err
		}
		if req.VariantID != "" {
			id, err := uuid.Parse(req.VariantID)
			if err != nil {
				return nil, ErrPrintVariantNotFound
			}
			req.VariantID = id.String()
		}
	}

	if req.ReceiptLineID != nil {
		if err := resolvePrintReceiptLine(ctx, tx, &req); err != nil {
			return nil, err
		}
	}
	var explicit []string
	if len(req.UnitCodes) > 0 {
		explicit, err = orderExplicitPrintCodes(ctx, tx, &req)
		if err != nil {
			return nil, err
		}
	}
	if req.SupplierID != "" && req.VariantID != "" {
		if _, err := productPrintVariant(ctx, tx, req.ProductID, req.VariantID); err != nil {
			return nil, err
		}
	}

	jobID := uuid.NewString()
	claim, err := tx.ExecContext(ctx, `INSERT INTO spncx_print_jobs(id,idempotency_key,product_id)
		VALUES($1,$2,$3) ON CONFLICT (idempotency_key) DO NOTHING`, jobID, req.IdempotencyKey, req.ProductID)
	if err != nil {
		return nil, err
	}
	claimed, err := claim.RowsAffected()
	if err != nil {
		return nil, err
	}
	var payload json.RawMessage
	if claimed == 0 {
		payload, err = replayPrintJob(ctx, tx, req, explicit)
	} else {
		payload, err = markPrintJobUnits(ctx, tx, req, explicit, jobID)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return payload, nil
}

func resolvePrintReceiptLine(ctx context.Context, tx *sql.Tx, req *MarkUnitsPrintedRequest) error {
	var shipment string
	// The mint events identify units even on legacy receipt lines without a variant.
	err := tx.QueryRowContext(ctx, `SELECT l.shipment_id,l.product_id,COALESCE(v.id::text,'')
		FROM inbound_inventory_lines l LEFT JOIN product_variants v ON v.product_id=l.product_id AND v.id::text=l.variant_id
		WHERE l.id=$1`, *req.ReceiptLineID).Scan(&shipment, &req.ProductID, &req.VariantID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPrintReceiptLineNotFound
	}
	if err != nil {
		return err
	}
	if err := lockWarehouseQCReceiptScopeTx(ctx, tx, shipment, req.ProductID, *req.ReceiptLineID, ""); err != nil {
		if errors.Is(err, ErrInboundProvenance) {
			return ErrPrintReceiptLineNotFound
		}
		return err
	}

	return nil
}

func orderExplicitPrintCodes(ctx context.Context, tx *sql.Tx, req *MarkUnitsPrintedRequest) ([]string, error) {
	// Only immutable identities are read here. Eligibility is checked later
	// under the unit locks. Match the lock order of the count and receipt scans.
	rows, err := tx.QueryContext(ctx, `SELECT code,product_id,variant_id FROM product_units
		WHERE code=ANY($1) AND ($2='' OR product_id=$2) ORDER BY sequence,variant_id`, pq.Array(req.UnitCodes), req.ProductID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ordered []string
	for rows.Next() {
		var code, product, variant string
		if err := rows.Scan(&code, &product, &variant); err != nil {
			return nil, err
		}
		if req.VariantID != "" && req.VariantID != variant {
			return nil, ErrPrintVariantMismatch
		}
		if req.ProductID == "" {
			// A warehouse batch can span products. The first unit in lock
			// order supplies the job's required product FK.
			req.ProductID = product
		}
		ordered = append(ordered, code)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if req.ProductID == "" {
		return nil, missingPrintUnits(req.UnitCodes)
	}
	return ordered, nil
}

func missingPrintUnits(codes []string) *PrintNotPrintableError {
	out := &PrintNotPrintableError{}
	for _, code := range codes {
		out.Units = append(out.Units, PrintUnitError{Code: code, Error: "unit_not_found"})
	}
	return out
}

func replayPrintJob(ctx context.Context, tx *sql.Tx, req MarkUnitsPrintedRequest, explicit []string) (json.RawMessage, error) {
	var id, product, variant string
	var receipt *string
	var codesJSON []byte
	var payload json.RawMessage
	err := tx.QueryRowContext(ctx, `SELECT id,product_id,COALESCE(variant_id::text,''),receipt_line_id,unit_codes,payload
		FROM spncx_print_jobs WHERE idempotency_key=$1`, req.IdempotencyKey).
		Scan(&id, &product, &variant, &receipt, &codesJSON, &payload)
	if err != nil {
		return nil, err
	}
	mismatch := &PrintConflictError{Reason: ErrPrintIdempotencyMismatch, JobID: id}
	if product != req.ProductID || variant != req.VariantID ||
		!equalPrintReceiptLines(receipt, req.ReceiptLineID) {
		return nil, mismatch
	}
	if len(req.UnitCodes) > 0 {
		var codes []string
		if err := json.Unmarshal(codesJSON, &codes); err != nil {
			return nil, err
		}
		if len(explicit) != len(req.UnitCodes) || !slices.Equal(sortedPrintCodes(codes), sortedPrintCodes(req.UnitCodes)) {
			return nil, mismatch
		}
		if req.SupplierID == "" {
			hidden, err := hiddenWarehousePrintCodes(ctx, tx, req.UnitCodes)
			if err != nil {
				return nil, err
			}
			if len(hidden) > 0 {
				return nil, missingPrintUnits(hidden)
			}
		}
	}
	return payload, nil
}

func equalPrintReceiptLines(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sortedPrintCodes(codes []string) []string {
	sorted := slices.Clone(codes)
	slices.Sort(sorted)
	return sorted
}

const printLockedColumns = `id,code,status,COALESCE(balance_id,''),printed_at IS NOT NULL`
const printableStatusesSQL = `status IN ('defined','in_stock','reserved','quarantined')`

func lockPrintUnits(ctx context.Context, tx *sql.Tx, req MarkUnitsPrintedRequest, explicit []string) ([]lockedProductUnit, error) {
	if len(req.UnitCodes) > 0 {
		locked := make(map[string]lockedProductUnit, len(explicit))
		for _, code := range explicit {
			var u lockedProductUnit
			err := tx.QueryRowContext(ctx, `SELECT `+printLockedColumns+` FROM product_units WHERE code=$1 FOR UPDATE`, code).
				Scan(&u.id, &u.code, &u.status, &u.balance, &u.printed)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			locked[code] = u
		}
		// Return in the request's order. A missing row keeps its code for the
		// validation response, with an empty ID.
		units := make([]lockedProductUnit, 0, len(req.UnitCodes))
		for _, code := range req.UnitCodes {
			u := locked[code]
			u.code = code
			units = append(units, u)
		}
		return units, nil
	}

	var rows *sql.Rows
	var err error
	if req.ReceiptLineID != nil {
		rows, err = tx.QueryContext(ctx, `SELECT `+printLockedColumns+` FROM product_units u
			WHERE product_id=$1 AND printed_at IS NULL AND `+printableStatusesSQL+`
			AND EXISTS (SELECT 1 FROM product_unit_events e WHERE e.unit_id=u.id
				AND e.event_type='minted' AND e.reference_type='inbound_receive' AND e.reference_id=$2)
			ORDER BY sequence,variant_id FOR UPDATE`, req.ProductID, *req.ReceiptLineID)
	} else {
		filter := ""
		if req.UnprintedOnly {
			filter = " AND printed_at IS NULL"
		}
		rows, err = tx.QueryContext(ctx, `SELECT `+printLockedColumns+` FROM product_units
			WHERE variant_id=$1`+filter+` AND `+printableStatusesSQL+` ORDER BY sequence LIMIT $2 FOR UPDATE`,
			req.VariantID, *req.Count)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLockedUnits(rows)
}

func hiddenWarehousePrintCodes(ctx context.Context, tx *sql.Tx, codes []string) ([]string, error) {
	staff := WarehouseStaffID(ctx)
	if staff == "" || len(codes) == 0 {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT u.code FROM product_units u
		JOIN inventory_balances b ON b.id=u.balance_id
		JOIN warehouses site ON site.id=b.warehouse_id JOIN users staff ON staff.id=$1
		WHERE u.code=ANY($2) AND `+warehouseStaffCityMatch+` FOR SHARE OF site,staff`, staff, pq.Array(codes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	visible := make(map[string]bool, len(codes))
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		visible[code] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var hidden []string
	for _, code := range codes {
		if !visible[code] {
			hidden = append(hidden, code)
		}
	}
	return hidden, nil
}

func markPrintJobUnits(ctx context.Context, tx *sql.Tx, req MarkUnitsPrintedRequest, explicit []string, jobID string) (json.RawMessage, error) {
	units, err := lockPrintUnits(ctx, tx, req, explicit)
	if err != nil {
		return nil, err
	}
	var hidden map[string]bool
	if req.SupplierID == "" {
		codes, err := hiddenWarehousePrintCodes(ctx, tx, req.UnitCodes)
		if err != nil {
			return nil, err
		}
		hidden = make(map[string]bool, len(codes))
		for _, code := range codes {
			hidden[code] = true
		}
	}
	invalid := &PrintNotPrintableError{}
	var printedCodes []string
	var firstPrintedID string
	for _, u := range units {
		if u.id == "" || hidden[u.code] {
			invalid.Units = append(invalid.Units, PrintUnitError{Code: u.code, Error: "unit_not_found"})
			continue
		}
		switch u.status {
		case "defined", "in_stock", "reserved", "quarantined":
		default:
			invalid.Units = append(invalid.Units, PrintUnitError{Code: u.code, Error: ErrPrintNotPrintable.Error(), Status: u.status})
		}
		if u.printed && !req.Reprint {
			if len(printedCodes) == 0 {
				firstPrintedID = u.id
			}
			printedCodes = append(printedCodes, u.code)
		}
	}
	if len(invalid.Units) > 0 {
		return nil, invalid
	}
	if len(printedCodes) > 0 {
		var priorJob string
		err := tx.QueryRowContext(ctx, `SELECT j.id FROM spncx_print_job_units ju
			JOIN spncx_print_jobs j ON j.id=ju.job_id WHERE ju.unit_id=$1
			ORDER BY j.created_at DESC,j.id DESC LIMIT 1`, firstPrintedID).Scan(&priorJob)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, &PrintConflictError{Reason: ErrPrintAlreadyPrinted, JobID: priorJob, UnitCodes: printedCodes}
	}
	return savePrintJob(ctx, tx, req, units, jobID)
}

func savePrintJob(ctx context.Context, tx *sql.Tx, req MarkUnitsPrintedRequest, units []lockedProductUnit, jobID string) (json.RawMessage, error) {
	out := printedUnitLabels{Units: make([]PrintLabelPayload, 0, len(units))}
	codes := make([]string, 0, len(units))
	for _, unit := range units {
		label := PrintLabelPayload{UnitLabelRow: UnitLabelRow{Code: unit.code, Status: unit.status}, BarcodeValue: unit.code}
		event := "printed"
		if unit.printed {
			event = "reprinted"
		}
		err := tx.QueryRowContext(ctx, `WITH marked AS (
			UPDATE product_units SET printed_at=COALESCE(printed_at,NOW()),last_printed_at=NOW(),
				print_count=print_count+1,printed_by=$2 WHERE id=$1
			RETURNING id,code,status,variant_id,product_id,printed_at,last_printed_at,print_count
		), history AS (
			INSERT INTO product_unit_events(unit_id,code,event_type,from_status,to_status,reference_type,reference_id,actor_user_id,metadata)
			SELECT id,code,$3,status,status,'label_print',$4,$2,
				jsonb_build_object('idempotency_key',$5::text,'physical_print_unconfirmed',true) FROM marked
		), job_units AS (
			INSERT INTO spncx_print_job_units(job_id,unit_id,code)
			SELECT $4::uuid,id,code FROM marked
		)
		SELECT m.variant_id,m.product_id,COALESCE(v.spnc,''),COALESCE(v.variant_name,''),
			COALESCE(p.spn,''),COALESCE(p.title,''),m.printed_at,m.last_printed_at,m.print_count
		FROM marked m JOIN product_variants v ON v.id=m.variant_id JOIN products p ON p.id=m.product_id`,
			unit.id, req.ActorUserID, event, jobID, req.IdempotencyKey).
			Scan(&label.VariantID, &label.ProductID, &label.SPNC, &label.VariantName,
				&label.SPN, &label.Title, &label.PrintedAt, &label.LastPrintedAt, &label.PrintCount)
		if err != nil {
			return nil, err
		}
		out.Units = append(out.Units, label)
		codes = append(codes, unit.code)
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	codesJSON, err := json.Marshal(codes)
	if err != nil {
		return nil, err
	}
	// Return the database's JSONB representation on the first call and on replay.
	err = tx.QueryRowContext(ctx, `UPDATE spncx_print_jobs SET unit_codes=$2,payload=$3,variant_id=NULLIF($4,'')::uuid,
		receipt_line_id=$5,reprint=$6,actor_user_id=$7 WHERE id=$1 RETURNING payload`,
		jobID, string(codesJSON), string(payload), req.VariantID, req.ReceiptLineID, req.Reprint, req.ActorUserID).Scan(&payload)
	return payload, err
}
