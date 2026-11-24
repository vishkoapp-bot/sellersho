package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

// HandleGetUnit serves GET /api/v1/units/{code} (§7.1). Staff-only; the
// route guard (RequireRoles admin / warehouse_staff / logistics_specialist /
// supplier) already rejected unauthenticated and wrong-role callers. A
// supplier sees only their own products' units: anything else — unknown code
// or another supplier's unit — is the same 404 {"error":"unit_not_found"},
// never a 403 and never a hint the unit exists.
func HandleGetUnit(w http.ResponseWriter, r *http.Request, db *sql.DB, supplierRepo *repository.SupplierRepository, code string) {
	if !t01MethodOnly(w, r, http.MethodGet) {
		return
	}
	supplierID, scoped := unitLookupScope(r, supplierRepo)
	if scoped && supplierID == "" {
		// Supplier caller with no supplier profile owns no products, so
		// every code is outside their scope: 404, same body as unknown.
		t01WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unit_not_found"})
		return
	}
	result, err := repository.GetUnitByCode(r.Context(), db, supplierID, code)
	writeSPNCXResult(w, result, err)
}

// HandleListUnits serves GET /api/v1/units (§7.1) with the spec's filters.
// Same staff guard and supplier scoping as HandleGetUnit.
func HandleListUnits(w http.ResponseWriter, r *http.Request, db *sql.DB, supplierRepo *repository.SupplierRepository) {
	if !t01MethodOnly(w, r, http.MethodGet) {
		return
	}
	supplierID, scoped := unitLookupScope(r, supplierRepo)
	if scoped && supplierID == "" {
		t01WriteJSON(w, http.StatusOK, repository.ListUnitsResponse{Units: []repository.UnitLabelRow{}})
		return
	}
	query := r.URL.Query()
	opts := repository.ListUnitsOptions{
		VariantID:   query.Get("variant_id"),
		Status:      query.Get("status"),
		Q:           query.Get("q"),
		OrderID:     query.Get("order_id"),
		BalanceID:   query.Get("balance_id"),
		OrderItemID: query.Get("order_item_id"),
		Cursor:      query.Get("cursor"),
	}
	var err error
	if limit := query.Get("limit"); limit != "" {
		opts.Limit, err = strconv.Atoi(limit)
		if err != nil || opts.Limit < 1 {
			writeSPNCXError(w, repository.ErrUnitInvalidLimit)
			return
		}
	}
	result, err := repository.ListUnits(r.Context(), db, supplierID, opts)
	writeSPNCXResult(w, result, err)
}

// unitLookupScope resolves the §7.1 supplier scope from the request claims.
// Admin and warehouse callers are unscoped (the spec names no warehouse-city
// restriction for lookup). A supplier caller — or any staff caller without a
// staff capability — is scoped to their own supplier id; the second return
// value reports that scoping. With no claims in context (direct handler
// calls in tests) the lookup is unscoped.
func unitLookupScope(r *http.Request, supplierRepo *repository.SupplierRepository) (supplierID string, scoped bool) {
	claims, _ := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if claims == nil {
		return "", false
	}
	if claims.HasCapability("admin") || claims.HasCapability("warehouse_staff") ||
		claims.HasCapability("logistics_specialist") {
		return "", false
	}
	return getSupplierIDFromContext(r, supplierRepo), true
}

func supplierUnitLabelRoute(sub string) (productID, action string) {
	productID, action, ok := strings.Cut(sub, "/labels/")
	if ok && productID != "" && !strings.Contains(productID, "/") && (action == "units" || action == "print") {
		return productID, action
	}
	return "", ""
}

func (h *SupplierHandlers) HandleGetProductUnitLabels(w http.ResponseWriter, r *http.Request, productID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	query := r.URL.Query()
	opts := repository.UnitLabelListOptions{
		VariantID: query.Get("variant_id"),
		Status:    query.Get("status"),
		Cursor:    query.Get("cursor"),
	}
	var err error
	if limit := query.Get("limit"); limit != "" {
		opts.Limit, err = strconv.Atoi(limit)
		if err != nil || opts.Limit < 1 {
			writeSPNCXError(w, repository.ErrPrintInvalidLimit)
			return
		}
	}
	if unprinted := query.Get("unprinted_only"); unprinted != "" {
		opts.UnprintedOnly, err = strconv.ParseBool(unprinted)
		if err != nil {
			t01WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_unprinted_only"})
			return
		}
	}
	result, err := h.supplierRepo.ListProductUnitLabels(r.Context(), supplierID, productID, opts)
	writeSPNCXResult(w, result, err)
}

type printUnitsBody struct {
	UnitCodes      []string `json:"unit_codes"`
	IdempotencyKey string   `json:"idempotency_key"`
	Reprint        bool     `json:"reprint"`
}

func (h *SupplierHandlers) HandlePrintProductUnitLabels(w http.ResponseWriter, r *http.Request, productID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var body struct {
		printUnitsBody
		VariantID     string `json:"variant_id"`
		Count         *int   `json:"count"`
		UnprintedOnly bool   `json:"unprinted_only"`
	}
	if !decodeSPNCXPrintBody(w, r, &body) {
		return
	}
	result, err := h.supplierRepo.MarkUnitsPrinted(r.Context(), repository.MarkUnitsPrintedRequest{
		SupplierID: supplierID, ProductID: productID, ActorUserID: supplierLiveParityClaimsUserID(r),
		UnitCodes: body.UnitCodes, VariantID: body.VariantID, Count: body.Count, UnprintedOnly: body.UnprintedOnly,
		IdempotencyKey: body.IdempotencyKey, Reprint: body.Reprint,
	})
	writeSPNCXResult(w, result, err)
}

func HandlePrintWarehouseUnits(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) {
		return
	}
	var body struct {
		printUnitsBody
		ReceiptLineID *string `json:"receipt_line_id"`
	}
	if !decodeSPNCXPrintBody(w, r, &body) {
		return
	}
	result, err := repository.MarkUnitsPrintedTx(r.Context(), db, repository.MarkUnitsPrintedRequest{
		ActorUserID: t01ClaimsUserID(r), UnitCodes: body.UnitCodes, ReceiptLineID: body.ReceiptLineID,
		IdempotencyKey: body.IdempotencyKey, Reprint: body.Reprint,
	})
	writeSPNCXResult(w, result, err)
}

func decodeSPNCXPrintBody(w http.ResponseWriter, r *http.Request, body any) bool {
	// A full variant (9,999 codes) fits within the body limit.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(body); err != nil || decoder.Decode(new(json.RawMessage)) != io.EOF {
		t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func writeSPNCXResult(w http.ResponseWriter, result any, err error) {
	if err != nil {
		writeSPNCXError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, result)
}

func writeSPNCXError(w http.ResponseWriter, err error) {
	var invalid *repository.PrintNotPrintableError
	var conflict *repository.PrintConflictError
	var mismatch *repository.UnitMismatchError
	switch {
	case errors.As(err, &mismatch):
		t01WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": mismatch.Error(), "errors": mismatch.Errors, "line_id": mismatch.LineID})
	case errors.Is(err, repository.ErrUnitCodesRequireReceiptLine):
		t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, repository.ErrIncompleteReturnReceipt), errors.Is(err, repository.ErrUnitCapExceeded):
		t01WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.Is(err, repository.ErrInboundProvenance), errors.Is(err, repository.ErrInboundLifecycleClosed):
		t01WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.As(err, &invalid):
		t01WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": invalid.Error(), "errors": invalid.Units})
	case errors.As(err, &conflict):
		body := map[string]any{"error": conflict.Error(), "job_id": conflict.JobID}
		if errors.Is(err, repository.ErrPrintAlreadyPrinted) {
			body["unit_codes"] = conflict.UnitCodes
		}
		t01WriteJSON(w, http.StatusConflict, body)
	case errors.Is(err, repository.ErrPrintProductNotFound), errors.Is(err, repository.ErrPrintVariantNotFound),
		errors.Is(err, repository.ErrPrintReceiptLineNotFound), errors.Is(err, repository.ErrUnitNotFound), errors.Is(err, repository.ErrInboundLineNotFound):
		t01WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, repository.ErrUnitInvalidLimit), errors.Is(err, repository.ErrUnitInvalidCursor),
		errors.Is(err, repository.ErrPrintInvalidStatus), errors.Is(err, repository.ErrPrintInvalidLimit),
		errors.Is(err, repository.ErrPrintInvalidCursor), errors.Is(err, repository.ErrPrintKeyRequired),
		errors.Is(err, repository.ErrPrintInvalidKey), errors.Is(err, repository.ErrPrintVariantRequired),
		errors.Is(err, repository.ErrPrintInvalidCount), errors.Is(err, repository.ErrPrintSelectionRequired),
		errors.Is(err, repository.ErrPrintDuplicateCodes), errors.Is(err, repository.ErrPrintVariantMismatch),
		errors.Is(err, repository.ErrPrintConflictingSelection):
		t01WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	default:
		writeInternalError(w, err)
	}
}
