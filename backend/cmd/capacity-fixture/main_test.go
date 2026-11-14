package main

import (
	"encoding/json"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/testdb"
	"os"
	"regexp"
	"testing"
	"time"
)

func TestCapacityDataFloor(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	if err := seed(db); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"users": 1000, "suppliers": 100, "stores": 100, "products": 10000, "product_variants": 50000, "orders": 100000} {
		var got int
		if err := db.QueryRow("SELECT count(*) FROM " + table + " WHERE " + prefixColumn(table) + " LIKE 'cap-%'").Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got < want {
			t.Fatalf("%s floor: got %d want >= %d", table, got, want)
		}
	}
	var stockUnits int64
	var attributedProducts int
	if err := db.QueryRow("SELECT count(DISTINCT product_id),COALESCE(sum(quantity),0) FROM inventory_balances WHERE product_id LIKE 'cap-product-%' AND status='sellable' AND supplier_warehouse_id IS NOT NULL").Scan(&attributedProducts, &stockUnits); err != nil {
		t.Fatal(err)
	}
	if attributedProducts != 10000 || stockUnits != 5000000 {
		t.Fatalf("attributed capacity stock missing: products=%d units=%d", attributedProducts, stockUnits)
	}
	seedAndVerifyHotSKU(t, db)
	seedAndVerifyStaff(t, db)
	if key := os.Getenv("CAPACITY_CARRIER_KEY"); key != "" {
		if os.Getenv("APP_ENV") != "test" || len(key) < 32 || !regexp.MustCompile(`^[a-f0-9]{24}$`).MatchString(os.Getenv("SELLERSHO_CAPACITY_RUN")) {
			t.Fatal("carrier fixture requires private test capability")
		}
		if _, err := db.Exec(`UPDATE suppliers SET tapin_token=$1,tapin_shop_id=replace(id,'cap-supplier-','cap-shop-') WHERE id LIKE 'cap-supplier-%'`, key); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE supplier_warehouses SET tapin_province_id=1,tapin_city_id=1 WHERE id LIKE 'cap-swh-%'; INSERT INTO tapin_geo_snapshots(kind,payload) VALUES('tree','[{"code":1,"title":"Tehran","cities":[{"code":1,"title":"Tehran"}]}]') ON CONFLICT(kind) DO UPDATE SET payload=excluded.payload`); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("CAPACITY_FINANCE_HISTORY") == "1" {
		seedAndVerifyFinancialHistory(t, db)
	}
	if target := os.Getenv("CAPACITY_SESSION_FILE"); target != "" {
		secret := os.Getenv("JWT_SECRET")
		if len(secret) < 32 {
			t.Fatal("fixture requires ephemeral JWT secret")
		}
		rows, err := db.Query("SELECT id::text,mobile,full_name,role FROM users WHERE full_name LIKE 'cap-customer-%' ORDER BY mobile LIMIT 1000")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		users := []map[string]string{}
		for rows.Next() {
			var id, mobile, name, role string
			if err := rows.Scan(&id, &mobile, &name, &role); err != nil {
				t.Fatal(err)
			}
			token, err := auth.NewJWTService(secret).GenerateToken(id, mobile, name, role, 3*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			users = append(users, map[string]string{"id": id, "mobile": mobile, "token": token, "role": role})
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		staffRows, err := db.Query(`SELECT u.id::text,u.mobile,u.full_name,u.role,COALESCE(s.id,''),COALESCE(p.id,'') FROM users u LEFT JOIN stores s ON s.owner_user_id=u.id LEFT JOIN suppliers p ON p.owner_user_id=u.id WHERE u.full_name LIKE 'cap-staff-%' OR u.full_name IN (SELECT 'cap-seller-'||i FROM generate_series(1,5)i) OR u.full_name IN (SELECT 'cap-supplier-user-'||i FROM generate_series(1,5)i) ORDER BY u.role,u.mobile`)
		if err != nil {
			t.Fatal(err)
		}
		defer staffRows.Close()
		staff := []map[string]string{}
		for staffRows.Next() {
			var id, mobile, name, role, storeID, supplierID string
			if err := staffRows.Scan(&id, &mobile, &name, &role, &storeID, &supplierID); err != nil {
				t.Fatal(err)
			}
			token, err := auth.NewJWTService(secret).GenerateToken(id, mobile, name, role, 3*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			staff = append(staff, map[string]string{"id": id, "mobile": mobile, "token": token, "role": role, "storeId": storeID, "supplierId": supplierID})
		}
		if err := staffRows.Err(); err != nil {
			t.Fatal(err)
		}
		if len(staff) != 20 {
			t.Fatalf("staff session cohort=%d want20", len(staff))
		}
		b, err := json.Marshal(map[string]any{"users": users, "staff": staff, "scope": "synthetic sessions; not startup login evidence"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func prefixColumn(table string) string {
	if table == "users" {
		return "full_name"
	}
	if table == "product_variants" {
		return "variant_name"
	}
	return "id"
}
