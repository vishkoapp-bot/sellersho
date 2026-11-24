package main

import (
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

func handleAdminOrdersSubtree(w http.ResponseWriter, r *http.Request, db *sql.DB, orderRepo *repository.OrderRepository) {
	subpath, hasPrefix := strings.CutPrefix(r.URL.Path, "/api/v1/admin/orders/")
	orderID, action, hasAction := strings.Cut(subpath, "/")
	if !hasPrefix || !hasAction || orderID == "" || action != "retry-capture" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	r.SetPathValue("order_id", orderID)
	HandleRetryAdminOrderCapture(w, r, db, orderRepo)
}

func HandleRetryAdminOrderCapture(w http.ResponseWriter, r *http.Request, _ *sql.DB, orderRepo *repository.OrderRepository) {
	if !t01MethodOnly(w, r, http.MethodPost) {
		return
	}
	result, err := orderRepo.RetryFailedCapture(r.Context(), r.PathValue("order_id"))
	var captureFailure *repository.RetryCaptureFailedError
	switch {
	case err == nil:
		t01WriteJSON(w, http.StatusOK, result)
	case errors.Is(err, repository.ErrRetryOrderNotFound):
		t01WriteJSON(w, http.StatusNotFound, map[string]string{"error": "order_not_found"})
	case errors.Is(err, repository.ErrRetryCaptureAlreadyCompleted):
		t01WriteJSON(w, http.StatusConflict, map[string]string{"error": "capture_already_completed"})
	case errors.Is(err, repository.ErrRetryPaymentNotRetryable):
		t01WriteJSON(w, http.StatusConflict, map[string]string{"error": "payment_not_retryable"})
	case errors.As(err, &captureFailure):
		t01WriteJSON(w, http.StatusConflict, map[string]string{"error": captureFailure.Failure})
	default:
		writeInternalError(w, err)
	}
}

func handleAdminSpncxSubtree(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	subpath, ok := strings.CutPrefix(r.URL.Path, "/api/v1/admin/spncx/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case subpath == "backfill" && r.Method == http.MethodPost:
		handleAdminBackfill(w, r, db)
	case subpath == "backfill/status" && r.Method == http.MethodGet:
		result, err := repository.ReadBackfillStatus(r.Context(), db)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, result)
	case subpath == "backfill/rollback" && r.Method == http.MethodPost:
		handleAdminBackfillRollback(w, r, db)
	case subpath == "backfill/excess-decision" && r.Method == http.MethodPost:
		handleAdminBackfillExcessDecision(w, r, db)
	default:
		http.NotFound(w, r)
	}
}

func backfillActor(r *http.Request) string {
	claims, _ := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if claims != nil {
		if claims.UserID != "" {
			return claims.UserID
		}
		if claims.FullName != "" {
			return claims.FullName
		}
	}
	return "unknown"
}

func writeBackfillError(w http.ResponseWriter, status int, code string) {
	t01WriteJSON(w, status, map[string]string{"error": code})
}

func decodeBackfillRequest(w http.ResponseWriter, r *http.Request, request any) bool {
	decoder := json.NewDecoder(r.Body)
	var extra json.RawMessage
	if err := decoder.Decode(request); err != nil || !errors.Is(decoder.Decode(&extra), io.EOF) {
		writeBackfillError(w, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

type adminBackfillRequest struct {
	DryRun          *bool             `json:"dry_run"`
	ProductIDs      []string          `json:"product_ids"`
	BatchSize       int               `json:"batch_size"`
	ConfirmPlanDiff bool              `json:"confirm_plan_diff"`
	Reinclude       map[string]string `json:"reinclude"`
}

func handleAdminBackfill(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req adminBackfillRequest
	if !decodeBackfillRequest(w, r, &req) {
		return
	}
	if req.DryRun == nil {
		writeBackfillError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	ctx := r.Context()
	expectations := repository.ProdBackfillExpectations()
	var approval *repository.BackfillApproval
	if !*req.DryRun {
		var err error
		approval, err = repository.LoadBackfillApproval(ctx, db, req.ProductIDs)
		switch {
		case errors.Is(err, repository.ErrBackfillProductNotFound):
			writeBackfillError(w, http.StatusNotFound, "product_not_found")
			return
		case errors.Is(err, repository.ErrBackfillDryRunRequired):
			writeBackfillError(w, http.StatusConflict, "dry_run_required")
			return
		case err != nil:
			writeInternalError(w, err)
			return
		}
	}
	plan, err := repository.ComputeBackfillPlan(ctx, db, req.ProductIDs, expectations)
	if errors.Is(err, repository.ErrBackfillProductNotFound) {
		writeBackfillError(w, http.StatusNotFound, "product_not_found")
		return
	}
	if err != nil {
		writeInternalError(w, fmt.Errorf("compute live backfill plan: %w", err))
		return
	}
	if *req.DryRun {
		if err := repository.PersistBackfillDryRunPlan(ctx, db, plan); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, plan)
		return
	}

	diff, err := approval.Compare(plan)
	if errors.Is(err, repository.ErrBackfillPlanHashMismatch) {
		writeBackfillError(w, http.StatusConflict, "plan_hash_mismatch")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if diff != nil && len(diff.ChangedProducts) != 0 && !req.ConfirmPlanDiff {
		t01WriteJSON(w, http.StatusConflict, map[string]any{"error": "plan_diff_requires_reapproval", "diff": diff})
		return
	}
	actor := backfillActor(r)
	err = repository.AuditBackfillReinclude(ctx, db, plan, req.Reinclude, actor)
	switch {
	case errors.Is(err, repository.ErrBackfillProductNotExcluded):
		writeBackfillError(w, http.StatusUnprocessableEntity, "product_not_excluded")
		return
	case errors.Is(err, repository.ErrBackfillReincludeReasonRequired):
		writeBackfillError(w, http.StatusUnprocessableEntity, "reinclude_reason_required")
		return
	case errors.Is(err, repository.ErrBackfillReincludeOutsideScope):
		writeBackfillError(w, http.StatusBadRequest, "invalid_request")
		return
	case err != nil:
		writeInternalError(w, err)
		return
	}
	if !plan.AllPassed() {
		writeBackfillPreflightFailure(w, plan)
		return
	}
	result, err := repository.RunBackfill(ctx, db, repository.BackfillRunOptions{
		ProductIDs: req.ProductIDs, BatchSize: req.BatchSize, Actor: actor,
		PlanHash: plan.PlanHash, Expectations: expectations, Reinclude: req.Reinclude,
	})
	switch {
	case errors.Is(err, repository.ErrBackfillAlreadyRunning):
		writeBackfillError(w, http.StatusLocked, "backfill_already_running")
	case errors.Is(err, repository.ErrBackfillPreflightFailed):
		writeBackfillPreflightFailure(w, result.Plan)
	case errors.Is(err, repository.ErrBackfillProductNotFound):
		writeBackfillError(w, http.StatusNotFound, "product_not_found")
	case err != nil:
		writeInternalError(w, fmt.Errorf("run SPNCX backfill: %w", err))
	default:
		writeBackfillRun(w, plan.PlanHash, result)
	}
}

func writeBackfillPreflightFailure(w http.ResponseWriter, plan *repository.BackfillPlan) {
	t01WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"error": "backfill_preflight_failed", "global_assertions": plan.GlobalAssertions,
	})
}

type adminBackfillRunProduct struct {
	ProductID       string   `json:"product_id"`
	Status          string   `json:"status"`
	UnitsMinted     int      `json:"units_minted"`
	UnitsTarget     int      `json:"units_target"`
	FlippedVariants []string `json:"flipped_variants"`
	Excess          int      `json:"excess"`
}

func writeBackfillRun(w http.ResponseWriter, hash string, result *repository.BackfillRunResult) {
	products := make([]adminBackfillRunProduct, 0, len(result.Products))
	for _, p := range result.Products {
		products = append(products, adminBackfillRunProduct{
			ProductID: p.ProductID, Status: p.Status, UnitsMinted: p.UnitsMinted, UnitsTarget: p.UnitsTarget,
			FlippedVariants: append([]string{}, p.FlippedVariants...), Excess: p.Excess,
		})
	}
	t01WriteJSON(w, http.StatusOK, map[string]any{
		"dry_run": false, "plan_hash": hash, "completed": result.Completed, "skipped_completed": result.SkippedCompleted,
		"failed": result.Failed, "excluded_skipped": result.ExcludedSkipped, "total_minted": result.TotalMinted, "products": products,
	})
}

type adminBackfillRollbackResult struct {
	ProductID             string   `json:"product_id"`
	RolledBack            bool     `json:"rolled_back"`
	PrintedCodesToDestroy []string `json:"printed_codes_to_destroy"`
	RestoredSnapshot      bool     `json:"restored_snapshot"`
}

func backfillRollbackResponse(result repository.BackfillRollbackResult) adminBackfillRollbackResult {
	return adminBackfillRollbackResult{
		ProductID: result.ProductID, RolledBack: result.RolledBack, RestoredSnapshot: result.RestoredSnapshot,
		PrintedCodesToDestroy: append([]string{}, result.PrintedCodesToDestroy...),
	}
}

func handleAdminBackfillRollback(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ProductID string `json:"product_id"`
		All       bool   `json:"all"`
		Confirm   bool   `json:"confirm_printed_destruction"`
	}
	if !decodeBackfillRequest(w, r, &req) {
		return
	}
	if (req.ProductID != "") == req.All {
		writeBackfillError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if req.All {
		handleAdminBackfillRollbackAll(w, r, db, req.Confirm)
		return
	}
	ctx := r.Context()
	exists, err := repository.BackfillProductExists(ctx, db, req.ProductID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !exists {
		writeBackfillError(w, http.StatusNotFound, "product_not_found")
		return
	}
	result, err := repository.RollbackBackfill(ctx, db, req.ProductID, backfillActor(r), req.Confirm)
	switch {
	case err == nil:
		t01WriteJSON(w, http.StatusOK, backfillRollbackResponse(*result))
	case errors.Is(err, repository.ErrBackfillNotCompleted):
		writeBackfillError(w, http.StatusConflict, "rollback_not_completed")
	case errors.Is(err, repository.ErrBackfillRollbackNeedsConfirmation):
		t01WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "rollback_needs_printed_confirmation", "printed_codes_to_destroy": result.PrintedCodesToDestroy,
		})
	case errors.Is(err, repository.ErrBackfillRollbackRefused):
		writeBackfillError(w, http.StatusConflict, "rollback_refused")
	default:
		writeInternalError(w, fmt.Errorf("roll back SPNCX product %s: %w", req.ProductID, err))
	}
}

func handleAdminBackfillRollbackAll(w http.ResponseWriter, r *http.Request, db *sql.DB, confirm bool) {
	results, err := repository.RollbackBackfillAll(r.Context(), db, nil, backfillActor(r), confirm)
	if err != nil && results == nil {
		writeInternalError(w, fmt.Errorf("list completed backfill products for rollback: %w", err))
		return
	}
	// B5 joins only failed attempts in the same order as the full result list.
	var failures []error
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		failures = joined.Unwrap()
	}
	type item struct {
		adminBackfillRollbackResult
		Error *string `json:"error"`
	}
	response := make([]item, 0, len(results))
	for _, result := range results {
		row := item{adminBackfillRollbackResult: backfillRollbackResponse(result)}
		if !result.RolledBack {
			code := "rollback_refused"
			if len(failures) != 0 {
				if errors.Is(failures[0], repository.ErrBackfillRollbackNeedsConfirmation) {
					code = "rollback_needs_printed_confirmation"
				}
				failures = failures[1:]
			}
			row.Error = &code
		}
		response = append(response, row)
	}
	t01WriteJSON(w, http.StatusOK, map[string]any{"all": true, "results": response})
}

func handleAdminBackfillExcessDecision(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ProductID json.RawMessage `json:"product_id"`
		Decision  json.RawMessage `json:"excess_decision"`
	}
	if !decodeBackfillRequest(w, r, &req) {
		return
	}
	var decision string
	if err := json.Unmarshal(req.Decision, &decision); err != nil || !repository.BackfillExcessDecisionValid(decision) {
		writeBackfillError(w, http.StatusUnprocessableEntity, "invalid_excess_decision")
		return
	}
	var productID string
	if err := json.Unmarshal(req.ProductID, &productID); err != nil || productID == "" {
		writeBackfillError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	result, err := repository.RecordBackfillExcessDecision(r.Context(), db, productID, decision, backfillActor(r))
	switch {
	case errors.Is(err, repository.ErrBackfillExcessProductNotFound):
		writeBackfillError(w, http.StatusNotFound, "product_not_found")
	case errors.Is(err, repository.ErrBackfillNoExcessToDecide):
		writeBackfillError(w, http.StatusConflict, "no_excess_to_decide")
	case errors.Is(err, repository.ErrBackfillInvalidExcessDecision):
		writeBackfillError(w, http.StatusUnprocessableEntity, "invalid_excess_decision")
	case err != nil:
		writeInternalError(w, err)
	default:
		t01WriteJSON(w, http.StatusOK, result)
	}
}

func handleAdminReconciliation(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	report, err := repository.RunReconciliationReport(r.Context(), db)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, report)
}
