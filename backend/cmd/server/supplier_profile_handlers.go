package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

// FIX 49 — live supplier profile tab (Tn): PUT /api/v1/supplier/profile.
// Partial update: only keys present in the body change. A changed warehouse
// city resets warehouse_city_confirmed (the server decides confirmation).
// Logo deletion is updateProfile({logo_url: ""}) from the client.
func (h *SupplierHandlers) HandleUpdateSupplierProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var userID string
	if claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil {
		userID = claims.UserID
	}
	var supID string
	if scopedID, ok := r.Context().Value(supplierIDKey).(string); ok && scopedID != "" {
		supID = scopedID
	} else if userID != "" {
		sup, err := h.supplierRepo.GetSupplierByUserID(r.Context(), userID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if sup == nil {
			http.Error(w, `{"error":"Supplier profile not found"}`, http.StatusNotFound)
			return
		}
		supID = sup.ID
	}
	if supID == "" {
		http.Error(w, `{"error":"Supplier profile not found"}`, http.StatusNotFound)
		return
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body == nil {
		http.Error(w, `{"error":"Invalid request body"}`, http.StatusBadRequest)
		return
	}
	var description, warehouseCity, logoURL *string
	if raw, ok := body["description"]; ok {
		var v *string
		if err := json.Unmarshal(raw, &v); err != nil || v == nil {
			http.Error(w, `{"error":"Invalid description"}`, http.StatusBadRequest)
			return
		}
		if utf8.RuneCountInString(*v) > repository.SupplierProfileDescriptionLimit {
			http.Error(w, `{"error":"Description too long"}`, http.StatusBadRequest)
			return
		}
		description = v
	}
	if raw, ok := body["warehouse_city"]; ok {
		var v *string
		if err := json.Unmarshal(raw, &v); err != nil || v == nil {
			http.Error(w, `{"error":"Invalid warehouse city"}`, http.StatusBadRequest)
			return
		}
		city := strings.TrimSpace(*v)
		if city == "" {
			http.Error(w, `{"error":"Warehouse city required"}`, http.StatusBadRequest)
			return
		}
		if utf8.RuneCountInString(city) > repository.SupplierWarehouseCityLimit {
			http.Error(w, `{"error":"Warehouse city must be at most 50 characters"}`, http.StatusBadRequest)
			return
		}
		warehouseCity = &city
	}
	if raw, ok := body["logo_url"]; ok {
		var v *string
		if err := json.Unmarshal(raw, &v); err != nil || v == nil {
			http.Error(w, `{"error":"Invalid logo url"}`, http.StatusBadRequest)
			return
		}
		logoURL = v
	}
	updated, err := h.supplierRepo.UpdateSupplierProfile(r.Context(), supID, description, warehouseCity, logoURL)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if updated == nil {
		http.Error(w, `{"error":"Supplier profile not found"}`, http.StatusNotFound)
		return
	}
	_, permissions, _, accessErr := h.supplierRepo.GetSupplierAccess(r.Context(), userID)
	if accessErr != nil {
		writeInternalError(w, accessErr)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"permissions":              permissions,
		"id":                       updated.ID,
		"name":                     updated.Name,
		"company_name":             updated.Name,
		"rating":                   updated.Rating,
		"total_products":           updated.TotalProducts,
		"delivery_score":           updated.DeliveryScore,
		"warehouse_city":           updated.WarehouseCity,
		"warehouse_city_confirmed": updated.WarehouseCityConfirmed,
		"logo_url":                 updated.LogoURL,
		"description":              updated.Description,
		"badge":                    updated.Badge,
		"supportsDirectShipping":   derivedSupportsDirectShipping(r.Context(), h.supplierRepo, updated.ID, updated.SupportsDirectShipping),
		"approvalStatus":           updated.ApprovalStatus,
		"signedContractVersion":    updated.SignedContractVersion,
	})
}
