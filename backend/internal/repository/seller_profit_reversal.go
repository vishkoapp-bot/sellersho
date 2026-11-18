package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// FIN-009 seller-profit reversals (D-01).
//
// A dispute/admin refund reverses the seller's released profit pro-rata,
// like a wallet return (FIN-005) and like the FIN-011 referral reversal:
// the seller keeps only the share of RETAINED order value. The reversal is
// an order-level compensating event in seller_profit_reversals (see
// migration 202611190001), paired with a platform ledger debit, and is
// NEVER a mutation of the order row or of the profit_released flag.
//
// Why NOT customer_return_financial_events: that table is per-return-ITEM
// (return_id / return_item_id are NOT NULL FKs to customer_returns /
// return_items, and fin005_return_entitlement_proven counts its rows per
// return against the cost+margin expectation). Dispute/admin refunds have
// no customer_returns row at all, so the seller leg lives here, next to
// the referral reversal it mirrors.
//
// Pro-rata base: dropshipper_profit is order-level only, so the refunded
// share is derived from the refunded final_unit_price proportion:
//   target = min(trunc(profit * cumulativeRefunded / orderTotal), profit)
//   amount = target - alreadyReversed   (floored at 0)
// computed by referralReversalTarget, the single cumulative pro-rata rule
// both writers consult. cumulativeRefunded counts EVERY refunded value of
// the order exactly ONCE — succeeded dispute/admin_refund obligations
// PLUS refunded wallet returns — and alreadyReversed counts every
// seller_profit reversal already recorded for the order (wallet
// customer_return_financial_events rows plus rows here), so split
// refunds across both paths sum exactly to the whole-order target
// whatever the split. The "once" matters: a wallet return's
// customer_return obligation is itself succeeded, so the unfiltered
// obligations sum (referralSucceededRefundsTx, which the referral
// writer correctly trusts alone) already contains the wallet value —
// adding the wallet return_items sum on top of it counted the same
// money twice (round-2 mixed-path double-count, fin009-fix.md §12).
// The obligations leg here is therefore purpose-filtered to
// dispute/admin_refund; the wallet leg is the return_items sum.
//
// Balances read this table on RELEASED orders only (see
// sellerAvailableSQL), exactly like referral reversals: a reversal on an
// unreleased order nets against the FIN-008 treasury hold instead
// (finance_treasury.go heldInOrders), never against released
// availability. The release gate (disputeReleaseStateTx) refuses to
// release while any dispute/admin obligation exists, so a pre-release
// dispute refund can never make released profit appear.

// sellerProfitOrderSumsTx reads the order-time seller-profit snapshot and
// the totals the reversal rule needs. orderTotal is the merchandise value
// (SUM final_unit_price*quantity); a 0 total means the profit has no
// value base and no reversal is computable. alreadyReversed counts every
// seller_profit reversal already recorded for the order: wallet-return
// events in customer_return_financial_events (signed, same expression
// the balances use) plus prior rows in seller_profit_reversals.
func sellerProfitOrderSumsTx(ctx context.Context, tx *sql.Tx, orderID string) (storeID sql.NullString, profit, orderTotal, alreadyReversed int64, err error) {
	if err = tx.QueryRowContext(ctx, `SELECT store_id, COALESCE(dropshipper_profit,0) FROM orders WHERE id=$1`, orderID).Scan(&storeID, &profit); err != nil {
		return storeID, 0, 0, 0, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(final_unit_price*quantity),0) FROM order_items WHERE order_id=$1`, orderID).Scan(&orderTotal); err != nil {
		return storeID, 0, 0, 0, err
	}
	if !storeID.Valid || storeID.String == "" || profit <= 0 {
		return storeID, profit, orderTotal, 0, nil
	}
	// The events table may be absent in minimal-schema fixtures; a
	// missing table contributes 0 instead of failing the refund.
	if err = tx.QueryRowContext(ctx, `SELECT CASE WHEN to_regclass('customer_return_financial_events') IS NOT NULL THEN COALESCE((SELECT SUM(CASE WHEN operation='debit' THEN amount ELSE -amount END) FROM customer_return_financial_events WHERE order_id=$1 AND component='seller_profit'),0) ELSE 0 END`, orderID).Scan(&alreadyReversed); err != nil {
		return storeID, 0, 0, 0, err
	}
	var prior int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM seller_profit_reversals WHERE order_id=$1`, orderID).Scan(&prior); err != nil {
		return storeID, 0, 0, 0, err
	}
	return storeID, profit, orderTotal, alreadyReversed + prior, nil
}

// sellerProfitRefundedValueTx is the refunded-value record the pro-rata
// rule needs: succeeded dispute/admin_refund obligations (purpose
// IN ('dispute','admin_refund')) PLUS the merchandise value refunded
// through wallet customer returns (status 'refunded', same source the
// wallet path refunds from). Each refunded value counts ONCE: a wallet
// return's customer_return obligation is itself succeeded, so the
// unfiltered obligations sum already contains the wallet value — it
// must NOT be added again via the return_items sum (round-2
// mixed-path double-count: the pre-fix version called the unfiltered
// referralSucceededRefundsTx here). The referral writer is unaffected:
// it trusts the obligations alone and counts the wallet value exactly
// once through them.
func sellerProfitRefundedValueTx(ctx context.Context, tx *sql.Tx, orderID string) (int64, error) {
	var obligations int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(ob.amount_toman),0)
		FROM order_refund_obligations ob
		WHERE ob.order_id = $1
		  AND ob.purpose IN ('dispute', 'admin_refund')
		  AND COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id = ob.id ORDER BY e.event_no DESC LIMIT 1), ob.status) = 'succeeded'
	`, orderID).Scan(&obligations); err != nil {
		return 0, err
	}
	var wallet int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(oi.final_unit_price*ri.qty),0)
		FROM return_items ri
		JOIN order_items oi ON oi.id::text = ri.order_item_id
		JOIN customer_returns cr ON cr.id = ri.return_id
		WHERE cr.order_id = $1 AND cr.status = 'refunded' AND ri.received_at IS NOT NULL AND ri.qc_status IN ('ok', 'defective')
	`, orderID).Scan(&wallet); err != nil {
		return 0, err
	}
	return obligations + wallet, nil
}

// insertSellerProfitReversalTx writes one reversal row plus its platform
// ledger debit (target: the seller store, type debit). Both writes are
// idempotent on the source key, so a replayed refund writes nothing
// twice; the caller must hold the order finance lock.
func insertSellerProfitReversalTx(ctx context.Context, tx *sql.Tx, orderID, storeID, source, sourceID, reason, actorUserID string, amount int64) error {
	idemKey := "spr:" + source + ":" + sourceID
	ledgerID := "SPR-LED-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(idemKey)).String()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
		VALUES ($1, NOW(), $2, 'seller', 'debit', $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`, ledgerID, storeID, amount, reason, actorUserID, sourceID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO seller_profit_reversals (id, order_id, store_id, amount, source, source_id, ledger_entry_id, idempotency_key, reason, actor_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, "SPR-"+uuid.NewString(), orderID, storeID, amount, source, sourceID, ledgerID, idemKey, reason, actorUserID)
	return err
}

// recordSellerProfitReversalForOrderRefundTx writes the seller-profit
// reversal when an original_payment refund obligation reaches 'succeeded'
// (dispute / admin_refund). Call it only on the succeeded transition,
// inside the same transaction that records the outcome, while the order
// finance lock is held. Customer-return (wallet) refunds are handled by
// recordReturnReversalsTx instead; cancellations write nothing (a
// cancelled order can never release, so its profit is never earned).
// Only dropshipper stores carry a seller margin to reverse; a missing
// store row means "not a dropshipper store", never a failed refund.
// Minimal-schema fixtures that do not create seller_profit_reversals
// skip the leg (same guard recordReturnReversalsTx uses for the events
// table); the full migrated schema and the statement fixture always
// create it (migration 202611190001; openGapMoneyDB DDL).
func recordSellerProfitReversalForOrderRefundTx(ctx context.Context, tx *sql.Tx, obligationID, actorUserID string) error {
	var orderID, purpose, businessID string
	if err := tx.QueryRowContext(ctx, `SELECT order_id, purpose, business_id FROM order_refund_obligations WHERE id=$1`, obligationID).Scan(&orderID, &purpose, &businessID); err != nil {
		return err
	}
	if purpose != "dispute" && purpose != "admin_refund" {
		return nil
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&status); err != nil {
		return err
	}
	if status == "cancelled" {
		return nil
	}
	var hasTable bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('seller_profit_reversals') IS NOT NULL`).Scan(&hasTable); err != nil {
		return err
	}
	if !hasTable {
		return nil
	}
	storeID, profit, orderTotal, alreadyReversed, err := sellerProfitOrderSumsTx(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if !storeID.Valid || storeID.String == "" || profit <= 0 || orderTotal <= 0 {
		return nil
	}
	var ownerType string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(owner_type, '') FROM stores WHERE id = $1`, storeID.String).Scan(&ownerType); err != nil && err != sql.ErrNoRows {
		return err
	}
	if ownerType != "dropshipper" {
		return nil
	}
	cumulative, err := sellerProfitRefundedValueTx(ctx, tx, orderID)
	if err != nil {
		return err
	}
	amount := referralReversalTarget(profit, cumulative, orderTotal, alreadyReversed)
	if amount <= 0 {
		return nil
	}
	reason := fmt.Sprintf("برگشت سود فروشنده — استرداد %s", businessID)
	return insertSellerProfitReversalTx(ctx, tx, orderID, storeID.String, purpose, obligationID, reason, actorUserID, amount)
}
