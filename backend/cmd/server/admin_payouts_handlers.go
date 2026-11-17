package main

import (
	"database/sql"
	"errors"
	"github.com/drup/platform/internal/pricing"
	"net/http"
	"strings"
	"time"

	"github.com/drup/platform/internal/repository"
)

// Admin payout confirmation preserves its existing bank-transfer contract.
// Admin order refunds reserve a source-bound original-payment request and return
// accepted/pending or review state. A request is never proof of a PSP payout.

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
		if writeFinanceBusy(w, err) {
			return
		}
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
	result, err := repository.NewOrderRepository(db).RequestAdminPaymentRefund(r.Context(), id, r.Header.Get("Idempotency-Key"), req.Reason, wr69Operator(r), req.Amount)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrStoreInput), errors.Is(err, pricing.ErrInvalidMoney):
			http.Error(w, `{"error":"valid reason, amount and Idempotency-Key are required"}`, http.StatusBadRequest)
		case errors.Is(err, sql.ErrNoRows):
			http.Error(w, `{"error":"Payment not found"}`, http.StatusNotFound)
		case errors.Is(err, repository.ErrRefundPrincipalUnresolved), errors.Is(err, repository.ErrFinanceCommandConflict):
			http.Error(w, `{"error":"refund principal is reserved, unresolved or request identity changed"}`, http.StatusConflict)
		default:
			writeInternalError(w, err)
		}
		return
	}
	t01WriteJSON(w, http.StatusAccepted, result)
}
