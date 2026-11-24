package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var (
	ErrUnitMismatch                = errors.New("unit_mismatch")
	ErrInboundLineNotFound         = errors.New("inbound_line_not_found")
	ErrIncompleteReturnReceipt     = errors.New("incomplete_return_receipt")
	ErrUnitCodesRequireReceiptLine = errors.New("unit_codes_require_receipt_line")
)

type UnitMismatch struct {
	Code     string `json:"code"`
	Error    string `json:"error"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type UnitMismatchError struct {
	LineID string
	Errors []UnitMismatch
}

func (e *UnitMismatchError) Error() string { return ErrUnitMismatch.Error() }
func (e *UnitMismatchError) Unwrap() error { return ErrUnitMismatch }

func (e *UnitMismatchError) add(line, code, expected, actual string) {
	if len(e.Errors) == 0 {
		e.LineID = line
	}
	e.Errors = append(e.Errors, UnitMismatch{Code: code, Error: ErrUnitMismatch.Error(), Expected: expected, Actual: actual})
}

func (e *UnitMismatchError) err() error {
	if len(e.Errors) == 0 {
		return nil
	}
	return e
}

func (e *UnitMismatchError) checkCount(line string, codes []string, quantity int) {
	if len(codes) != quantity {
		e.add(line, "", fmt.Sprintf("%d unit codes", quantity), fmt.Sprintf("%d unit codes", len(codes)))
	}
}

func (e *UnitMismatchError) checkDuplicates(line string, codes []string, seen map[string]bool) {
	for _, code := range codes {
		if seen[code] {
			e.add(line, code, "unique in request", "duplicate")
		}
		seen[code] = true
	}
}

func (e *UnitMismatchError) rejectCodes(line string, codes []string, expected, actual string) {
	if len(codes) == 0 {
		e.add(line, "", expected, actual)
	}
	for _, code := range codes {
		e.add(line, code, expected, actual)
	}
}

type UnitReceiptResult struct {
	LineID     string         `json:"line_id"`
	Quantity   int            `json:"quantity"`
	UnitCodes  []string       `json:"unit_codes"`
	Mismatched []UnitMismatch `json:"mismatched"`
}

func unitReceipt(line string, quantity int, codes []string) UnitReceiptResult {
	return UnitReceiptResult{LineID: line, Quantity: quantity, UnitCodes: append([]string{}, codes...), Mismatched: []UnitMismatch{}}
}

func parsePreprintedCode(code, spnc string) (int, bool) {
	prefix := spnc + "-X"
	suffix, ok := strings.CutPrefix(code, prefix)
	if !ok || len(suffix) != 4 {
		return 0, false
	}
	seq, err := strconv.Atoi(suffix)
	if err != nil || seq < 1 || seq > unitSequenceCap || code != formatUnitCode(spnc, seq) {
		return 0, false
	}
	return seq, true
}

func mintUnitsWithCodesTx(ctx context.Context, tx *sql.Tx, variantID, productID, balanceID string, codes []string, refType, refID, actor string) ([]domain.ProductUnit, error) {
	var spnc string
	if err := tx.QueryRowContext(ctx, `SELECT v.spnc FROM inventory_balances b JOIN product_variants v ON v.id=b.variant_id AND v.product_id=b.product_id
		WHERE b.id=$1 AND v.id=$2 AND b.product_id=$3 AND b.status='quarantine' AND b.location_key NOT LIKE 'return:%'`, balanceID, variantID, productID).Scan(&spnc); err != nil {
		return nil, err
	}
	next, err := lockOrCreateUnitCounterTx(ctx, tx, variantID)
	if err != nil {
		return nil, err
	}
	sequences := make([]int, len(codes))
	first, last := unitSequenceCap, 0
	for i, code := range codes {
		if raw, ok := strings.CutPrefix(code, spnc+"-X"); ok {
			if seq, err := strconv.Atoi(raw); err == nil && seq > unitSequenceCap {
				return nil, &UnitCapError{VariantID: variantID, SPNC: spnc, Requested: len(codes), Available: max(0, unitSequenceCap-next+1)}
			}
		}
		seq, ok := parsePreprintedCode(code, spnc)
		if !ok {
			return nil, ErrUnitMismatch
		}
		sequences[i] = seq
		first, last = min(first, seq), max(last, seq)
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT code FROM product_units WHERE code=ANY($1) LIMIT 1`, pq.Array(codes)).Scan(&existing)
	if err == nil {
		return nil, ErrUnitCodeCollision
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	units, err := insertMintedUnitsTx(ctx, tx, variantID, productID, balanceID, "quarantined", codes, sequences, refType, refID, actor)
	if err != nil {
		var pg *pq.Error
		if errors.As(err, &pg) && pg.Code == "23505" {
			return nil, ErrUnitCodeCollision
		}
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE product_variant_unit_counters SET next_sequence=$2,updated_at=NOW() WHERE variant_id=$1`, variantID, max(next, last+1)); err != nil {
		return nil, err
	}
	if err = creditMintedUnitsTx(ctx, tx, variantID, balanceID, len(codes), first, refType, refID, actor, true); err != nil {
		return nil, err
	}
	return units, nil
}

func lockUnitsByCodesTx(ctx context.Context, tx *sql.Tx, codes []string) (map[string]lockedProductUnit, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,code,status,COALESCE(balance_id,''),printed_at IS NOT NULL,variant_id
		FROM product_units WHERE code=ANY($1) ORDER BY id FOR UPDATE`, pq.Array(codes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byCode := make(map[string]lockedProductUnit, len(codes))
	for rows.Next() {
		var u lockedProductUnit
		if err := rows.Scan(&u.id, &u.code, &u.status, &u.balance, &u.printed, &u.variant); err != nil {
			return nil, err
		}
		byCode[u.code] = u
	}
	return byCode, rows.Err()
}

func sortedLotCodesTx(ctx context.Context, tx *sql.Tx, balanceID string) ([]string, error) {
	if balanceID == "" {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT code FROM product_units WHERE balance_id=$1 ORDER BY sequence`, balanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, rows.Err()
}

func codesForQuarantineLotTx(ctx context.Context, tx *sql.Tx, balanceID string) (map[string]bool, error) {
	codes, err := sortedLotCodesTx(ctx, tx, balanceID)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(codes))
	for _, code := range codes {
		set[code] = true
	}
	return set, nil
}

func inboundLineLotTx(ctx context.Context, tx *sql.Tx, l inboundPersistedLine, status string) (string, error) {
	id, err := inventoryLotIDTx(ctx, tx, l.Product, l.Variant, "inbound-line:"+l.ID, status)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func incomingUnitMismatch(u lockedProductUnit, lot, status, variant string) string {
	switch {
	case u.id == "":
		return "not_found"
	case u.status != status:
		return u.status
	case u.variant != variant:
		return "wrong_variant"
	case lot == "" || u.balance != lot:
		return "wrong_lot"
	default:
		return ""
	}
}

func lockInboundUnitSelectionTx(ctx context.Context, tx *sql.Tx, line, lot, status, variant string, codes []string, quantity int) ([]lockedProductUnit, error) {
	var mismatches UnitMismatchError
	mismatches.checkCount(line, codes, quantity)
	mismatches.checkDuplicates(line, codes, map[string]bool{})
	byCode, err := lockUnitsByCodesTx(ctx, tx, codes)
	if err != nil {
		return nil, err
	}
	units := make([]lockedProductUnit, 0, len(codes))
	for _, code := range codes {
		u := byCode[code]
		if actual := incomingUnitMismatch(u, lot, status, variant); actual != "" {
			mismatches.add(line, code, status, actual)
		} else {
			units = append(units, u)
		}
	}
	return units, mismatches.err()
}

func unitCodes(units []lockedProductUnit) []string {
	codes := make([]string, 0, len(units))
	for _, u := range units {
		codes = append(codes, u.code)
	}
	return codes
}

func (r *SupplierRepository) ReceiveInboundLine(ctx context.Context, lineID string, cumulativeReceived int, codes []string, actor string) (UnitReceiptResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return UnitReceiptResult{}, err
	}
	defer tx.Rollback()
	var shipment string
	err = tx.QueryRowContext(ctx, `SELECT shipment_id FROM inbound_inventory_lines WHERE id=$1`, lineID).Scan(&shipment)
	if err == sql.ErrNoRows {
		err = ErrInboundLineNotFound
	}
	if err != nil {
		return UnitReceiptResult{}, err
	}
	if err = lockInboundShipmentTx(ctx, tx, shipment); err != nil {
		return UnitReceiptResult{}, err
	}
	lines, err := inboundLinesTx(ctx, tx, shipment)
	if err != nil {
		return UnitReceiptResult{}, err
	}
	for _, l := range lines {
		if l.ID != lineID {
			continue
		}
		if err := lockWarehouseQCReceiptScopeTx(ctx, tx, shipment, l.Product, lineID, l.Supplier); err != nil {
			return UnitReceiptResult{}, err
		}
		result, err := receiveInboundLineResultTx(ctx, tx, l, cumulativeReceived, codes, actor)
		if err != nil {
			return UnitReceiptResult{}, err
		}
		return result, tx.Commit()
	}
	return UnitReceiptResult{}, ErrInboundLineNotFound
}

func receiveInboundLineResultTx(ctx context.Context, tx *sql.Tx, l inboundPersistedLine, cumulative int, codes []string, actor string) (UnitReceiptResult, error) {
	if err := applyInboundLineReceiptTx(ctx, tx, l, cumulative, codes, actor); err != nil {
		return UnitReceiptResult{}, err
	}
	return inboundLineReceiptEchoTx(ctx, tx, l, cumulative)
}

func inboundLineReceiptEchoTx(ctx context.Context, tx *sql.Tx, l inboundPersistedLine, cumulative int) (UnitReceiptResult, error) {
	lot, err := inboundLineLotTx(ctx, tx, l, "quarantine")
	if err != nil {
		return UnitReceiptResult{}, err
	}
	echo, err := sortedLotCodesTx(ctx, tx, lot)
	return unitReceipt(l.ID, cumulative, echo), err
}

type inboundUnitArrivals struct {
	quarantine string
	codes      []string
	units      []lockedProductUnit
}

func validateInboundArrivalsTx(ctx context.Context, tx *sql.Tx, l inboundPersistedLine, cumulative int, codes []string, seen map[string]bool, mismatches *UnitMismatchError) (inboundUnitArrivals, error) {
	var arrivals inboundUnitArrivals
	if cumulative < l.Received || cumulative > l.Dispatched {
		return arrivals, ErrInboundProvenance
	}
	mismatches.checkDuplicates(l.ID, codes, seen)
	mismatches.checkCount(l.ID, codes, cumulative)
	if l.Variant == "" {
		mismatches.rejectCodes(l.ID, codes, "variant SPNC", "line has no variant")
		return arrivals, nil
	}
	var err error
	arrivals.quarantine, err = inboundLineLotTx(ctx, tx, l, "quarantine")
	if err != nil {
		return arrivals, err
	}
	previous, err := codesForQuarantineLotTx(ctx, tx, arrivals.quarantine)
	if err != nil {
		return arrivals, err
	}
	var transit, spnc string
	if l.Kind == "external_replenishment" {
		err = tx.QueryRowContext(ctx, `SELECT spnc FROM product_variants WHERE id=$1`, l.Variant).Scan(&spnc)
	} else {
		transit, err = inboundLineLotTx(ctx, tx, l, "in_transit")
	}
	if err != nil {
		return arrivals, err
	}
	byCode, err := lockUnitsByCodesTx(ctx, tx, codes)
	if err != nil {
		return arrivals, err
	}
	start := len(mismatches.Errors)
	for _, code := range codes {
		u := byCode[code]
		if previous[code] && u.status == "quarantined" && u.variant == l.Variant {
			continue
		}
		arrivals.codes = append(arrivals.codes, code)
		if l.Kind == "external_replenishment" {
			beforeSequence := strings.LastIndex(code, "-X")
			_, valid := parsePreprintedCode(code, spnc)
			switch {
			case beforeSequence >= 0 && code[:beforeSequence] != spnc:
				mismatches.add(l.ID, code, spnc, code[:beforeSequence])
			case u.id != "":
				mismatches.add(l.ID, code, "not-yet-issued", u.status)
			case !valid:
				mismatches.add(l.ID, code, spnc+"-X0001…X9999", code)
			}
		} else if actual := incomingUnitMismatch(u, transit, "reserved", l.Variant); actual != "" {
			mismatches.add(l.ID, code, "reserved", actual)
		} else {
			arrivals.units = append(arrivals.units, u)
		}
	}
	if len(arrivals.codes) != cumulative-l.Received && len(mismatches.Errors) == start {
		if len(arrivals.codes) > cumulative-l.Received {
			mismatches.rejectCodes(l.ID, arrivals.codes, "quarantined or new arrival for this receipt", "cumulative set omits previously received units")
		} else {
			mismatches.add(l.ID, "", fmt.Sprintf("%d new arrivals", cumulative-l.Received), fmt.Sprintf("%d new arrivals", len(arrivals.codes)))
		}
	}
	return arrivals, nil
}

func applyInboundLineReceiptTx(ctx context.Context, tx *sql.Tx, l inboundPersistedLine, cumulative int, codes []string, actor string) error {
	if cumulative < l.Received || cumulative > l.Dispatched {
		return ErrInboundProvenance
	}
	var arrivals inboundUnitArrivals
	if codes != nil {
		var mismatches UnitMismatchError
		var err error
		arrivals, err = validateInboundArrivalsTx(ctx, tx, l, cumulative, codes, map[string]bool{}, &mismatches)
		if err != nil {
			return err
		}
		if err := mismatches.err(); err != nil {
			return err
		}
	}
	return applyInboundArrivalsTx(ctx, tx, l, cumulative, arrivals, actor)
}

func applyInboundArrivalsTx(ctx context.Context, tx *sql.Tx, l inboundPersistedLine, cumulative int, arrivals inboundUnitArrivals, actor string) error {
	n := cumulative - l.Received
	if n == 0 {
		return nil
	}
	if l.Kind == "external_replenishment" && len(arrivals.codes) > 0 {
		lot := arrivals.quarantine
		if lot == "" {
			var err error
			lot, err = creditReturnLotTx(ctx, tx, l.Product, l.Supplier, l.Variant, "quarantine", "inbound-line:"+l.ID, "warehouse:"+l.Warehouse, 0)
			if err != nil {
				return err
			}
		}
		if _, err := mintUnitsWithCodesTx(ctx, tx, l.Variant, l.Product, lot, arrivals.codes, "inbound_receive", l.ID, actor); err != nil {
			if errors.Is(err, ErrUnitCodeCollision) || errors.Is(err, ErrUnitMismatch) {
				var mismatches UnitMismatchError
				mismatches.rejectCodes(l.ID, arrivals.codes, "not-yet-issued", err.Error())
				return &mismatches
			}
			return err
		}
	} else {
		from := "in_transit"
		if l.Kind == "external_replenishment" {
			from = "external"
		}
		if err := inboundMoveTx(ctx, tx, l, from, "quarantine", n, actor, arrivals.units...); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE inbound_inventory_lines SET received_quantity=$2 WHERE id=$1`, l.ID, cumulative)
	return err
}

func moveSelectedInboundUnitsTx(ctx context.Context, tx *sql.Tx, units []lockedProductUnit, source, target, status, refType, refID, actor string) error {
	var err error
	if status == "defined" && refType == "inbound_qc_disposition" {
		err = writeUnitTransitionsTx(ctx, tx, units, status, target, "qc_accepted", refType, refID, actor)
	} else {
		err = transitionUnitsTx(ctx, tx, units, status, target, status, refType, refID, actor)
	}
	if err != nil {
		return err
	}
	if err := refreshUnitTrackedLotTx(ctx, tx, source); err != nil {
		return err
	}
	return refreshUnitTrackedLotTx(ctx, tx, target)
}

func recordShipmentItemReceiptTx(ctx context.Context, tx *sql.Tx, shipmentID, variantID string, receivedQty int, qcStatus string) error {
	_, err := tx.ExecContext(ctx, `UPDATE shipment_items SET received_qty=$3,inventory_received_qty=$3,qc_status=$4 WHERE shipment_id=$1 AND variant_id=$2`, shipmentID, variantID, receivedQty, qcStatus)
	return err
}

type inboundDispositionCounts struct {
	passed, failed, shortage int
	reason                   string
}

func latestInboundDispositionTx(ctx context.Context, tx *sql.Tx, line string) (inboundDispositionCounts, error) {
	var prior inboundDispositionCounts
	err := tx.QueryRowContext(ctx, `SELECT quantity_passed,quantity_failed,quantity_shortage,reason FROM inbound_qc_dispositions
		WHERE receipt_line_id=$1 ORDER BY cumulative_disposed DESC LIMIT 1`, line).Scan(&prior.passed, &prior.failed, &prior.shortage, &prior.reason)
	if err == sql.ErrNoRows {
		err = nil
	}
	return prior, err
}

type dispositionUnitSelection struct {
	PassedCodes, FailedCodes []string
	passedUnits, failedUnits []lockedProductUnit
	passedEcho               *[]string
}

func selectDispositionUnitsTx(ctx context.Context, tx *sql.Tx, l inboundPersistedLine, codes []string, passed, failed int, echo *[]string) (dispositionUnitSelection, error) {
	selection := dispositionUnitSelection{passedEcho: echo}
	var mismatches UnitMismatchError
	if l.Variant == "" {
		mismatches.rejectCodes(l.ID, codes, "variant SPNC", "line has no variant")
	}
	if passed == 0 && failed == 0 {
		mismatches.rejectCodes(l.ID, codes, "unit_codes omitted without a pass or fail delta", "unit_codes present")
	}
	if err := mismatches.err(); err != nil {
		return selection, err
	}
	lot, err := inboundLineLotTx(ctx, tx, l, "quarantine")
	if err != nil {
		return selection, err
	}
	// Mixed submissions scan failed units even when both deltas are equal.
	// The passed units are picked FIFO after this failed selection moves.
	if failed > 0 {
		selection.FailedCodes = codes
		selection.failedUnits, err = lockInboundUnitSelectionTx(ctx, tx, l.ID, lot, "quarantined", l.Variant, codes, failed)
	} else {
		selection.PassedCodes = codes
		selection.passedUnits, err = lockInboundUnitSelectionTx(ctx, tx, l.ID, lot, "quarantined", l.Variant, codes, passed)
	}
	return selection, err
}

func (r *WarehouseRepository) QCReceiptLine(ctx context.Context, lineID string) (shipmentID, productID string, err error) {
	err = r.db.QueryRowContext(ctx, `SELECT shipment_id, product_id FROM inbound_inventory_lines WHERE id=$1`, lineID).Scan(&shipmentID, &productID)
	return
}

func (r *WarehouseRepository) RecordQCLinePassDelta(ctx context.Context, lineID string, deltaQty int, codes []string, actor string) (UnitReceiptResult, error) {
	shipment, product, err := r.QCReceiptLine(ctx, lineID)
	if err == sql.ErrNoRows {
		err = ErrInboundLineNotFound
	}
	if err != nil {
		return UnitReceiptResult{}, err
	}
	return r.RecordResolvedQCLinePassDelta(ctx, shipment, product, lineID, deltaQty, codes, actor)
}

func (r *WarehouseRepository) RecordResolvedQCLinePassDelta(ctx context.Context, shipment, product, lineID string, deltaQty int, codes []string, actor string) (UnitReceiptResult, error) {
	if deltaQty <= 0 {
		return UnitReceiptResult{}, ErrInboundProvenance
	}
	var echo []string
	err := r.recordInboundDisposition(ctx, product, shipment, actor, 0, 0, 0, "", codes, deltaQty, &echo, lineID)
	return unitReceipt(lineID, deltaQty, echo), err
}

type CustomerReturnUnitItem struct {
	OrderItemID string   `json:"order_item_id"`
	Qty         int      `json:"qty"`
	UnitCodes   []string `json:"unit_codes"`
}

func (r *OrderRepository) ReceiveCustomerReturnUnits(ctx context.Context, id string, items []CustomerReturnUnitItem, actor string) ([]UnitReceiptResult, error) {
	returnID, err := uuid.Parse(id)
	if err != nil {
		return nil, sql.ErrNoRows
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var receipts []UnitReceiptResult
	if _, err = r.receiveCustomerReturnTx(ctx, tx, returnID.String(), nil, items, &receipts, actor); err != nil {
		return nil, err
	}
	return receipts, nil
}

func validateCustomerReturnReceiptTx(ctx context.Context, tx *sql.Tx, id string, items []CustomerReturnUnitItem) ([]CustomerReturnItemQC, map[string][]string, []UnitReceiptResult, error) {
	rows, err := tx.QueryContext(ctx, `SELECT ri.id::text, ri.order_item_id, ri.qty, COALESCE(ri.variant_id::text,oi.variant_id::text,''), oi.id IS NOT NULL
		FROM return_items ri LEFT JOIN order_items oi ON oi.id::text=ri.order_item_id WHERE ri.return_id=$1`, id)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	type returnLine struct {
		id, variant string
		qty         int
		orderExists bool
	}
	lines := map[string]returnLine{}
	for rows.Next() {
		var line returnLine
		var orderItem string
		if err := rows.Scan(&line.id, &orderItem, &line.qty, &line.variant, &line.orderExists); err != nil {
			return nil, nil, nil, err
		}
		// Historical returns can split one order item across multiple
		// return rows. This request shape has no return-item ID to disambiguate.
		if _, duplicate := lines[orderItem]; duplicate {
			return nil, nil, nil, ErrIncompleteReturnReceipt
		}
		lines[orderItem] = line
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	qc := make([]CustomerReturnItemQC, 0, len(items))
	receipts := make([]UnitReceiptResult, 0, len(items))
	codes := make(map[string][]string, len(items))
	whole := len(items) > 0 && len(items) == len(lines)
	for _, item := range items {
		line, exists := lines[item.OrderItemID]
		if !exists || !line.orderExists {
			return nil, nil, nil, sql.ErrNoRows
		}
		_, duplicate := codes[line.id]
		whole = whole && !duplicate && item.Qty > 0 && item.Qty == line.qty
		qc = append(qc, CustomerReturnItemQC{ReturnItemID: line.id, QCStatus: "ok"})
		receipts = append(receipts, unitReceipt(line.id, line.qty, nil))
		codes[line.id] = item.UnitCodes
	}
	if !whole {
		return nil, nil, nil, ErrIncompleteReturnReceipt
	}

	var allCodes []string
	for _, item := range items {
		allCodes = append(allCodes, item.UnitCodes...)
	}
	var locked map[string]lockedProductUnit
	if len(allCodes) > 0 {
		locked, err = lockUnitsByCodesTx(ctx, tx, allCodes)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	var mismatches UnitMismatchError
	seen := map[string]bool{}
	// Validate every line under the same transaction as settlement. A bad scan
	// invalidates the whole receipt and leaves every item and unit untouched.
	for _, item := range items {
		if item.UnitCodes == nil {
			continue
		}
		line := lines[item.OrderItemID]
		mismatches.checkCount(line.id, item.UnitCodes, line.qty)
		mismatches.checkDuplicates(line.id, item.UnitCodes, seen)
		if err := validateReturnItemCodesTx(ctx, tx, line.id, item.OrderItemID, line.variant, item.UnitCodes, locked, &mismatches); err != nil {
			return nil, nil, nil, err
		}
	}
	if err := mismatches.err(); err != nil {
		return nil, nil, nil, err
	}
	return qc, codes, receipts, nil
}

func validateReturnItemCodesTx(ctx context.Context, tx *sql.Tx, line, orderItem, variant string, codes []string, locked map[string]lockedProductUnit, mismatches *UnitMismatchError) error {
	if len(codes) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT u.code, ou.restored_at IS NULL AND u.variant_id::text=$3
		FROM order_item_units ou JOIN product_units u ON u.id=ou.unit_id
		WHERE ou.order_item_id=$1 AND u.code=ANY($2)`, orderItem, pq.Array(codes), variant)
	if err != nil {
		return err
	}
	defer rows.Close()
	captured := map[string]bool{}
	for rows.Next() {
		var code string
		var eligible bool
		if err := rows.Scan(&code, &eligible); err != nil {
			return err
		}
		captured[code] = eligible
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, code := range codes {
		u := locked[code]
		eligible, linked := captured[code]
		switch {
		case u.id == "":
			mismatches.add(line, code, "sold", "not_found")
		case !linked:
			mismatches.add(line, code, "sold", "not_captured_for_item")
		case !eligible || u.status != "sold":
			mismatches.add(line, code, "sold", u.status)
		}
	}
	return nil
}
