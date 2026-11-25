package repository

import (
	"os"
	"os/exec"
	"testing"
)

func TestBatch9VariantRenameMigration(t *testing.T) {
	db := fin014FixtureDB(t)
	// Rewind migrations 202611240001, 202611230004, and 202611230002 on this disposable,
	// empty serialization schema before replaying the CLI. Deleting only
	// Batch 9 history fails the ordering guard.
	var units int
	if err := db.QueryRow(`SELECT COUNT(*) FROM product_units`).Scan(&units); err != nil || units != 0 {
		t.Fatalf("migration rehearsal requires empty units: count=%d err=%v", units, err)
	}
	down, err := os.ReadFile("../../migrations/202611230002_spncx_product_units.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	storageReceiptDown, err := os.ReadFile("../../migrations/202611230004_spncx_inbound_storage_receipt.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`ALTER TABLE IF EXISTS platform_settings DROP COLUMN IF EXISTS early_withdrawal_minimum_toman`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DELETE FROM schema_migrations WHERE version='202611240001_seller_early_withdrawal_minimum.up.sql'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(string(storageReceiptDown)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DELETE FROM schema_migrations WHERE version='202611230004_spncx_inbound_storage_receipt.up.sql'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DELETE FROM schema_migrations WHERE version='202611230002_spncx_product_units.up.sql'`); err != nil {
		t.Fatal(err)
	}
	var removed bool
	if err := tx.QueryRow(`SELECT to_regclass('product_units') IS NULL AND NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='inventory_balances' AND column_name='unit_tracked')`).Scan(&removed); err != nil || !removed {
		t.Fatalf("SPNCX down schema: %t %v", removed, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
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
	// This migration test also runs in source archives without VCS metadata.
	cmd := exec.Command("go", "run", "-buildvcs=false", "./cmd/migrate", "-dir", "migrations")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "DATABASE_URL="+os.Getenv("OPERATIONS_TEST_DATABASE_URL"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("replay Batch 9 with migration CLI: %v\n%s", err, output)
	}

	var restored bool
	if err := db.QueryRow(`SELECT to_regclass('product_units') IS NOT NULL AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='inventory_balances' AND column_name='unit_tracked')
		AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='platform_settings' AND column_name='early_withdrawal_minimum_toman')`).Scan(&restored); err != nil || !restored {
		t.Fatalf("SPNCX up schema: %t %v", restored, err)
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
