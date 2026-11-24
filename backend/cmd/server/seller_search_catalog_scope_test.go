package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/testdb"
)

func TestDRP2SellerSearchUsesVerifiedPublicCatalogScope(t *testing.T) {
	catalog, stores := storeScopedRepos(t)
	db, err := testdb.Open(t, os.Getenv("TEAM04_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE products SET brand='پاوربانك اخبار' WHERE id='noise-01'`)
	exec(`INSERT INTO search_queries(term_normalized,store_slug,result_count,catalog_result_count) VALUES ('واژه قدیمی ناشناخته','shopx',0,0)`)
	term := "پاور بانك اخبار" // Matches by normalized brand, not title.
	publicList := storeScopedPageCtx(t, catalog, stores, context.Background(), "/api/v1/products/?search="+url.QueryEscape(term))
	if publicList.total != 1 {
		t.Fatalf("central listing should match exactly one product, got %d", publicList.total)
	}
	for _, query := range []string{term, "کالای ناشناخته آزمون", "جدید مرکزی"} {
		path := "/api/v1/products/?storeSlug=shopx&search=" + url.QueryEscape(query)
		if page := storeScopedPageCtx(t, catalog, stores, context.Background(), path); page.total != 0 {
			t.Fatalf("store listing %q unexpectedly has %d results", query, page.total)
		}
	}
	var storedResult, centralResult int
	err = db.QueryRow(`SELECT result_count,catalog_result_count FROM search_queries
		WHERE store_slug='shopx' AND term_normalized='پاور بانک اخبار'`).Scan(&storedResult, &centralResult)
	if err != nil || storedResult != 0 || centralResult != publicList.total {
		t.Errorf("recorded store/central counts = %d/%d, want 0/%d (err %v)", storedResult, centralResult, publicList.total, err)
	}
	// Catalog changes after the zero-result event. It must move to
	// quick-add now even though historical central count was genuinely 0.
	exec(`UPDATE products SET title='جدید مرکزی' WHERE id='noise-02'`)

	ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: "owner-shopx"})
	handler := func(w http.ResponseWriter, r *http.Request) { handleT01SellerSearchInsight(w, r.WithContext(ctx), db) }
	rec := team04Get(t, handler, "/api/v1/seller/search-insight?store_id=store-shopx")
	if rec.Code != http.StatusOK {
		t.Fatalf("insights status = %d, body %s", rec.Code, rec.Body.String())
	}
	type match struct {
		ProductID string `json:"productId"`
	}
	type entry struct {
		Term    string  `json:"term"`
		Matches []match `json:"catalogMatches"`
	}
	var insight struct {
		UnmetInStore []entry `json:"unmetInStore"`
		UnmetCatalog []entry `json:"unmetCatalog"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &insight); err != nil {
		t.Fatal(err)
	}
	if len(insight.UnmetCatalog) != 1 || insight.UnmetCatalog[0].Term != "کالای ناشناخته آزمون" {
		t.Errorf("central misses = %+v; only the verified and still missing result belongs here", insight.UnmetCatalog)
	}
	wantStore := map[string]string{"پاور بانک اخبار": "noise-01", "جدید مرکزی": "noise-02", "واژه قدیمی ناشناخته": ""}
	for _, item := range insight.UnmetInStore {
		want, ok := wantStore[item.Term]
		if !ok {
			t.Errorf("unexpected / duplicate store term %q", item.Term)
		}
		delete(wantStore, item.Term)
		if want == "" && len(item.Matches) != 0 || want != "" && (len(item.Matches) != 1 || item.Matches[0].ProductID != want) {
			t.Errorf("store term %q matched %+v, want %s", item.Term, item.Matches, want)
		}
	}
	if len(wantStore) != 0 {
		t.Errorf("missing store terms: %v", wantStore)
	}

	// Exercise the actual scoped mutation used by the quick-add button,
	// keeping the old choices (including the globally inactive ones).
	add := httptest.NewRequest(http.MethodPut, "/api/v1/stores/shopx/products",
		strings.NewReader(`{"productIds":["p-old-1","p-old-2","p-old-3","noise-01"]}`)).WithContext(ctx)
	addRec := httptest.NewRecorder()
	handleSellerStore(addRec, add, stores, nil, "shopx", "products", "")
	if addRec.Code != http.StatusOK {
		t.Fatalf("quick-add PUT status %d: %s", addRec.Code, addRec.Body.String())
	}
	var saved struct {
		ActiveProductIDs []string `json:"activeProductIds"`
	}
	if err := json.Unmarshal(addRec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(saved.ActiveProductIDs, []string{"p-old-1", "p-old-2", "p-old-3", "noise-01"}) {
		t.Fatalf("acknowledged selection = %v", saved.ActiveProductIDs)
	}
	visible := storeScopedList(t, catalog, stores, "/api/v1/products/?storeSlug=shopx&search="+url.QueryEscape(term))
	if got := listIDs(visible); !slices.Equal(got, []string{"noise-01"}) {
		t.Fatalf("new choice not visible in storefront: %v", got)
	}
}
