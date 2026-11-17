package repository

import (
	"context"
	"os"
	"testing"

	"github.com/drup/platform/internal/testdb"
)

func TestRequestedAttributesPublishAtomically(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_FLOW_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated supplier database required")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var slug, pid string
	if err = tx.QueryRow(`SELECT slug FROM categories LIMIT 1`).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(`SELECT id FROM products LIMIT 1`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	data := map[string]interface{}{"requestedAttributes": map[string]interface{}{"audit requested material": "audit requested value"}}
	for i := 0; i < 2; i++ {
		if err = approveRequestedAttributes(context.Background(), tx, data, slug, pid); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM product_attribute_values p JOIN dk_attributes a ON a.dk_attr_id=p.dk_attribute_id JOIN dk_attribute_values v ON v.dk_attr_id=p.dk_attribute_id AND v.dk_value_id=p.dk_value_id JOIN dk_category_attribute_values c ON c.dk_attr_id=a.dk_attr_id AND c.dk_value_id=v.dk_value_id WHERE p.product_id=$1 AND a.title='audit requested material' AND v.title_fa='audit requested value'`, pid).Scan(&count); err != nil || count != 1 {
		t.Fatalf("publication count %d: %v", count, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM dk_attributes WHERE title='audit requested material'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback leaked facet: %d %v", count, err)
	}
}
