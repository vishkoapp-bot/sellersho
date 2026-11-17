package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	"strings"
)

var ErrDisputeConflict = errors.New("conflicting dispute verdict")
var ErrDisputeInput = errors.New("invalid dispute verdict")

type DisputeResolutionInput struct {
	Action, ResponsibleParty, ActorUserID string
	ExpectedClaimAmount                   *int64
}

func markDisputeWriterTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT set_config('sellersho.fin005_writer','v1',true)`)
	return err
}
func normalizeDisputeVerdict(action, party string) (string, error) {
	switch action {
	case "release_to_seller", "refund_supplier", "refund_seller", "refund_carrier", "refund_warehouse":
	default:
		return "", ErrDisputeInput
	}
	expected := strings.TrimPrefix(action, "refund_")
	if action == "release_to_seller" {
		expected = "seller"
	}
	if strings.TrimSpace(party) == "" {
		return expected, nil
	}
	normalized, ok := disputePartyLedgerType(party, "")
	if !ok || normalized != expected {
		return "", ErrDisputeInput
	}
	return normalized, nil
}
func (r *FinanceRepository) ResolveDisputeWithOutcome(ctx context.Context, id string, in DisputeResolutionInput) (domain.DisputeOutcome, error) {
	var out domain.DisputeOutcome
	party, err := normalizeDisputeVerdict(in.Action, in.ResponsibleParty)
	if err != nil || strings.TrimSpace(in.ActorUserID) == "" {
		return out, ErrDisputeInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var orderID string
	if err = tx.QueryRowContext(ctx, `SELECT order_id FROM disputes WHERE id=$1`, id).Scan(&orderID); err != nil {
		return out, err
	}
	if err = lockOrderFinanceTx(ctx, tx, orderID, true); err != nil {
		return out, err
	}
	if err = markDisputeWriterTx(ctx, tx); err != nil {
		return out, err
	}
	var actualOrder, status string
	var amount int64
	if err = tx.QueryRowContext(ctx, `SELECT order_id,status,claim_amount FROM disputes WHERE id=$1 FOR UPDATE`, id).Scan(&actualOrder, &status, &amount); err != nil {
		return out, err
	}
	if actualOrder != orderID {
		return out, ErrFinanceAccountChanged
	}
	if amount <= 0 || validateSignedFinanceMoney(amount) != nil {
		return out, ErrDisputeInput
	}
	if in.ExpectedClaimAmount != nil && (*in.ExpectedClaimAmount <= 0 || *in.ExpectedClaimAmount != amount) {
		return out, ErrDisputeInput
	}
	var oldAction, oldParty string
	err = tx.QueryRowContext(ctx, `SELECT action,responsible_party FROM dispute_financial_decisions WHERE dispute_id=$1`, id).Scan(&oldAction, &oldParty)
	releasedNow := false
	replay := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if replay {
		if oldAction != in.Action || oldParty != party {
			return out, ErrDisputeConflict
		}
	} else if status != "pending" {
		// Historical projections cannot authorize another financial operation.
		var historicalAction, historicalParty string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(resolution_action,''),COALESCE(responsible_party,'') FROM disputes WHERE id=$1`, id).Scan(&historicalAction, &historicalParty); err != nil {
			return out, err
		}
		historicalNormalized, normalizeErr := normalizeDisputeVerdict(historicalAction, historicalParty)
		if historicalAction != in.Action || normalizeErr != nil || historicalNormalized != party {
			return out, ErrDisputeConflict
		}
		tx.Rollback()
		return r.GetDisputeOutcome(ctx, id)
	} else {
		if in.Action != "release_to_seller" {
			if _, err = tx.ExecContext(ctx, `UPDATE disputes SET resolution_action=$2,responsible_party=$3 WHERE id=$1`, id, in.Action, party); err != nil {
				return out, err
			}
			if _, err = reserveOrderRefundTx(ctx, tx, orderID, "dispute", id, "original_payment", in.ActorUserID, amount, ""); err != nil {
				return out, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) VALUES($1,NOW(),$7,$2,'debit',$3,$4,$5,$6)`, "ADJ-"+uuid.NewString(), party, amount, fmt.Sprintf("صدور حکم داوری پرونده %s به مسئولیت %s", id, party), in.ActorUserID, id, disputeAdjustmentTarget(in.ResponsibleParty, party)); err != nil {
				return out, err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO dispute_financial_decisions(dispute_id,order_id,action,responsible_party,claim_amount,actor_user_id,evidence_reference) VALUES($1,$2,$3,$4,$5,$6,(SELECT dispute_id FROM dispute_order_evidence WHERE dispute_id=$1))`, id, orderID, in.Action, party, amount, in.ActorUserID); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE disputes SET status='resolved',resolution_action=$2,responsible_party=$3,resolution_date=(SELECT decided_at FROM dispute_financial_decisions WHERE dispute_id=$1) WHERE id=$1`, id, in.Action, party); err != nil {
			return out, err
		}
		if _, err = reconcileDisputeEligibilityTx(ctx, tx, orderID); err != nil {
			return out, err
		}
		if releasedNow, err = releaseClosedReturnWindowTx(ctx, tx, orderID); err != nil {
			return out, err
		}
		if err = recordDisputeEligibilityTx(ctx, tx, id, orderID, in.ActorUserID); err != nil {
			return out, err
		}
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	if releasedNow {
		var profit int64
		if e := r.db.QueryRowContext(ctx, `SELECT dropshipper_profit FROM orders WHERE id=$1`, orderID).Scan(&profit); e == nil {
			notifyProfitReleased(ctx, r.db, orderID, profit)
		}
	}
	if !replay {
		notifyDisputeResolved(ctx, r.db, id, orderID, in.Action, party, amount)
	}
	out, err = r.GetDisputeOutcome(ctx, id)
	out.Replay = replay
	return out, err
}
func (r *FinanceRepository) GetDisputeOutcome(ctx context.Context, id string) (domain.DisputeOutcome, error) {
	out := domain.DisputeOutcome{DisputeID: id, RefundScope: "order", HoldReasons: []string{}}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	var released bool
	err = tx.QueryRowContext(ctx, `SELECT d.order_id,COALESCE(f.action,d.resolution_action,''),COALESCE(f.responsible_party,d.responsible_party,''),d.claim_amount,COALESCE(f.decided_at,CASE WHEN EXISTS(SELECT 1 FROM dispute_order_evidence e WHERE e.dispute_id=d.id AND e.evidence_kind='reviewed_legacy') THEN d.resolution_date END),COALESCE(f.actor_user_id,''),o.status,o.profit_released FROM disputes d JOIN orders o ON o.id=d.order_id LEFT JOIN dispute_financial_decisions f ON f.dispute_id=d.id WHERE d.id=$1`, id).Scan(&out.OrderID, &out.Action, &out.ResponsibleParty, &out.ClaimAmount, &out.DecisionTime, &out.ActorUserID, &out.OrderStatus, &released)
	if err != nil {
		return out, err
	}
	orders := []domain.Order{{ID: out.OrderID}}
	if err = attachRefundSummariesFrom(ctx, tx, orders); err != nil {
		return out, err
	}
	if orders[0].Refund == nil {
		return out, errors.New("missing order refund projection")
	}
	out.Refund = *orders[0].Refund
	out.SellerReleaseState, out.HoldReasons, err = disputeReleaseStateTx(ctx, tx, out.OrderID)
	if out.DecisionTime == nil && out.Action != "" {
		out.SellerReleaseState = "needs_review"
		out.HoldReasons = append(out.HoldReasons, "legacy_decision")
	}
	return out, err
}

func disputeAdjustmentTarget(supplied, normalized string) string {
	if value := strings.TrimSpace(supplied); value != "" {
		return value
	}
	return normalized
}
