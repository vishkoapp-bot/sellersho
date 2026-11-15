package main

import (
	"context"
	"encoding/json"
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
		if strings.Contains(msg, "order not found") || strings.Contains(msg, "order reference") || strings.Contains(msg, "store mismatch") || strings.Contains(msg, "product title") {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, msg), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(dispute)
}

// handleResolveDispute resolves a dispute via finance_repo.go
// (called, never modified) and then applies the D37 live effect: a
// refund resolution also moves the order to cancelled and its payment
// transaction to refunded (OrderRepository.ApplyDisputeRefundEffect).
// TODO(U21): no PSP money movement is claimed or attempted here.
func handleResolveDispute(w http.ResponseWriter, r *http.Request, ctx context.Context, id string, finRepo *repository.FinanceRepository, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	var req struct {
		Action      string `json:"action"` // "release_to_seller" | "refund_carrier" | "refund_supplier"
		Party       string `json:"responsibleParty"`
		Operator    string `json:"operator"`
		ClaimAmount int64  `json:"claimAmount"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}

	if req.Operator == "" {
		req.Operator = "کارشناس ارشد داوری و مالی"
	}

	if err := finRepo.ResolveDispute(ctx, id, req.Action, req.Party, req.Operator, req.ClaimAmount); err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "already resolved"):
			http.Error(w, `{"error":"Dispute is already resolved"}`, http.StatusConflict)
		case strings.Contains(msg, "dispute not found"):
			http.Error(w, `{"error":"Dispute not found"}`, http.StatusNotFound)
		case strings.Contains(msg, "unknown responsible party"):
			http.Error(w, `{"error":"Unknown responsible party"}`, http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// D37: refund resolutions move order + payment transaction state.
	// A failure here is reported (the dispute itself is already
	// resolved in finance_repo.go); the effect is idempotent on retry
	// because it only touches orders not already cancelled.
	refundRequested := false
	if orderRepo != nil {
		applied, effErr := orderRepo.ApplyDisputeRefundEffect(ctx, id)
		if effErr != nil {
			http.Error(w, effErr.Error(), http.StatusInternalServerError)
			return
		}
		refundRequested = applied
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":         true,
		"disputeId":       id,
		"status":          "resolved",
		"orderRefunded":   false,
		"refundRequested": refundRequested,
		"message":         "Dispute resolved",
	})
}
