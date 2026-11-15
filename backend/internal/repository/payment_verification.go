package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/payment"
	"github.com/drup/platform/internal/pricing"
	"github.com/google/uuid"
)

var ErrPaymentVerificationIdentity = errors.New("payment verification identity changed")

// BeginPaymentVerification commits a stable source snapshot before provider I/O.
// fundingKind comes from the server's adapter configuration, never callback JSON.
// A retained request with no receipt is an outstanding inquiry, not proof of cash.
func (r *OrderRepository) BeginPaymentVerification(ctx context.Context, token, fundingKind string) (string, error) {
	if fundingKind != "gateway" && fundingKind != "simulated" {
		return "", ErrPaymentVerificationIdentity
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var orderID string
	if err = tx.QueryRowContext(ctx, `SELECT order_id FROM payment_transactions WHERE gateway_token=$1`, token).Scan(&orderID); err != nil {
		return "", err
	}
	if err = lockOrderFinanceTx(ctx, tx, orderID, true); err != nil {
		return "", err
	}
	var paymentID string
	var amount int64
	var customer sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT p.id,p.amount,o.customer_user_id::text FROM payment_transactions p JOIN orders o ON o.id=p.order_id WHERE p.gateway_token=$1 AND p.order_id=$2`, token, orderID).Scan(&paymentID, &amount, &customer); err != nil {
		return "", err
	}
	if amount <= 0 || amount > pricing.MaxTransportMoney {
		return "", pricing.ErrInvalidMoney
	}
	id := "OPV-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(paymentID)).String()
	if _, err = tx.ExecContext(ctx, `INSERT INTO order_payment_verifications(id,payment_id,order_id,customer_id,amount_toman,currency,funding_kind) VALUES($1,$2,$3,$4::uuid,$5,'TOMAN',$6) ON CONFLICT(payment_id) DO NOTHING`, id, paymentID, orderID, customer, amount, fundingKind); err != nil {
		return "", err
	}
	var unchanged bool
	if err = tx.QueryRowContext(ctx, `SELECT id=$1 AND order_id=$2 AND customer_id IS NOT DISTINCT FROM $3::uuid AND amount_toman=$4 AND funding_kind=$5 FROM order_payment_verifications WHERE payment_id=$6`, id, orderID, customer, amount, fundingKind, paymentID).Scan(&unchanged); err != nil {
		return "", err
	}
	if !unchanged {
		return "", ErrPaymentVerificationIdentity
	}
	return id, tx.Commit()
}

// RecordPaymentVerification accepts only a backend adapter result. It records
// confirmed funding before any Redis/admission check and atomically compensates
// an already cancelled order. It never captures stock or makes a provider call.
func (r *OrderRepository) RecordPaymentVerification(ctx context.Context, verificationID string, result payment.VerificationResult) error {
	if !result.Success {
		return r.RecordPaymentVerificationUnknown(ctx, verificationID, "verification_unconfirmed")
	}
	if strings.TrimSpace(result.RRN) == "" || len(result.RRN) > 30 || result.VerifiedAt.IsZero() || result.VerifiedAt.After(time.Now().Add(5*time.Second)) {
		return ErrPaymentVerificationIdentity
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var orderID string
	if err = tx.QueryRowContext(ctx, `SELECT order_id FROM order_payment_verifications WHERE id=$1`, verificationID).Scan(&orderID); err != nil {
		return err
	}
	if err = lockOrderFinanceTx(ctx, tx, orderID, true); err != nil {
		return err
	}
	var expected int64
	if err = tx.QueryRowContext(ctx, `SELECT amount_toman FROM order_payment_verifications WHERE id=$1 AND order_id=$2`, verificationID, orderID).Scan(&expected); err != nil {
		return err
	}
	if expected != result.VerifiedAmount {
		if err = recordVerificationEventTx(ctx, tx, verificationID, "amount_mismatch", result.VerifiedAmount, result.RRN, "confirmed_amount_requires_review"); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return payment.ErrAmountMismatch
	}
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT provider_reference FROM order_payment_receipts WHERE verification_id=$1`, verificationID).Scan(&previous)
	if err == nil {
		if previous != result.RRN {
			return ErrPaymentVerificationIdentity
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		id := "OPR-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(verificationID)).String()
		if _, err = tx.ExecContext(ctx, `INSERT INTO order_payment_receipts(id,verification_id,funding_kind,payment_id,order_id,customer_id,amount_toman,currency,provider_reference,verified_at) SELECT $1,id,funding_kind,payment_id,order_id,customer_id,amount_toman,currency,$3,$4 FROM order_payment_verifications WHERE id=$2`, id, verificationID, result.RRN, result.VerifiedAt); err != nil {
			return err
		}
	} else {
		return err
	}
	if err = recordVerificationEventTx(ctx, tx, verificationID, "verified", result.VerifiedAmount, result.RRN, "confirmed"); err != nil {
		return err
	}
	var cancelled bool
	if err = tx.QueryRowContext(ctx, `SELECT status='cancelled' FROM orders WHERE id=$1`, orderID).Scan(&cancelled); err != nil {
		return err
	}
	if cancelled {
		if err = synchronizeVerifiedPaymentFactsTx(ctx, tx, orderID); err != nil {
			return err
		}
		if err = ensureCancellationRefundsTx(ctx, tx, orderID, "system:verified-payment"); err != nil {
			return err
		}
	}
	if !cancelled {
		if err = reserveExtraVerifiedCaptureTx(ctx, tx, orderID, verificationID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// No response, a timeout or a decline from an adapter without a certified
// no-capture contract is uncertain. Keep the inquiry and all principal holds.
func (r *OrderRepository) RecordPaymentVerificationUnknown(ctx context.Context, id, reason string) error {
	if reason != "provider_unavailable" && reason != "verification_unconfirmed" {
		return ErrPaymentVerificationIdentity
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = recordVerificationEventTx(ctx, tx, id, "unknown", nil, "", reason); err != nil {
		return err
	}
	return tx.Commit()
}

func recordVerificationEventTx(ctx context.Context, tx *sql.Tx, id, status string, amount any, reference, reason string) error {
	key := id + ":" + status + ":" + reference + ":" + reason
	if n, ok := amount.(int64); ok {
		key += ":" + strconv.FormatInt(n, 10)
	}
	event := "OPVE-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(key)).String()
	_, err := tx.ExecContext(ctx, `INSERT INTO order_payment_verification_events(id,verification_id,status,observed_amount_toman,provider_reference,reason) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO NOTHING`, event, id, status, amount, reference, reason)
	return err
}

// Called under the financial prefix by cancellation/expiry, not by pending
// fulfillment: a receipt alone must not authorize dispatch before stock capture.
func synchronizeVerifiedPaymentFactsTx(ctx context.Context, tx *sql.Tx, orderID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE payment_transactions p SET status='paid',rrn=r.provider_reference,compensation_capture=TRUE FROM order_payment_receipts r WHERE p.id=r.payment_id AND p.order_id=$1 AND p.status<>'refunded'`, orderID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE orders SET payment_status=CASE WHEN payment_status='refunded' THEN 'refunded' ELSE 'paid' END,updated_at=NOW() WHERE id=$1 AND EXISTS(SELECT 1 FROM order_payment_receipts WHERE order_id=$1)`, orderID)
	return err
}

var errPaymentAlreadyCaptured = errors.New("payment already captured inventory")

// A missing Redis hold can be a retry after successful capture. Inspect it under
// the very same order guard as cancellation, so it cannot cancel that success.
func (r *OrderRepository) RejectVerifiedPaymentAdmission(ctx context.Context, token string) (bool, error) {
	p, err := r.GetPaymentTransaction(ctx, token)
	if err != nil {
		return false, err
	}
	if p == nil {
		return false, sql.ErrNoRows
	}
	err = r.cancelOrder(ctx, p.OrderID, "system:admission-rejected", true, p.ID)
	if errors.Is(err, errPaymentAlreadyCaptured) {
		return false, nil
	}
	return err == nil, err
}

// The earliest independently verified gateway receipt funds fulfillment. Any
// later real receipt preserves its own money and compensation without capturing
// a second unit or cancelling goods funded by the first receipt.
func reserveExtraVerifiedCaptureTx(ctx context.Context, tx *sql.Tx, orderID, verificationID string) error {
	var extra bool
	var paymentID string
	if err := tx.QueryRowContext(ctx, `SELECT a.payment_id,EXISTS(SELECT 1 FROM order_payment_receipts b WHERE b.order_id=a.order_id AND b.payment_id<>a.payment_id AND b.funding_kind='gateway' AND (b.created_at,b.id COLLATE "C")<(a.created_at,a.id COLLATE "C")) AND a.funding_kind='gateway' FROM order_payment_receipts a WHERE a.verification_id=$1`, verificationID).Scan(&paymentID, &extra); err != nil {
		return err
	}
	if !extra {
		var legacySlot bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM payment_transactions other WHERE other.order_id=o.id AND other.id<>p.id AND other.status='paid' AND NOT other.compensation_capture) OR (p.status<>'paid' AND (o.inventory_captured OR o.status IN ('shipped','delivered','delivered_unconfirmed','delivered_confirmed','delivery_disputed'))) FROM orders o JOIN payment_transactions p ON p.order_id=o.id WHERE p.id=$1 AND o.id=$2`, paymentID, orderID).Scan(&legacySlot); err != nil {
			return err
		}
		if !legacySlot {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE payment_transactions p SET status='paid',rrn=r.provider_reference,compensation_capture=TRUE FROM order_payment_receipts r WHERE r.payment_id=p.id AND p.id=$1 AND r.funding_kind='gateway' AND p.status<>'refunded'`, paymentID); err != nil {
			return err
		}
		sources, err := refundRecordedSourcesTx(ctx, tx, orderID)
		if err != nil {
			return err
		}
		for _, s := range sources {
			if s.id != paymentID {
				continue
			}
			available, e := refundSourceAvailableTx(ctx, tx, s)
			if e != nil {
				return e
			}
			if available > 0 {
				return insertLegacyRefundHoldTx(ctx, tx, orderID, "system:extra-payment-review", "extra-history:"+paymentID, s, available)
			}
			return nil
		}
		return ErrRefundPrincipalUnresolved
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_transactions p SET status='paid',rrn=r.provider_reference,compensation_capture=TRUE FROM order_payment_receipts r WHERE r.payment_id=p.id AND p.id=$1 AND p.status<>'refunded'`, paymentID); err != nil {
		return err
	}
	sources, err := refundRecordedSourcesTx(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if err = reserveLegacyRefundEvidenceTx(ctx, tx, orderID, "system:extra-payment", sources); err != nil {
		return err
	}
	for _, s := range sources {
		if s.id != paymentID {
			continue
		}
		available, e := refundSourceAvailableTx(ctx, tx, s)
		if e != nil {
			return e
		}
		if available == 0 {
			return nil
		}
		id := "ORF-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte("duplicate_payment:"+paymentID)).String()
		_, err = tx.ExecContext(ctx, `INSERT INTO order_refund_obligations(id,order_id,source_payment_id,customer_id,source_amount_toman,amount_toman,currency,purpose,business_id,destination,policy_version,status,actor_user_id) VALUES($1,$2,$3,$4::uuid,$5,$6,'TOMAN','duplicate_payment',$3,'original_payment','original_payment_v1','pending','system:extra-payment')`, id, orderID, s.id, s.customer, s.amount, available)
		return err
	}
	return ErrRefundPrincipalUnresolved
}
