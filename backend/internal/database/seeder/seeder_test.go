package seeder

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	_ "github.com/lib/pq"
)

func TestFailClosedEnvironmentGuard(t *testing.T) {
	cases := []struct {
		env     string
		allowed bool
	}{
		// Allowed Environments
		{"development", true},
		{"dev", true},
		{"test", true},
		{"local", true},
		// Normalized Casing Variations
		{"DEVELOPMENT", true},
		{" Dev ", true},
		{"Local", true},

		// Blocked Production Variants
		{"production", false},
		{"prod", false},
		{"staging", false},
		{"live", false},
		{"prd", false},
		{"", false},
		{"   ", false},
		{"unknown", false},
		{"production-live", false},
		{"prod-cluster", false},
		{"k8s-prod", false},
		{"stage-1", false},
	}

	for _, c := range cases {
		isDev := IsDevEnvironment(c.env)
		if isDev != c.allowed {
			t.Errorf("IsDevEnvironment(%q) = %v; expected %v", c.env, isDev, c.allowed)
		}

		// Verify that Seeder methods enforce the exact same fail-closed policy
		s := NewSeeder(nil, c.env)
		ctx := context.Background()

		errDev := s.SeedDev(ctx)
		errClean := s.CleanDev(ctx)
		errReset := s.ResetDev(ctx)

		if c.allowed {
			// In allowed envs, it should not fail on guard (it may fail on nil db, but NOT on security guard)
			if errDev != nil && errDev.Error()[:18] == "security violation" {
				t.Errorf("SeedDev unexpectedly blocked for allowed env %q", c.env)
			}
			if errClean != nil && errClean.Error()[:18] == "security violation" {
				t.Errorf("CleanDev unexpectedly blocked for allowed env %q", c.env)
			}
			if errReset != nil && errReset.Error()[:18] == "security violation" {
				t.Errorf("ResetDev unexpectedly blocked for allowed env %q", c.env)
			}
		} else {
			// In blocked envs, it MUST fail with security violation
			if errDev == nil {
				t.Errorf("SeedDev expected security violation for env %q, got nil", c.env)
			}
			if errClean == nil {
				t.Errorf("CleanDev expected security violation for env %q, got nil", c.env)
			}
			if errReset == nil {
				t.Errorf("ResetDev expected security violation for env %q, got nil", c.env)
			}
		}
	}
}

func TestLiveSeederIdempotencyAndReset(t *testing.T) {
	// Live integration test with postgres container
	t.Setenv("ADMIN_PASSWORD", "TestAdmin!2026")
	t.Setenv("FINANCE_PASSWORD", "TestFinance!2026")
	t.Setenv("LOGISTICS_PASSWORD", "TestLogistics!2026")
	t.Setenv("WAREHOUSE_PASSWORD", "TestWarehouse!2026")
	t.Setenv("SUPPORT_PASSWORD", "TestSupport!2026")
	t.Setenv("SUPPLIER_PASSWORD", "TestSupplier!2026")
	dbURL := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	db, err := testdb.Open(t, dbURL)
	if err != nil {
		t.Skipf("Skipping live database seeder test: %v", err)
		return
	}
	defer db.Close()
	if os.Getenv("SELLERSHO_TEST_SCHEMA_ONLY") != "1" {
		t.Fatal("seeder lifecycle requires an empty current-schema fixture: python scripts/test-isolated.py --schema-only -- go test ./internal/database/seeder -count=1")
	}
	var superuser, createdb, createrole bool
	if err := db.QueryRow(`SELECT rolsuper, rolcreatedb, rolcreaterole FROM pg_roles WHERE rolname = current_user`).Scan(&superuser, &createdb, &createrole); err != nil {
		t.Fatal(err)
	}
	if superuser || createdb || createrole {
		t.Fatal("seeder fixture must use the guarded non-superuser test role")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Skipf("Database unreachable, skipping integration test: %v", err)
		return
	}

	s := NewSeeder(db, "development")

	// 1. Run SeedBootstrap
	if err := s.SeedBootstrap(ctx); err != nil {
		t.Fatalf("SeedBootstrap failed: %v", err)
	}

	// 2. Run SeedDev (First pass)
	if err := s.SeedDev(ctx); err != nil {
		t.Fatalf("First SeedDev failed: %v", err)
	}

	var productCount1, storeCount1 int
	_ = db.QueryRowContext(ctx, "SELECT count(*) FROM products").Scan(&productCount1)
	_ = db.QueryRowContext(ctx, "SELECT count(*) FROM stores").Scan(&storeCount1)
	if productCount1 == 0 || storeCount1 == 0 {
		t.Fatal("SeedDev did not populate products and stores")
	}

	// 3. Run SeedDev AGAIN (Test Idempotency: seed -> seed)
	if err := s.SeedDev(ctx); err != nil {
		t.Fatalf("Second SeedDev (idempotency check) failed: %v", err)
	}

	var productCount2, storeCount2 int
	_ = db.QueryRowContext(ctx, "SELECT count(*) FROM products").Scan(&productCount2)
	_ = db.QueryRowContext(ctx, "SELECT count(*) FROM stores").Scan(&storeCount2)

	if productCount1 != productCount2 || storeCount1 != storeCount2 {
		t.Errorf("Idempotency violation! Counts changed on second run: products %d->%d, stores %d->%d",
			productCount1, productCount2, storeCount1, storeCount2)
	}

	// The current schema forbids ledger TRUNCATE even when the ledger is empty.
	// The old test expected forbidden cleanup to succeed. Preserve that protection
	// and dispose of the entire isolated database through the runner instead.
	snapshot := func() map[string]int64 {
		t.Helper()
		rows, err := db.QueryContext(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = current_schema() ORDER BY tablename`)
		if err != nil {
			t.Fatal(err)
		}
		var tables []string
		for rows.Next() {
			var table string
			if err := rows.Scan(&table); err != nil {
				t.Fatal(err)
			}
			tables = append(tables, table)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		counts := make(map[string]int64, len(tables))
		for _, table := range tables {
			var count int64
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			counts[table] = count
		}
		return counts
	}
	before := snapshot()
	for name, operation := range map[string]func(context.Context) error{"CleanDev": s.CleanDev, "ResetDev": s.ResetDev} {
		err := operation(ctx)
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%s must refuse immutable-ledger truncation, got %v", name, err)
		}
		if after := snapshot(); !reflect.DeepEqual(before, after) {
			t.Fatalf("%s changed fixture table counts despite ledger guard: before=%v after=%v", name, before, after)
		}
	}
}
