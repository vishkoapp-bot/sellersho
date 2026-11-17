package repository

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

// Unlike the purpose-built domain fixtures, this report gate uses the actual
// migrated tables, including UUID order_items.id and TEXT capture identities.
func TestCancellationAuditReportMigratedSchema(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	suffix := uuid.NewString()
	productID, supplierID := "inv01-p-"+suffix, "inv01-s-"+suffix
	capturedOrder, legacyOrder, controlOrder := "inv01-a-"+suffix, "inv01-b-"+suffix, "inv01-c-"+suffix
	capturedItem, legacyItem, controlItem := uuid.NewString(), uuid.NewString(), uuid.NewString()
	t.Cleanup(func() {
		for _, id := range []string{capturedOrder, legacyOrder, controlOrder} {
			db.Exec(`DELETE FROM order_inventory_captures WHERE order_id=$1`, id)
			db.Exec(`DELETE FROM orders WHERE id=$1`, id)
		}
		db.Exec(`DELETE FROM products WHERE id=$1`, productID)
		db.Exec(`DELETE FROM suppliers WHERE id=$1`, supplierID)
	})
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO suppliers(id,name) VALUES($1,'audit fixture')`, supplierID)
	exec(`INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock) VALUES($1,'audit fixture',$1,$2,100,50,10,17)`, productID, supplierID)
	for _, row := range []struct {
		order, item        string
		capture, warehouse bool
	}{{capturedOrder, capturedItem, true, false}, {legacyOrder, legacyItem, false, true}, {controlOrder, controlItem, true, false}} {
		exec(`INSERT INTO orders(id,order_number,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,status,payment_status,inventory_captured,shipped_at) VALUES($1,$2,'audit',300,300,'fixture','09000000000','fixture','fixture','fixture','0000000000',$1,'cancelled','cancelled',true,CASE WHEN $3 THEN now() ELSE NULL END)`, row.order, "AUD-"+row.item[:12], row.warehouse)
		exec(`INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price) VALUES($1,$2,$3,3,50,10,40,100)`, row.item, row.order, productID)
		if row.capture {
			exec(`INSERT INTO order_inventory_captures(order_item_id,order_id,product_id,quantity,restored_quantity,inventory_mode) VALUES($1,$2,$3,3,3,'legacy')`, row.item, row.order, productID)
		}
	}
	exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,shipping_address,customer_phone,customer_name,status,shipped_at) VALUES($1,$2,$3,'{}','09000000000','fixture','failed',now())`, "inv01-leg-"+suffix, capturedOrder, supplierID)
	snapshot := func() string {
		t.Helper()
		var value string
		err := db.QueryRowContext(ctx, `SELECT jsonb_build_object(
 'orders',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM orders o WHERE id IN ($1,$2,$3)),
 'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM order_items i WHERE order_id IN ($1,$2,$3)),
 'captures',(SELECT jsonb_agg(to_jsonb(c) ORDER BY order_item_id) FROM order_inventory_captures c WHERE order_id IN ($1,$2,$3)),
 'shipments',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM supplier_shipments s WHERE order_id IN ($1,$2,$3)),
 'product',(SELECT to_jsonb(p) FROM products p WHERE id=$4))::text`, capturedOrder, legacyOrder, controlOrder, productID).Scan(&value)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	script, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "audit-cancelled-dispatched.sql"))
	if err != nil {
		t.Fatal(err)
	}
	// Execute the complete delivered SQL, including BEGIN READ ONLY and COMMIT,
	// on one connection. Any query error rolls back its explicit transaction.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	rows, err := conn.QueryContext(ctx, string(script))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := map[string]string{}
	for {
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			ptrs := make([]any, len(columns))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			report := map[string]any{}
			for i, c := range columns {
				v := values[i]
				if b, ok := v.([]byte); ok {
					v = string(b)
				}
				report[c] = v
			}
			id, _ := report["order_id"].(string)
			if id != capturedOrder && id != legacyOrder && id != controlOrder {
				continue
			}
			encoded, _ := json.Marshal(report)
			t.Logf("reconciliation row: %s", encoded)
			if id == controlOrder {
				t.Fatal("undispatched control appeared in reconciliation")
			}
			evidence, _ := report["evidence"].(string)
			found[id] = evidence
			if id == capturedOrder {
				if report["order_item_id"] != capturedItem || report["restored_quantity"] != int64(3) || !strings.Contains(report["dispatched_supplier_legs"].(string), "inv01-leg-"+suffix) {
					t.Fatalf("incomplete capture report: %s", encoded)
				}
			}
			if id == legacyOrder {
				if report["order_item_id"] != legacyItem || report["restored_quantity"] != nil {
					t.Fatalf("legacy evidence invented capture: %s", encoded)
				}
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if !rows.NextResultSet() {
			break
		}
	}
	if found[capturedOrder] != "recorded_restore_after_dispatch" || found[legacyOrder] != "legacy_capture_restore_unproven" {
		t.Fatalf("missing report evidence: %#v", found)
	}
	if after := snapshot(); after != before {
		t.Fatal("read-only report changed stock, capture or order state")
	}
}
