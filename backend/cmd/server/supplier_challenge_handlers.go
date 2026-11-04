package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// FIX 52 — live challenge-create (Jl challenge box): the challenger sends
// the branch, the proposed wholesale price, the variant detail and the
// full proposal. The server resolves the holder from the proposal title
// (same rule as the 409 path) and stores the request pending for review.
func (h *SupplierHandlers) HandleCreateSupplierExclusivityChallenge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)
	if supplierID == "" {
		http.Error(w, `{"error":"Supplier required"}`, http.StatusForbidden)
		return
	}
	var body struct {
		Branch           string                 `json:"branch"`
		ProposedPrice    int64                  `json:"proposedPrice"`
		VariantName      string                 `json:"variantName"`
		VariantAttrs     map[string]string      `json:"variantAttributes"`
		VariantNames     []string               `json:"variantNames"`
		Proposal         map[string]interface{} `json:"proposal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if body.ProposedPrice <= 0 {
		http.Error(w, `{"error":"قیمت پیشنهادی را درست وارد کن."}`, http.StatusBadRequest)
		return
	}
	if body.Branch == "same_variant" && strings.TrimSpace(body.VariantName) == "" {
		http.Error(w, `{"error":"نام تنوع را دقیقاً مثل تنوع موجود وارد کن."}`, http.StatusBadRequest)
		return
	}
	if body.Branch == "new_variants" && len(body.VariantNames) == 0 {
		http.Error(w, `{"error":"حداقل نام یک تنوع جدید را وارد کن."}`, http.StatusBadRequest)
		return
	}
	if body.Branch != "same_variant" && body.Branch != "new_variants" {
		http.Error(w, `{"error":"Invalid challenge branch"}`, http.StatusBadRequest)
		return
	}
	if body.Proposal == nil {
		http.Error(w, `{"error":"Proposal required"}`, http.StatusBadRequest)
		return
	}
	title, _ := body.Proposal["title"].(string)
	holder, err := h.supplierRepo.FindDuplicateProduct(r.Context(), supplierID, title)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if holder == nil {
		http.Error(w, `{"error":"کالای مورد چالش یافت نشد."}`, http.StatusNotFound)
		return
	}
	variantAttrsJSON := ""
	if body.VariantAttrs != nil {
		raw, _ := json.Marshal(body.VariantAttrs)
		variantAttrsJSON = string(raw)
	}
	id, err := h.supplierRepo.CreateExclusivityChallenge(r.Context(), supplierID, body.Branch, body.ProposedPrice, body.Proposal, holder, body.VariantName, variantAttrsJSON, body.VariantNames)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "pending"})
}
