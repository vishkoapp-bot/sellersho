package repository

import (
	"context"
	"database/sql"
	"time"
)

// PurchaseSellerService serializes wallet debits with payouts on the store row.
// The server chooses the price and entitlement; client prices are never accepted.
// SMS credit packs (sms_500/sms_2000) are NOT sold: there is no sending path
// yet, so the purchase is rejected before any wallet debit, purchase-history
// write, or smsCredits grant. The smsCredits column itself is untouched.
type SellerServiceAcknowledgment struct {
	ID            string    `json:"id"`
	ActivePlanID  string    `json:"activePlanId"`
	PlanExpiresAt time.Time `json:"planExpiresAt"`
	Amount        int64     `json:"amount"`
	DurationDays  int       `json:"durationDays"`
}

func (r *FinanceRepository) PurchaseSellerService(ctx context.Context, ownerID, storeID, service string, keys ...string) error {
	_, err := r.PurchaseSellerServiceAcknowledgment(ctx, ownerID, storeID, service, keys...)
	return err
}
func (r *FinanceRepository) PurchaseSellerServiceAcknowledgment(ctx context.Context, ownerID, storeID, service string, clientKeys ...string) (*SellerServiceAcknowledgment, error) {
	command, err := newFinanceCommand(FinancialAccount{Type: "seller", ID: storeID}, "purchase-service-v1", clientKeys, []interface{}{ownerID, service})
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var accountLocks TransactionLockSet
	if err := LockAccountsTx(ctx, tx, &accountLocks, FinancialAccount{Type: "seller", ID: storeID}); err != nil {
		return nil, err
	}
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM stores WHERE id=$1 AND owner_user_id=$2 AND owner_type='dropshipper'`, storeID, ownerID).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrStoreForbidden
		}
		return nil, err
	}
	var acknowledgment SellerServiceAcknowledgment
	if replay, err := command.replayTx(ctx, tx, &acknowledgment); err != nil {
		return nil, err
	} else if replay {
		if acknowledgment.ID != storeID || acknowledgment.Amount <= 0 || acknowledgment.DurationDays <= 0 || acknowledgment.PlanExpiresAt.IsZero() {
			return nil, ErrFinancePrincipalUnresolved
		}
		return &acknowledgment, nil
	}
	if service == "sms_500" || service == "sms_2000" {
		return nil, ErrSMSServiceUnavailable
	}
	prices := map[string]int64{"standard": 890000, "pro": 1490000, "vip": 3900000}
	days := map[string]int{"standard": 90, "pro": 180, "vip": 365}
	price, ok := prices[service]
	if !ok {
		return nil, ErrUnknownSellerService
	}
	available, balanceErr := BalanceTx(ctx, tx, FinancialAccount{Type: "seller", ID: storeID})
	if err := balanceErr; err != nil {
		return nil, err
	}
	if available < price {
		return nil, ErrStoreInsufficientBalance
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO seller_service_purchases(store_id,service_type,amount) VALUES($1,$2,$3)`, storeID, service, price); err != nil {
		return nil, err
	}
	if duration, ok := days[service]; ok {
		if _, err := tx.ExecContext(ctx, `UPDATE stores SET active_plan_id=$2,plan_expires_at=GREATEST(COALESCE(plan_expires_at,NOW()),NOW()) + make_interval(days=>$3),updated_at=NOW() WHERE id=$1`, storeID, service, duration); err != nil {
			return nil, err
		}
	} else {
		// Unreachable today: every priced service has a plan duration,
		// and SMS packs are rejected above. Defensive: never grant an
		// entitlement (e.g. smsCredits) for an unpriced service.
		return nil, ErrUnknownSellerService
	}
	acknowledgment.ID = storeID
	acknowledgment.ActivePlanID = service
	acknowledgment.Amount = price
	acknowledgment.DurationDays = days[service]
	if err := tx.QueryRowContext(ctx, `SELECT plan_expires_at FROM stores WHERE id=$1`, storeID).Scan(&acknowledgment.PlanExpiresAt); err != nil {
		return nil, err
	}
	if err := command.recordTx(ctx, tx, acknowledgment); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &acknowledgment, nil
}
