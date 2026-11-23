package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func legacyAggregateFixture(t *testing.T, local, held int) (*sql.DB, opsFixture, string, string) {
	t.Helper()
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	source := "sw-" + uuid.NewString()
	opsExec(t, db, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,$1,'p','c')`, source, f.supplier)
	opsExec(t, db, `UPDATE products SET origin_supplier_warehouse_id=$1 WHERE id=$2`, source, f.product)
	opsExec(t, db, `INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,supplier_warehouse_id) VALUES($1,$2,$3,'supplier','supplier',$4,'sellable',$5,$6)`, uuid.NewString(), f.product, f.supplier, "supplier-warehouse:"+source, local, source)
	warehouse := seedCanonicalWarehouseStock(t, db, f.product, f.supplier, held)
	return db, f, source, warehouse
}

func legacyAggregateQuantities(t *testing.T, db *sql.DB, product, source, warehouse string) (int, int, int) {
	t.Helper()
	var local, held, total int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity) FILTER(WHERE supplier_warehouse_id=$2 AND status='sellable'),0),COALESCE(SUM(quantity) FILTER(WHERE warehouse_id=$3 AND status='sellable'),0) FROM inventory_balances WHERE product_id=$1`, product, source, warehouse).Scan(&local, &held); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT stock FROM products WHERE id=$1`, product).Scan(&total); err != nil {
		t.Fatal(err)
	}
	return local, held, total
}

func TestAuditSupplyLegacyAggregateEditDoesNotAddWarehouseStockTwice(t *testing.T) {
	db, f, source, warehouse := legacyAggregateFixture(t, 5, 5)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	for i := 0; i < 20; i++ {
		if err := repo.UpdateProductStockForVariant(ctx, f.supplier, f.product, "", 10, "counted unchanged", f.user); err != nil {
			t.Fatal(err)
		}
		local, held, total := legacyAggregateQuantities(t, db, f.product, source, warehouse)
		if local != 5 || held != 5 || total != 10 {
			t.Fatalf("retry %d: supplier=%d warehouse=%d aggregate=%d; want 5+5=10", i, local, held, total)
		}
	}
	if err := repo.UpdateProductStockForVariant(ctx, f.supplier, f.product, "", 8, "counted correction", f.user); err != nil {
		t.Fatal(err)
	}
	local, held, total := legacyAggregateQuantities(t, db, f.product, source, warehouse)
	if local != 3 || held != 5 || total != 8 {
		t.Fatalf("save8: supplier=%d warehouse=%d aggregate=%d", local, held, total)
	}
	if err := repo.UpdateProductStockForVariant(ctx, f.supplier, f.product, "", 4, "cannot remove held goods", f.user); err == nil {
		t.Fatal("saved total below warehouse-held stock")
	}
	local, held, total = legacyAggregateQuantities(t, db, f.product, source, warehouse)
	if local != 3 || held != 5 || total != 8 {
		t.Fatalf("refused save mutated balances %d/%d/%d", local, held, total)
	}
	var moves, delta int
	var actor, reason string
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(quantity),0),COALESCE(MIN(actor_user_id),''),COALESCE(MIN(metadata->>'reason'),'') FROM inventory_movements WHERE product_id=$1 AND reference_type='legacy_stock_adjustment'`, f.product).Scan(&moves, &delta, &actor, &reason); err != nil || moves != 1 || delta != 2 || actor != f.user || reason != "counted correction" {
		t.Fatalf("adjustment evidence moves=%d qty=%d actor-match=%v reason=%q err=%v", moves, delta, actor == f.user, reason, err)
	}
}

func TestLegacyAggregateCaptureRaceHasSerializedAbsoluteTotals(t *testing.T) {
	for i := 0; i < 12; i++ {
		db, f, source, warehouse := legacyAggregateFixture(t, 5, 5)
		ctx := context.Background()
		order, token := opsOrder(t, db, f, false)
		seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := NewOrderRepository(db).MarkPaymentPaid(ctx, token, "R"+uuid.NewString()[:20], "mask")
			results <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			results <- NewSupplierRepository(db).UpdateProductStockForVariant(ctx, f.supplier, f.product, "", 10, "absolute count race", f.user)
		}()
		close(start)
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal(err)
			}
		}
		local, held, total := legacyAggregateQuantities(t, db, f.product, source, warehouse)
		if held != 4 || !((local == 5 && total == 9) || (local == 6 && total == 10)) {
			t.Fatalf("nonserialized stock local=%d warehouse=%d aggregate=%d", local, held, total)
		}
	}
}

func TestLegacyAggregateBoundsAndUnrelatedQuantities(t *testing.T) {
	db, f, source, warehouse := legacyAggregateFixture(t, 0, 0)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	for i, status := range []string{"sales_reserved", "in_transit", "quarantine"} {
		opsExec(t, db, `INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,warehouse_id) VALUES($1,$2,$3,'supplier','warehouse',$4,$5,$6,$7)`, uuid.NewString(), f.product, f.supplier, "warehouse:"+warehouse, status, 11+i, warehouse)
	}
	other := opsFixtureNew(t, db)
	otherWarehouse := seedCanonicalWarehouseStock(t, db, other.product, other.supplier, 7)
	for _, quantity := range []int{0, 6, 0} {
		if err := repo.UpdateProductStockForVariant(ctx, f.supplier, f.product, "", quantity, "counted local stock", f.user); err != nil {
			t.Fatal(err)
		}
	}
	var gapOperations int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT metadata->>'operation') FROM inventory_movements WHERE product_id=$1 AND reference_type='unit_coverage_gap' AND metadata->>'operation' IN ('inbound','adjust_decrease') AND (metadata->>'shortfall')::int=6`, f.product).Scan(&gapOperations); err != nil || gapOperations != 2 {
		t.Fatalf("increase/decrease gap operations=%d err=%v", gapOperations, err)
	}
	for _, input := range []struct {
		quantity int
		reason   string
	}{{-1, "invalid"}, {int(int64(2147483647) + 1), "overflow"}, {5, " "}} {
		if err := repo.UpdateProductStockForVariant(ctx, f.supplier, f.product, "", input.quantity, input.reason, f.user); err == nil {
			t.Fatal("invalid aggregate accepted")
		}
	}
	local, held, total := legacyAggregateQuantities(t, db, f.product, source, warehouse)
	if local != 0 || held != 0 || total != 0 {
		t.Fatalf("rejected inputs changed inventory %d/%d/%d", local, held, total)
	}
	var untouched int
	if err := db.QueryRow(`SELECT SUM(quantity) FROM inventory_balances WHERE product_id=$1 AND status IN ('sales_reserved','in_transit','quarantine')`, f.product).Scan(&untouched); err != nil || untouched != 36 {
		t.Fatalf("reserved/transit/quarantine changed: %d err=%v", untouched, err)
	}
	_, otherHeld, otherTotal := legacyAggregateQuantities(t, db, other.product, "unused", otherWarehouse)
	if otherHeld != 7 || otherTotal != 7 {
		t.Fatalf("other supplier changed: %d/%d", otherHeld, otherTotal)
	}
	if err := repo.UpdateProductStockForVariant(ctx, other.supplier, f.product, "", 0, "foreign edit", other.user); err == nil {
		t.Fatal("foreign supplier adjusted stock")
	}
	// A zero declaration with no canonical balances/origin does not invent stock.
	blank := opsFixtureNew(t, db)
	if err := repo.UpdateProductStockForVariant(ctx, blank.supplier, blank.product, "", 0, "counted zero", blank.user); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateProductStockForVariant(ctx, blank.supplier, blank.product, "", 3, "unattributed increase", blank.user); err == nil {
		t.Fatal("positive declaration guessed an origin")
	}
	blankSource := "sw-" + uuid.NewString()
	opsExec(t, db, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,$1,'p','c')`, blankSource, blank.supplier)
	opsExec(t, db, `UPDATE products SET origin_supplier_warehouse_id=$1 WHERE id=$2`, blankSource, blank.product)
	if err := repo.UpdateProductStockForVariant(ctx, blank.supplier, blank.product, "", 3, "counted new source", blank.user); err != nil {
		t.Fatal(err)
	}
	local, _, total = legacyAggregateQuantities(t, db, blank.product, blankSource, "unused")
	if local != 3 || total != 3 {
		t.Fatalf("no-balance declaration=%d/%d", local, total)
	}
}

func TestLegacyAggregateReadOnlyReconciliationReport(t *testing.T) {
	db, f, _, _ := legacyAggregateFixture(t, 5, 5)
	if err := NewSupplierRepository(db).UpdateProductStockForVariant(context.Background(), f.supplier, f.product, "", 8, "counted correction", f.user); err != nil {
		t.Fatal(err)
	}
	report, err := os.ReadFile("../../../scripts/audit-legacy-stock-locations.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	rows, err := tx.Query(string(report))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var product, supplier, next string
		var projection, sellable int
		var hasMore bool
		var lots, movements, audits []byte
		if err = rows.Scan(&product, &supplier, &projection, &sellable, &lots, &movements, &audits, &next, &hasMore); err != nil {
			t.Fatal(err)
		}
		if product != f.product {
			continue
		}
		found = true
		var physical, events, history []map[string]interface{}
		if err = json.Unmarshal(lots, &physical); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(movements, &events); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(audits, &history); err != nil {
			t.Fatal(err)
		}
		// The physical adjustment remains exactly one event; serialization adds
		// exactly one quantity-only coverage audit, which the report must expose.
		adjustments, gaps := 0, 0
		for _, event := range events {
			if event["reference_type"] == "legacy_stock_adjustment" {
				adjustments++
			}
			if event["reference_type"] == "unit_coverage_gap" {
				metadata, ok := event["metadata"].(map[string]interface{})
				if !ok || metadata["operation"] != "adjust_decrease" || metadata["requested"] != float64(2) || metadata["allocated"] != float64(0) || metadata["shortfall"] != float64(2) {
					t.Fatalf("invalid gap evidence: %+v", event)
				}
				gaps++
			}
		}
		if projection != 8 || sellable != 8 || len(physical) != 2 || len(events) != 2 || adjustments != 1 || gaps != 1 || len(history) != 1 || next != "counted_reconciliation_required" {
			t.Fatalf("incomplete counted reconciliation report projection=%d sellable=%d lots=%d events=%d audits=%d", projection, sellable, len(physical), len(events), len(history))
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("stock report omitted fixture")
	}
}

func TestLegacyAggregatePublicationAmendmentDoesNotReseed(t *testing.T) {
	db, f, source, warehouse := legacyAggregateFixture(t, 5, 5)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	if err := repo.UpdateProductStockForVariant(ctx, f.supplier, f.product, "", 8, "counted before historical amendment", f.user); err != nil {
		t.Fatal(err)
	}
	data := map[string]interface{}{"title": "Existing publication amendment", "categorySlug": "digital", "supplierBaseCost": float64(1000), "stock": float64(50), "supplierWarehouseId": source}
	proposal, err := repo.ProposeProduct(ctx, f.supplier, data)
	if err != nil {
		t.Fatal(err)
	}
	// Historical proposals may already contain this identity. Client submission
	// strips it; reproduce the persisted historical amendment explicitly.
	data["product_id"] = f.product
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `UPDATE pending_approvals SET requested_data=$2::jsonb WHERE id=$1`, proposal, string(raw))
	var decisions []FieldDecision
	for key, value := range ReviewLeaves(data) {
		decisions = append(decisions, FieldDecision{Key: key, Decision: "approved", ValueHash: ReviewValueHash(value)})
	}
	if err = repo.SaveFieldReview(ctx, proposal, "test-admin", decisions); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ApproveProposal(ctx, proposal, ""); err != nil {
		t.Fatal(err)
	}
	local, held, total := legacyAggregateQuantities(t, db, f.product, source, warehouse)
	if local != 3 || held != 5 || total != 8 {
		t.Fatalf("historical approval reseeded stock local=%d warehouse=%d projection=%d; want 3/5/8", local, held, total)
	}
}

func TestLegacyAggregateConcurrentFirstPublicationSeedsOnlyOnce(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewSupplierRepository(db)
	product := "pub-race-" + uuid.NewString()[:20]
	var proposals []string
	sources := map[string]int{}
	for _, quantity := range []int{3, 7} {
		source := "sw-" + uuid.NewString()
		sources[source] = quantity
		opsExec(t, db, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,$1,'p','c')`, source, f.supplier)
		data := map[string]interface{}{"title": "Concurrent historical publication", "categorySlug": "digital", "supplierBaseCost": float64(1000), "stock": float64(quantity), "supplierWarehouseId": source}
		proposal, err := repo.ProposeProduct(ctx, f.supplier, data)
		if err != nil {
			t.Fatal(err)
		}
		data["product_id"] = product
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		opsExec(t, db, `UPDATE pending_approvals SET requested_data=$2::jsonb WHERE id=$1`, proposal, string(raw))
		var decisions []FieldDecision
		for key, value := range ReviewLeaves(data) {
			decisions = append(decisions, FieldDecision{Key: key, Decision: "approved", ValueHash: ReviewValueHash(value)})
		}
		if err = repo.SaveFieldReview(ctx, proposal, "test-admin", decisions); err != nil {
			t.Fatal(err)
		}
		proposals = append(proposals, proposal)
	}
	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = blocker.ExecContext(ctx, `LOCK TABLE products IN SHARE MODE`); err != nil {
		blocker.Rollback()
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	defer func() { blocker.Rollback(); wg.Wait() }()
	for _, proposal := range proposals {
		wg.Add(1)
		go func(id string) { defer wg.Done(); _, err := repo.ApproveProposal(ctx, id, ""); results <- err }(proposal)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked int
		if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE NOT l.granted AND l.relation='products'::regclass AND a.datname=current_database() AND POSITION('INSERT INTO products' IN a.query)>0`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("both first-publication inserts were not blocked: %d", blocked)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err = blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(results)
	for err = range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var lots, total, projection int
	if err = db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(quantity),0) FROM inventory_balances WHERE product_id=$1`, product).Scan(&lots, &total); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT stock FROM products WHERE id=$1`, product).Scan(&projection); err != nil || lots != 1 || (total != 3 && total != 7) || projection != total {
		t.Fatalf("first publication duplicated lots=%d total=%d projection=%d err=%v", lots, total, projection, err)
	}
	var origin string
	var wrongOrigin int
	if err = db.QueryRow(`SELECT origin_supplier_warehouse_id FROM products WHERE id=$1`, product).Scan(&origin); err != nil || sources[origin] != total {
		t.Fatalf("publication winner source and quantity differ err=%v", err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM inventory_balances WHERE product_id=$1 AND supplier_warehouse_id IS DISTINCT FROM $2`, product, origin).Scan(&wrongOrigin); err != nil || wrongOrigin != 0 {
		t.Fatalf("losing approval redirected winner stock: %d err=%v", wrongOrigin, err)
	}
}

func TestLegacyAggregateReconciliationPagesCoverWholeCatalog(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	prefix := "audit-page-" + uuid.NewString()[:8] + "-"
	opsExec(t, db, `INSERT INTO products(id,title,slug,supplier_id,stock,original_price,supplier_base_cost,platform_margin) SELECT $1||lpad(g::text,4,'0'),'Audit page fixture',$1||lpad(g::text,4,'0'),$2,0,1000,500,0 FROM generate_series(1,501) g`, prefix, f.supplier)
	report, err := os.ReadFile("../../../scripts/audit-legacy-stock-locations.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	cursor := prefix
	seen := map[string]bool{}
	own := 0
	for page := 0; page < 10; page++ {
		if _, err = tx.Exec(`SELECT set_config('sellersho.audit_after_product_id',$1,TRUE)`, cursor); err != nil {
			t.Fatal(err)
		}
		rows, err := tx.Query(string(report))
		if err != nil {
			t.Fatal(err)
		}
		more := false
		size := 0
		for rows.Next() {
			var product, supplier, next string
			var projection, sellable int
			var lots, events, audits []byte
			if err = rows.Scan(&product, &supplier, &projection, &sellable, &lots, &events, &audits, &next, &more); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if seen[product] || product <= cursor {
				rows.Close()
				t.Fatal("audit pages repeat or regress product identity")
			}
			seen[product] = true
			cursor = product
			size++
			if strings.HasPrefix(product, prefix) {
				own++
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if page == 0 && (size != 500 || !more) {
			t.Fatalf("first page size=%d has_more=%v", size, more)
		}
		if !more {
			if own != 501 {
				t.Fatalf("audit pages covered %d of 501 fixture products", own)
			}
			return
		}
	}
	t.Fatal("audit cursor did not terminate")
}
