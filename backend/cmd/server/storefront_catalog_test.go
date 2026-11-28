package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/drup/platform/internal/domain"
)

type fixtureStorefrontLookup map[string]*domain.Store

func (lookup fixtureStorefrontLookup) GetStoreBySlug(_ context.Context, slug string) (*domain.Store, error) {
	return lookup[slug], nil
}

func TestResolvePublicStorefrontDoesNotUseCentralFallback(t *testing.T) {
	active := &domain.Store{ID: "a", Slug: "shop-a", OwnerType: "dropshipper", Status: "active"}
	lookup := fixtureStorefrontLookup{
		active.Slug: active,
		"shop-b":    {ID: "b", Slug: "shop-b", OwnerType: "dropshipper", Status: "suspended"},
		"central":   {ID: "central", Slug: "central", OwnerType: "central", Status: "active"},
	}
	resolved, err := resolvePublicStorefront(context.Background(), lookup, active.Slug)
	if err != nil || resolved != active {
		t.Fatalf("resolved=%v err=%v", resolved, err)
	}
	for _, slug := range []string{"", "unknown", "shop-b", "central"} {
		if wrong, err := resolvePublicStorefront(context.Background(), lookup, slug); wrong != nil || !errors.Is(err, errPublicStorefrontNotFound) {
			t.Fatalf("slug %q resolved=%v err=%v", slug, wrong, err)
		}
	}
}

func TestStorefrontAvailabilityUsesVisibleVariantStock(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  bool
	}{
		{"?storeSlug=shop-a&storefront=1&in_stock=true", true},
		{"?storeSlug=shop-a&storefront=1", false},
		{"?in_stock=true", false},
	} {
		filter, err := parseCatalogPageFilter(httptest.NewRequest("GET", "/api/v1/products/"+tc.query, nil))
		if err != nil || filter.VisibleStockOnly != tc.want {
			t.Fatalf("query %q filter=%+v err=%v", tc.query, filter, err)
		}
	}
}
