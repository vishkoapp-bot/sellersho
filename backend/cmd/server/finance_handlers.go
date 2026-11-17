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

func handleGetFilteredLedger(w http.ResponseWriter, r *http.Request, ctx context.Context, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	dateFrom := r.URL.Query().Get("dateFrom")
	dateTo := r.URL.Query().Get("dateTo")
	entries, err := finRepo.GetLedgerFiltered(ctx, dateFrom, dateTo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []domain.LedgerEntry{}
	}
	json.NewEncoder(w).Encode(entries)
}

func handleGetStoreBalance(w http.ResponseWriter, r *http.Request, ctx context.Context, storeID string, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	balance, err := finRepo.GetStoreBalance(ctx, storeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(balance)
}

func handleGetStoreStatement(w http.ResponseWriter, r *http.Request, ctx context.Context, storeID string, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	stmt, err := finRepo.GetStoreStatement(ctx, storeID, month)
	if err != nil {
		if strings.Contains(err.Error(), "invalid month") {
			http.Error(w, `{"error":"month must be YYYY-MM"}`, http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(stmt)
}

func handleGetPendingSettlements(w http.ResponseWriter, r *http.Request, ctx context.Context, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	list, err := finRepo.GetPendingSettlements(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []repository.SettlementRequest{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"settlements": list,
	})
}

func handleGetSettlementDetails(w http.ResponseWriter, r *http.Request, ctx context.Context, id string, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	settlement, err := finRepo.GetSettlementDetails(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if settlement == nil {
		http.Error(w, `{"error":"Settlement not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(settlement)
}

func handleReviewSettlement(w http.ResponseWriter, r *http.Request, ctx context.Context, id, status string, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	operator := "کارشناس مالی"
	if claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil {
		operator = claims.FullName
	}
	if err := finRepo.ReviewSettlement(ctx, id, status, req.Reason, operator); err != nil {
		if writeFinanceBusy(w, err) {
			return
		}
		if strings.Contains(err.Error(), "already reviewed") {
			http.Error(w, `{"error":"Settlement is already reviewed"}`, http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"requestId": id,
		"status":    status,
		"reason":    req.Reason,
	})
}

// handleConfirmManualTransfer implements POST
// /api/v1/finance/settlements/<id>/transfer-confirmed (D32). The body
// carries {"transferReference": "..."}. Only an "approved" payout can be
// confirmed; the ledger debit is written by ConfirmManualTransfer.
func handleConfirmManualTransfer(w http.ResponseWriter, r *http.Request, ctx context.Context, id string, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		TransferReference string `json:"transferReference"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.TransferReference == "" {
		http.Error(w, `{"error":"transferReference required"}`, http.StatusBadRequest)
		return
	}
	operator := "کارشناس مالی"
	if claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil {
		operator = claims.FullName
	}
	if err := finRepo.ConfirmManualTransfer(ctx, id, req.TransferReference, operator); err != nil {
		if writeFinanceBusy(w, err) {
			return
		}
		if strings.Contains(err.Error(), "not approved") {
			http.Error(w, `{"error":"Payout is not approved"}`, http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":           true,
		"requestId":         id,
		"status":            "transfer_confirmed",
		"transferReference": req.TransferReference,
	})
}

func handleCreateManualDispute(w http.ResponseWriter, r *http.Request, ctx context.Context, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		OrderID      string `json:"orderId"`
		OrderNumber  string `json:"orderNumber"`
		Type         string `json:"type"`
		Amount       int64  `json:"amount"`
		Reason       string `json:"reason"`
		StoreID      string `json:"storeId"`
		CustomerName string `json:"customerName"`
		ProductTitle string `json:"productTitle"`
		SupplierName string `json:"supplierName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	// Fix69 A5: no placeholders. The old handler hardcoded
	// StoreID="store-bibi" (no such store exists in either DB),
	// CustomerName="مشتری فروشگاه", ProductTitle="کالای مورد اختلاف",
	// forced Type="delivery_undelivered" discarding the request's type,
	// and flipped the order to delivery_disputed even when the order
	// lookup missed. Now the order is mandatory and is resolved by
	// CreateDispute (store/customer/product fall back to the resolved
	// order); a missing order fails closed there and is mapped to 400.
	if req.OrderID == "" && req.OrderNumber == "" {
		http.Error(w, `{"error":"orderId or orderNumber is required"}`, http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		req.Type = "delivery_undelivered"
	}
	if req.Type != "return_7day" && req.Type != "delivery_undelivered" {
		http.Error(w, `{"error":"Dispute type must be return_7day or delivery_undelivered"}`, http.StatusBadRequest)
		return
	}
	if req.Amount < 0 {
		http.Error(w, `{"error":"amount must not be negative"}`, http.StatusBadRequest)
		return
	}

	dispID := "DISP-" + uuid.NewString()
	d := domain.Dispute{
		ID:           dispID,
		OrderID:      req.OrderID,
		OrderNumber:  req.OrderNumber,
		Type:         req.Type,
		CustomerName: req.CustomerName,
		ProductTitle: req.ProductTitle,
		ClaimAmount:  req.Amount,
		Reason:       req.Reason,
		StoreID:      req.StoreID,
		SupplierName: req.SupplierName,
		Status:       "pending",
		CreatedAt:    time.Now(),
	}
	if err := finRepo.CreateDispute(ctx, d); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "order not found") || strings.Contains(msg, "order reference") || strings.Contains(msg, "store mismatch") || strings.Contains(msg, "product title") {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, msg), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"disputeId": dispID,
	})
}

func handleGetRevenueReport(w http.ResponseWriter, r *http.Request, ctx context.Context, finRepo *repository.FinanceRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	period := r.URL.Query().Get("period")
	if period == "" {
		// No period given: the current calendar quarter. The old
		// hard-coded "2026-Q1" default is retired with the all-time
		// computation it accompanied.
		now := time.Now()
		period = fmt.Sprintf("%d-Q%d", now.Year(), (int(now.Month())-1)/3+1)
	}
	rep, err := finRepo.GetRevenueReport(ctx, period)
	if err != nil {
		if strings.Contains(err.Error(), "invalid period") {
			http.Error(w, `{"error":"period must be YYYY-Qn or YYYY-MM"}`, http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(rep)
}
