package repository

import (
	"context"
	"database/sql"
	"strings"

	"github.com/drup/platform/internal/pricing"
	"github.com/google/uuid"
)

type RefundReservation struct {
	IDs    []string `json:"-"`
	Amount int64    `json:"amountToman"`
	State  string   `json:"status"`
}

// Caller already owns the complete order/payment/account prefix. A new monetary
// route must reserve here before any wallet effect, bookkeeping or provider I/O.
// Failed and unknown outcomes remain reserved; no destination can spend them.
func reserveOrderRefundTx(ctx context.Context, tx *sql.Tx, orderID, purpose, business, destination, actor string, amount int64, onlySource string) (RefundReservation, error) {
	var result RefundReservation
	if amount < 0 || amount > pricing.MaxTransportMoney || strings.TrimSpace(actor) == "" || business == "" {
		return result, pricing.ErrInvalidMoney
	}
	if purpose != "customer_return" && purpose != "admin_refund" && purpose != "dispute" {
		return result, ErrRefundPrincipalUnresolved
	}
	if destination != "wallet" && destination != "original_payment" {
		return result, ErrRefundPrincipalUnresolved
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,amount_toman,destination,source_payment_id,status FROM order_refund_obligations WHERE order_id=$1 AND purpose=$2 AND business_id=$3 ORDER BY id COLLATE "C"`, orderID, purpose, business)
	if err != nil {
		return result, err
	}
	result.State = "pending"
	for rows.Next() {
		var id, dest, source, state string
		var n int64
		if err = rows.Scan(&id, &n, &dest, &source, &state); err != nil {
			rows.Close()
			return result, err
		}
		if dest != destination || (onlySource != "" && source != onlySource) || n > pricing.MaxTransportMoney-result.Amount {
			rows.Close()
			return result, ErrRefundPrincipalUnresolved
		}
		result.Amount += n
		result.IDs = append(result.IDs, id)
		if state == "unknown" {
			result.State = "unknown"
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if len(result.IDs) > 0 {
		if amount != 0 && amount != result.Amount {
			return result, ErrRefundPrincipalUnresolved
		}
		return result, nil
	}
	sources, err := refundRecordedSourcesTx(ctx, tx, orderID)
	if err != nil {
		return result, err
	}
	if err = reserveLegacyRefundEvidenceTx(ctx, tx, orderID, actor, sources); err != nil {
		return result, err
	}
	type allocation struct {
		source    refundRecordedSource
		available int64
		verified  bool
	}
	var allocations []allocation
	var available int64
	for _, source := range sources {
		if onlySource != "" && source.id != onlySource {
			continue
		}
		var real bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=$1 AND funding_kind='gateway' AND amount_toman=$2)`, source.id, source.amount).Scan(&real); err != nil {
			return result, err
		}
		if destination == "wallet" && (!real || !source.customer.Valid) {
			continue
		}
		n, err := refundSourceAvailableTx(ctx, tx, source)
		if err != nil {
			return result, err
		}
		if n > pricing.MaxTransportMoney-available {
			return result, pricing.ErrInvalidMoney
		}
		available += n
		allocations = append(allocations, allocation{source, n, real})
	}
	if amount == 0 && purpose == "admin_refund" {
		amount = available
	}
	if amount <= 0 || amount > available {
		return result, ErrRefundPrincipalUnresolved
	}
	left := amount
	policy := "original_payment_v1"
	if purpose == "customer_return" {
		policy = "return_wallet_v1"
	}
	for _, a := range allocations {
		take := min(left, a.available)
		if take == 0 {
			continue
		}
		id := "ORF-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(purpose+":"+business+":"+a.source.id)).String()
		if _, err = tx.ExecContext(ctx, `INSERT INTO order_refund_obligations(id,order_id,source_payment_id,customer_id,source_amount_toman,amount_toman,currency,purpose,business_id,destination,policy_version,status,actor_user_id) VALUES($1,$2,$3,$4::uuid,$5,$6,'TOMAN',$7,$8,$9,$10,'unknown',$11)`, id, orderID, a.source.id, a.source.customer, a.source.amount, take, purpose, business, destination, policy, actor); err != nil {
			return result, err
		}
		result.IDs = append(result.IDs, id)
		result.Amount += take
		if !a.verified {
			result.State = "unknown"
		}
		left -= take
		if left == 0 {
			break
		}
	}
	return result, nil
}

func refundRecordedSourcesTx(ctx context.Context, tx *sql.Tx, orderID string) ([]refundRecordedSource, error) {
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.amount,o.customer_user_id::text,p.status FROM payment_transactions p JOIN orders o ON o.id=p.order_id WHERE p.order_id=$1 AND (p.status IN ('paid','refunded') OR EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=p.id)) ORDER BY p.id COLLATE "C" LIMIT 101`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []refundRecordedSource
	for rows.Next() {
		var source refundRecordedSource
		if err = rows.Scan(&source.id, &source.amount, &source.customer, &source.status); err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(sources) > 100 {
		return nil, ErrRefundPrincipalUnresolved
	}
	return sources, nil
}

func creditReservedWalletRefundsTx(ctx context.Context, tx *sql.Tx, ids []string) error {
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO order_refund_wallet_credits(obligation_id,customer_id,amount_toman) SELECT id,customer_id,amount_toman FROM order_refund_obligations WHERE id=$1 AND destination='wallet' ON CONFLICT(obligation_id) DO NOTHING`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO order_refund_events(id,obligation_id,source_payment_id,amount_toman,currency,destination,status,operation_key,provider_reference,reason) SELECT 'wallet-success:'||id,id,source_payment_id,amount_toman,currency,destination,'succeeded','refund:'||id,'wallet:'||id,'wallet_credited' FROM order_refund_obligations WHERE id=$1 ON CONFLICT(id) DO NOTHING`, id); err != nil {
			return err
		}
	}
	return nil
}

// ReserveAdminPaymentRefundTx is bookkeeping of a durable original-source
// request, never a statement that a bank transfer has already happened.
func ReserveAdminPaymentRefundTx(ctx context.Context, tx *sql.Tx, orderID, paymentID, operationID, actor string, amount int64) (RefundReservation, error) {
	return reserveOrderRefundTx(ctx, tx, orderID, "admin_refund", "admin:"+orderID+":"+operationID, "original_payment", actor, amount, paymentID)
}
