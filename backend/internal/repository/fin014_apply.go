package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// FIN-014 repair-batch apply (W6, audit §5.2).
//
// Bounded (25 operations per run), resumable (the cursor is the last
// applied operation id, stored on the batch row), predicate-checked
// in-transaction (SELECT ... FOR UPDATE on the source rows, compare,
// abort that operation on mismatch), with effects through the existing
// domain writers where they exist. Three actions use documented guarded
// direct writes because no writer exists for them (payout reject,
// cancellation-obligation creation, parcel reversal mirroring
// postParcelCharge) — see the per-action notes; no posted row is ever
// UPDATEd or DELETEd. Settlement-item repairs are EXCLUDED from
// apply (report-only, audit §8 Q4) — they arrive as held operations
// and are never applied. Storage merged lots and unknown-PSP cases
// are NOT applicable either — they stay held.
//
// The batch must already carry its Q8 approval record (approved_by +
// approved_at, status approved/applying) before apply runs; the apply
// command never approves its own batch and refuses with a clear error
// when the owner has not named the approver. The cutover switch
// itself is NOT built here (FIN014CutoverGate, audit §8 Q3).

// FIN014ApplyMaxOps is the per-run bound (audit §5.2).
const FIN014ApplyMaxOps = 25

var fin014DisposableName = regexp.MustCompile(`^sellersho_test_[a-z0-9_]+$`)

// FIN014RequireDisposableDB refuses any database without the
// disposable guard marker (same rule as internal/testdb: the database
// name must be sellersho_test_* and the public.sellersho_test_guard
// row must name it). The apply command and the report CLI both call
// it before touching a database.
func FIN014RequireDisposableDB(ctx context.Context, db *sql.DB) error {
	var actual, marker string
	err := db.QueryRowContext(ctx, `SELECT current_database(), database_name FROM public.sellersho_test_guard WHERE singleton = TRUE`).Scan(&actual, &marker)
	if err != nil {
		return fmt.Errorf("fin014 refuses target: disposable guard marker missing (%v)", err)
	}
	if actual != marker || !fin014DisposableName.MatchString(actual) {
		return fmt.Errorf("fin014 refuses target: %q is not a marked disposable database", actual)
	}
	return nil
}

// FIN014ApplyResult is the outcome of one bounded apply run.
type FIN014ApplyResult struct {
	BatchID string
	Applied int
	Failed  int
	// FailedOps maps operation id -> the reason it aborted (predicate
	// mismatch or effect refusal). A failed operation is terminal for
	// its batch; it may be re-proposed with fresh predicates (W6
	// resumability) once the underlying data is fixed.
	FailedOps map[string]string
	// Done is true when no proposed operation remains in the batch
	// (the batch is then marked applied).
	Done bool
}

// FIN014ApplyBatch applies up to limit (max FIN014ApplyMaxOps)
// proposed operations of one approved batch, in id order, resuming
// after the batch's last-applied-operation cursor. Held operations
// are never applied. A predicate mismatch or an effect refusal aborts
// THAT operation (marked failed with the reason, in its own
// transaction) and the run continues; the batch stays consistent.
func FIN014ApplyBatch(ctx context.Context, db *sql.DB, batchID string, limit int) (*FIN014ApplyResult, error) {
	if err := FIN014RequireDisposableDB(ctx, db); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = FIN014ApplyMaxOps
	}
	if limit > FIN014ApplyMaxOps {
		return nil, fmt.Errorf("fin014 apply limit %d exceeds the per-run bound %d", limit, FIN014ApplyMaxOps)
	}
	var status, approvedBy string
	var approvedAt sql.NullTime
	var cursor sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT status, COALESCE(approved_by,''), approved_at, last_applied_operation_id FROM fin014_repair_batches WHERE id=$1`, batchID).Scan(&status, &approvedBy, &approvedAt, &cursor); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("fin014 batch %s not found", batchID)
		}
		return nil, err
	}
	// Q8: the apply command requires the approval record in the batch
	// row (approver + granularity to be named by the owner). If the
	// owner has not named the approver, the command refuses to apply.
	if status != "approved" && status != "applying" && status != "applied" {
		return nil, fmt.Errorf("fin014 batch %s has status %q: apply requires an approved batch (audit §8 Q8 — the owner has not named the approver / the batch is not approved)", batchID, status)
	}
	if approvedBy == "" || !approvedAt.Valid {
		return nil, fmt.Errorf("fin014 batch %s has no approval record (approved_by/approved_at): apply refused (audit §8 Q8 — the owner has not named the approver)", batchID)
	}
	// An already-applied batch is a no-op (the rerun proof's second
	// apply): it writes nothing and reports done.
	if status == "applied" {
		return &FIN014ApplyResult{BatchID: batchID, FailedOps: map[string]string{}, Done: true}, nil
	}
	if _, err := db.ExecContext(ctx, `UPDATE fin014_repair_batches SET status='applying' WHERE id=$1 AND status='approved'`, batchID); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, class, record_id, record_kind, store_id, order_id, effect, expected_before, explanation, cash_basis, confidence FROM fin014_repair_operations WHERE batch_id=$1 AND status='proposed' AND ($2::text IS NULL OR id > $2) ORDER BY id LIMIT $3`,
		batchID, nullableString(cursor), limit)
	if err != nil {
		return nil, err
	}
	type opRow struct {
		op   FIN014Operation
		rawE json.RawMessage
		rawP json.RawMessage
	}
	var ops []opRow
	for rows.Next() {
		var o opRow
		if err := rows.Scan(&o.op.ID, &o.op.Class, &o.op.RecordID, &o.op.RecordKind, &o.op.StoreID, &o.op.OrderID, &o.rawE, &o.rawP, &o.op.Explanation, &o.op.CashBasis, &o.op.Confidence); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(o.rawE, &o.op.Effect); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(o.rawP, &o.op.ExpectedBefore); err != nil {
			rows.Close()
			return nil, err
		}
		ops = append(ops, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := &FIN014ApplyResult{BatchID: batchID, FailedOps: map[string]string{}}
	for _, o := range ops {
		if err := fin014ApplyOne(ctx, db, batchID, o.op); err != nil {
			result.Failed++
			result.FailedOps[o.op.ID] = err.Error()
			continue
		}
		result.Applied++
	}
	var remaining, failedTotal int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM fin014_repair_operations WHERE batch_id=$1 AND status='proposed'), (SELECT COUNT(*) FROM fin014_repair_operations WHERE batch_id=$1 AND status='failed')`, batchID).Scan(&remaining, &failedTotal); err != nil {
		return nil, err
	}
	// The batch is applied only when nothing is left proposed AND
	// nothing failed: a failed operation keeps the batch resumable
	// (it may be re-proposed with fresh predicates once the
	// underlying data is fixed).
	if remaining == 0 && failedTotal == 0 {
		if _, err := db.ExecContext(ctx, `UPDATE fin014_repair_batches SET status='applied', applied_at=NOW() WHERE id=$1`, batchID); err != nil {
			return nil, err
		}
		result.Done = true
	}
	return result, nil
}

func nullableString(s sql.NullString) any {
	if s.Valid {
		return s.String
	}
	return nil
}

// fin014ApplyOne applies one operation in its own transaction:
// predicates first (SELECT ... FOR UPDATE), then the effect, then
// the operation + batch cursor updates, all atomic. A predicate
// mismatch or an effect refusal rolls the effect back and marks the
// operation failed with the reason.
func fin014ApplyOne(ctx context.Context, db *sql.DB, batchID string, op FIN014Operation) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fin014CheckPredicates(ctx, tx, op.ExpectedBefore); err != nil {
		if markErr := fin014MarkFailed(ctx, db, batchID, op.ID, "predicate mismatch: "+err.Error()); markErr != nil {
			return fmt.Errorf("predicate mismatch (%v); also failed to record it: %v", err, markErr)
		}
		return fmt.Errorf("predicate mismatch: %w", err)
	}
	if err := fin014RunEffect(ctx, tx, op); err != nil {
		if markErr := fin014MarkFailed(ctx, db, batchID, op.ID, "effect refused: "+err.Error()); markErr != nil {
			return fmt.Errorf("effect refused (%v); also failed to record it: %v", err, markErr)
		}
		return fmt.Errorf("effect refused: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fin014_repair_operations SET status='applied', applied_at=NOW() WHERE id=$1 AND batch_id=$2 AND status='proposed'`, op.ID, batchID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fin014_repair_batches SET last_applied_operation_id=$2 WHERE id=$1`, batchID, op.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func fin014MarkFailed(ctx context.Context, db *sql.DB, batchID, opID, reason string) error {
	_, err := db.ExecContext(ctx, `UPDATE fin014_repair_operations SET status='failed', hold_reason=$3 WHERE id=$1 AND batch_id=$2 AND status='proposed'`, opID, batchID, reason)
	return err
}

// fin014CheckPredicates evaluates expected-before predicates
// in-transaction, locking the source rows (SELECT ... FOR UPDATE).
func fin014CheckPredicates(ctx context.Context, tx *sql.Tx, preds []FIN014Predicate) error {
	for _, pred := range preds {
		if err := fin014CheckPredicate(ctx, tx, pred); err != nil {
			return err
		}
	}
	return nil
}

func fin014CheckPredicate(ctx context.Context, tx *sql.Tx, pred FIN014Predicate) error {
	cols, ok := fin014PredicateTables[pred.Table]
	if !ok {
		return fmt.Errorf("predicate table %q is not allowlisted", pred.Table)
	}
	switch pred.Kind {
	case "equals":
		if !cols[pred.Column] {
			return fmt.Errorf("predicate column %q is not allowlisted for %s", pred.Column, pred.Table)
		}
		var current string
		q := fmt.Sprintf(`SELECT %s::text FROM %s WHERE id=$1 FOR UPDATE`, pred.Column, pred.Table)
		if err := tx.QueryRowContext(ctx, q, pred.ID).Scan(&current); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("%s/%s: row missing (expected %s=%v)", pred.Table, pred.ID, pred.Column, pred.Expected)
			}
			return err
		}
		if current != fin014ExpectedText(pred.Expected) {
			return fmt.Errorf("%s/%s: %s=%s, want %s", pred.Table, pred.ID, pred.Column, current, fin014ExpectedText(pred.Expected))
		}
		return nil
	case "absent":
		if !cols[pred.Column] {
			return fmt.Errorf("predicate column %q is not allowlisted for %s", pred.Column, pred.Table)
		}
		var where strings.Builder
		where.WriteString(fmt.Sprintf(`%s=$1`, pred.Column))
		args := []any{pred.Value}
		i := 2
		for col, val := range pred.Match {
			if !cols[col] {
				return fmt.Errorf("predicate match column %q is not allowlisted for %s", col, pred.Table)
			}
			fmt.Fprintf(&where, ` AND %s=$%d`, col, i)
			args = append(args, val)
			i++
		}
		var exists bool
		q := fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE %s FOR UPDATE)`, pred.Table, where.String())
		if err := tx.QueryRowContext(ctx, q, args...).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("%s: a row matching %s=%v already exists", pred.Table, pred.Column, pred.Value)
		}
		return nil
	case "latest_event_status":
		if pred.Table != "order_refund_obligations" {
			return fmt.Errorf("latest_event_status needs order_refund_obligations, got %s", pred.Table)
		}
		var current string
		err := tx.QueryRowContext(ctx, `SELECT status FROM order_refund_events WHERE obligation_id=$1 ORDER BY event_no DESC LIMIT 1 FOR UPDATE`, pred.ID).Scan(&current)
		if err == sql.ErrNoRows {
			if err := tx.QueryRowContext(ctx, `SELECT status FROM order_refund_obligations WHERE id=$1 FOR UPDATE`, pred.ID).Scan(&current); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if current != fin014ExpectedText(pred.Expected) {
			return fmt.Errorf("obligation %s: latest event status=%s, want %s", pred.ID, current, fin014ExpectedText(pred.Expected))
		}
		return nil
	default:
		return fmt.Errorf("unknown predicate kind %q", pred.Kind)
	}
}

func fin014ExpectedText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}

// fin014RunEffect runs one operation's effect inside the caller's
// transaction, through the existing domain writers (or, where no
// writer exists for the correction, a guarded direct write that the
// code documents — see the per-action notes).
func fin014RunEffect(ctx context.Context, tx *sql.Tx, op FIN014Operation) error {
	effect := op.Effect
	action, _ := effect["action"].(string)
	amount, _ := fin014AsInt64(effect["amount"])
	switch action {
	case "reject_payout_request":
		// No money moves: a pending/approved request is refused.
		// The lifecycle transition (rejected + processed_at) is the
		// payout request's own state machine, not a writer call.
		id, _ := effect["payout_request_id"].(string)
		res, err := tx.ExecContext(ctx, `UPDATE payout_requests SET status='rejected', rejection_reason='FIN-014 repair: payout exceeds signed availability', processed_at=NOW() WHERE id=$1 AND status IN ('pending','approved')`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("payout request %s is no longer pending/approved", id)
		}
		return nil
	case "create_cancellation_obligation":
		// Guard-validated insert (same shape as the W2 fixture):
		// the guard trigger overwrites status (pending if
		// gateway-verified, else unknown) and source_amount_toman.
		// The obligation moves no cash; the money moves only at a
		// succeeded refund event (provider attempt + gateway
		// receipt required by the guard).
		orderID, _ := effect["order_id"].(string)
		paymentID, _ := effect["source_payment_id"].(string)
		oblID := "fin014-obl-" + uuid.NewString()
		_, err := tx.ExecContext(ctx, `INSERT INTO order_refund_obligations(id, order_id, source_payment_id, customer_id, amount_toman, currency, purpose, business_id, destination, policy_version, status, state_reason, actor_user_id) VALUES($1,$2,$3,NULL,$4,'TOMAN','cancellation',$2,'original_payment','original_payment_v1','unknown','fin014 repair: cancellation obligation created from dry-run proposal','fin014-apply')`,
			oblID, orderID, paymentID, amount)
		return err
	case "post_parcel_reversal":
		// No parcel-correction writer exists for pre-receipt
		// parcels (CorrectReturnParcelCharge needs
		// parcel_charge_receipts). The repair posts the compensating
		// events directly: a ledger credit for the recorded debit
		// plus a reversal event referencing it. The credit is
		// bounded by the recorded debit amount.
		parcelID, _ := effect["parcel_id"].(string)
		eventID, _ := effect["event_id"].(string)
		var requestID, supplierID, paymentMethod string
		if err := tx.QueryRowContext(ctx, `SELECT return_request_id, supplier_id, payment_method FROM supplier_return_financial_events WHERE id=$1 FOR UPDATE`, eventID).Scan(&requestID, &supplierID, &paymentMethod); err != nil {
			return err
		}
		revEventID := "rev-" + uuid.NewString()
		ledgerID := "led-" + uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries(id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute, created_at) VALUES($1, NOW(), $2, 'supplier', 'credit', $3, $4, 'fin014-apply', NULL, NOW())`,
			ledgerID, supplierID, amount, "FIN-014 parcel reversal "+revEventID+" for "+eventID+" (duplicate/missing/shared charge)"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO supplier_return_financial_events(id, return_request_id, parcel_id, supplier_id, operation, amount, payment_method, ledger_entry_id, idempotency_key, reason, actor_user_id, charge_component) VALUES($1,$2,$3,$4,'reversal',$5,$6,$7,$8,$9,'fin014-apply','adjustment')`,
			revEventID, requestID, parcelID, supplierID, amount, paymentMethod, ledgerID, "fin014-rev-"+uuid.NewString(), "FIN-014 repair: reversal of "+eventID)
		return err
	case "release_dispute_profit":
		// The dispute was resolved release_to_seller: the delivery
		// stands. Record that determination (status flip, FIN-005
		// writer flag), reconcile eligibility, then run the staged
		// release through its own Tx function. The delivery
		// confirmation is the ORIGINAL delivery — the goods reached
		// the customer before the dispute was opened (disputed_at),
		// not the repair date: stamping NOW() would put the
		// confirmation after the (already closed) return window and
		// the release path would honestly refuse. If the release
		// refuses (window open / not eligible) the operation fails
		// and the defect stays reported.
		orderID, _ := effect["order_id"].(string)
		if err := markDisputeWriterTx(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE orders SET status='delivered_confirmed', confirmed_at=COALESCE(confirmed_at, disputed_at, NOW()) WHERE id=$1 AND status='delivery_disputed'`, orderID); err != nil {
			return err
		}
		if _, err := reconcileDisputeEligibilityTx(ctx, tx, orderID); err != nil {
			return err
		}
		released, err := releaseClosedReturnWindowTx(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if !released {
			return fmt.Errorf("release path refused for order %s (dispute not eligible or return window still open)", orderID)
		}
		return nil
	case "correct_coupon_used_count":
		// FIN-007 writer flag, then set used_count to the occupying
		// count the proposal computed. The deferred consistency
		// trigger validates the count at commit.
		couponID, _ := effect["coupon_id"].(string)
		target, _ := fin014AsInt64(effect["used_count"])
		if _, err := tx.ExecContext(ctx, `SELECT set_config('sellersho.fin007_writer','v1',true)`); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE seller_discount_codes SET used_count=$2 WHERE id=$1`, couponID, target)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("coupon %s not found", couponID)
		}
		return nil
	case "record_seller_profit_reversal":
		// The domain writer: compensating seller-profit reversal +
		// ledger debit, idempotent on spr:<source>:<sourceID>.
		// Legitimate only for the succeeded dispute/admin refund
		// the predicate checked.
		oblID, _ := effect["obligation_id"].(string)
		return recordSellerProfitReversalForOrderRefundTx(ctx, tx, oblID, "fin014-apply")
	default:
		return fmt.Errorf("unknown effect action %q", action)
	}
}
