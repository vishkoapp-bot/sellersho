package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/config"
	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func openSupplyTierTestDB(t *testing.T) (*sql.DB, *repository.SupplierRepository, string) {
	t.Helper()
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Skipf("database configuration unavailable: %v", err)
	}
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		t.Skipf("database unavailable: %v", err)
	}
	productID := "tst-tier-" + fmt.Sprintf("%d", time.Now().UnixNano())
	productID = productID[:minInt(len(productID), 48)]
	_, err = db.Exec(`
		INSERT INTO products (id, title, slug, category_slug, supplier_id, original_price, supplier_base_cost, platform_margin, stock)
		VALUES ($1, $2, $3, 'digital', 'sup-technolife', 2000000, 1500000, 100000, 10)
		ON CONFLICT (id) DO UPDATE SET stock = EXCLUDED.stock, supplier_base_cost = EXCLUDED.supplier_base_cost`, productID, productID, productID)
	if err != nil {
		db.Close()
		t.Skipf("test product could not be created: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM products WHERE id = $1`, productID)
		_ = db.Close()
	})
	return db, repository.NewSupplierRepository(db), productID
}

func TestUpdateSupplyOption_InvalidValue(t *testing.T) {
	_, repo, productID := openSupplyTierTestDB(t)
	err := repo.UpdateProductSupplyOption(context.Background(), "sup-technolife", productID, "invalid_option")
	if err == nil || !strings.Contains(err.Error(), "invalid supply_option") {
		t.Fatalf("expected invalid supply_option error, got %v", err)
	}
}

func TestUpdateSupplyOption_ValidValue(t *testing.T) {
	db, repo, productID := openSupplyTierTestDB(t)
	if err := repo.UpdateProductSupplyOption(context.Background(), "sup-technolife", productID, "3hour_local"); err != nil {
		t.Fatalf("update supply option: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT supply_option FROM products WHERE id = $1`, productID).Scan(&got); err != nil {
		t.Fatalf("read supply option: %v", err)
	}
	if got != "3hour_local" {
		t.Fatalf("expected 3hour_local, got %s", got)
	}
}
