package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func handleGetBins(w http.ResponseWriter, r *http.Request, ctx context.Context, wmsRepo *repository.WMSRepository) {
	w.Header().Set("Content-Type", "application/json")
	if wmsRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	bins, err := wmsRepo.GetBins(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(bins)
}

func handleGetWarehouseOverview(w http.ResponseWriter, r *http.Request, ctx context.Context, wmsRepo *repository.WMSRepository) {
	w.Header().Set("Content-Type", "application/json")
	if wmsRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	overview, err := wmsRepo.GetOverview(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(overview)
}

func handleUpdateBinCapacity(w http.ResponseWriter, r *http.Request, ctx context.Context, binCode string, wmsRepo *repository.WMSRepository) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		CapacityVolumeCm3 int64 `json:"capacityVolumeCm3"`
		MaxWeightGrams    int64 `json:"maxWeightGrams"`
		Verify            bool  `json:"verify"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	claims, _ := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if err := wmsRepo.UpdateBinCapacity(ctx, binCode, claims.UserID, req.CapacityVolumeCm3, req.MaxWeightGrams, req.Verify); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "binCode": binCode, "capacityStatus": map[bool]string{true: "verified", false: "unverified"}[req.Verify]})
}

func handleGetInbounds(w http.ResponseWriter, r *http.Request, ctx context.Context, wmsRepo *repository.WMSRepository) {
	if !t01MethodOnly(w, r, http.MethodGet) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if wmsRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	inbounds, err := wmsRepo.GetInboundShipments(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(inbounds)
}

func handleGetManifests(w http.ResponseWriter, r *http.Request, ctx context.Context, wmsRepo *repository.WMSRepository) {
	w.Header().Set("Content-Type", "application/json")
	if wmsRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	manifests, err := wmsRepo.GetManifests(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(manifests)
}

func handleDispatchManifest(w http.ResponseWriter, r *http.Request, ctx context.Context, id string, wmsRepo *repository.WMSRepository) {
	w.Header().Set("Content-Type", "application/json")
	if wmsRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	if err := wmsRepo.DispatchManifest(ctx, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "manifestId": id, "status": "handed_over"})
}

func handleGetLedger(w http.ResponseWriter, r *http.Request, ctx context.Context, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	entries, err := finRepo.GetLedgerEntries(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(entries)
}

func handleGetDisputes(w http.ResponseWriter, r *http.Request, ctx context.Context, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	disputes, err := finRepo.GetDisputes(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if disputes == nil {
		disputes = []domain.Dispute{}
	}
	json.NewEncoder(w).Encode(disputes)
}

func handleCreateDispute(w http.ResponseWriter, r *http.Request, ctx context.Context, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	var req struct {
		OrderNumber  string `json:"orderNumber"`
		Type         string `json:"type"`
		CustomerName string `json:"customerName"`
		ProductTitle string `json:"productTitle"`
		ClaimAmount  int64  `json:"claimAmount"`
		Reason       string `json:"reason"`
		StoreID      string `json:"storeId"`
		SupplierName string `json:"supplierName"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}

	dispute := domain.Dispute{
		ID:           "DISP-" + uuid.NewString(),
		OrderNumber:  req.OrderNumber,
		Type:         req.Type,
		CustomerName: req.CustomerName,
		ProductTitle: req.ProductTitle,
		ClaimAmount:  req.ClaimAmount,
		Reason:       req.Reason,
		StoreID:      req.StoreID,
		SupplierName: req.SupplierName,
		Status:       "pending",
		CreatedAt:    time.Now(),
	}

	if err := finRepo.CreateDispute(ctx, dispute); err != nil {
		msg := err.Error()
		if errors.Is(err,repository.ErrDisputeInput)||strings.Contains(msg, "order not found") || strings.Contains(msg, "order reference") || strings.Contains(msg, "store mismatch") || strings.Contains(msg, "product title") {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, msg), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(dispute)
}

// The transaction records a verdict and reserves a refund; payment progress is order-level.
func handleResolveDispute(w http.ResponseWriter, r *http.Request, ctx context.Context, id string, finRepo *repository.FinanceRepository, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	claims, _ := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		Action      string `json:"action"`
		Party       string `json:"responsibleParty"`
		ClaimAmount *int64 `json:"claimAmount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	outcome, err := finRepo.ResolveDisputeWithOutcome(ctx, id, repository.DisputeResolutionInput{Action: req.Action, ResponsibleParty: req.Party, ActorUserID: claims.UserID, ExpectedClaimAmount: req.ClaimAmount})
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			http.Error(w, `{"error":"Dispute not found"}`, http.StatusNotFound)
		case errors.Is(err, repository.ErrDisputeInput):
			http.Error(w, `{"error":"Invalid dispute verdict"}`, http.StatusBadRequest)
		case errors.Is(err, repository.ErrDisputeConflict):
			http.Error(w, `{"error":"Conflicting dispute verdict"}`, http.StatusConflict)
		default:
			http.Error(w, `{"error":"Decision outcome could not be confirmed. Reload or retry the same verdict."}`, http.StatusInternalServerError)
		}
		return
	}
	json.NewEncoder(w).Encode(outcome)
}
