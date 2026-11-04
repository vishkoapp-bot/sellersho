package main

import (
	"errors"
	"net/http"

	"github.com/drup/platform/internal/repository"
)

// settlementSellerError maps early-withdrawal errors to seller-facing
// responses (branch wording, kept in Persian). It covers only the
// seller request path: the finance review/list routes and the admin
// settings route already exist in live-parity
// (handleT01EarlyWithdrawalsList, handleT01EarlyWithdrawalReview,
// handleT01AdminSettingsSettlement) and are deliberately NOT
// duplicated here.
func settlementSellerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrEarlyWithdrawalTooSmall):
		http.Error(w, `{"error":"حداقل مبلغ برداشت زودهنگام ۱٬۰۰۰٬۰۰۰ تومان است."}`, http.StatusBadRequest)
	case errors.Is(err, repository.ErrEarlyWithdrawalFeeUnset):
		http.Error(w, `{"error":"کارمزد برداشت زودهنگام هنوز توسط مدیر تنظیم نشده است."}`, http.StatusBadRequest)
	case errors.Is(err, repository.ErrStoreInsufficientBalance):
		http.Error(w, `{"error":"موجودی کیف پول برای این درخواست کافی نیست."}`, http.StatusBadRequest)
	default:
		sellerError(w, err)
	}
}
