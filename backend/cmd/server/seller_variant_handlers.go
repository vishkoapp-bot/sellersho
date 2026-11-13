package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
)

// Seller per-store variant settings (P2(a) visibility, P2(c) default
// variant). Routes live under /api/v1/seller/stores/{slug}/... (spec
// paths); the store is addressed by slug, as in the /api/v1/stores/
// dispatch. Central store has no per-store settings (403); only the
// store owner (or an admin) may write.

// sellerVariantStore resolves + authorises a store for a per-store
// variant write. Returns nil after writing the error response.
func sellerVariantStore(w http.ResponseWriter, r *http.Request, storeRepo *repository.StoreRepository, slug string) *domain.Store {
	claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return nil
	}
	store, err := storeRepo.GetStoreBySlug(r.Context(), slug)
	if err != nil || store == nil {
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return nil
	}
	if store.Slug == "central" {
		http.Error(w, `{"error":"Central store has no per-store variant settings"}`, http.StatusForbidden)
		return nil
	}
	if !claims.HasDirectCapability("admin") && (store.OwnerUserID == nil || *store.OwnerUserID != claims.UserID) {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return nil
	}
	return store
}

// findStoreVariant loads a product's variants and returns the one with
// the given ID, or nil when the variant does not belong to the product.
func findStoreVariant(ctx context.Context, catalogRepo *repository.CatalogRepository, productID, variantID string) *domain.ProductVariant {
	variants, err := catalogRepo.GetProductVariants(ctx, productID)
	if err != nil {
		return nil
	}
	for _, v := range variants {
		if v.ID == variantID {
			vv := v
			return &vv
		}
	}
	return nil
}

// handleSellerStoreVariantVisibility serves:
// PUT /api/v1/seller/stores/{slug}/products/{productId}/variants/{variantId}/visibility
// Body: { "isVisible": false }. isVisible=true DELETEs the row (reset
// to default-visible); false upserts it. The product must be in the
// store's assortment and the variant must belong to the product.
func handleSellerStoreVariantVisibility(w http.ResponseWriter, r *http.Request, storeRepo *repository.StoreRepository, catalogRepo *repository.CatalogRepository, slug, productID, variantID string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPut {
		writeMethodNotAllowed(w)
		return
	}
	store := sellerVariantStore(w, r, storeRepo, slug)
	if store == nil {
		return
	}
	var body struct {
		IsVisible *bool `json:"isVisible"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IsVisible == nil {
		http.Error(w, `{"error":"isVisible is required"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	active, err := storeRepo.StoreProductActive(ctx, store.ID, productID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !active {
		http.Error(w, `{"error":"Product is not in this store's assortment"}`, http.StatusUnprocessableEntity)
		return
	}
	if findStoreVariant(ctx, catalogRepo, productID, variantID) == nil {
		http.Error(w, `{"error":"Variant not found for product"}`, http.StatusUnprocessableEntity)
		return
	}
	if err := storeRepo.SetStoreVariantVisibility(ctx, store.ID, productID, variantID, *body.IsVisible); err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"productId": productID,
		"variantId": variantID,
		"isVisible": *body.IsVisible,
	})
}

// handleSellerStoreVariantDefault serves:
// PUT    /api/v1/seller/stores/{slug}/products/{productId}/default-variant  Body: { "variantId": "<uuid>" }
// DELETE /api/v1/seller/stores/{slug}/products/{productId}/default-variant
// PUT upserts (replaces the previous default — the PK guarantees one
// row); DELETE clears back to the product-level fallback. The variant
// must belong to the product, be active, and be visible in the store
// (a hidden variant cannot be the default).
func handleSellerStoreVariantDefault(w http.ResponseWriter, r *http.Request, storeRepo *repository.StoreRepository, catalogRepo *repository.CatalogRepository, slug, productID string) {
	w.Header().Set("Content-Type", "application/json")
	store := sellerVariantStore(w, r, storeRepo, slug)
	if store == nil {
		return
	}
	ctx := r.Context()
	if r.Method == http.MethodDelete {
		if err := storeRepo.ClearStoreVariantDefault(ctx, store.ID, productID); err != nil {
			writeInternalError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"productId": productID,
			"cleared":   true,
		})
		return
	}
	if r.Method != http.MethodPut {
		writeMethodNotAllowed(w)
		return
	}
	var body struct {
		VariantID string `json:"variantId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.VariantID) == "" {
		http.Error(w, `{"error":"variantId is required"}`, http.StatusBadRequest)
		return
	}
	active, err := storeRepo.StoreProductActive(ctx, store.ID, productID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !active {
		http.Error(w, `{"error":"Product is not in this store's assortment"}`, http.StatusUnprocessableEntity)
		return
	}
	variant := findStoreVariant(ctx, catalogRepo, productID, body.VariantID)
	if variant == nil {
		http.Error(w, `{"error":"Variant not found for product"}`, http.StatusUnprocessableEntity)
		return
	}
	if !variant.IsActive {
		http.Error(w, `{"error":"Variant is not active"}`, http.StatusUnprocessableEntity)
		return
	}
	visible, err := storeRepo.IsVariantVisibleInStore(ctx, store.ID, variant.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !visible {
		http.Error(w, `{"error":"Variant is hidden in this store"}`, http.StatusUnprocessableEntity)
		return
	}
	if err := storeRepo.SetStoreVariantDefault(ctx, store.ID, productID, variant.ID); err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"productId": productID,
		"variantId": variant.ID,
	})
}
