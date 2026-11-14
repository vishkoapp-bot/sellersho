package repository

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestCapacitySupplierWarehouseGeographyMigrated(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	supplierID := "cap-geo-" + uuid.NewString()
	warehouseID := "cap-geo-warehouse-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO suppliers(id,name) VALUES($1,'Synthetic geo supplier')`, supplierID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM supplier_warehouses WHERE id=$1`, warehouseID)
		db.Exec(`DELETE FROM suppliers WHERE id=$1`, supplierID)
	})
	if _, err := db.Exec(`INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,'Synthetic unknown origin','Unknown','Unknown')`, warehouseID, supplierID); err != nil {
		t.Fatal(err)
	}
	repo := NewTapinRepository(db)
	unknown, err := repo.GetSupplierWarehouseGeo(context.Background(), warehouseID)
	if err != nil {
		t.Fatalf("migrated unknown geography read: %v", err)
	}
	if unknown == nil || unknown.TapinProvinceID != 0 || unknown.TapinCityID != 0 {
		t.Fatal("unknown geography was guessed")
	}
	for _, column := range []string{"tapin_province_id", "tapin_city_id"} {
		var kind, nullable string
		var defaultValue sql.NullString
		if err := db.QueryRow(`SELECT data_type,is_nullable,column_default FROM information_schema.columns WHERE table_schema='public' AND table_name='supplier_warehouses' AND column_name=$1`, column).Scan(&kind, &nullable, &defaultValue); err != nil {
			t.Fatal(err)
		}
		if kind != "integer" || nullable != "YES" || defaultValue.Valid {
			t.Fatalf("geography column compatibility: %s %s default=%t", kind, nullable, defaultValue.Valid)
		}
	}
	if _, err := db.Exec(`UPDATE supplier_warehouses SET tapin_province_id=11,tapin_city_id=1101 WHERE id=$1`, warehouseID); err != nil {
		t.Fatal(err)
	}
	known, err := repo.GetSupplierDefaultWarehouseGeo(context.Background(), supplierID)
	if err != nil || known == nil || known.ID != warehouseID || known.TapinProvinceID != 11 || known.TapinCityID != 1101 {
		t.Fatalf("explicit synthetic mapping did not round-trip: %v", err)
	}
	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "202611140007_supplier_warehouse_geography.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, rollbackErr := tx.Exec(string(down))
	tx.Rollback() // even a broken rollback guard cannot commit destructive test DDL
	if rollbackErr == nil || !strings.Contains(rollbackErr.Error(), "rollback requires audited reconciliation") {
		t.Fatal("rollback did not refuse recorded geography")
	}
	preserved, err := repo.GetSupplierWarehouseGeo(context.Background(), warehouseID)
	if err != nil || preserved == nil || preserved.TapinCityID != 1101 {
		t.Fatal("rollback attempt lost recorded geography")
	}
}
