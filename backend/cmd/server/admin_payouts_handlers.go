package main

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// Fix69 (W-ROUTES) R4: admin payout confirmation + admin payment refund.
//
// Live evidence (live bundles, /opt/drup/dist/assets, read-only):
//   AdminDashboardView-MERGED.js:
//     confirmManualTransfer: POST /admin/payouts/<id>/confirm-manual-transfer
//       body {transfer_reference, transfer_date} where transfer_date
//       defaults client-side to new Date().toISOString().
//     refundPayment: POST /admin/payments/<id>/refund
//       body {reason, amount?} where amount is omitted for a full
//       refund (UI: "یا خالی برای کل مبلغ"); the client requires
//       reason, requires amount > 0 when given, and treats a
//       {success: true} response as done. The refund form text says
//       only paid orders are refundable and that the PSP refunds
//       first and the ledger entry follows.
//
// Payout confirm: delegates to
// FinanceRepository.ConfirmManualTransferWithDate, which is the ONLY
// step that writes the ledger debit (D32: approval writes no ledger
// entry; nothing here touches any approval path) and which stores the
// client-supplied transfer_date INSIDE the same transaction as the
// status move and the ledger debit (W2 fix: the Fix69 handler stored
// it with a second UPDATE after the confirmed commit, so a failure
// there left a confirmed payout with no date). The transfer_date
// column EXISTS (migration 202610190002); when it is absent the date
// is not passed in and transfer_confirmed_at=NOW() (set by the repo)
// stands, as the task's fallback requires. transfer_date accepts
// RFC3339 or a YYYY-MM-DD date.
//
// Payment refund: NO PSP call is made here. That matches the
// codebase's own convention (order_repo ApplyDisputeRefundEffect
// documents U21: whether live refunds move real PSP money is
// unverified, and no PSP gateway is configured in this candidate).
// This handler records the refund locally in one transaction:
// paid payment validation, cumulative-refund cap, status moves
// (full refund only: payment_transactions -> refunded and
// orders.payment_status -> refunded; partial refunds leave both
// statuses untouched), and one ledger entry. The <id> is resolved
// as a payment_transactions id first, then as an order id /
// order_number (W2 bundle check: the live refund form's <id> IS the
// typed order id — input placeholder "شناسه سفارش (الزامی)" — so the
// live-shaped call resolves through the order path; the payment-id
// reading is extra, not live-proven). The ledger target/type for a refund is a
// candidate-local choice (target_account = order id,
// target_type = platform, type = credit) and is NOT claimed as
// live: the live ledger shape for refunds is unproven.

func wr69AdminPayouts(w http.ResponseWriter, r *http.Request, db *sql.DB, finRepo *repository.FinanceRepository) {
	if t01DBNil(w, db) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/payouts/")
	rest = strings.TrimSuffix(rest, "/")
	if !strings.HasSuffix(rest, "/confirm-manual-transfer") {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSuffix(rest, "/confirm-manual-transfer")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if !t01MethodOnly(w, r, http.MethodPost) {
		return
	}
	wr69AdminPayoutConfirm(w, r, db, finRepo, id)
}

func wr69AdminPayoutConfirm(w http.ResponseWriter, r *http.Request, db *sql.DB, finRepo *repository.FinanceRepository, id string) {
	var req struct {
		TransferReference      string `json:"transfer_reference"`
		TransferReferenceCamel string `json:"transferReference"`
		TransferDate           string `json:"transfer_date"`
		TransferDateCamel      string `json:"transferDate"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	ref := req.TransferReference
	if ref == "" {
		ref = req.TransferReferenceCamel
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		http.Error(w, `{"error":"transfer_reference required"}`, http.StatusBadRequest)
		return
	}
	dateRaw := req.TransferDate
	if dateRaw == "" {
		dateRaw = req.TransferDateCamel
	}
	var transferDate time.Time
	var haveDate bool
	if strings.TrimSpace(dateRaw) != "" {
		var err error
		transferDate, err = time.Parse(time.RFC3339, strings.TrimSpace(dateRaw))
		if err != nil {
			transferDate, err = time.Parse("2006-01-02", strings.TrimSpace(dateRaw))
		}
		if err != nil {
			http.Error(w, `{"error":"transfer_date must be RFC3339 or YYYY-MM-DD"}`, http.StatusBadRequest)
			return
		}
		haveDate = true
	}
	ctx := r.Context()
	var current string
	err := db.QueryRowContext(ctx, `SELECT status FROM payout_requests WHERE id = $1`, id).Scan(&current)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"Payout not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if current != "approved" {
		http.Error(w, `{"error":"Payout is not approved"}`, http.StatusConflict)
		return
	}
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	// D32: this call alone writes the ledger debit, and (W2) stores
	// transfer_date inside the same transaction, so the status move,
	// the date and the ledger debit commit or roll back together. The
	// column probe below stays outside the tx (it is a schema check
	// only); the UPDATE itself is transactional. A date failure now
	// aborts the whole confirmation instead of returning 500 after it
	// already committed. A nil date (none supplied, or column absent)
	// leaves the column untouched.
	var transferDateArg *time.Time
	stored := false
	if haveDate && t01ColumnExists(ctx, db, "payout_requests", "transfer_date") {
		transferDateArg = &transferDate
		stored = true
	}
	if err := finRepo.ConfirmManualTransferWithDate(ctx, id, ref, wr69Operator(r), transferDateArg); err != nil {
		if strings.Contains(err.Error(), "not approved") {
			http.Error(w, `{"error":"Payout is not approved"}`, http.StatusConflict)
			return
		}
		writeInternalError(w, err)
		return
	}
	resp := map[string]interface{}{
		"success": true, "requestId": id, "status": "transfer_confirmed",
		"transferReference": ref, "transferDateStored": stored,
	}
	if haveDate {
		resp["transferDate"] = transferDate.Format(time.RFC3339)
	}
	t01WriteJSON(w, http.StatusOK, resp)
}

func wr69AdminPayments(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/payments/")
	rest = strings.TrimSuffix(rest, "/")
	if !strings.HasSuffix(rest, "/refund") {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSuffix(rest, "/refund")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if !t01MethodOnly(w, r, http.MethodPost) {
		return
	}
	wr69AdminPaymentRefund(w, r, db, id)
}

func wr69AdminPaymentRefund(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var req struct {
		Reason string `json:"reason"`
		Amount *int64 `json:"amount"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		http.Error(w, `{"error":"reason is required"}`, http.StatusBadRequest)
		return
	}
	if req.Amount != nil && *req.Amount <= 0 {
		http.Error(w, `{"error":"amount must be greater than 0"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer tx.Rollback()
	// Resolve <id>: payment id, then order id on payment_transactions,
	// then order id / order_number via orders.
	var paymentID, orderID string
	var paidAmount int64
	var payStatus string
	err = tx.QueryRowContext(ctx, `SELECT id, order_id, amount, status FROM payment_transactions WHERE id = $1 FOR UPDATE`, id).Scan(&paymentID, &orderID, &paidAmount, &payStatus)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `SELECT id, order_id, amount, status FROM payment_transactions WHERE order_id = $1 AND status = 'paid' ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, id).Scan(&paymentID, &orderID, &paidAmount, &payStatus)
	}
	if err == sql.ErrNoRows {
		var resolvedOrder string
		if oerr := tx.QueryRowContext(ctx, `SELECT id FROM orders WHERE id = $1 OR order_number = $1`, id).Scan(&resolvedOrder); oerr == nil {
			err = tx.QueryRowContext(ctx, `SELECT id, order_id, amount, status FROM payment_transactions WHERE order_id = $1 AND status = 'paid' ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, resolvedOrder).Scan(&paymentID, &orderID, &paidAmount, &payStatus)
		} else if oerr == sql.ErrNoRows {
			err = sql.ErrNoRows
		} else {
			writeInternalError(w, oerr)
			return
		}
	}
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"Payment not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if payStatus != "paid" {
		http.Error(w, `{"error":"Only paid payments can be refunded"}`, http.StatusConflict)
		return
	}
	// Cumulative refunds already recorded by this handler (same
	// reason prefix + related order id) cap further refunds.
	var alreadyRefunded int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE related_order_or_dispute = $1 AND reason LIKE 'استرداد وجه سفارش:%'`, orderID).Scan(&alreadyRefunded); err != nil {
		writeInternalError(w, err)
		return
	}
	refundAmount := paidAmount - alreadyRefunded
	if req.Amount != nil {
		refundAmount = *req.Amount
	}
	if refundAmount <= 0 {
		http.Error(w, `{"error":"Payment already fully refunded"}`, http.StatusConflict)
		return
	}
	if alreadyRefunded+refundAmount > paidAmount {
		http.Error(w, `{"error":"refund amount exceeds the paid amount"}`, http.StatusBadRequest)
		return
	}
	full := alreadyRefunded+refundAmount == paidAmount
	now := time.Now()
	ledgerID := "REF-LED-" + uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
		VALUES ($1, $2, $3, 'platform', 'credit', $4, $5, $6, $7)`, ledgerID, now, orderID, refundAmount, "استرداد وجه سفارش: "+reason, wr69Operator(r), orderID); err != nil {
		writeInternalError(w, err)
		return
	}
	if full {
		if _, err := tx.ExecContext(ctx, `UPDATE payment_transactions SET status = 'refunded' WHERE id = $1`, paymentID); err != nil {
			writeInternalError(w, err)
			return
		}
		if _, err := tx.ExecContext(ctx, `UPDATE orders SET payment_status = 'refunded' WHERE id = $1 AND payment_status = 'paid'`, orderID); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "paymentId": paymentID, "orderId": orderID,
		"refundedAmount": refundAmount, "alreadyRefunded": alreadyRefunded, "fullRefund": full,
	})
}
