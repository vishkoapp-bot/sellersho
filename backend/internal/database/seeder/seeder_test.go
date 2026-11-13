package seeder

import (
	"context"
	"os"
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

	// 4. Test CleanDev (clean -> verify 0)
	if err := s.CleanDev(ctx); err != nil {
		t.Fatalf("CleanDev failed: %v", err)
	}

	var productCountClean int
	_ = db.QueryRowContext(ctx, "SELECT count(*) FROM products").Scan(&productCountClean)
	if productCountClean != 0 {
		t.Errorf("Expected 0 products after CleanDev, got %d", productCountClean)
	}

	// 5. Test ResetDev (reset -> verify fully populated again)
	if err := s.ResetDev(ctx); err != nil {
		t.Fatalf("ResetDev failed: %v", err)
	}

	var productCountReset int
	_ = db.QueryRowContext(ctx, "SELECT count(*) FROM products").Scan(&productCountReset)
	if productCountReset != productCount1 {
		t.Errorf("Expected %d products after ResetDev, got %d", productCount1, productCountReset)
	}
}
