package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestInboundReviewASNCumulativeReceipt(t *testing.T) {
	_, schema, _, _ := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	ctx := context.Background()
	for _, q := range []string{
		`ALTER TABLE inbound_shipments ADD COLUMN IF NOT EXISTS supplier_id TEXT,ADD COLUMN IF NOT EXISTS items JSONB,ADD COLUMN IF NOT EXISTS scanned_items JSONB DEFAULT '[]',ADD COLUMN IF NOT EXISTS started_receiving_at TIMESTAMPTZ,ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ`,
		`CREATE TABLE inbound_receiving_logs(id TEXT PRIMARY KEY,inbound_id TEXT,staff_user_id TEXT,action TEXT,scanned_item JSONB,created_at TIMESTAMPTZ DEFAULT now() )`,
		`INSERT INTO products(id,supplier_id,stock,schema_version) VALUES('legacy','s',0,0)`,
		`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled,supplier_warehouse_id) VALUES('asn-source','legacy','s','supplier','supplier','supplier-warehouse:sw1','sellable',5,TRUE,'sw1')`,
		`INSERT INTO inbound_shipments(id,status,warehouse_id,supplier_id,items) VALUES('INB-cumulative','in_transit','w1','s','[{"product_id":"legacy","quantity":5}]')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := lockInventoryParentsTx(ctx, tx, []inventoryCaptureItem{{ProductID: "legacy"}}); err != nil {
		t.Fatal(err)
	}
	if err := reserveInboundTransferTx(ctx, tx, "INB-cumulative", "s", "w1", []InboundTransferLine{{ProductID: "legacy", Quantity: 5, SourceKind: "transfer", Source: InventoryOrigin{Type: "supplier", SupplierID: "s", SupplierWarehouseID: "sw1"}}}); err != nil {
		t.Fatal(err)
	}
	if err := dispatchInboundTransferTx(ctx, tx, "INB-cumulative"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	repo := NewWarehouseRepository(db)
	for _, delta := range []int{2, 3, 0} {
		if delta > 0 {
			if err := repo.StartReceiving(ctx, "INB-cumulative", "staff"); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.ScanItem(ctx, "INB-cumulative", map[string]interface{}{"productId": "legacy", "quantity": float64(delta)}, "staff"); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.CompleteInbound(ctx, "INB-cumulative", "staff"); err != nil {
			t.Fatalf("cumulative completion after delta %d: %v", delta, err)
		}
		if delta == 2 {
			var before, after int
			if err := db.QueryRow(`SELECT COUNT(*) FROM inbound_receiving_logs`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if err := repo.CompleteInbound(ctx, "INB-cumulative", "staff"); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM inbound_receiving_logs`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatalf("unchanged partial replay appended receipt effects: logs %d -> %d", before, after)
			}
			var status string
			if err := db.QueryRow(`SELECT status FROM inbound_shipments WHERE id='INB-cumulative'`).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "receiving" {
				t.Fatalf("partial ASN prematurely closed: %s", status)
			}
		}

	}
	if _, err := repo.ScanItem(ctx, "INB-cumulative", map[string]interface{}{"productId": "legacy", "quantity": float64(1)}, "staff"); err == nil {
		if err := repo.CompleteInbound(ctx, "INB-cumulative", "staff"); err == nil {
			t.Fatal("sixth ASN unit accepted")
		}
	}
	var n int
	if err := db.QueryRow(`SELECT SUM(quantity) FROM inventory_balances WHERE product_id='legacy'`).Scan(&n); err != nil || n != 5 {
		t.Fatalf("ASN conservation=%d err=%v", n, err)
	}
	var received int
	if err := db.QueryRow(`SELECT received_quantity FROM inbound_inventory_lines WHERE shipment_id='INB-cumulative'`).Scan(&received); err != nil || received != 5 {
		t.Fatalf("ASN cumulative=%d err=%v", received, err)
	}
}

func waitInboundBlocked(t *testing.T, db *sql.DB, pid int) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		var n int
		if err := db.QueryRow(`SELECT cardinality(pg_blocking_pids($1))`, pid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("candidate did not reach controlled blocker")
}
func TestInboundReviewGlobalBalanceOrder(t *testing.T) {
	for _, mode := range []string{"booking", "receipt-guards"} {
		t.Run(mode, func(t *testing.T) {
			_, schema, red, blue := skuInventoryFixture(t)
			db := skuScopedDB(t, schema)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			// Keep immutable unit references intact. Assign the globally later lot
			// to product p and the earlier lot to q instead of rewriting lot IDs.
			var later, earlier string
			if err := db.QueryRow(`SELECT id FROM inventory_balances WHERE variant_id=$1`, red).Scan(&later); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT id FROM inventory_balances WHERE variant_id=$1`, blue).Scan(&earlier); err != nil {
				t.Fatal(err)
			}
			if later < earlier {
				later, earlier, red, blue = earlier, later, blue, red
			}
			if _, err := db.Exec(`INSERT INTO products(id,supplier_id,stock,schema_version) VALUES('q','s',0,2)`); err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{`UPDATE product_variants SET product_id='q' WHERE id=$1`, `UPDATE inventory_balances SET product_id='q' WHERE variant_id=$1`, `UPDATE product_units SET product_id='q' WHERE variant_id=$1`} {
				if _, err := db.Exec(q, blue); err != nil {
					t.Fatal(err)
				}
			}
			shipment := uuid.NewString()
			if mode == "receipt-guards" {
				if _, err := db.Exec(`INSERT INTO shipments(id,status) VALUES($1,'draft')`, shipment); err != nil {
					t.Fatal(err)
				}
				for i, l := range []struct{ product, variant string }{{"p", red}, {"q", blue}} {
					id := "line-p"
					if i == 1 {
						id = "line-q"
					}
					if _, err := db.Exec(`INSERT INTO inbound_inventory_lines(id,shipment_id,supplier_id,warehouse_id,product_id,variant_id,source_kind,source_warehouse_id,quantity) VALUES($1,$2,'s','w1',$3,$4,'transfer','sw1',1)`, id, shipment, l.product, l.variant); err != nil {
						t.Fatal(err)
					}
				}
			}
			blocker, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			if _, err := blocker.Exec(`SELECT id FROM inventory_balances WHERE id=$1 FOR UPDATE`, earlier); err != nil {
				t.Fatal(err)
			}
			candidate, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer candidate.Rollback()
			var pid int
			if err := candidate.QueryRow(`SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				if mode == "receipt-guards" {
					done <- lockInboundShipmentTx(ctx, candidate, shipment)
					return
				}
				if err := lockInventoryParentsTx(ctx, candidate, []inventoryCaptureItem{{ProductID: "p", VariantID: red}, {ProductID: "q", VariantID: blue}}); err != nil {
					done <- err
					return
				}
				done <- reserveInboundTransferTx(ctx, candidate, shipment, "s", "w1", []InboundTransferLine{{ProductID: "p", VariantID: red, Quantity: 1, SourceKind: "transfer", Source: InventoryOrigin{Type: "supplier", SupplierID: "s", SupplierWarehouseID: "sw1"}}, {ProductID: "q", VariantID: blue, Quantity: 1, SourceKind: "transfer", Source: InventoryOrigin{Type: "supplier", SupplierID: "s", SupplierWarehouseID: "sw1"}}})
			}()
			waitInboundBlocked(t, db, pid)
			probe, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			_, probeErr := probe.Exec(`SELECT id FROM inventory_balances WHERE id=$1 FOR UPDATE NOWAIT`, later)
			probe.Rollback()
			blocker.Rollback()
			err = <-done
			candidate.Rollback()
			if err != nil {
				t.Fatal(err)
			}
			if probeErr != nil {
				t.Fatalf("later balance acquired before globally earlier blocked balance: %v", probeErr)
			}
		})
	}
}
func TestInboundReviewExternalDiscoveryChanged(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := db.Exec(`INSERT INTO products(id,supplier_id,stock,schema_version) VALUES('q','s',0,2)`); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.Exec(`SELECT id FROM suppliers WHERE id='s' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	// A dedicated connection makes the candidate's blocking state observable.
	db.SetMaxOpenConns(8)
	done := make(chan error, 1)
	go func() {
		_, err := NewSupplierRepository(db).createCartonShipmentAttempt(ctx, "s", "w1", "changed-discovery", "fingerprint", []CartonShipmentItem{{red, 1}}, CartonInboundSource{Kind: "external_replenishment", ExternalReference: "external-change", Reason: "counted"})
		done <- err
	}()
	var pid int
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		err := db.QueryRow(`SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND query LIKE 'SELECT id FROM suppliers WHERE id=$1%' AND cardinality(pg_blocking_pids(pid))>0`).Scan(&pid)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("candidate did not reach supplier guard after discovery")
	}
	if _, err := db.Exec(`UPDATE product_variants SET product_id='q' WHERE id=$1`, red); err != nil {
		t.Fatal(err)
	}
	blocker.Rollback()
	if err := <-done; !errors.Is(err, errCartonDiscoveryChanged) {
		t.Fatalf("external mode accepted changed, unguarded product: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM shipments`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("changed discovery committed %d headers: %v", n, err)
	}
}
