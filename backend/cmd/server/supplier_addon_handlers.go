package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/repository"
)

// FIX 50 — live addons widget (Ql) writes: create / update (staged when the
// addon is already approved) / active toggle / product attachments.
func addonSupplierID(w http.ResponseWriter, r *http.Request, h *SupplierHandlers) (string, bool) {
	id := getSupplierIDFromContext(r, h.supplierRepo)
	if id == "" {
		http.Error(w, `{"error":"Supplier profile not found"}`, http.StatusNotFound)
		return "", false
	}
	return id, true
}

func decodeAddonBody(w http.ResponseWriter, r *http.Request) (*repository.SupplierAddon, bool) {
	var body struct {
		Title           string  `json:"title"`
		Description     string  `json:"description"`
		Kind            string  `json:"kind"`
		PriceMode       string  `json:"priceMode"`
		PriceValue      float64 `json:"priceValue"`
		CapToman        int64   `json:"capToman"`
		SortOrder       int     `json:"sortOrder"`
		LinkedProductID *string `json:"linkedProductId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"Invalid request body"}`, http.StatusBadRequest)
		return nil, false
	}
	if body.Kind != "service" && body.Kind != "physical" {
		body.Kind = "service"
	}
	if body.PriceMode != "fixed" && body.PriceMode != "percent" {
		body.PriceMode = "fixed"
	}
	if body.PriceMode == "percent" {
		if body.CapToman < 0 {
			body.CapToman = 0
		}
	} else {
		body.CapToman = 0
	}
	return &repository.SupplierAddon{
		Title: body.Title, Description: body.Description, Kind: body.Kind,
		PriceMode: body.PriceMode, PriceValue: body.PriceValue, CapToman: body.CapToman,
		SortOrder: body.SortOrder, LinkedProductID: body.LinkedProductID,
	}, true
}

func (h *SupplierHandlers) HandleCreateSupplierAddon(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := addonSupplierID(w, r, h)
	if !ok {
		return
	}
	a, ok := decodeAddonBody(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(a.Title) == "" {
		http.Error(w, `{"error":"Title is required"}`, http.StatusBadRequest)
		return
	}
	if a.PriceValue < 0 {
		http.Error(w, `{"error":"Invalid price"}`, http.StatusBadRequest)
		return
	}
	if a.LinkedProductID != nil && *a.LinkedProductID != "" {
		a.Kind = "physical"
		a.PriceMode = "fixed"
		a.PriceValue = 0
		a.CapToman = 0
	}
	created, err := h.supplierRepo.CreateSupplierAddon(r.Context(), supplierID, a)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

func (h *SupplierHandlers) HandleUpdateSupplierAddon(w http.ResponseWriter, r *http.Request, addonID string) {
	supplierID, ok := addonSupplierID(w, r, h)
	if !ok {
		return
	}
	a, ok := decodeAddonBody(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(a.Title) == "" {
		http.Error(w, `{"error":"Title is required"}`, http.StatusBadRequest)
		return
	}
	if a.PriceValue < 0 {
		http.Error(w, `{"error":"Invalid price"}`, http.StatusBadRequest)
		return
	}
	existing, err := h.supplierRepo.GetSupplierAddon(r.Context(), supplierID, addonID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if existing == nil {
		http.Error(w, `{"error":"Addon not found"}`, http.StatusNotFound)
		return
	}
	a.ID = addonID
	if a.LinkedProductID != nil && *a.LinkedProductID != "" {
		a.Kind = "physical"
		a.PriceMode = "fixed"
		a.PriceValue = 0
		a.CapToman = 0
	}
	if existing.ApprovalStatus == "approved" {
		if err := h.supplierRepo.StageSupplierAddonUpdate(r.Context(), supplierID, a); err != nil {
			writeInternalError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"staged": true})
		return
	}
	if err := h.supplierRepo.UpdateSupplierAddonInPlace(r.Context(), supplierID, a); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Addon not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"staged": false})
}

func (h *SupplierHandlers) HandleSetSupplierAddonActive(w http.ResponseWriter, r *http.Request, addonID string) {
	supplierID, ok := addonSupplierID(w, r, h)
	if !ok {
		return
	}
	var body struct {
		IsActive bool `json:"isActive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"Invalid request body"}`, http.StatusBadRequest)
		return
	}
	if err := h.supplierRepo.SetSupplierAddonActive(r.Context(), supplierID, addonID, body.IsActive); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Addon not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

func (h *SupplierHandlers) HandleSetSupplierAddonAttachments(w http.ResponseWriter, r *http.Request, addonID string) {
	supplierID, ok := addonSupplierID(w, r, h)
	if !ok {
		return
	}
	var body struct {
		Attachments []struct {
			ScopeType string `json:"scopeType"`
			ProductID string `json:"productId"`
		} `json:"attachments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"Invalid request body"}`, http.StatusBadRequest)
		return
	}
	productIDs := []string{}
	for _, att := range body.Attachments {
		if att.ScopeType == "product" && att.ProductID != "" {
			productIDs = append(productIDs, att.ProductID)
		}
	}
	if err := h.supplierRepo.SetSupplierAddonAttachments(r.Context(), supplierID, addonID, productIDs); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Addon not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}
