package repository

import (
	"context"
	"testing"
)

func TestPublishedReviewPageKeepsTotalsAndOnlyPublicAuthorData(t *testing.T) {
	db := operationsDB(t)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	for _, statement := range []string{
		"CREATE TEMP TABLE users(id uuid PRIMARY KEY, full_name text)",
		"CREATE TEMP TABLE product_reviews(id text PRIMARY KEY, product_id text, user_id text, rating smallint, comment text, status text, created_at timestamptz)",
		"INSERT INTO users VALUES ('64bd0cb5-d256-49b9-868e-31d3ee5e6d45', 'خریدار نمونه')",
		`INSERT INTO product_reviews VALUES
			('1', 'product-a', 'historical-author', 3, 'second', 'published', '2026-01-02T00:00:00Z'),
			('2', 'product-a', '64bd0cb5-d256-49b9-868e-31d3ee5e6d45', 5, 'first', 'published', '2026-01-02T00:00:00Z'),
			('3', 'product-a', 'hidden-author', 1, 'hidden', 'hidden', '2026-01-03T00:00:00Z'),
			('4', 'product-b', 'another-author', 1, 'different product', 'published', '2026-01-03T00:00:00Z')`,
	} {
		opsExec(t, db, statement)
	}

	repo := NewCatalogRepository(db)
	for _, tc := range []struct {
		offset int
		id     string
		name   string
		body   string
	}{
		{offset: 0, id: "2", name: "خریدار نمونه", body: "first"},
		{offset: 1, id: "1", name: "", body: "second"},
		{offset: 100},
	} {
		items, count, rating, err := repo.GetPublishedReviewPage(context.Background(), "product-a", 1, tc.offset)
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 || rating != 4 {
			t.Fatalf("offset %d: count %d, rating %v", tc.offset, count, rating)
		}
		if tc.id == "" {
			if len(items) != 0 {
				t.Fatalf("offset past last review: got %+v", items)
			}
			continue
		}
		if len(items) != 1 || items[0].ID != tc.id || items[0].DisplayName != tc.name || items[0].Body != tc.body {
			t.Fatalf("offset %d: unexpected published review %+v", tc.offset, items)
		}
	}
}
