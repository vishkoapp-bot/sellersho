package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/drup/platform/internal/repository"
)

func handleCorrectReturnCharge(w http.ResponseWriter, r *http.Request, ctx context.Context, returnID, parcelID string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	actor, err := returnActor(ctx)
	if err != nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	key, ok := requireFinanceRequestKey(w, r)
	if !ok {
		return
	}
	// Pointers distinguish required zero values from omitted amounts.
	var body struct {
		ExpectedAmount *int64 `json:"expectedAmount"`
		FinalAmount    *int64 `json:"finalAmount"`
		Source         string `json:"source"`
		Reference      string `json:"reference"`
		Reason         string `json:"reason"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&body); err != nil || body.ExpectedAmount == nil || body.FinalAmount == nil {
		http.Error(w, "invalid correction payload", http.StatusBadRequest)
		return
	}
	if err = decoder.Decode(new(interface{})); err != io.EOF {
		http.Error(w, "invalid correction payload", http.StatusBadRequest)
		return
	}
	result, err := repo.CorrectReturnParcelCharge(ctx, returnID, parcelID, actor, key, repository.ParcelChargeCorrectionInput{ExpectedAmount: *body.ExpectedAmount, FinalAmount: *body.FinalAmount, Source: body.Source, Reference: body.Reference, Reason: body.Reason})
	if err != nil {
		if writeFinanceBusy(w, err) {
			return
		}
		status := http.StatusConflict
		if errors.Is(err, repository.ErrParcelChargeInput) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	json.NewEncoder(w).Encode(result)
}
