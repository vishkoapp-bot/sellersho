package main

// Team 04 live parity: catalog review dossier endpoint (D61, backend
// part). The CatalogReviewDossier component itself belongs to Team 08;
// this file serves the data it needs: package dimensions, product
// height, warranty, central-warehouse vs direct-supplier shipping, and
// any stored rejection reason. The endpoint accepts either a product
// id (post-approval dossier) or a pending_approvals proposal id
// (pre-approval dossier), because review happens on both sides of
// approval. The approval note is the message recorded in MASTER D61
// ("on approval, product is created without origin"); it is a data
// field for the component, not a behaviour this endpoint enforces.
// The dossier chunk's example rejection reasons (such as brand
// spelling) are reviewer-entered free text in the source system, not a
// database enum: this endpoint returns stored reasons only and invents
// no checklist.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/repository"
)

// handleCatalogReviewDossier serves GET
// /api/v1/admin/catalog/review/<product-or-proposal-id>. Admin-only;
// the route registration (main.go) wraps it in RequireRoles admin.
func handleCatalogReviewDossier(w http.ResponseWriter, r *http.Request, catalogRepo *repository.CatalogRepository, id string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	id = strings.Trim(strings.TrimSuffix(id, "/"), "/")
	if catalogRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	if id == "" {
		http.Error(w, `{"error":"Not found"}`, http.StatusNotFound)
		return
	}
	dossier, err := catalogRepo.GetCatalogReviewDossier(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if dossier == nil {
		http.Error(w, `{"error":"Not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(dossier)
}
