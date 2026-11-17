package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/drup/platform/internal/pricing"
)

type AdminRefundRequestResult struct {
	Success         bool   `json:"success"`
	PaymentID       string `json:"paymentId"`
	OrderID         string `json:"orderId"`
	RequestedAmount int64  `json:"requestedAmountToman"`
	Status          string `json:"status"`
}

func (r *OrderRepository) RequestAdminPaymentRefund(ctx context.Context, id, key, reason, actor string, amount *int64) (AdminRefundRequestResult, error) {
	var result AdminRefundRequestResult
	key, reason = strings.TrimSpace(key), strings.TrimSpace(reason)
	if key == "" || len(key) > 128 || reason == "" || len(reason) > 1000 || strings.TrimSpace(actor) == "" {
		return result, ErrStoreInput
	}
	if amount != nil && (*amount <= 0 || *amount > pricing.MaxTransportMoney) {
		return result, pricing.ErrInvalidMoney
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var orderID, paymentID string
	err = tx.QueryRowContext(ctx, `SELECT order_id,id FROM payment_transactions WHERE id=$1`, id).Scan(&orderID, &paymentID)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `SELECT p.order_id,p.id FROM payment_transactions p JOIN orders o ON o.id=p.order_id WHERE (o.id=$1 OR o.order_number=$1) AND (p.status IN ('paid','refunded') OR EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=p.id)) ORDER BY p.created_at DESC,p.id COLLATE "C" LIMIT 1`, id).Scan(&orderID, &paymentID)
	}
	if err != nil {
		return result, err
	}
	if err = lockOrderFinanceTx(ctx, tx, orderID, true); err != nil {
		return result, err
	}
	var stable bool
	if err = tx.QueryRowContext(ctx, `SELECT order_id=$2 AND (status IN ('paid','refunded') OR EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=$1)) FROM payment_transactions WHERE id=$1`, paymentID, orderID).Scan(&stable); err != nil {
		return result, err
	}
	if !stable {
		return result, ErrRefundPrincipalUnresolved
	}
	// Fingerprint the immutable caller intent, not a freshly selected latest
	// payment. The persisted result already freezes its accepted source; later
	// payment history must not turn a response-loss retry into a new operation.
	payload, err := json.Marshal(struct {
		TargetID string
		Amount   *int64
		Reason   string
	}{id, amount, reason})
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(payload)
	fingerprint := hex.EncodeToString(sum[:])
	var previous string
	var encoded []byte
	err = tx.QueryRowContext(ctx, `SELECT fingerprint,result FROM order_refund_request_keys WHERE order_id=$1 AND client_key=$2`, orderID, key).Scan(&previous, &encoded)
	if err == nil {
		if previous != fingerprint {
			return result, ErrFinanceCommandConflict
		}
		if err = json.Unmarshal(encoded, &result); err != nil {
			return result, err
		}
		return result, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return result, err
	}
	var requested int64
	if amount != nil {
		requested = *amount
	}
	reservation, err := ReserveAdminPaymentRefundTx(ctx, tx, orderID, paymentID, key, actor, requested)
	if err != nil {
		return result, err
	}
	result = AdminRefundRequestResult{true, paymentID, orderID, reservation.Amount, reservation.State}
	encoded, err = json.Marshal(result)
	if err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO order_refund_request_keys(order_id,client_key,fingerprint,result) VALUES($1,$2,$3,$4::jsonb)`, orderID, key, fingerprint, string(encoded)); err != nil {
		return result, err
	}
	return result, tx.Commit()
}
