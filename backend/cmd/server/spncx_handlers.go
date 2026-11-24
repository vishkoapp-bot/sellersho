package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

// All three route groups derive their scope from existing authenticated
// principals. The lookup body keeps serials out of URI/access logs.
func installSPNCXRoutes(mux *http.ServeMux, jwt *auth.JWTService, suppliers *repository.SupplierRepository, units *repository.SPNCXRepository) {
	type staffRole struct {
		prefix string
		wrap   func(http.HandlerFunc) http.HandlerFunc
		access func(*http.Request, *auth.UserClaims) repository.SPNCXAccess
	}
	for _, role := range []staffRole{
		{"/api/v1/admin/spncx/", RequireRoles(jwt, "admin"), func(_ *http.Request, claims *auth.UserClaims) repository.SPNCXAccess {
			return repository.SPNCXAdminAccess(claims.UserID)
		}},
		{"/api/v1/supplier/inventory/spncx/", RequireSupplierAccess(jwt, suppliers), func(r *http.Request, claims *auth.UserClaims) repository.SPNCXAccess {
			supplier, _ := r.Context().Value(supplierIDKey).(string)
			return repository.SPNCXSupplierAccess(claims.UserID, supplier)
		}},
		{"/api/v1/warehouse/inventory/spncx/", requireWarehouseCityRoles(jwt, "warehouse_staff"), func(r *http.Request, _ *auth.UserClaims) repository.SPNCXAccess {
			return repository.SPNCXWarehouseAccess(repository.WarehouseStaffID(r.Context()))
		}},
	} {
		register := func(suffix string, action func(http.ResponseWriter, *http.Request, repository.SPNCXAccess, *repository.SPNCXRepository)) {
			mux.HandleFunc(role.prefix+suffix, role.wrap(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.Header().Set("Allow", http.MethodPost)
					writeMethodNotAllowed(w)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "private, no-store")
				claims, _ := r.Context().Value(userClaimsKey).(*auth.UserClaims)
				if claims == nil {
					writeSPNCXProblem(w, repository.ErrSPNCXNotFound)
					return
				}
				action(w, r, role.access(r, claims), units)
			}))
		}
		register("lookup", handleSPNCXLookup)
		register("unprinted", handleSPNCXUnprinted)
		register("print-jobs", handleSPNCXPrint)
	}
}

func readSPNCXJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return false
	}
	return dec.Decode(new(any)) == io.EOF
}

func handleSPNCXLookup(w http.ResponseWriter, r *http.Request, access repository.SPNCXAccess, units *repository.SPNCXRepository) {
	var request struct {
		Code string `json:"code"`
	}
	if !readSPNCXJSON(w, r, &request) {
		writeSPNCXProblem(w, repository.ErrSPNCXInput)
		return
	}
	result, err := units.LookupUnit(r.Context(), access, request.Code)
	if err != nil {
		writeSPNCXProblem(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func handleSPNCXUnprinted(w http.ResponseWriter, r *http.Request, access repository.SPNCXAccess, units *repository.SPNCXRepository) {
	var request struct {
		SPNC  string `json:"spnc"`
		Limit int    `json:"limit"`
	}
	if !readSPNCXJSON(w, r, &request) {
		writeSPNCXProblem(w, repository.ErrSPNCXInput)
		return
	}
	result, err := units.UnprintedLabels(r.Context(), access, request.SPNC, request.Limit)
	if err != nil {
		writeSPNCXProblem(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func handleSPNCXPrint(w http.ResponseWriter, r *http.Request, access repository.SPNCXAccess, units *repository.SPNCXRepository) {
	var request repository.SPNCXPrintRequest
	if !readSPNCXJSON(w, r, &request) {
		writeSPNCXProblem(w, repository.ErrSPNCXInput)
		return
	}
	result, err := units.PrintLabels(r.Context(), access, r.Header.Get("Idempotency-Key"), request)
	if err != nil {
		writeSPNCXProblem(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func writeSPNCXProblem(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "spncx_unavailable"
	switch {
	case errors.Is(err, repository.ErrSPNCXInput):
		status, code = http.StatusBadRequest, "invalid_spncx_request"
	case errors.Is(err, repository.ErrSPNCXNotFound):
		status, code = http.StatusNotFound, "spncx_unit_not_found"
	case errors.Is(err, repository.ErrSPNCXPrintConflict):
		status, code = http.StatusConflict, "spncx_print_conflict"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
