package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (h *SupplierHandlers) HandleGetCampaigns(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	items, err := h.supplierRepo.GetCampaigns(r.Context(), getSupplierIDFromContext(r, h.supplierRepo))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(items)
}

func (h *SupplierHandlers) HandleCampaignStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Status != "active" && req.Status != "paused") {
		http.Error(w, `{"error":"Invalid campaign status"}`, http.StatusBadRequest)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/campaigns/"), "/status")
	if err := h.supplierRepo.SetCampaignStatus(r.Context(), getSupplierIDFromContext(r, h.supplierRepo), id, req.Status); err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (h *SupplierHandlers) HandleSettlementBalance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	balance, err := h.supplierRepo.GetSettlementBalance(r.Context(), getSupplierIDFromContext(r, h.supplierRepo))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(balance)
}
