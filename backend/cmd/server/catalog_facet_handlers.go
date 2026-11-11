package main

// Team 04 live parity: Digikala facet endpoints (D40).
//
// DECISION (D40, explicit): build the endpoints on the dk data instead
// of leaving the live 404s. The data model supports them cleanly: the
// dk tables are keyed by categories.id, so /categories/<slug>/brands
// and /categories/<slug>/attributes are plain joins, /brands is a
// distinct-brand projection, and /categories/search is a title/slug
// match over the real category table. These endpoints did NOT exist in
// live (they 404ed while the live frontend called them and swallowed
// the error), so their response shapes below are NEW API shapes, not
// live-proven shapes, and are documented as such. No facet semantics
// are invented beyond the tables: attributes are the category's OWN
// attributes only (no inherited/descendant union; the migration comment
// for 202610280011 describes a recursive-CTE union as a design idea,
// not a proven live behaviour, so it is not implemented here).
// The supplier-proposal-form use of dk facets (authenticated path) is
// TODO U30: it may use a different route prefix, which this file does
// not guess.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
)

// handleBrandsList serves GET /api/v1/brands: every distinct Digikala
// brand found in dk_category_brands, ordered by title. Brands here are
// reference-only suggestion data (migration 202610280011): they are NOT
// merged into products.brand, which stays free text.
func handleBrandsList(w http.ResponseWriter, r *http.Request, catalogRepo *repository.CatalogRepository) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if catalogRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	brands, err := catalogRepo.GetBrands(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(brands)
}

// handleCategoryFacets serves the /api/v1/categories/ subtree that live
// 404ed (D40):
//   - GET /api/v1/categories/search?q=<term> (also ?search=)
//   - GET /api/v1/categories/<slug|id>/brands
//   - GET /api/v1/categories/<slug|id>/attributes
//
// Anything else under the prefix is a 404, matching live behaviour for
// unknown category subpaths.
func handleCategoryFacets(w http.ResponseWriter, r *http.Request, catalogRepo *repository.CatalogRepository, subPath string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if catalogRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	trimmed := strings.Trim(strings.TrimSuffix(subPath, "/"), "/")
	if trimmed == "search" {
		q := r.URL.Query().Get("q")
		if q == "" {
			q = r.URL.Query().Get("search")
		}
		results, err := catalogRepo.SearchCategories(r.Context(), q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(results)
		return
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		// FIX 14: live answers unknown category subpaths with Go's
		// default text 404 ("404 page not found"), not JSON
		// (probed live 2026-11-03 for /categories/xyz).
		http.NotFound(w, r)
		return
	}
	slug, action := parts[0], parts[1]
	switch action {
	case "product-attributes":
		attributes, err := catalogRepo.GetCategoryProductAttributes(r.Context(), slug)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		brands, err := catalogRepo.GetCategoryBrandsBySlug(r.Context(), slug)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"attributes": attributes, "brands": brands})
	case "brands":
		brands, err := catalogRepo.GetCategoryBrandsBySlug(r.Context(), slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(brands)
	case "attributes":
		attrs, err := catalogRepo.GetCategoryAttributesBySlug(r.Context(), slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// FIX 51 — live's ql editor also reads colors, brands and the
		// inherited flag (attributes gathered from subcategories when the
		// category itself has none).
		inherited := false

		colors, _ := catalogRepo.GetCategoryColorsBySlug(r.Context(), slug)
		brands, _ := catalogRepo.GetCategoryBrandsBySlug(r.Context(), slug)
		if colors == nil {
			colors = []repository.DkCategoryColor{}
		}
		if brands == nil {
			brands = []domain.CategoryBrand{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"attributes": attrs, "colors": colors, "brands": brands, "inherited": inherited})
	default:
		// FIX 14: see above -- Go default text 404 like live.
		http.NotFound(w, r)
	}
}
