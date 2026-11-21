package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drup/platform/internal/pricing"
	"github.com/google/uuid"
)

// FIN-014 repair batches (W5/W6, audit §5).
//
// A proposal is built from a dry-run report (see FIN014ProposeBatch
// in fin014_report.go): every certain defect gets exactly one
// operation, every ambiguous/unresolved case gets a held operation (or
// none) with a reason. Operation IDs are content-derived
// (FIN014OperationID: class + immutable record IDs + proposed effect),
// so re-proposing the same report regenerates the same IDs and the
// store rejects the duplicate instead of duplicating the repair.
// Random UUIDs are used only for batch/run IDs, never for operations.

// FIN014CutoverGate is the named gate for the cutover switch (audit
// §8 Q3, unanswered): there is deliberately NO writer switch in this
// round. The report/proposal/apply tooling is built with the switch
// point marked here (config flag + writer fence following the
// FIN-005/FIN-007 set_config pattern, to be wired when Q3 lands).
// What is gated: switching the live writers to the repaired
// (cutover) shapes — i.e. which writers move to the new behaviour and
// when, relative to FIN-010. Until Q3 lands, the apply command only
// posts compensating events through the existing domain writers on a
// disposable database; nothing switches in production.
const FIN014CutoverGate = "fin014-cutover"

// FIN014Predicate is one expected-before check, evaluated by the
// apply command in-transaction (SELECT ... FOR UPDATE) before the
// operation's effect runs. Kinds:
//   - "equals": the row (table, id) must have column == expected.
//   - "absent": no row in table may match column == value (plus the
//     extra equality filters in match, e.g. purpose=cancellation).
//   - "latest_event_status": the latest order_refund_events row for
//     the obligation (table, id) must have status == expected.
//
// Only the tables/columns in fin014PredicateTables may appear; the
// apply evaluator rejects anything else instead of guessing.
type FIN014Predicate struct {
	Kind     string            `json:"kind"`
	Table    string            `json:"table"`
	ID       string            `json:"id,omitempty"`
	Column   string            `json:"column,omitempty"`
	Expected any               `json:"expected,omitempty"`
	Value    any               `json:"value,omitempty"`
	Match    map[string]string `json:"match,omitempty"`
}

// FIN014Operation is one proposed repair (or held case) for one
// report row. Effect is the proposed action as structured JSON, e.g.
// {"action":"reject_payout_request","payout_request_id":"..."}.
// CashBasis gates the operation: no cash-moving operation may be
// proposed for anything but verified_gateway (audit §5.1); class-8
// lookups and every held case carry action "none".
type FIN014Operation struct {
	ID             string            `json:"id"`
	Class          string            `json:"class"`
	RecordID       string            `json:"recordId"`
	RecordKind     string            `json:"recordKind"`
	StoreID        string            `json:"storeId"`
	OrderID        string            `json:"orderId"`
	Effect         map[string]any    `json:"effect"`
	ExpectedBefore []FIN014Predicate `json:"expectedBefore"`
	Explanation    string            `json:"explanation"`
	CashBasis      string            `json:"cashBasis"`
	Confidence     string            `json:"confidence"`
	Status         string            `json:"status"`
	HoldReason     string            `json:"holdReason,omitempty"`
}

// FIN014Proposal is one batch proposed from one report.
type FIN014Proposal struct {
	BatchID     string            `json:"batchId"`
	ReportRunID string            `json:"reportRunId"`
	Operations  []FIN014Operation `json:"operations"`
}

// fin014PredicateTables is the allowlist the apply evaluator trusts:
// table -> the columns a predicate may read. Anything else is
// rejected, never guessed.
var fin014PredicateTables = map[string]map[string]bool{
	"payout_requests":                  {"status": true, "amount": true},
	"monthly_settlement_items":         {"net_amount": true, "status": true},
	"orders":                           {"status": true, "payment_status": true, "profit_released": true, "payable_amount": true},
	"order_refund_obligations":         {"status": true, "order_id": true, "purpose": true},
	"supplier_return_parcels":          {"id": true},
	"supplier_return_financial_events": {"operation": true, "amount": true, "ledger_entry_id": true},
	"disputes":                         {"status": true, "resolution_action": true},
	"seller_discount_codes":            {"used_count": true, "max_uses": true},
	"seller_profit_reversals":          {"order_id": true},
}

// fin014KnownActions is the allowlist of effect actions the builder
// may emit. Anything else is rejected, never guessed.
var fin014KnownActions = map[string]bool{
	"reject_payout_request":          true,
	"create_cancellation_obligation": true,
	"post_parcel_reversal":           true,
	"release_dispute_profit":         true,
	"correct_coupon_used_count":      true,
	"record_seller_profit_reversal":  true,
	"none":                           true,
}

// fin014PayoutActions is the set of effect actions that pay money out
// based on a funding claim. They may only be proposed for
// verified_gateway rows. It is currently EMPTY — and that is the
// point: none of the actions above pays claimed funding out.
//   - create_cancellation_obligation moves no cash at insert: its
//     guard parks an unverified source as 'unknown', and the money
//     only moves at a succeeded refund event, which the guard
//     requires a retained provider attempt + gateway receipt for.
//   - post_parcel_reversal credits back exactly a recorded debit
//     (bounded by the recorded charge event); it pays out no new
//     money and claims no funding.
//   - record_seller_profit_reversal claws back recorded released
//     profit (bounded by the recorded release); same.
//   - reject_payout_request / release_dispute_profit /
//     correct_coupon_used_count move no cash to a party.
//
// Adding a future action that pays out (a refund, a payout) MUST add
// it here first; the gate below then enforces verified_gateway.
var fin014PayoutActions = map[string]bool{}

// ValidateFIN014Proposal checks a proposal before it is stored:
// FIN-012 bounds on every amount, predicate shape against the
// allowlist, cash-basis gating, and one operation per certain defect
// (held operations must carry a reason).
func ValidateFIN014Proposal(p *FIN014Proposal) error {
	if p == nil {
		return fmt.Errorf("fin014 proposal is nil")
	}
	if p.BatchID == "" || p.ReportRunID == "" {
		return fmt.Errorf("fin014 proposal needs a batch id and a report run id")
	}
	seen := map[string]bool{}
	for i, op := range p.Operations {
		if op.ID == "" {
			return fmt.Errorf("fin014 operation %d: missing id", i)
		}
		if seen[op.ID] {
			return fmt.Errorf("fin014 duplicate operation id %s (re-proposal must collide, not duplicate)", op.ID)
		}
		seen[op.ID] = true
		if op.Class == "" || op.RecordID == "" {
			return fmt.Errorf("fin014 operation %s: missing class or record id", op.ID)
		}
		if op.Status != "proposed" && op.Status != "held" {
			return fmt.Errorf("fin014 operation %s: status %q, want proposed or held", op.ID, op.Status)
		}
		if op.Status == "held" && strings.TrimSpace(op.HoldReason) == "" {
			return fmt.Errorf("fin014 operation %s: held without a reason", op.ID)
		}
		if op.Confidence != FIN014ConfidenceCertain && op.Confidence != FIN014ConfidenceAmbiguous && op.Confidence != FIN014ConfidenceUnresolved {
			return fmt.Errorf("fin014 operation %s: unknown confidence %q", op.ID, op.Confidence)
		}
		if op.CashBasis != FIN014CashVerifiedGateway && op.CashBasis != FIN014CashSimulated && op.CashBasis != FIN014CashUnverified && op.CashBasis != FIN014CashUnknown {
			return fmt.Errorf("fin014 operation %s: unknown cash basis %q", op.ID, op.CashBasis)
		}
		action, _ := op.Effect["action"].(string)
		if action == "" {
			return fmt.Errorf("fin014 operation %s: effect has no action", op.ID)
		}
		if !fin014KnownActions[action] {
			return fmt.Errorf("fin014 operation %s: unknown action %q", op.ID, action)
		}
		if op.Status == "proposed" && action == "none" {
			return fmt.Errorf("fin014 operation %s: proposed with action none (use held + reason)", op.ID)
		}
		if fin014PayoutActions[action] && op.CashBasis != FIN014CashVerifiedGateway {
			return fmt.Errorf("fin014 operation %s: payout action %q proposed for cash basis %q (only verified_gateway may move cash)", op.ID, action, op.CashBasis)
		}
		if err := fin014ValidateAmounts(op.Effect); err != nil {
			return fmt.Errorf("fin014 operation %s: %w", op.ID, err)
		}
		for j, pred := range op.ExpectedBefore {
			if err := fin014ValidatePredicate(pred); err != nil {
				return fmt.Errorf("fin014 operation %s predicate %d: %w", op.ID, j, err)
			}
		}
	}
	return nil
}

// fin014ValidateAmounts enforces the FIN-012 bound (amounts in Toman,
// integers in [0, MaxTransportMoney]) on every amount in the effect.
func fin014ValidateAmounts(effect map[string]any) error {
	for key, v := range effect {
		if !strings.Contains(key, "amount") && key != "used_count" && key != "max_uses" {
			continue
		}
		n, ok := fin014AsInt64(v)
		if !ok {
			return fmt.Errorf("effect %q is not an integer amount", key)
		}
		if n < 0 || n > pricing.MaxTransportMoney {
			return fmt.Errorf("effect %q=%d violates the FIN-012 bound [0, %d]", key, n, pricing.MaxTransportMoney)
		}
	}
	return nil
}

func fin014AsInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n == float64(int64(n)) {
			return int64(n), true
		}
		return 0, false
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func fin014ValidatePredicate(pred FIN014Predicate) error {
	if pred.Kind != "equals" && pred.Kind != "absent" && pred.Kind != "latest_event_status" {
		return fmt.Errorf("unknown predicate kind %q", pred.Kind)
	}
	cols, ok := fin014PredicateTables[pred.Table]
	if !ok {
		return fmt.Errorf("predicate table %q is not allowlisted", pred.Table)
	}
	if pred.Kind == "latest_event_status" {
		if pred.Table != "order_refund_obligations" || pred.ID == "" || pred.Expected == nil {
			return fmt.Errorf("latest_event_status predicate needs an order_refund_obligations id and an expected status")
		}
		return nil
	}
	if pred.Kind == "equals" {
		if pred.ID == "" || pred.Column == "" {
			return fmt.Errorf("equals predicate needs id and column")
		}
		if !cols[pred.Column] {
			return fmt.Errorf("predicate column %q is not allowlisted for %s", pred.Column, pred.Table)
		}
		if pred.Expected == nil {
			return fmt.Errorf("equals predicate needs an expected value")
		}
		if n, ok := fin014AsInt64(pred.Expected); ok && (n < 0 || n > pricing.MaxTransportMoney) {
			return fmt.Errorf("predicate expected value violates the FIN-012 bound")
		}
		return nil
	}
	if pred.Column == "" || pred.Value == nil {
		return fmt.Errorf("absent predicate needs column and value")
	}
	if !cols[pred.Column] {
		return fmt.Errorf("predicate column %q is not allowlisted for %s", pred.Column, pred.Table)
	}
	for col := range pred.Match {
		if !cols[col] {
			return fmt.Errorf("predicate match column %q is not allowlisted for %s", col, pred.Table)
		}
	}
	return nil
}

// StoreFIN014Proposal validates and persists a proposal: the report
// run row, the batch row (status proposed), and one operation row per
// operation, in a single transaction. A duplicate operation ID — in
// this batch or any earlier one — rejects the whole store (the
// re-proposal must collide, not duplicate). The batch is stored in
// proposed status; nothing is applied here.
func StoreFIN014Proposal(ctx context.Context, db *sql.DB, report *FIN014Report, proposal *FIN014Proposal, oracleChecks map[string]int64) error {
	if err := ValidateFIN014Proposal(proposal); err != nil {
		return err
	}
	if report == nil {
		return fmt.Errorf("fin014 proposal needs its report")
	}
	reportJSON, err := FIN014ReportJSON(report)
	if err != nil {
		return err
	}
	oracleJSON, err := json.Marshal(oracleChecks)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO fin014_report_runs(id, prefix, cycle_period, schema_version, total_rows, certain_count, ambiguous_count, unresolved_count, oracle_checks, fin011_pinned, report_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		proposal.ReportRunID, "", "", report.SchemaVersion, report.TotalRows, report.CertainCount, report.AmbiguousCount, report.UnresolvedCount, oracleJSON, report.FIN011Pinned, reportJSON); err != nil {
		return fmt.Errorf("fin014 store report run: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO fin014_repair_batches(id, report_run_id, status, created_by) VALUES($1,$2,'proposed','fin014-proposal')`,
		proposal.BatchID, proposal.ReportRunID); err != nil {
		return fmt.Errorf("fin014 store batch: %w", err)
	}
	for _, op := range proposal.Operations {
		effectJSON, err := json.Marshal(op.Effect)
		if err != nil {
			return err
		}
		predJSON, err := json.Marshal(op.ExpectedBefore)
		if err != nil {
			return err
		}
		var existingStatus string
		err = tx.QueryRowContext(ctx, `SELECT status FROM fin014_repair_operations WHERE id=$1`, op.ID).Scan(&existingStatus)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			// A failed operation may be re-proposed with fresh
			// predicates (W6 resumability): the failed row is
			// refreshed into the new batch as proposed. Any other
			// existing status rejects the store — a re-proposal
			// must collide, not duplicate.
			if existingStatus != "failed" {
				return fmt.Errorf("fin014 duplicate operation id %s (already stored with status %s — re-proposal must collide, not duplicate)", op.ID, existingStatus)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE fin014_repair_operations SET batch_id=$2, expected_before=$3, status='proposed', hold_reason=NULL, applied_at=NULL WHERE id=$1 AND status='failed'`, op.ID, proposal.BatchID, predJSON); err != nil {
				return fmt.Errorf("fin014 re-propose failed operation %s: %w", op.ID, err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO fin014_repair_operations(id, batch_id, class, record_id, record_kind, store_id, order_id, effect, expected_before, explanation, cash_basis, confidence, status, hold_reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			op.ID, proposal.BatchID, op.Class, op.RecordID, op.RecordKind, op.StoreID, op.OrderID, effectJSON, predJSON, op.Explanation, op.CashBasis, op.Confidence, op.Status, op.HoldReason); err != nil {
			return fmt.Errorf("fin014 store operation %s: %w", op.ID, err)
		}
	}
	return tx.Commit()
}

// FIN014NewBatchID mints a random batch/run ID. Random UUIDs are used
// only for batch/run IDs, never for operation IDs.
func FIN014NewBatchID() string { return "fin014-batch-" + uuid.NewString() }
func FIN014NewRunID() string   { return "fin014-run-" + uuid.NewString() }
