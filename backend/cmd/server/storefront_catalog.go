package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/drup/platform/internal/domain"
)

var errPublicStorefrontNotFound = errors.New("public storefront not found")

type storefrontStoreLookup interface {
	GetStoreBySlug(context.Context, string) (*domain.Store, error)
}

func resolvePublicStorefront(ctx context.Context, lookup storefrontStoreLookup, slug string) (*domain.Store, error) {
	if slug == "" {
		return nil, errPublicStorefrontNotFound
	}
	store, err := lookup.GetStoreBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if store == nil || store.OwnerType != "dropshipper" || store.Status != "active" {
		return nil, errPublicStorefrontNotFound
	}
	return store, nil
}

func requirePublicStorefront(w http.ResponseWriter, r *http.Request, lookup storefrontStoreLookup) *domain.Store {
	store, err := resolvePublicStorefront(r.Context(), lookup, r.URL.Query().Get("storeSlug"))
	if errors.Is(err, errPublicStorefrontNotFound) {
		http.Error(w, `{"error":"storefront not found"}`, http.StatusNotFound)
	} else if err != nil {
		writeInternalError(w, err)
	}
	return store
}
