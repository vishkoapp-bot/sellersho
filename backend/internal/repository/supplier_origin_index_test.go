package repository

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Copy the actual migrated UUID columns and indexes, not a text-ID substitute.
// Everything is isolated in a private schema and rolled back, including ANALYZE.
// TEST-02 round 5: do NOT copy the definitions with LIKE public — in the
// shared DB a destructive fixture rebuilds public.order_items with a TEXT
// id, so LIKE copies the polluted TEXT type and the UUID index proof
// fails (the planner must cast). Define the canonical migrated types
// explicitly (UUID ids, the idx_order_item_origins_order index) so the
// proof tests the query, not the shared DB's current rebuild shape.
func TestSupplierOriginAuthorityUsesCanonicalUUIDIndex(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	schema := pq.QuoteIdentifier("origin_index_" + uuid.NewString()[:8])
	for _, query := range []string{
		`CREATE SCHEMA ` + schema,
		`CREATE TABLE ` + schema + `.orders (id TEXT PRIMARY KEY, payment_status TEXT, status TEXT)`,
		`CREATE TABLE ` + schema + `.order_items (id UUID PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT, wholesale_cost BIGINT, platform_fee BIGINT, seller_margin BIGINT, final_unit_price BIGINT)`,
		`CREATE TABLE ` + schema + `.order_item_origins (id TEXT PRIMARY KEY, order_id TEXT, order_item_id UUID, leg_index INT, origin_type TEXT, supplier_id TEXT, origin_label TEXT, quantity INT)`,
		`CREATE INDEX idx_order_item_origins_order ON ` + schema + `.order_item_origins (order_id, order_item_id, leg_index)`,
		`SET LOCAL search_path = ` + schema,
		`INSERT INTO orders(id,payment_status,status) VALUES('origin-index-order','paid','delivered')`,
		`INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price)
 SELECT md5('origin-index-'||i)::uuid,'origin-index-order','origin-index-product',1,100,0,0,100 FROM generate_series(1,5000) i`,
		`INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,origin_label,quantity)
 SELECT 'origin-index-'||i,'origin-index-order',md5('origin-index-'||i)::uuid,0,'legacy','origin-index-supplier','Synthetic',1 FROM generate_series(1,5000) i`,
		`ANALYZE orders`, `ANALYZE order_items`, `ANALYZE order_item_origins`,
		// This is an index-eligibility proof, not a claimed production plan.
		// A full index scan with a casted Filter still fails the Index Cond check.
		`SET LOCAL enable_seqscan = off`, `SET LOCAL statement_timeout = '3s'`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	query := `SELECT ` + supplierTermFor(supplierTermEarned, "$1")
	var raw []byte
	if err := tx.QueryRowContext(ctx, `EXPLAIN (FORMAT JSON) `+query, "origin-index-supplier").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	var indexedLookup bool
	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if relation, _ := node["Relation Name"].(string); relation == "order_item_origins" {
				condition, _ := node["Index Cond"].(string)
				if strings.Contains(condition, "order_item_id") && !strings.Contains(condition, "::text") {
					indexedLookup = true
				}
			}
			for _, child := range node {
				visit(child)
			}
		case []any:
			for _, child := range node {
				visit(child)
			}
		}
	}
	visit(plan)
	if !indexedLookup {
		t.Fatal("immutable supplier authority cannot use the canonical UUID index for each item; casted full scans are not an indexed lookup")
	}
	var earned int64
	if err := tx.QueryRowContext(ctx, query, "origin-index-supplier").Scan(&earned); err != nil {
		t.Fatal(err)
	}
	if earned != 500000 {
		t.Fatalf("immutable earned=%d, want500000", earned)
	}
}
