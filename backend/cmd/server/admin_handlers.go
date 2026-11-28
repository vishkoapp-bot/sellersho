package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/drup/platform/internal/repository"
)

func handleGetApprovals(w http.ResponseWriter, r *http.Request, ctx context.Context, adminRepo *repository.AdminRepository) {
	w.Header().Set("Content-Type", "application/json")
	if adminRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	status := r.URL.Query().Get("status")
	list, err := adminRepo.GetPendingApprovals(ctx, status)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	if list == nil {
		list = []repository.PendingApproval{}
	}
	json.NewEncoder(w).Encode(list)
}

func handleCreateApproval(w http.ResponseWriter, r *http.Request, ctx context.Context, adminRepo *repository.AdminRepository) {
	w.Header().Set("Content-Type", "application/json")
	if adminRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	var req repository.PendingApproval
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		req.ID = fmt.Sprintf("APP-%d", time.Now().UnixMilli()%100000)
	}

	if err := adminRepo.CreatePendingApproval(ctx, req); err != nil {
		writeInternalError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req)
}

func handleReviewApproval(w http.ResponseWriter, r *http.Request, ctx context.Context, id string, adminRepo *repository.AdminRepository, supplierRepo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	if adminRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	var req struct {
		Status          string `json:"status"` // 'approved' | 'rejected'
		RejectionReason string `json:"rejectionReason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}

	if req.Status != "approved" && req.Status != "rejected" {
		t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": repository.ErrInvalidApprovalDecision.Error()})
		return
	}

	// Route product proposals through the publication pipeline.
	entityType, err := adminRepo.GetPendingApprovalEntityType(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"Approval not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if entityType == "proposed_product" {
		if supplierRepo == nil {
			http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
			return
		}
		switch req.Status {
		case "approved":
			productID, err := supplierRepo.ApproveProposal(ctx, id, "")
			if err != nil {
				writeProposalPublicationError(w, err)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":   true,
				"id":        id,
				"status":    "approved",
				"productId": productID,
			})
			return
		case "rejected":
			if err := supplierRepo.RejectProposal(ctx, id, req.RejectionReason); err != nil {
				if errors.Is(err, repository.ErrProposalNotPending) {
					http.Error(w, `{"error":"Proposal is not pending"}`, http.StatusConflict)
					return
				}
				writeInternalError(w, err)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"id":      id,
				"status":  "rejected",
			})
			return
		}
	}

	if err := adminRepo.ReviewPendingApproval(ctx, id, req.Status, req.RejectionReason); err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			t01WriteJSON(w, http.StatusNotFound, map[string]string{"error": "Approval not found"})
		case errors.Is(err, repository.ErrApprovalNotPending), errors.Is(err, repository.ErrApprovalTargetChanged), errors.Is(err, repository.ErrStoreSlugTaken):
			t01WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case errors.Is(err, repository.ErrStoreInput):
			t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			writeInternalError(w, err)
		}
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"id":      id,
		"status":  req.Status,
	})
}

func writeProposalPublicationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrProposalNotPending):
		t01WriteJSON(w, http.StatusConflict, map[string]string{"error": "Proposal is not pending"})
	case errors.Is(err, repository.ErrInvalidPublicationCategory):
		t01WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"code": "invalid_publication_category", "error": "دسته‌بندی این کالا دیگر معتبر نیست. دسته‌بندی را اصلاح کنید.",
		})
	case errors.Is(err, repository.ErrPublicationWarehouseUnavailable):
		t01WriteJSON(w, http.StatusConflict, map[string]string{
			"code": "publication_warehouse_unavailable", "error": "انبار مبدأ این کالا در دسترس نیست. یک انبار فعالِ تأمین‌کننده انتخاب کنید.",
		})
	default:
		writeInternalError(w, err)
	}
}

func handleGetAuditLogs(w http.ResponseWriter, r *http.Request, ctx context.Context, adminRepo *repository.AdminRepository) {
	w.Header().Set("Content-Type", "application/json")
	if adminRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	category := r.URL.Query().Get("category")
	logs, err := adminRepo.GetAuditLogs(ctx, category)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	json.NewEncoder(w).Encode(logs)
}

func handleCreateAuditLog(w http.ResponseWriter, r *http.Request, ctx context.Context, adminRepo *repository.AdminRepository) {
	w.Header().Set("Content-Type", "application/json")
	if adminRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	var req repository.AuditLog
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		req.ID = fmt.Sprintf("LOG-%d", time.Now().UnixMilli()%100000)
	}

	if err := adminRepo.InsertAuditLog(ctx, req); err != nil {
		writeInternalError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req)
}

func handleGetAdminUsers(w http.ResponseWriter, r *http.Request, ctx context.Context, adminRepo *repository.AdminRepository) {
	w.Header().Set("Content-Type", "application/json")
	if adminRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	role := r.URL.Query().Get("role")
	status := r.URL.Query().Get("status")
	users, err := adminRepo.GetUsers(ctx, role, status)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if users == nil {
		users = []repository.AdminUser{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"users":   users,
	})
}

func handleSetUserActiveStatus(w http.ResponseWriter, r *http.Request, ctx context.Context, userID string, isActive bool, adminRepo *repository.AdminRepository) {
	w.Header().Set("Content-Type", "application/json")
	if adminRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	if err := adminRepo.SetUserActiveStatus(ctx, userID, isActive); err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"userId":   userID,
		"isActive": isActive,
	})
}

func handleGetPendingStores(w http.ResponseWriter, r *http.Request, ctx context.Context, adminRepo *repository.AdminRepository) {
	w.Header().Set("Content-Type", "application/json")
	if adminRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	stores, err := adminRepo.GetPendingStores(ctx)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if stores == nil {
		stores = []repository.AdminStore{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"stores":  stores,
	})
}

func handleReviewStore(w http.ResponseWriter, r *http.Request, ctx context.Context, storeID, status string, adminRepo *repository.AdminRepository) {
	w.Header().Set("Content-Type", "application/json")
	if adminRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := adminRepo.ReviewStore(ctx, storeID, status, req.Reason); err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"storeId": storeID,
		"status":  status,
		"reason":  req.Reason,
	})
}

func handleLockStoreMargin(w http.ResponseWriter, r *http.Request, ctx context.Context, storeID string, adminRepo *repository.AdminRepository) {
	w.Header().Set("Content-Type", "application/json")
	if adminRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		LockedMargin float64 `json:"lockedMargin"`
		MaxMargin    float64 `json:"maxMargin"`
		IsLocked     bool    `json:"isLocked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	margin := req.LockedMargin
	if margin == 0 && req.MaxMargin > 0 {
		margin = req.MaxMargin
	}
	if err := adminRepo.LockStoreMargin(ctx, storeID, margin); err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"storeId":      storeID,
		"lockedMargin": margin,
		"maxMargin":    margin,
		"isLocked":     true,
	})
}

func handleGetSuspiciousTransactions(w http.ResponseWriter, r *http.Request, ctx context.Context, adminRepo *repository.AdminRepository) {
	if !t01MethodOnly(w, r, http.MethodGet) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if adminRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	txs, err := adminRepo.GetSuspiciousTransactions(ctx)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if txs == nil {
		txs = []repository.SuspiciousTransaction{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":                true,
		"suspiciousTransactions": txs,
	})
}
