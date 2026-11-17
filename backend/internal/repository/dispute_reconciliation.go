package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	"time"
)

// No HTTP endpoint consumes this format. An authorized reviewer first records
// the retained source identifiers and accepted facts in an audited review row.
// A timestamp or a client assertion by itself is never historical proof.
type legacyDisputeReview struct {
	DisputeID       string    `json:"disputeId"`
	OrderID         string    `json:"orderId"`
	PriorStatus     string    `json:"priorStatus"`
	ConfirmedAt     time.Time `json:"confirmedAt"`
	WindowEndsAt    time.Time `json:"windowEndsAt"`
	HoldAt          time.Time `json:"holdAt"`
	SourceRecordIDs []string  `json:"sourceRecordIds"`
}

func recordDisputeReviewTx(ctx context.Context, tx *sql.Tx, id, order, actor, reason string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO dispute_order_transitions(id,dispute_id,order_id,old_status,new_status,actor_user_id,reason,result) SELECT $1,$2,id,status,status,$4,$5,'needs_review' FROM orders WHERE id=$3 ON CONFLICT(dispute_id,reason,result,new_status) DO NOTHING`, uuid.NewString(), id, order, actor, reason)
	return err
}

// ReconcileReviewedDispute requires an audit row accepted by root/domain review;
// callers supply only its retained ID and authenticated reviewer, never snapshots.
func (r *FinanceRepository) ReconcileReviewedDispute(ctx context.Context, id, reference, actor string) (domain.DisputeOutcome, error) {
	var out domain.DisputeOutcome
	if id == "" || reference == "" || actor == "" {
		return out, ErrDisputeInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var order string
	if err = tx.QueryRowContext(ctx, `SELECT order_id FROM disputes WHERE id=$1`, id).Scan(&order); err != nil {
		return out, err
	}
	if err = lockOrderFinanceTx(ctx, tx, order, true); err != nil {
		return out, err
	}
	var locked string
	if err = tx.QueryRowContext(ctx, `SELECT order_id FROM disputes WHERE id=$1 FOR UPDATE`, id).Scan(&locked); err != nil {
		return out, err
	}
	if locked != order {
		return out, ErrFinanceAccountChanged
	}
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT details FROM audit_logs WHERE id=$1 AND category='dispute' AND action='FIN005 reviewed legacy evidence' AND operator=$2`, reference, actor).Scan(&raw)
	var review legacyDisputeReview
	valid := err == nil && json.Unmarshal([]byte(raw), &review) == nil && review.DisputeID == id && review.OrderID == order && review.PriorStatus == "delivered_confirmed" && !review.ConfirmedAt.IsZero() && !review.WindowEndsAt.Before(review.ConfirmedAt) && !review.HoldAt.IsZero() && len(review.SourceRecordIDs) > 0
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if valid {
		for _, source := range review.SourceRecordIDs {
			var exists bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM audit_logs WHERE id=$1 AND id<>$2)`, source, reference).Scan(&exists); err != nil {
				return out, err
			}
			if !exists {
				valid = false
			}
		}
	}
	if !valid {
		if err = recordDisputeReviewTx(ctx, tx, id, order, actor, "review_provenance_missing"); err != nil {
			return out, err
		}
	} else {
		var sameEvidence, applied bool
		evidenceErr := tx.QueryRowContext(ctx, `SELECT e.evidence_kind='reviewed_legacy' AND e.review_reference=$3 AND e.actor_user_id=$4 AND e.prior_status=$5 AND e.prior_confirmed_at=$6 AND e.prior_window_ends_at=$7 AND e.hold_at=$8,
 o.status=e.prior_status AND o.confirmed_at IS NOT DISTINCT FROM e.prior_confirmed_at AND o.return_window_ends_at IS NOT DISTINCT FROM e.prior_window_ends_at AND o.disputed_at IS NOT DISTINCT FROM e.prior_disputed_at
 FROM dispute_order_evidence e JOIN orders o ON o.id=e.order_id WHERE e.dispute_id=$1 AND e.order_id=$2`, id, order, reference+":"+raw, actor, review.PriorStatus, review.ConfirmedAt, review.WindowEndsAt, review.HoldAt).Scan(&sameEvidence, &applied)
		if evidenceErr != nil && !errors.Is(evidenceErr, sql.ErrNoRows) {
			return out, evidenceErr
		}
		if evidenceErr == nil && !sameEvidence {
			if err = recordDisputeReviewTx(ctx, tx, id, order, actor, "review_current_state_conflict"); err != nil {
				return out, err
			}
		} else if sameEvidence && applied {
			// Exact immutable review identity and its actual applied physical result:
			// retry advances only the common guarded release, never creates quarantine.
			if _, err = releaseClosedReturnWindowTx(ctx, tx, order); err != nil {
				return out, err
			}
		} else {
			var matches bool
			if err = tx.QueryRowContext(ctx, `SELECT status='delivery_disputed' AND disputed_at=$2 AND NOT profit_released FROM orders WHERE id=$1`, order, review.HoldAt).Scan(&matches); err != nil {
				return out, err
			}
			if !matches {
				if err = recordDisputeReviewTx(ctx, tx, id, order, actor, "review_current_state_conflict"); err != nil {
					return out, err
				}
			} else {
				if _, err = tx.ExecContext(ctx, `INSERT INTO dispute_order_evidence(dispute_id,order_id,prior_status,prior_confirmed_at,prior_window_ends_at,prior_profit_released,prior_referral_released,hold_at,evidence_kind,actor_user_id,review_reference) VALUES($1,$2,'delivered_confirmed',$3,$4,FALSE,FALSE,$5,'reviewed_legacy',$6,$7) ON CONFLICT(dispute_id) DO NOTHING`, id, order, review.ConfirmedAt, review.WindowEndsAt, review.HoldAt, actor, reference+":"+raw); err != nil {
					return out, err
				}
				if _, err = reconcileDisputeEligibilityTx(ctx, tx, order); err != nil {
					return out, err
				}
				if _, err = releaseClosedReturnWindowTx(ctx, tx, order); err != nil {
					return out, err
				}
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return r.GetDisputeOutcome(ctx, id)
}
