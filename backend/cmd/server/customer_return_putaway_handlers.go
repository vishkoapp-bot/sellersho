package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func handleCustomerReturnBinEntry(w http.ResponseWriter, r *http.Request, repo *repository.WarehouseRepository) {
	w.Header().Set("Content-Type", "application/json")
	actor := callerUserID(r)
	if actor == "" {
		http.Error(w, `{"error":"ورود به حساب کارکنان الزامی است"}`, 401)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/customer-returns/"), "/")
	if len(parts) != 2 && len(parts) != 4 {
		http.NotFound(w, r)
		return
	}
	if uuid.Validate(parts[0]) != nil {
		http.Error(w, `{"error":"شناسه مرجوعی معتبر نیست"}`, 400)
		return
	}
	var result any
	var err error
	if len(parts) == 2 && parts[1] == "putaway-sources" {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		result, err = repo.ListCustomerReturnBinSources(r.Context(), parts[0])
	} else if len(parts) == 4 && parts[1] == "items" && parts[3] == "putaway" {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		var req struct {
			BinCode  string `json:"binCode"`
			Quantity *int   `json:"quantity"`
			Reason   string `json:"reason"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		decodeErr := decoder.Decode(&req)
		if decodeErr != nil || decoder.Decode(&struct{}{}) != io.EOF || uuid.Validate(parts[2]) != nil || req.Quantity == nil || *req.Quantity <= 0 || int64(*req.Quantity) > 2147483647 || strings.TrimSpace(req.BinCode) == "" || len(req.BinCode) > 200 || strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 2000 || strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" || len(r.Header.Get("Idempotency-Key")) > 200 {
			http.Error(w, `{"error":"قفسه، تعداد صحیح مثبت، دلیل و کلید درخواست معتبر الزامی است"}`, 400)
			return
		}
		result, err = repo.PlaceCustomerReturnItemInBin(r.Context(), parts[0], parts[2], req.BinCode, *req.Quantity, req.Reason, actor, r.Header.Get("Idempotency-Key"))
	} else {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		message := "ثبت یا دریافت جانمایی مرجوعی انجام نشد"
		switch {
		case errors.Is(err, repository.ErrReturnBinInput):
			status = 400
			message = err.Error()
		case errors.Is(err, sql.ErrNoRows):
			status = 404
			message = "قلم مرجوعی پیدا نشد"
		case errors.Is(err, repository.ErrReturnBinConflict):
			status = 409
			message = err.Error()
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": message})
		return
	}
	json.NewEncoder(w).Encode(result)
}
