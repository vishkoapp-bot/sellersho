package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drup/platform/internal/repository"
)

func TestSettlementSellerError(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{repository.ErrEarlyWithdrawalTooSmall, http.StatusBadRequest},
		{repository.ErrEarlyWithdrawalFeeUnset, http.StatusBadRequest},
		{repository.ErrStoreInsufficientBalance, http.StatusBadRequest},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		settlementSellerError(rec, c.err)
		if rec.Code != c.want {
			t.Errorf("settlementSellerError(%v) status = %d, want %d", c.err, rec.Code, c.want)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("settlementSellerError(%v) wrote an empty body", c.err)
		}
	}
}
