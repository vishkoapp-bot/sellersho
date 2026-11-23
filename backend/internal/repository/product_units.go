package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var (
	ErrUnitCapExceeded                = errors.New("unit_cap_exceeded")
	ErrUnitCodeCollision              = errors.New("unit_code_collision")
	ErrInvalidUnitTransition          = errors.New("invalid_unit_transition")
	ErrInvalidUnitMint                = errors.New("invalid_unit_mint")
	ErrUnitCoverageGap                = fmt.Errorf("unit_coverage_gap: %w", ErrInsufficientInventory)
	ErrCapturePendingManualResolution = errors.New("capture_pending_manual_resolution")
	spncxStrict, _                    = strconv.ParseBool(os.Getenv("SPNCX_STRICT"))
)

const unitSequenceCap = 9999

type UnitCapError struct {
	VariantID string
	SPNC      string
	Requested int
	Available int
}

func (e *UnitCapError) Error() string { return ErrUnitCapExceeded.Error() }
func (e *UnitCapError) Unwrap() error { return ErrUnitCapExceeded }

func formatUnitCode(spnc string, seq int) string {
	return fmt.Sprintf("%s-X%04d", spnc, seq)
}

func unitStatusForLot(status, location string) string {
	switch status {
	case "sellable":
		return "in_stock"
	case "sales_reserved", "return_requested", "inbound_reserved", "in_transit":
		return "reserved"
	case "quarantine":
		if strings.HasPrefix(location, "return:") {
			return "returned"
		}
		return "quarantined"
	case "qc_accepted", "unreconciled":
		return "defined"
	case "damaged", "sold":
		return status
	}
	return ""
}

func unitTransitionAllowed(from, to string) bool {
	switch from {
	case "defined":
		return to == "in_stock" || to == "quarantined" || to == "damaged" || to == "removed"
	case "in_stock":
		return to == "reserved" || to == "sold" || to == "quarantined" || to == "damaged" || to == "removed"
	case "reserved":
		return to == "in_stock" || to == "quarantined" || to == "damaged" || to == "removed"
	case "sold":
		return to == "in_stock" || to == "returned"
	case "returned":
		return to == "in_stock" || to == "quarantined" || to == "damaged"
	case "quarantined":
		return to == "in_stock" || to == "damaged" || to == "removed"
	case "damaged":
		return to == "removed"
	}
	return false
}

func lockExistingUnitCounterTx(ctx context.Context, tx *sql.Tx, variant string) error {
	var next int
	err := tx.QueryRowContext(ctx, `SELECT next_sequence FROM product_variant_unit_counters WHERE variant_id=$1 FOR UPDATE`, variant).Scan(&next)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

// mintUnitsTx holds the counter lock before changing any lot or unit rows.
// A caller that preclaims its idempotent movement passes that movement's key.
func mintUnitsTx(ctx context.Context, tx *sql.Tx, variantID, productID, balanceID string, n int, status, refType, refID, actor string, movementKey ...string) ([]domain.ProductUnit, error) {
	if n < 1 || (status != "defined" && status != "in_stock" && status != "quarantined") {
		return nil, ErrInvalidUnitMint
	}
	var lotStatus, location, spnc string
	err := tx.QueryRowContext(ctx, `SELECT b.status,b.location_key,v.spnc FROM inventory_balances b JOIN product_variants v ON v.id=b.variant_id AND v.product_id=b.product_id WHERE b.id=$1 AND b.variant_id=$2 AND b.product_id=$3`, balanceID, variantID, productID).Scan(&lotStatus, &location, &spnc)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && unitStatusForLot(lotStatus, location) != status) {
		return nil, ErrInvalidUnitMint
	}
	if err != nil {
		return nil, err
	}
	var next int
	readCounter := func() error {
		return tx.QueryRowContext(ctx, `SELECT next_sequence FROM product_variant_unit_counters WHERE variant_id=$1 FOR UPDATE`, variantID).Scan(&next)
	}
	if err = readCounter(); errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO product_variant_unit_counters(variant_id) VALUES($1) ON CONFLICT DO NOTHING`, variantID); err != nil {
			return nil, err
		}
		err = readCounter()
	}
	if err != nil {
		return nil, err
	}

	for attempt := 0; attempt < 2; attempt++ {
		available := unitSequenceCap - next + 1
		if n > available {
			log.Printf("unit_cap_exceeded variant_id=%s requested=%d available=%d", variantID, n, available)
			return nil, &UnitCapError{VariantID: variantID, SPNC: spnc, Requested: n, Available: available}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE product_variant_unit_counters SET next_sequence=$2,updated_at=NOW() WHERE variant_id=$1`, variantID, next+n); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `SAVEPOINT spncx_mint`); err != nil {
			return nil, err
		}
		codes := make([]string, n)
		for i := range codes {
			codes[i] = formatUnitCode(spnc, next+i)
		}
		rows, err := tx.QueryContext(ctx, `WITH minted AS (
			INSERT INTO product_units(code,variant_id,product_id,sequence,status,balance_id)
			SELECT code,$1,$2,$3::int+ordinality::int-1,$4,$5 FROM unnest($6::text[]) WITH ORDINALITY AS code_list(code,ordinality)
			RETURNING id,code,sequence,issued_at,created_at,updated_at
		), events AS (
			INSERT INTO product_unit_events(unit_id,code,event_type,to_status,reference_type,reference_id,actor_user_id)
			SELECT id,code,'minted',$4,$7,NULLIF($8,''),NULLIF($9,'') FROM minted
		) SELECT id,code,sequence,issued_at,created_at,updated_at FROM minted ORDER BY sequence`, variantID, productID, next, status, balanceID, pq.Array(codes), refType, refID, actor)
		var units []domain.ProductUnit
		if err == nil {
			for rows.Next() {
				unit := domain.ProductUnit{VariantID: variantID, ProductID: productID, Status: status, BalanceID: &balanceID}
				if err = rows.Scan(&unit.ID, &unit.Code, &unit.Sequence, &unit.IssuedAt, &unit.CreatedAt, &unit.UpdatedAt); err != nil {
					break
				}
				units = append(units, unit)
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
		}
		if err != nil {
			var pg *pq.Error
			if !errors.As(err, &pg) || pg.Code != "23505" || (pg.Constraint != "product_units_code_key" && pg.Constraint != "product_units_variant_id_sequence_key") {
				return nil, err
			}
			if attempt == 1 {
				log.Printf("unit_code_collision variant_id=%s spnc=%s", variantID, spnc)
				return nil, ErrUnitCodeCollision
			}
			if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT spncx_mint`); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT spncx_mint`); err != nil {
				return nil, err
			}
			if err := readCounter(); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT spncx_mint`); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity+$2,updated_at=NOW() WHERE id=$1`, balanceID, n); err != nil {
			return nil, err
		}
		if err := refreshUnitTrackedLotTx(ctx, tx, balanceID); err != nil {
			return nil, err
		}
		if len(movementKey) == 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata)
				SELECT $1,product_id,supplier_id,variant_id,$2,'external',status,'external',location_key,$3,$4,$5,NULLIF($6,''),'{}' FROM inventory_balances WHERE id=$7`, "IM-"+uuid.NewString(), n, refType, refID, fmt.Sprintf("spncx:%s:%s:%s:%d", refType, refID, variantID, next), actor, balanceID); err != nil {
				return nil, err
			}
		}
		if next+n-1 >= 9000 {
			log.Printf("unit_sequence_high_watermark variant_id=%s sequence=%d", variantID, next+n-1)
		}
		return units, nil
	}
	return nil, ErrUnitCodeCollision
}

// Keep existing mixed lots untracked until every physical unit is covered.
func refreshUnitTrackedLotTx(ctx context.Context, tx *sql.Tx, balance string) error {
	if balance == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE inventory_balances b SET unit_tracked=(b.quantity=(SELECT COUNT(*) FROM product_units u WHERE u.balance_id=b.id))
		WHERE b.id=$1 AND b.variant_id IS NOT NULL`, balance)
	return err
}

type lockedProductUnit struct {
	id, code, status, balance string
	printed                   bool
}

// transitionUnitsTx receives already locked units and a destination lot whose
// quantity has been updated by the caller in the same transaction.
func transitionUnitsTx(ctx context.Context, tx *sql.Tx, units []lockedProductUnit, to, balance, event, refType, refID, actor string) error {
	for _, u := range units {
		if !unitTransitionAllowed(u.status, to) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidUnitTransition, u.status, to)
		}
	}
	return writeUnitTransitionsTx(ctx, tx, units, to, balance, event, refType, refID, actor)
}

func writeUnitTransitionsTx(ctx context.Context, tx *sql.Tx, units []lockedProductUnit, to, balance, event, refType, refID, actor string) error {
	if len(units) == 0 {
		return nil
	}
	ids := make([]string, len(units))
	for i, u := range units {
		ids[i] = u.id
	}
	_, err := tx.ExecContext(ctx, `WITH changed AS (
		UPDATE product_units SET status=$2,balance_id=NULLIF($3,''),updated_at=NOW()
		WHERE id=ANY($1::uuid[]) RETURNING id,code
	)
	INSERT INTO product_unit_events(unit_id,code,event_type,from_status,to_status,reference_type,reference_id,actor_user_id)
	SELECT c.id,c.code,$4,src.status,$2,$5,NULLIF($6,''),NULLIF($7,'') FROM changed c JOIN unnest($1::uuid[],$8::text[]) src(id,status) ON src.id=c.id`, pq.Array(ids), to, balance, event, refType, refID, actor, pq.Array(unitStatuses(units)))
	return err
}

func unitStatuses(units []lockedProductUnit) []string {
	statuses := make([]string, len(units))
	for i, u := range units {
		statuses[i] = u.status
	}
	return statuses
}

func selectLotUnitsTx(ctx context.Context, tx *sql.Tx, balance, status string, n int, retire bool) ([]lockedProductUnit, error) {
	order := "sequence"
	if retire {
		order = "(printed_at IS NULL) DESC, sequence DESC"
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,code,status,balance_id,printed_at IS NOT NULL FROM product_units WHERE balance_id=$1 AND status=$2 ORDER BY `+order+` LIMIT $3 FOR UPDATE`, balance, status, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLockedUnits(rows)
}

func scanLockedUnits(rows *sql.Rows) ([]lockedProductUnit, error) {
	var units []lockedProductUnit
	for rows.Next() {
		var u lockedProductUnit
		if err := rows.Scan(&u.id, &u.code, &u.status, &u.balance, &u.printed); err != nil {
			return nil, err
		}
		units = append(units, u)
	}
	return units, rows.Err()
}

type UnitCoverageGap struct {
	Shortfall int    `json:"shortfall"`
	Reason    string `json:"reason"`
}

func auditUnitGapTx(ctx context.Context, tx *sql.Tx, product, variant, operation, order string, requested, allocated int) error {
	if requested <= allocated {
		return nil
	}
	var orderID *string
	if order != "" {
		orderID = &order
	}
	raw, err := json.Marshal(map[string]any{"unit_coverage_gap": true, "variant_id": variant, "operation": operation, "requested": requested, "allocated": allocated, "shortfall": requested - allocated, "order_id": orderID})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,metadata)
		SELECT $1,id,supplier_id,NULLIF($2,'')::uuid,$3,'coverage_gap','coverage_gap','coverage_gap','coverage_gap','unit_coverage_gap',$4,$1,$5 FROM products WHERE id=$6`, "IM-"+uuid.NewString(), variant, requested-allocated, operation, raw, product)
	if err == nil {
		log.Printf("unit_coverage_gap product_id=%s %s", product, raw)
	}
	return err
}

func selectCaptureUnitsTx(ctx context.Context, tx *sql.Tx, lots []skuAllocation, count int) (map[string][]lockedProductUnit, error) {
	ids := make([]string, len(lots))
	for i, a := range lots {
		ids[i] = a.BalanceID
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,code,status,balance_id,printed_at IS NOT NULL FROM product_units
		WHERE balance_id=ANY($1) AND status='in_stock' ORDER BY sequence,id LIMIT $2 FOR UPDATE`, pq.Array(ids), count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	units, err := scanLockedUnits(rows)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]lockedProductUnit)
	for _, u := range units {
		result[u.balance] = append(result[u.balance], u)
	}
	return result, nil
}

func allocateUnitsForCaptureTx(ctx context.Context, tx *sql.Tx, it inventoryCaptureItem, source string, units []lockedProductUnit) ([]string, error) {
	codes := make([]string, 0, len(units))
	if len(units) == 0 {
		return codes, nil
	}
	sold := "INV-" + uuid.NewString()
	_, err := tx.ExecContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled,unit_tracked)
		SELECT $1,product_id,supplier_id,variant_id,owner_type,'warehouse',$2,'sold',$3,TRUE,TRUE FROM inventory_balances WHERE id=$4`, sold, "spncx-sold:"+it.ID+":"+source, len(units), source)
	if err != nil {
		return nil, err
	}
	if err = transitionUnitsTx(ctx, tx, units, "sold", sold, "sold", "order_capture", it.ID, ""); err != nil {
		return nil, err
	}
	for _, u := range units {
		if _, err = tx.ExecContext(ctx, `INSERT INTO order_item_units(order_item_id,unit_id,code,order_id) VALUES($1,$2,$3,$4)`, it.ID, u.id, u.code, it.OrderID); err != nil {
			return nil, err
		}
		codes = append(codes, u.code)
	}
	if err := refreshUnitTrackedLotTx(ctx, tx, source); err != nil {
		return nil, err
	}
	return codes, nil
}

func capturedUnitsTx(ctx context.Context, tx *sql.Tx, orderItem string, codes []string, states []string, limit int) ([]lockedProductUnit, error) {
	rows, err := tx.QueryContext(ctx, `SELECT u.id,u.code,u.status,u.balance_id,u.printed_at IS NOT NULL
		FROM order_item_units ou JOIN product_units u ON u.id=ou.unit_id
		WHERE ou.order_item_id=$1 AND ou.restored_at IS NULL AND ($2::text[] IS NULL OR u.code=ANY($2)) AND u.status=ANY($3)
		ORDER BY u.sequence LIMIT $4 FOR UPDATE OF u`, orderItem, pq.Array(codes), pq.Array(states), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLockedUnits(rows)
}

func debitUnitSourceLotsTx(ctx context.Context, tx *sql.Tx, units []lockedProductUnit) error {
	for _, u := range units {
		res, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-1,updated_at=NOW() WHERE id=$1 AND quantity>0`, u.balance)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			return fmt.Errorf("missing unit source balance %s: %w", u.balance, ErrInsufficientInventory)
		}
	}
	return nil
}

func markUnitsRestoredTx(ctx context.Context, tx *sql.Tx, item string, units []lockedProductUnit) error {
	ids := make([]string, len(units))
	for i, u := range units {
		ids[i] = u.id
	}
	_, err := tx.ExecContext(ctx, `UPDATE order_item_units SET restored_at=NOW() WHERE order_item_id=$1 AND unit_id=ANY($2::uuid[]) AND restored_at IS NULL`, item, pq.Array(ids))
	return err
}

func restoreUnitsTx(ctx context.Context, tx *sql.Tx, item string, a skuAllocation) error {
	if len(a.UnitCodes) == 0 {
		return nil
	}
	units, err := capturedUnitsTx(ctx, tx, item, a.UnitCodes, []string{"sold"}, len(a.UnitCodes))
	if err != nil {
		return err
	}
	if len(units) != len(a.UnitCodes) {
		return fmt.Errorf("%w: incomplete unit restore", ErrOrderCancellationState)
	}
	if err = debitUnitSourceLotsTx(ctx, tx, units); err != nil {
		return err
	}
	if err = transitionUnitsTx(ctx, tx, units, "in_stock", a.BalanceID, "restored", "order_cancel", item, ""); err != nil {
		return err
	}
	return markUnitsRestoredTx(ctx, tx, item, units)
}

func captureFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrUnitCoverageGap):
		return "unit_coverage_gap"
	case errors.Is(err, ErrUnitCapExceeded):
		return "unit_cap_exceeded"
	case errors.Is(err, ErrUnitCodeCollision):
		return "unit_code_collision"
	case errors.Is(err, ErrInsufficientInventory):
		return "insufficient_inventory"
	default:
		return "capture_failed"
	}
}

func guardFailedCaptureTx(ctx context.Context, tx *sql.Tx, order string) error {
	var available, queued bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('spncx_failed_captures') IS NOT NULL`).Scan(&available); err != nil || !available {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM spncx_failed_captures WHERE order_id=$1)`, order).Scan(&queued); err != nil {
		return err
	}
	if queued {
		return ErrCapturePendingManualResolution
	}
	return nil
}

func retireUnitsTx(ctx context.Context, tx *sql.Tx, product, variant, balance string, n, held int, actor string) ([]string, []string, error) {
	units, err := selectLotUnitsTx(ctx, tx, balance, "in_stock", n, true)
	if err != nil {
		return nil, nil, err
	}
	if held > 0 {
		var inStock int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_units WHERE variant_id=$1 AND status='in_stock'`, variant).Scan(&inStock); err != nil {
			return nil, nil, err
		}
		if inStock-len(units) < held {
			return nil, nil, ErrStockBelowHeld
		}
	}
	if len(units) < n {
		if spncxStrict {
			return nil, nil, ErrUnitCoverageGap
		}
		if err = auditUnitGapTx(ctx, tx, product, variant, "adjust_decrease", "", n, len(units)); err != nil {
			return nil, nil, err
		}
	}
	if err = transitionUnitsTx(ctx, tx, units, "removed", "", "retired_by_adjustment", "sku_stock_adjustment", variant, actor); err != nil {
		return nil, nil, err
	}
	codes, printed := make([]string, 0, len(units)), []string{}
	for _, u := range units {
		codes = append(codes, u.code)
		if u.printed {
			printed = append(printed, u.code)
		}
	}
	return codes, printed, nil
}

// moveLotUnitsTx accompanies an already booked quantity move. A relocation
// within the same status records custody without changing the unit lifecycle.
func moveLotUnitsTx(ctx context.Context, tx *sql.Tx, product, variant, source, destination, from, to string, quantity int, operation, refType, refID, actor string) ([]string, error) {
	units, err := selectLotUnitsTx(ctx, tx, source, from, quantity, false)
	if err != nil {
		return nil, err
	}
	if len(units) < quantity {
		if spncxStrict {
			return nil, ErrUnitCoverageGap
		}
		if err = auditUnitGapTx(ctx, tx, product, variant, operation, "", quantity, len(units)); err != nil {
			return nil, err
		}
	}
	switch {
	case from == to:
		err = writeUnitTransitionsTx(ctx, tx, units, to, destination, "relocated", refType, refID, actor)
	case from == "quarantined" && to == "defined" && refType == "inbound_qc_disposition":
		// The ASN QC staging step is scoped to its immutable disposition.
		err = writeUnitTransitionsTx(ctx, tx, units, to, destination, "qc_accepted", refType, refID, actor)
	default:
		err = transitionUnitsTx(ctx, tx, units, to, destination, to, refType, refID, actor)
	}
	if err != nil {
		return nil, err
	}
	if err = refreshUnitTrackedLotTx(ctx, tx, source); err != nil {
		return nil, err
	}
	if err = refreshUnitTrackedLotTx(ctx, tx, destination); err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(units))
	for _, u := range units {
		codes = append(codes, u.code)
	}
	return codes, nil
}

func inventoryLotIDTx(ctx context.Context, tx *sql.Tx, product, variant, location, status string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM inventory_balances WHERE product_id=$1 AND variant_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid
		AND location_key=$3 AND status=$4 FOR UPDATE`, product, variant, location, status).Scan(&id)
	return id, err
}
