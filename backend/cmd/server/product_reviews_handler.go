package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/drup/platform/internal/repository"
)

// FIX 48 — live supplier reviews modal (jl/rv) data source, also the
// public product-reviews list. GET /api/v1/products/{id}/reviews with
// ?limit=&offset= (live's ZC page size is 10). Items carry live's rv
// shape: displayName (buyer full name, fallback buyer), rating, title
// (no title column exists, always empty), body (comment), createdAt.
func handlePublicProductReviews(w http.ResponseWriter, r *http.Request, catalogRepo *repository.CatalogRepository, productID string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	limit := 10
	offset := 0
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 50 {
			limit = n
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get("offset")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	product, err := catalogRepo.GetProductByID(r.Context(), productID)
	if err != nil || product == nil {
		if err == sql.ErrNoRows || product == nil {
			http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	reviews, err := catalogRepo.GetProductReviews(r.Context(), product.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	total := len(reviews)
	var sum int64
	for _, rv := range reviews {
		sum += int64(rv.Rating)
	}
	var rating float64
	if total > 0 {
		rating = float64(sum) / float64(total)
	}
	type item struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Rating      int    `json:"rating"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		CreatedAt   string `json:"createdAt"`
	}
	items := []item{}
	end := offset + limit
	if end > total {
		end = total
	}
	if offset < total {
		for _, rv := range reviews[offset:end] {
			name := rv.UserID
			if u, err := catalogRepo.GetUserDisplayName(r.Context(), rv.UserID); err == nil && strings.TrimSpace(u) != "" {
				name = u
			} else if strings.TrimSpace(name) == "" {
				name = "خریدار"
			}
			items = append(items, item{ID: rv.ID, DisplayName: name, Rating: rv.Rating, Title: "", Body: rv.Comment, CreatedAt: rv.CreatedAt.Format("2006-01-02T15:04:05Z07:00")})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"items":        items,
		"total":        total,
		"rating":       rating,
		"reviewsCount": total,
	})
}
