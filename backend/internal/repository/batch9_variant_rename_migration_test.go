package repository

import (
	"os"
	"os/exec"
	"testing"
)

func TestBatch9VariantRenameMigration(t *testing.T) {
	db := fin014FixtureDB(t)
	if _, err := db.Exec(`
		-- Allow a historical NULL only in this disposable fixture.
		ALTER TABLE product_variants ALTER COLUMN attributes DROP NOT NULL;
		INSERT INTO products (id, title, slug, original_price, supplier_base_cost, platform_margin, schema_version, product_type, attribute_schema)
		VALUES ('batch9-variable', 'Batch 9 variable', 'batch9-variable', 100, 80, 0, 2, 'variable', '{}'),
		       ('batch9-simple', 'Batch 9 simple', 'batch9-simple', 100, 80, 0, 2, 'simple', '{}'),
		       ('batch9-legacy', 'Batch 9 legacy', 'batch9-legacy', 100, 80, 0, 0, 'legacy', '{}');

		INSERT INTO product_variants (product_id, spnc, variant_name, attributes)
		VALUES ('batch9-variable', 'BATCH9ATTR', 'پیش‌فرض', '{"size":"بزرگ","color":"زرد"}'),
		       ('batch9-variable', 'BATCH9EMPTY', 'پیش‌فرض', '{}'),
		       ('batch9-simple', 'BATCH9SIMPLE', 'پیش‌فرض', '{"color":"زرد"}'),
		       ('batch9-legacy', 'BATCH9LEGACY', 'پیش‌فرض', '{"color":"زرد"}'),
		       ('batch9-legacy', 'BATCH9NAMED', 'نگین‌دار', '{}'),
		       ('batch9-variable', 'BATCH9NULL', 'پیش‌فرض', NULL),
		       ('batch9-variable', 'BATCH9BLANK', 'پیش‌فرض', '{"size":""}'),
		       ('batch9-variable', 'BATCH9MIXED', 'پیشفرض', '{"color":"زرد","size":""}');

		DELETE FROM schema_migrations WHERE version = '202611230001_batch9_variant_rename.up.sql';
	`); err != nil {
		t.Fatalf("seed pre-migration fixtures: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`
			DELETE FROM products WHERE id IN ('batch9-variable', 'batch9-simple', 'batch9-legacy');
			ALTER TABLE product_variants ALTER COLUMN attributes SET NOT NULL;
		`); err != nil {
			t.Errorf("delete migration fixtures: %v", err)
		}
	})

	// The disposable provisioner already migrated the empty database. Replay
	// the data-only migration with the same CLI after seeding historical rows.
	cmd := exec.Command("go", "run", "./cmd/migrate", "-dir", "migrations")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "DATABASE_URL="+os.Getenv("OPERATIONS_TEST_DATABASE_URL"))
	if err := cmd.Run(); err != nil {
		t.Fatalf("replay Batch 9 with migration CLI: %v", err)
	}

	for spnc, want := range map[string]string{
		"BATCH9ATTR": "زرد / بزرگ", "BATCH9EMPTY": "پیش‌فرض", "BATCH9SIMPLE": "پیش‌فرض",
		"BATCH9LEGACY": "زرد", "BATCH9NAMED": "نگین‌دار",
		"BATCH9NULL": "پیش‌فرض", "BATCH9BLANK": "پیش‌فرض", "BATCH9MIXED": "زرد",
	} {
		var name string
		if err := db.QueryRow(`SELECT variant_name FROM product_variants WHERE spnc = $1`, spnc).Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name != want {
			t.Errorf("variant %s name = %q, want %q", spnc, name, want)
		}
	}
}
