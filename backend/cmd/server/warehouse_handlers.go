package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

func getCallerUserID(ctx context.Context) string {
	claimsVal := ctx.Value(userClaimsKey)
	if claims, ok := claimsVal.(*auth.UserClaims); ok && claims != nil {
		return claims.UserID
	}
	return "staff-user-001"
}

func handleGetPendingInbounds(w http.ResponseWriter, r *http.Request, ctx context.Context, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	inbounds, err := repo.GetPendingInbounds(ctx)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if inbounds == nil {
		inbounds = []repository.WarehouseInbound{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"inbounds": inbounds,
	})
}

func handleStartReceivingInbound(w http.ResponseWriter, r *http.Request, ctx context.Context, id string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	staffID := getCallerUserID(ctx)
	if err := repo.StartReceiving(ctx, id, staffID); err != nil {
		writeInboundActionError(w, err, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"inboundId": id,
		"status":    "receiving",
	})
}

func handleScanInboundItem(w http.ResponseWriter, r *http.Request, ctx context.Context, id string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	staffID := getCallerUserID(ctx)
	count, err := repo.ScanItem(ctx, id, req, staffID)
	if err != nil {
		writeInboundActionError(w, err, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"inboundId":    id,
		"scannedItem":  req,
		"scannedCount": count,
	})
}

func handleCompleteInbound(w http.ResponseWriter, r *http.Request, ctx context.Context, id string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	staffID := getCallerUserID(ctx)
	if err := repo.CompleteInbound(ctx, id, staffID); err != nil {
		if errors.Is(err, repository.ErrInboundLifecycleClosed) || errors.Is(err, repository.ErrInboundProvenance) || errors.Is(err, repository.ErrInsufficientInventory) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeInboundActionError(w, err, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"inboundId": id,
		"status":    "received",
	})
}

func writeInboundActionError(w http.ResponseWriter, err error, status int) {
	if errors.Is(err, repository.ErrInboundOutsideStaffScope) {
		status = http.StatusNotFound
	}
	http.Error(w, err.Error(), status)
}

func handleGetQCQueue(w http.ResponseWriter, r *http.Request, ctx context.Context, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	queue, err := repo.GetQCQueue(ctx)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if queue == nil {
		queue = []repository.QCQueueItem{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"queue":   queue,
	})
}

func handlePassQCItem(w http.ResponseWriter, r *http.Request, ctx context.Context, productID string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		InboundID     string   `json:"inboundId"`
		Quantity      int      `json:"quantity"`
		ReceiptLineID *string  `json:"receiptLineId"`
		UnitCodes     []string `json:"unit_codes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	// The cumulative product route cannot attribute scans to a receipt line.
	if req.UnitCodes != nil {
		writeSPNCXError(w, repository.ErrUnitCodesRequireReceiptLine)
		return
	}
	var receiptLine []string
	if req.ReceiptLineID != nil {
		receiptLine = append(receiptLine, *req.ReceiptLineID)
	}
	staffID := getCallerUserID(ctx)
	if err := repo.RecordQCPass(ctx, productID, req.InboundID, staffID, req.Quantity, receiptLine...); err != nil {
		if errors.Is(err, repository.ErrInboundLifecycleClosed) || errors.Is(err, repository.ErrInboundProvenance) || errors.Is(err, repository.ErrInsufficientInventory) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"productId": productID,
		"status":    "passed",
	})
}

func handleFailQCItem(w http.ResponseWriter, r *http.Request, ctx context.Context, productID string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		DefectReason string `json:"defectReason"`
		Quantity     int    `json:"quantity"`
		InboundID    string `json:"inboundId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	staffID := getCallerUserID(ctx)
	if err := repo.RecordQCFail(ctx, productID, req.InboundID, staffID, req.DefectReason, req.Quantity); err != nil {
		if errors.Is(err, repository.ErrInboundLifecycleClosed) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"productId":    productID,
		"status":       "failed",
		"defectReason": req.DefectReason,
	})
}

func handleCompleteQC(w http.ResponseWriter, r *http.Request, ctx context.Context, productID string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		InboundID     string   `json:"inboundId"`
		Passed        int      `json:"passedQuantity"`
		Failed        int      `json:"failedQuantity"`
		Shortage      int      `json:"shortageQuantity"`
		Reason        string   `json:"defectReason"`
		ReceiptLineID *string  `json:"receiptLineId"`
		UnitCodes     []string `json:"unit_codes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	var receiptLine []string
	if req.ReceiptLineID != nil {
		receiptLine = append(receiptLine, *req.ReceiptLineID)
	}
	if err := repo.CompleteQCWithUnits(ctx, productID, req.InboundID, getCallerUserID(ctx), req.Passed, req.Failed, req.Shortage, req.Reason, req.UnitCodes, receiptLine...); err != nil {
		if errors.Is(err, repository.ErrUnitMismatch) {
			writeSPNCXError(w, err)
			return
		}
		if errors.Is(err, repository.ErrInboundLifecycleClosed) || errors.Is(err, repository.ErrInboundProvenance) || errors.Is(err, repository.ErrInsufficientInventory) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "productId": productID, "inboundId": req.InboundID, "status": "completed", "unit_codes": append([]string{}, req.UnitCodes...)})
}

func handleCreateQCDiscrepancy(w http.ResponseWriter, r *http.Request, ctx context.Context, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	dispID, err := repo.CreateQCDiscrepancy(ctx, req)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"discrepancyId": dispID,
	})
}

func handleGetPutawayQueue(w http.ResponseWriter, r *http.Request, ctx context.Context, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	queue, err := repo.GetPutawayQueue(ctx)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if queue == nil {
		queue = []repository.PutawayQueueItem{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"queue":   queue,
	})
}

func handleGetHistoricalInventoryReport(w http.ResponseWriter, r *http.Request, ctx context.Context, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	// PERF-02 Step 9: ?productId=<id> returns the single FULL row for
	// that product (bounded evidence lists) — the detail the list view
	// no longer ships. Same route, no new pattern: the route-table
	// conflict test parses literal patterns only.
	if productID := r.URL.Query().Get("productId"); productID != "" {
		row, err := repo.GetHistoricalInventoryReportRow(ctx, productID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, `{"error":"product not found"}`, http.StatusNotFound)
				return
			}
			writeInternalError(w, err)
			return
		}
		json.NewEncoder(w).Encode(row)
		return
	}
	opts, err := parseListPage(r)
	if err != nil {
		http.Error(w, `{"error":"Invalid limit"}`, http.StatusBadRequest)
		return
	}
	// PERF-02: OFFSET pagination is removed — ?page= is rejected so a
	// stale client cannot silently page over a shifting window. Use
	// ?cursor= from the previous response's nextCursor.
	if r.URL.Query().Get("page") != "" {
		http.Error(w, `{"error":"page pagination is removed; use cursor"}`, http.StatusBadRequest)
		return
	}
	// PERF-02 Step 8d: ?view=summary (default) returns counts/flags only —
	// no nested evidence JSONB is built; ?view=full returns the capped
	// evidence lists. Per-product evidence always comes from the
	// single-row endpoint.
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "summary"
	}
	if view != "summary" && view != "full" {
		http.Error(w, `{"error":"view must be summary or full"}`, http.StatusBadRequest)
		return
	}
	var report []repository.HistoricalInventoryReportRow
	var total int
	var nextCursor string
	if view == "summary" {
		report, total, nextCursor, err = repo.GetHistoricalInventoryReportSummaryCursorPage(ctx, opts.Limit, opts.Cursor)
	} else {
		report, total, nextCursor, err = repo.GetHistoricalInventoryReportCursorPage(ctx, opts.Limit, opts.Cursor)
	}
	if err != nil {
		if isInvalidCursorError(err) {
			http.Error(w, `{"error":"cursor is invalid"}`, http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	if report == nil {
		report = []repository.HistoricalInventoryReportRow{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"readOnly": true, "rows": report, "total": total, "limit": opts.Limit, "nextCursor": nextCursor, "hasMore": nextCursor != "", "view": view})
}

func handleReviewHistoricalInventory(w http.ResponseWriter, r *http.Request, ctx context.Context, returnRepo *repository.SupplierRepository, adminRepo *repository.AdminRepository) {
	w.Header().Set("Content-Type", "application/json")
	claims, _ := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if claims == nil || claims.UserID == "" {
		http.Error(w, "authenticated reviewer required", http.StatusUnauthorized)
		return
	}
	if returnRepo == nil {
		writeInternalError(w, errors.New("historical review repository not initialized"))
		return
	}
	var req struct {
		Status   string   `json:"status"`
		Reason   string   `json:"reason"`
		Evidence []string `json:"evidence"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	productID := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/inventory/reconciliation-reviews/")
	productID = strings.Trim(productID, "/")
	review, err := returnRepo.ReviewHistoricalInventory(ctx, productID, req.Status, req.Reason, claims.UserID, req.Evidence)
	if errors.Is(err, repository.ErrHistoricalEvidenceTooLarge) {
		http.Error(w, "historical review evidence exceeds the bounded selection (at most 50 entries of at most 256 bytes each)", http.StatusBadRequest)
		return
	}
	if errors.Is(err, repository.ErrInvalidHistoricalReview) {
		http.Error(w, "invalid historical review", http.StatusConflict)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "historical review product not found", http.StatusConflict)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(review)
}

func handleAssignPutaway(w http.ResponseWriter, r *http.Request, ctx context.Context, productID string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		BinLocation string `json:"binLocation"`
		Quantity    int    `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	staffID := getCallerUserID(ctx)
	taskID, err := repo.AssignPutaway(ctx, productID, req.BinLocation, req.Quantity, staffID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"taskId":      taskID,
		"productId":   productID,
		"binLocation": req.BinLocation,
		"quantity":    req.Quantity,
	})
}

func handleConfirmPutaway(w http.ResponseWriter, r *http.Request, ctx context.Context, taskID string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	if err := repo.ConfirmPutaway(ctx, taskID, getCallerUserID(ctx)); err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"taskId":  taskID,
		"status":  "completed",
	})
}

func handleAssignPutawayTask(w http.ResponseWriter, r *http.Request, ctx context.Context, taskID string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		BinLocation string `json:"binLocation"`
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
	if err := repo.AssignPutawayTask(ctx, taskID, req.BinLocation, claims.UserID); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "taskId": taskID, "binLocation": req.BinLocation})
}

func handleReviewSupplierReturn(w http.ResponseWriter, r *http.Request, ctx context.Context, returnID string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	claims, _ := ctx.Value(userClaimsKey).(*auth.UserClaims)
	actor := ""
	if claims != nil {
		actor = claims.UserID
	}
	if err := repo.ReviewReturn(ctx, returnID, req.Status, req.Reason, actor); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "returnId": returnID, "status": req.Status})
}

func handleGetSupplierReturns(w http.ResponseWriter, r *http.Request, ctx context.Context, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	items, err := repo.GetReturnRequests(ctx, "all")
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if items == nil {
		items = []repository.SupplierReturnRequest{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "returns": items})
}

func handleGetSupplierInventory(w http.ResponseWriter, r *http.Request, ctx context.Context, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, repo)
	items, err := repo.GetSupplierInventory(ctx, supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if items == nil {
		items = []repository.SupplierInventoryLocation{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "inventory": items})
}

func handleGetReturnParcels(w http.ResponseWriter, r *http.Request, ctx context.Context, returnID string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	items, err := repo.GetReturnParcels(ctx, returnID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if items == nil {
		items = []repository.ReturnParcel{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "parcels": items})
}

func handleGetReturnFinancialEvents(w http.ResponseWriter, r *http.Request, ctx context.Context, returnID string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	items, err := repo.GetReturnFinancialEvents(ctx, returnID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if items == nil {
		items = []repository.ReturnFinancialEvent{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "events": items})
}

func handleGetCarrierOptions(w http.ResponseWriter, r *http.Request, ctx context.Context, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	items, err := repo.GetCarrierOptions(ctx)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if items == nil {
		items = []repository.CarrierOption{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "carriers": items})
}

func handleCreateReturnParcel(w http.ResponseWriter, r *http.Request, ctx context.Context, returnID string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	var req repository.ReturnParcelInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	claims, _ := ctx.Value(userClaimsKey).(*auth.UserClaims)
	actor := ""
	if claims != nil {
		actor = claims.UserID
	}
	// Warehouse staff must use the supplier recorded on the return request. The
	// repository enforces that relationship and never trusts a supplier ID body field.
	var err error
	if actor == "" {
		err = fmt.Errorf("authenticated actor required")
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	// The warehouse route resolves the owning supplier inside the repository by
	// accepting the scoped request through a small lookup-free system value.
	parcel, err := repo.CreateReturnParcel(ctx, returnID, actor, req, r.Header.Get("Idempotency-Key"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(parcel)
}

func handleConfirmReturnCharge(w http.ResponseWriter, r *http.Request, ctx context.Context, returnID, parcelID string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	claims, _ := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	key, ok := requireFinanceRequestKey(w, r)
	if !ok {
		return
	}
	if err := repo.ConfirmReturnParcelCharge(ctx, returnID, parcelID, claims.UserID, key); err != nil {
		if writeFinanceBusy(w, err) {
			return
		}
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "returnId": returnID, "parcelId": parcelID, "status": "confirmed"})
}

func returnActor(ctx context.Context) (string, error) {
	claims, _ := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if claims == nil || claims.UserID == "" {
		return "", fmt.Errorf("authenticated actor required")
	}
	return claims.UserID, nil
}

func handleHandoverReturn(w http.ResponseWriter, r *http.Request, ctx context.Context, returnID string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	actor, err := returnActor(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if err := repo.HandoverReturn(ctx, returnID, actor, r.Header.Get("Idempotency-Key")); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "returnId": returnID, "status": "in_transit"})
}

func handleReceiveReturn(w http.ResponseWriter, r *http.Request, ctx context.Context, returnID string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	actor, err := returnActor(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if err := repo.ReceiveReturn(ctx, returnID, actor, r.Header.Get("Idempotency-Key")); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "returnId": returnID, "status": "received_pending_qc"})
}

func handleInspectReturn(w http.ResponseWriter, r *http.Request, ctx context.Context, returnID string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Accepted int    `json:"acceptedQuantity"`
		Failed   int    `json:"failedQuantity"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	actor, err := returnActor(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	status := "completed"
	if req.Failed > 0 {
		status = "qc_failed"
	}
	if err := repo.InspectReturn(ctx, returnID, actor, r.Header.Get("Idempotency-Key"), req.Accepted, req.Failed, req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "returnId": returnID, "status": status})
}

func handleRecordProductMeasurement(w http.ResponseWriter, r *http.Request, ctx context.Context, productID string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		VariantID   string `json:"variantId"`
		LengthMM    int    `json:"lengthMm"`
		WidthMM     int    `json:"widthMm"`
		HeightMM    int    `json:"heightMm"`
		WeightGrams int    `json:"weightGrams"`
		Reason      string `json:"reason"`
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
	physical, err := repo.RecordSKUPhysicalMeasurement(ctx, productID, req.VariantID, claims.UserID, req.LengthMM, req.WidthMM, req.HeightMM, req.WeightGrams, req.Reason)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "productId": productID, "variantId": req.VariantID, "measurementStatus": "verified", "physical": physical})
}

func handleCarrierTariffQuote(w http.ResponseWriter, r *http.Request, ctx context.Context, db *sql.DB) {
	w.Header().Set("Content-Type", "application/json")
	var req repository.CarrierTariffInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	quote, err := repository.CalculateCarrierTariff(ctx, db, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(quote)
}

func handleGetOrdersReadyToPick(w http.ResponseWriter, r *http.Request, ctx context.Context, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	orders, err := repo.GetOrdersReadyToPick(ctx)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if orders == nil {
		orders = []repository.ReadyToPickOrder{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"orders":  orders,
	})
}

func writeWarehouseOrderMutationError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrWarehouseOrderNotFound) {
		http.Error(w, `{"error":"Order not found"}`, http.StatusNotFound)
		return
	}
	writeInternalError(w, err)
}

func handleStartPickingOrder(w http.ResponseWriter, r *http.Request, ctx context.Context, orderID string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	staffID := getCallerUserID(ctx)
	if err := repo.StartPickingOrder(ctx, orderID, staffID); err != nil {
		writeWarehouseOrderMutationError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"orderId": orderID,
		"status":  "picking",
	})
}

func handlePickOrderItem(w http.ResponseWriter, r *http.Request, ctx context.Context, orderID string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	if err := repo.PickItem(ctx, orderID, req); err != nil {
		writeWarehouseOrderMutationError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"orderId":    orderID,
		"pickedItem": req,
		"status":     "picking",
	})
}

func handleCompletePickingOrder(w http.ResponseWriter, r *http.Request, ctx context.Context, orderID string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	if err := repo.CompletePickingOrder(ctx, orderID); err != nil {
		writeWarehouseOrderMutationError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"orderId": orderID,
		"status":  "picked",
	})
}

func handlePackOrder(w http.ResponseWriter, r *http.Request, ctx context.Context, orderID string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	// PackOrder takes the scale weight and the box the staff selected.
	// Body keys follow the warehouse packing tab state (weightGrams,
	// boxSize); an empty/absent body packs with 0/"" exactly as the
	// repo documents (0 = unknown weight, not persisted).
	var req struct {
		WeightGrams int    `json:"weightGrams"`
		BoxSize     string `json:"boxSize"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	trackingNumber, label, err := repo.PackOrder(ctx, orderID, req.WeightGrams, req.BoxSize)
	if err != nil {
		writeWarehouseOrderMutationError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"orderId":        orderID,
		"status":         "packed",
		"trackingNumber": trackingNumber,
		"shippingLabel":  label,
	})
}

func handleShipOrder(w http.ResponseWriter, r *http.Request, ctx context.Context, orderID string, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		Carrier        string `json:"carrier"`
		TrackingNumber string `json:"trackingNumber"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Carrier == "" {
		req.Carrier = "پست پیشتاز"
	}
	actor := callerUserID(r)
	if actor == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if err := repo.ShipOrderWithActor(ctx, orderID, req.Carrier, req.TrackingNumber, actor); err != nil {
		writeWarehouseOrderMutationError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"orderId":        orderID,
		"status":         "shipped",
		"carrier":        req.Carrier,
		"trackingNumber": req.TrackingNumber,
	})
}

func handleGetWarehouseShipment(w http.ResponseWriter, r *http.Request, ctx context.Context, code string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	shipment, err := repo.GetWarehouseShipmentByCode(ctx, code)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"Shipment not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"shipment": shipment})
}

func handleReceiveWarehouseShipment(w http.ResponseWriter, r *http.Request, ctx context.Context, code string, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		Items []repository.WarehouseReceiveItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	status, items, err := repo.ReceiveWarehouseShipmentWithUnits(ctx, code, req.Items)
	if errors.Is(err, repository.ErrUnitMismatch) || errors.Is(err, repository.ErrUnitCapExceeded) ||
		errors.Is(err, repository.ErrCartonNotReceivable) || errors.Is(err, repository.ErrDuplicateCartonReceiptItem) {
		writeSPNCXError(w, err)
		return
	}
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"Shipment not found"}`, http.StatusNotFound)
		return
	}
	if err != nil && (errors.Is(err, repository.ErrInboundProvenance) || errors.Is(err, repository.ErrInsufficientInventory) || err.Error() == "shipment already received") {
		http.Error(w, `{"error":"Shipment already received"}`, http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "shipment_code": code, "status": status, "items": items})
}

func dispatchInboundLineReceipt(w http.ResponseWriter, r *http.Request, repo *repository.SupplierRepository) {
	lineID, action, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/inbound-lines/"), "/")
	if !ok || lineID == "" || action != "receive" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	handleReceiveInboundLine(w, r, lineID, repo)
}

func handleReceiveInboundLine(w http.ResponseWriter, r *http.Request, lineID string, repo *repository.SupplierRepository) {
	if repo == nil {
		writeInternalError(w, errors.New("database not initialized"))
		return
	}
	var req struct {
		Quantity  int      `json:"received_quantity"`
		UnitCodes []string `json:"unit_codes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid payload"})
		return
	}
	result, err := repo.ReceiveInboundLine(r.Context(), lineID, req.Quantity, req.UnitCodes, getCallerUserID(r.Context()))
	if err != nil {
		writeSPNCXError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, result)
}

func dispatchWarehouseQCPass(w http.ResponseWriter, r *http.Request, segment string, repo *repository.WarehouseRepository) {
	ctx := r.Context()
	if repo == nil {
		handlePassQCItem(w, r, ctx, segment, repo)
		return
	}
	shipment, product, err := repo.QCReceiptLine(ctx, segment)
	switch {
	case err == sql.ErrNoRows:
		handlePassQCItem(w, r, ctx, segment, repo)
	case err != nil:
		writeInternalError(w, err)
	default:
		handlePassQCReceiptLine(w, r, shipment, product, segment, repo)
	}
}

func handlePassQCReceiptLine(w http.ResponseWriter, r *http.Request, shipment, product, lineID string, repo *repository.WarehouseRepository) {
	var req struct {
		Quantity  int      `json:"quantity"`
		UnitCodes []string `json:"unit_codes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid payload"})
		return
	}
	result, err := repo.RecordResolvedQCLinePassDelta(r.Context(), shipment, product, lineID, req.Quantity, req.UnitCodes, getCallerUserID(r.Context()))
	if err != nil {
		writeSPNCXError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, result)
}

func dispatchWarehouseReturnReceipt(w http.ResponseWriter, r *http.Request, returnID string, suppliers *repository.SupplierRepository, orders *repository.OrderRepository) {
	var req struct {
		Items []repository.CustomerReturnUnitItem `json:"items"`
	}
	body, err := io.ReadAll(r.Body)
	// Only a non-empty items array with an order-item ID on every entry
	// selects customer receipt. The supplier receipt ignores the body.
	customer := err == nil && json.Unmarshal(body, &req) == nil && len(req.Items) > 0
	for _, item := range req.Items {
		customer = customer && item.OrderItemID != ""
	}
	if customer {
		handleReceiveCustomerReturnUnits(w, r, returnID, req.Items, orders)
		return
	}
	if requireUnscopedWarehouseWork(w, r) {
		handleReceiveReturn(w, r, r.Context(), returnID, suppliers)
	}
}

func handleReceiveCustomerReturnUnits(w http.ResponseWriter, r *http.Request, returnID string, items []repository.CustomerReturnUnitItem, repo *repository.OrderRepository) {
	actor, err := returnActor(r.Context())
	if err != nil {
		t01WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	if repo == nil {
		writeInternalError(w, errors.New("database not initialized"))
		return
	}
	result, err := repo.ReceiveCustomerReturnUnits(r.Context(), returnID, items, actor)
	switch {
	case err == nil:
		t01WriteJSON(w, http.StatusOK, map[string]any{"items": result})
	case errors.Is(err, repository.ErrUnitMismatch), errors.Is(err, repository.ErrIncompleteReturnReceipt):
		writeSPNCXError(w, err)
	case errors.Is(err, sql.ErrNoRows):
		t01WriteJSON(w, http.StatusNotFound, map[string]string{"error": "Return or item not found, or return not approved"})
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
