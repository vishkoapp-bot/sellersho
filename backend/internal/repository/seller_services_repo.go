package repository

import (
	"context"
	"database/sql"
)

const sellerAvailableSQL = `SELECT GREATEST(0,
 COALESCE((SELECT SUM(dropshipper_profit) FROM orders WHERE store_id=$1 AND profit_released=TRUE AND status IN ('delivered','delivered_confirmed')),0)
 + COALESCE((SELECT SUM(amount) FROM referral_bonuses WHERE referrer_store_id=$1 AND status='released'),0)
 + COALESCE((SELECT SUM(referral_commission) FROM orders WHERE referrer_seller_id=$1 AND referral_commission_released=TRUE AND status<>'cancelled'),0)
 - COALESCE((SELECT SUM(amount) FROM payout_requests WHERE store_id=$1 AND status IN ('pending','approved','paid','processing','completed','transfer_confirmed')),0)
 - COALESCE((SELECT SUM(amount) FROM seller_service_purchases WHERE store_id=$1),0)
 - CASE WHEN to_regclass('public.seller_early_withdrawals') IS NOT NULL THEN COALESCE((SELECT SUM(fee_amount) FROM seller_early_withdrawals WHERE store_id=$1 AND status='approved'),0) ELSE 0 END
 - CASE WHEN to_regclass('public.seller_early_withdrawals') IS NOT NULL THEN COALESCE((SELECT SUM(gross_amount) FROM seller_early_withdrawals WHERE store_id=$1 AND status='pending'),0) ELSE 0 END)`

// PurchaseSellerService serializes wallet debits with payouts on the store row.
// The server chooses the price and entitlement; client prices are never accepted.
// SMS credit packs (sms_500/sms_2000) are NOT sold: there is no sending path
// yet, so the purchase is rejected before any wallet debit, purchase-history
// write, or smsCredits grant. The smsCredits column itself is untouched.
func (r *FinanceRepository) PurchaseSellerService(ctx context.Context, ownerID, storeID, service string) error {
	if service == "sms_500" || service == "sms_2000" {
		return ErrSMSServiceUnavailable
	}
	prices := map[string]int64{"standard": 890000, "pro": 1490000, "vip": 3900000}
	days := map[string]int{"standard": 90, "pro": 180, "vip": 365}
	price, ok := prices[service]
	if !ok {
		return ErrUnknownSellerService
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM stores WHERE id=$1 AND owner_user_id=$2 AND owner_type='dropshipper' FOR UPDATE`, storeID, ownerID).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return ErrStoreForbidden
		}
		return err
	}
	var available int64
	if err := tx.QueryRowContext(ctx, sellerAvailableSQL, storeID).Scan(&available); err != nil {
		return err
	}
	if available < price {
		return ErrStoreInsufficientBalance
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO seller_service_purchases(store_id,service_type,amount) VALUES($1,$2,$3)`, storeID, service, price); err != nil {
		return err
	}
	if duration, ok := days[service]; ok {
		if _, err := tx.ExecContext(ctx, `UPDATE stores SET active_plan_id=$2,plan_expires_at=GREATEST(COALESCE(plan_expires_at,NOW()),NOW()) + make_interval(days=>$3),updated_at=NOW() WHERE id=$1`, storeID, service, duration); err != nil {
			return err
		}
	} else {
		// Unreachable today: every priced service has a plan duration,
		// and SMS packs are rejected above. Defensive: never grant an
		// entitlement (e.g. smsCredits) for an unpriced service.
		return ErrUnknownSellerService
	}
	return tx.Commit()
}
