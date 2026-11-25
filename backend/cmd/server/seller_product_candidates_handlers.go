package main

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/drup/platform/internal/repository"
)

func handleSellerProductCandidates(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository) {
	if !t01MethodOnly(w, r, http.MethodGet) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		sellerError(w, repository.ErrStoreInput)
		return
	}
	slug := strings.TrimSpace(query.Get("storeSlug"))
	if slug == "" {
		sellerError(w, repository.ErrStoreInput)
		return
	}
	ctx := r.Context()
	store, err := repo.OwnedStore(ctx, userID, slug)
	if err != nil {
		sellerError(w, err)
		return
	}

	limit := 50
	if raw := query.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 {
			sellerError(w, repository.ErrStoreInput)
			return
		}
		limit = min(limit, 100)
	}
	var after string
	if cursor := query.Get("cursor"); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		after = string(decoded)
		if err != nil || !validSellerCandidateID(after) {
			sellerError(w, repository.ErrStoreInput)
			return
		}
	}
	var ids []string
	if query.Has("ids") {
		ids = strings.SplitN(query.Get("ids"), ",", 101)
		if len(ids) > 100 {
			sellerError(w, repository.ErrStoreInput)
			return
		}
		for _, id := range ids {
			if !validSellerCandidateID(id) {
				sellerError(w, repository.ErrStoreInput)
				return
			}
		}
	}
	products, hasMore, err := repo.ListSellerProductCandidates(ctx, store.ID, strings.TrimSpace(query.Get("q")), after, ids, limit)
	if err != nil {
		sellerError(w, err)
		return
	}
	nextCursor := ""
	if hasMore {
		nextCursor = base64.RawURLEncoding.EncodeToString([]byte(products[len(products)-1].ID))
	}
	t01WriteJSON(w, http.StatusOK, map[string]any{
		"success": true, "products": products, "nextCursor": nextCursor, "hasMore": hasMore,
	})
}

// products.id is VARCHAR(50).
func validSellerCandidateID(id string) bool {
	return id != "" && utf8.ValidString(id) && utf8.RuneCountInString(id) <= 50 && !strings.ContainsRune(id, 0)
}
