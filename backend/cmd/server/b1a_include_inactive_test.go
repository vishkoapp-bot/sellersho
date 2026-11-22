package main

// B1(a) carve-out tests: ?includeInactive=1 on the public list is
// honoured only for the authenticated owner of the requested
// dropshipper store. Anonymous callers, non-owners, and requests for
// a central or unknown store silently get the filtered list, and the
// flag is part of the cursor binding (a cursor issued with the flag
// does not decode without it).
//
// Fixture: storeScopedRepos (store_scoped_list_test.go) — shopx
// assortment is p-old-1 (inactive), p-old-2 (inactive) and p-old-3
// (active); shopx is owned by user "owner-shopx".

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

func b1aClaimsCtx(userID string) context.Context {
	if userID == "" {
		return context.Background()
	}
	return context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: userID, Role: "seller"})
}

func b1aListStatus(t *testing.T, catalogRepo *repository.CatalogRepository, storeRepo *repository.StoreRepository, ctx context.Context, path string) int {
	t.Helper()
	handler := func(w http.ResponseWriter, r *http.Request) {
		handleProductsList(w, r, ctx, catalogRepo, storeRepo, nil)
	}
	return team04Get(t, handler, path).Code
}

func TestStoreScopedListIncludeInactiveOwnerGated(t *testing.T) {
	catalogRepo, storeRepo := storeScopedRepos(t)
	owner := b1aClaimsCtx("owner-shopx")
	stranger := b1aClaimsCtx("someone-else")

	// Owner + flag: the whole assortment, position order, total 3.
	page := storeScopedPageCtx(t, catalogRepo, storeRepo, owner, "/api/v1/products/?storeSlug=shopx&includeInactive=1")
	if got := listIDs(page.items); fmt.Sprint(got) != fmt.Sprint([]string{"p-old-1", "p-old-2", "p-old-3"}) {
		t.Fatalf("owner +includeInactive ids = %v, want [p-old-1 p-old-2 p-old-3]", got)
	}
	if page.total != 3 || page.hasMore {
		t.Fatalf("owner +includeInactive total=%d hasMore=%v, want 3/false", page.total, page.hasMore)
	}
	// M-2 (Batch 3 fix): the envelope echoes that the flag was applied.
	if !page.includeInactiveApplied {
		t.Fatalf("owner +includeInactive includeInactiveApplied=false, want true")
	}

	// Owner without the flag: the filtered list (p-old-3 only).
	list := storeScopedListCtx(t, catalogRepo, storeRepo, owner, "/api/v1/products/?storeSlug=shopx")
	if got := listIDs(list); fmt.Sprint(got) != fmt.Sprint([]string{"p-old-3"}) {
		t.Fatalf("owner without flag ids = %v, want [p-old-3]", got)
	}
	// …and the echo is false: nothing was asked for, nothing applied.
	if pageNoFlag := storeScopedPageCtx(t, catalogRepo, storeRepo, owner, "/api/v1/products/?storeSlug=shopx"); pageNoFlag.includeInactiveApplied {
		t.Fatalf("owner without flag includeInactiveApplied=true, want false")
	}

	// Anonymous + flag: the flag is silently ignored.
	list = storeScopedListCtx(t, catalogRepo, storeRepo, context.Background(), "/api/v1/products/?storeSlug=shopx&includeInactive=1")
	if got := listIDs(list); fmt.Sprint(got) != fmt.Sprint([]string{"p-old-3"}) {
		t.Fatalf("anonymous +includeInactive ids = %v, want [p-old-3]", got)
	}
	// M-2: the echo says so — the management tab must treat this as
	// "not applied" (error/re-login), never as the whole assortment.
	if pageAnon := storeScopedPageCtx(t, catalogRepo, storeRepo, context.Background(), "/api/v1/products/?storeSlug=shopx&includeInactive=1"); pageAnon.includeInactiveApplied {
		t.Fatalf("anonymous +includeInactive includeInactiveApplied=true, want false")
	}

	// Non-owner + flag: the flag is silently ignored.
	list = storeScopedListCtx(t, catalogRepo, storeRepo, stranger, "/api/v1/products/?storeSlug=shopx&includeInactive=1")
	if got := listIDs(list); fmt.Sprint(got) != fmt.Sprint([]string{"p-old-3"}) {
		t.Fatalf("non-owner +includeInactive ids = %v, want [p-old-3]", got)
	}
	if pageStranger := storeScopedPageCtx(t, catalogRepo, storeRepo, stranger, "/api/v1/products/?storeSlug=shopx&includeInactive=1"); pageStranger.includeInactiveApplied {
		t.Fatalf("non-owner +includeInactive includeInactiveApplied=true, want false")
	}

	// Owner + flag on the central store: OwnedStore refuses central
	// stores, so the flag is ignored and the global list is served.
	central := storeScopedListCtx(t, catalogRepo, storeRepo, owner, "/api/v1/products/?storeSlug=central&includeInactive=1&limit=100")
	if len(central) != 46 {
		t.Fatalf("owner +includeInactive on central len = %d, want 46", len(central))
	}

	// Owner + flag on an unknown slug: no store to own, flag ignored,
	// unscoped fallback.
	unknown := storeScopedListCtx(t, catalogRepo, storeRepo, owner, "/api/v1/products/?storeSlug=no-such-store&includeInactive=1&limit=100")
	if len(unknown) != 46 {
		t.Fatalf("owner +includeInactive on unknown slug len = %d, want 46", len(unknown))
	}

	// Cursor round-trip with the flag: page 1 (limit=2) is the two
	// inactive rows; page 2 with the same flag is p-old-3.
	first := storeScopedPageCtx(t, catalogRepo, storeRepo, owner, "/api/v1/products/?storeSlug=shopx&includeInactive=1&limit=2")
	if got := listIDs(first.items); fmt.Sprint(got) != fmt.Sprint([]string{"p-old-1", "p-old-2"}) {
		t.Fatalf("owner +includeInactive page 1 ids = %v, want [p-old-1 p-old-2]", got)
	}
	if first.nextCursor == nil || !first.hasMore || first.total != 3 {
		t.Fatalf("owner +includeInactive page 1: cursor=%v hasMore=%v total=%d, want cursor set, true, 3", first.nextCursor, first.hasMore, first.total)
	}
	second := storeScopedListCtx(t, catalogRepo, storeRepo, owner, "/api/v1/products/?storeSlug=shopx&includeInactive=1&limit=2&cursor="+*first.nextCursor)
	if got := listIDs(second); fmt.Sprint(got) != fmt.Sprint([]string{"p-old-3"}) {
		t.Fatalf("owner +includeInactive page 2 ids = %v, want [p-old-3]", got)
	}
	// The same cursor WITHOUT the flag must not decode: the flag is
	// part of the cursor binding, so the handler answers 409 restart.
	if code := b1aListStatus(t, catalogRepo, storeRepo, owner, "/api/v1/products/?storeSlug=shopx&limit=2&cursor="+*first.nextCursor); code != http.StatusConflict {
		t.Fatalf("cursor-with-flag replayed without flag: status = %d, want 409", code)
	}
}
