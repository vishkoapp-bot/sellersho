package repository

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

const galleryFixtureVariant = "376cf52d-65ae-4120-af04-7925d2b8adf7"

func supplierGalleryFixture(t *testing.T) (*sql.DB, *SupplierRepository, string) {
	t.Helper()
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	root, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "supplier_gallery_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pq.QuoteIdentifier(schema)
	if _, err := root.Exec("CREATE SCHEMA " + quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.Exec("DROP SCHEMA " + quoted + " CASCADE") })
	connection, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	params := connection.Query()
	params.Set("search_path", schema)
	params.Set("application_name", schema)
	connection.RawQuery = params.Encode()
	db, err := testdb.Open(t, connection.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, supplier_id VARCHAR(50), image_url TEXT, updated_at TIMESTAMPTZ NOT NULL DEFAULT now());
		CREATE TABLE product_variants (id UUID PRIMARY KEY, product_id VARCHAR(50) NOT NULL REFERENCES products(id));
		CREATE TABLE product_images (id UUID DEFAULT gen_random_uuid() PRIMARY KEY, product_id VARCHAR(50) NOT NULL REFERENCES products(id), image_url TEXT NOT NULL, sort_order INT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now());
		CREATE TABLE product_variant_images (id UUID DEFAULT gen_random_uuid() PRIMARY KEY, variant_id UUID NOT NULL REFERENCES product_variants(id), image_url TEXT NOT NULL, sort_order INT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now());
		INSERT INTO products (id, supplier_id, image_url) VALUES ('product', 'supplier', 'legacy-image');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO product_variants (id, product_id) VALUES ($1, 'product')`, galleryFixtureVariant); err != nil {
		t.Fatal(err)
	}
	return db, NewSupplierRepository(db), schema
}

func TestSupplierProductGalleryCoverFollowsGallery(t *testing.T) {
	db, repo, _ := supplierGalleryFixture(t)
	ctx := context.Background()
	checkCover := func(want string) {
		t.Helper()
		var actual string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(image_url, '') FROM products WHERE id='product'`).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if actual != want {
			t.Fatalf("cover in catalog and supplier listing = %q, want %q", actual, want)
		}
	}
	if _, err := repo.AddProductImages(ctx, "supplier", "product", nil); err != nil {
		t.Fatal(err)
	}
	checkCover("legacy-image")
	images, err := repo.AddProductImages(ctx, "supplier", "product", []string{"first", "second", "third"})
	if err != nil || len(images) != 3 {
		t.Fatalf("add gallery = %+v, %v", images, err)
	}
	checkCover("first")
	if err := repo.SetProductCoverImage(ctx, "supplier", "product", images[2].ID); err != nil {
		t.Fatal(err)
	}
	checkCover("third")
	if err := repo.ReorderProductImages(ctx, "supplier", "product", []string{images[1].ID, images[0].ID, images[2].ID}); err != nil {
		t.Fatal(err)
	}
	checkCover("second")
	if err := repo.DeleteProductImage(ctx, "supplier", "product", images[1].ID); err != nil {
		t.Fatal(err)
	}
	checkCover("first")
	if err := repo.DeleteProductImage(ctx, "supplier", "product", images[0].ID); err != nil {
		t.Fatal(err)
	}
	checkCover("third")
	if err := repo.DeleteProductImage(ctx, "supplier", "product", images[2].ID); err != nil {
		t.Fatal(err)
	}
	checkCover("")
}

func TestSupplierGalleryConcurrentUploadsRespectCap(t *testing.T) {
	for _, variant := range []bool{false, true} {
		name := "product"
		if variant {
			name = "variant"
		}
		t.Run(name, func(t *testing.T) {
			db, repo, schema := supplierGalleryFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			table, cap, full := "product_images", 8, ErrGalleryFull
			add := func(urls []string) ([]SupplierProductImage, error) {
				return repo.AddProductImages(ctx, "supplier", "product", urls)
			}
			if variant {
				table, cap, full = "product_variant_images", MaxVariantImages, ErrVariantGalleryFull
				add = func(urls []string) ([]SupplierProductImage, error) {
					return repo.AddVariantImages(ctx, "supplier", "product", galleryFixtureVariant, urls)
				}
			}
			seed := make([]string, cap-1)
			for i := range seed {
				seed[i] = "seed"
			}
			if _, err := add(seed); err != nil {
				t.Fatal(err)
			}
			// Make the race deterministic by holding inserts after the capacity
			// check. Both requests must wait before the first commit can proceed.
			if _, err := db.Exec(`
				CREATE FUNCTION hold_gallery_insert() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN PERFORM pg_advisory_xact_lock(1, hashtext(TG_TABLE_SCHEMA || TG_TABLE_NAME)); RETURN NEW; END $$;
				CREATE TRIGGER hold_gallery BEFORE INSERT ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION hold_gallery_insert();
			`); err != nil {
				t.Fatal(err)
			}
			holder, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback()
			if _, err := holder.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1, hashtext($1))`, schema+table); err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 2)
			for _, suffix := range []string{"left", "right"} {
				go func(image string) {
					_, err := add([]string{image})
					results <- err
				}(suffix)
			}
			for {
				var waiting int
				if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock'`, schema).Scan(&waiting); err != nil {
					t.Fatalf("wait for both uploads: %v", err)
				}
				if waiting == 2 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := holder.Rollback(); err != nil {
				t.Fatal(err)
			}
			first, second := <-results, <-results
			if !((first == nil && errors.Is(second, full)) || (second == nil && errors.Is(first, full))) {
				t.Fatalf("one upload should hit the gallery cap, got %v and %v", first, second)
			}
			var actual int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&actual); err != nil || actual != cap {
				t.Fatalf("gallery size = %d, want %d, err = %v", actual, cap, err)
			}
		})
	}
}
