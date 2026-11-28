package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
)

func handlePublicProductReviews(w http.ResponseWriter, r *http.Request, catalogRepo *repository.CatalogRepository, productKey string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	limit := 10
	if n, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit"))); err == nil && n >= 1 && n <= 50 {
		limit = n
	}
	offset := 0
	if n, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset"))); err == nil && n >= 0 {
		offset = n
	}

	ctx := r.Context()
	var product *domain.Product
	var err error
	if strings.HasPrefix(productKey, "spn-") {
		spn, parseErr := parseSpnSegment(productKey)
		if parseErr == nil {
			product, err = catalogRepo.GetProductBySpn(ctx, spn)
		}
	} else {
		product, err = catalogRepo.GetProductByID(ctx, productKey)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeInternalError(w, err)
		return
	}
	if product == nil {
		t01WriteJSON(w, http.StatusNotFound, map[string]string{"error": "Product not found"})
		return
	}

	reviews, total, rating, err := catalogRepo.GetPublishedReviewPage(ctx, product.ID, limit, offset)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	type item struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Rating      int    `json:"rating"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		CreatedAt   string `json:"createdAt"`
	}
	items := make([]item, 0, len(reviews))
	for _, review := range reviews {
		name := review.DisplayName
		if name == "" || name == legacyProfileName {
			name = "خریدار"
		}
		items = append(items, item{
			ID: review.ID, DisplayName: name, Rating: review.Rating,
			Body: review.Body, CreatedAt: review.CreatedAt.Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"items": items, "total": total, "rating": rating, "reviewsCount": total,
	})
}
