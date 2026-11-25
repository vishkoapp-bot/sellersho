package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/drup/platform/internal/repository"
)

func (h *SupplierHandlers) HandleSupplierCartonDetails(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	id, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/cartons/"), "/")
	switch {
	case id == "" || (action != "" && action != "print-units"):
		http.NotFound(w, r)
	case action == "" && r.Method == http.MethodGet:
		result, err := h.supplierRepo.GetSupplierCartonDetails(r.Context(), supplierID, id)
		if errors.Is(err, sql.ErrNoRows) {
			t01WriteJSON(w, http.StatusNotFound, map[string]string{"error": "این محموله پیدا نشد."})
			return
		}
		writeSPNCXResult(w, result, err)
	case action == "print-units" && r.Method == http.MethodPost:
		var body struct {
			printUnitsBody
			LineID string `json:"line_id"`
		}
		if !decodeSPNCXPrintBody(w, r, &body) {
			return
		}
		result, err := h.supplierRepo.MarkUnitsPrinted(r.Context(), repository.MarkUnitsPrintedRequest{
			SupplierID: supplierID, CartonShipmentID: id, ReceiptLineID: &body.LineID,
			ActorUserID: supplierLiveParityClaimsUserID(r), UnitCodes: body.UnitCodes,
			IdempotencyKey: body.IdempotencyKey, Reprint: body.Reprint,
		})
		writeSPNCXResult(w, result, err)
	default:
		writeMethodNotAllowed(w)
	}
}

func (h *SupplierHandlers) HandleSupplierStorageAging(w http.ResponseWriter, r *http.Request) {
	if !t01MethodOnly(w, r, http.MethodGet) {
		return
	}
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	page := 0
	if value := r.URL.Query().Get("page"); value != "" {
		var err error
		page, err = strconv.Atoi(value)
		if err != nil || page < 0 || page > 100000 {
			t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "شماره صفحه معتبر نیست."})
			return
		}
	}
	report, err := h.supplierRepo.GetSupplierStorageAging(r.Context(), supplierID, page)
	writeSPNCXResult(w, report, err)
}
