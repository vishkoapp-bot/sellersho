package repository

import (
	"context"
	"os"
	"testing"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestProductPublicationPreservesExplicitOwnedStockOrigin(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	supplier := "pub-" + uuid.NewString()[:20]
	if _, err := db.Exec(`INSERT INTO suppliers(id,name,approval_status) VALUES($1,'Publication origin fixture','approved')`, supplier); err != nil {
		t.Fatal(err)
	}
	warehouse := "pub-wh-" + uuid.NewString()[:20]
	if _, err := db.Exec(`INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,'Publication source','Test province','Test city')`, warehouse, supplier); err != nil {
		t.Fatal(err)
	}
	other := "other-" + uuid.NewString()[:20]
	if _, err := db.Exec(`INSERT INTO suppliers(id,name,approval_status) VALUES($1,'Other publication supplier','approved')`, other); err != nil {
		t.Fatal(err)
	}
	for _, seed := range []struct {
		id, owner string
		active    bool
	}{{warehouse + "-off", supplier, false}, {warehouse + "-foreign", other, true}} {
		if _, err := db.Exec(`INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name,is_active) VALUES($1,$2,$1,'Test','Test',$3)`, seed.id, seed.owner, seed.active); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, origin    string
		typed, accepted bool
	}{{"legacy", warehouse, false, true}, {"typed", warehouse, true, true}, {"inactive", warehouse + "-off", true, false}, {"foreign", warehouse + "-foreign", true, false}, {"unknown", "missing-publication-origin", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			var before int
			if err := db.QueryRow(`SELECT COUNT(*) FROM products WHERE supplier_id=$1`, supplier).Scan(&before); err != nil {
				t.Fatal(err)
			}
			data := map[string]interface{}{
				"title": "Explicit publication source", "categorySlug": "digital",
				"supplierBaseCost": float64(1000), "stock": float64(3),
				"supplierWarehouseId": tc.origin,
			}
			if tc.typed {
				data["schemaVersion"] = float64(2)
				data["productType"] = "simple"
				data["productSchema"] = map[string]interface{}{"version": float64(2), "type": "simple", "attributes": []interface{}{}}
				data["variants"] = []interface{}{map[string]interface{}{"id": uuid.NewString(), "selection": map[string]interface{}{}, "stock": float64(3), "supplier_base_cost": float64(1000), "is_active": true}}
			}
			proposal, err := repo.ProposeProduct(ctx, supplier, data)
			if err != nil {
				t.Fatal(err)
			}
			var decisions []FieldDecision
			for key, value := range ReviewLeaves(data) {
				decisions = append(decisions, FieldDecision{Key: key, Decision: "approved", ValueHash: ReviewValueHash(value)})
			}
			if err := repo.SaveFieldReview(ctx, proposal, "test-admin", decisions); err != nil {
				t.Fatal(err)
			}
			product, err := repo.ApproveProposal(ctx, proposal, "")
			if !tc.accepted {
				if err == nil {
					t.Fatal("invalid publication source was accepted")
				}
				var after int
				var status string
				if err := db.QueryRow(`SELECT COUNT(*) FROM products WHERE supplier_id=$1`, supplier).Scan(&after); err != nil || after != before {
					t.Fatal("failed approval persisted a partial product", err)
				}
				if err := db.QueryRow(`SELECT status FROM pending_approvals WHERE id=$1`, proposal).Scan(&status); err != nil || status != "pending" {
					t.Fatal("failed approval changed proposal status", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var origin string
			var quantity, incorrect int
			if err := db.QueryRow(`SELECT origin_supplier_warehouse_id FROM products WHERE id=$1`, product).Scan(&origin); err != nil || origin != warehouse {
				t.Fatalf("publication origin=%q error=%v", origin, err)
			}
			if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0),COUNT(*) FILTER(WHERE supplier_warehouse_id IS DISTINCT FROM $2 OR location_key<>$3) FROM inventory_balances WHERE product_id=$1 AND status='sellable'`, product, warehouse, "supplier-warehouse:"+warehouse).Scan(&quantity, &incorrect); err != nil || quantity != 3 || incorrect != 0 {
				t.Fatalf("publication stock=%d incorrect origins=%d error=%v", quantity, incorrect, err)
			}
		})
	}
}
