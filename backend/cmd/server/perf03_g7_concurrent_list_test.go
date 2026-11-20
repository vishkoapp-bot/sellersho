package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
)

// PERF-03 G7: the first catalog burst on a cold price index used to
// produce exactly 19x500 out of 20 concurrent requests — every one a
// PostgreSQL serialization failure (40001): two RepeatableRead
// snapshot transactions refreshing the same stale catalog_public_prices
// rows collide, and all but one lose. The fix is ONE bounded retry of
// the whole snapshot tx (retryCatalogTxOnSerialization): the retry
// re-probes against the winner's committed refresh and finds no stale
// rows. This test fires 20 concurrent list requests at a deliberately
// emptied index and requires all 20 to return 200.
// Runs ONLY against a disposable migrated database.
func TestPERF03ConcurrentCatalogListColdIndexAll200(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SELLERSHO_TEST_DATABASE_URL is not set; skipping disposable-DB integration test")
	}
	catalogRepo, storeRepo := perf01HTTPRepos(t)
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	pricingRepo := repository.NewPricingRepository(db)

	// Cold index: every product is stale, so every request's snapshot
	// tx refreshes the same rows — the exact collision the audit's
	// run B hit (19x500). sort=cheapest takes the priced path, whose
	// probe covers the whole filtered set, maximising the collision
	// surface; the featured path refreshes only the page's IDs.
	if _, err := db.Exec(`DELETE FROM catalog_public_prices`); err != nil {
		t.Fatalf("empty price index: %v", err)
	}

	const workers = 20
	codes := make([]int, workers)
	bodies := make([]string, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodGet, "/api/v1/products?limit=20&sort=cheapest", nil)
			rec := httptest.NewRecorder()
			handleProductsList(rec, req, context.Background(), catalogRepo, storeRepo, pricingRepo)
			codes[i] = rec.Code
			bodies[i] = rec.Body.String()
		}(i)
	}
	close(start)
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("request %d: status = %d, body %s", i, code, bodies[i])
		}
	}
}
