package repository

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSPNCXPrintReplaysOriginalLabelWithoutMinting(t *testing.T) {
	_, schema, _, sku := skuInventoryFixture(t)
	ctx := context.Background()
	scoped := skuScopedDB(t, schema)
	repo := NewSPNCXRepository(scoped)
	admin := SPNCXAdminAccess("print-admin")
	c1, c2 := "A0C00002-X0001", "A0C00002-X0002"
	req := SPNCXPrintRequest{ProductID: "p", VariantID: sku, Codes: []string{c2, c1}}
	first, err := repo.PrintLabels(ctx, admin, "first", req)
	if err != nil || first.BarcodeFormat != "CODE128" || len(first.Codes) != 2 || first.Codes[0] != c1 || first.Codes[1] != c2 {
		t.Fatalf("first job=%+v err=%v", first, err)
	}
	req.Codes = []string{c1, c2}
	for i := 0; i < 2; i++ {
		replay, err := repo.PrintLabels(ctx, admin, "first", req)
		if err != nil || replay.ID != first.ID || !replay.CreatedAt.Equal(first.CreatedAt) {
			t.Fatalf("replayed job=%+v err=%v", replay, err)
		}
	}
	if _, err := repo.PrintLabels(ctx, admin, "first", SPNCXPrintRequest{ProductID: "p", VariantID: sku, Codes: []string{c1}}); !errors.Is(err, ErrSPNCXPrintConflict) {
		t.Fatalf("idempotency payload mutated: %v", err)
	}
	if _, err := repo.PrintLabels(ctx, admin, "duplicate", req); !errors.Is(err, ErrSPNCXPrintConflict) {
		t.Fatalf("second original print succeeded: %v", err)
	}
	if _, err := repo.PrintLabels(ctx, SPNCXSupplierAccess("someone", "foreign"), "first", req); !errors.Is(err, ErrSPNCXNotFound) {
		t.Fatalf("foreign supplier replay returned an existing job: %v", err)
	}
	req.Reprint = true
	reprint, err := repo.PrintLabels(ctx, admin, "reprint", req)
	if err != nil || reprint.ID == first.ID || reprint.Codes[0] != c1 || reprint.Codes[1] != c2 {
		t.Fatalf("reprint=%+v err=%v", reprint, err)
	}
	var total, counter, prints, reprints, jobUnits int
	var original, latest time.Time
	if err := scoped.QueryRow(`SELECT (SELECT count(*) FROM product_units WHERE variant_id=$1),
		(SELECT next_sequence FROM product_variant_unit_counters WHERE variant_id=$1),
		(SELECT print_count FROM product_units WHERE code=$2),
		(SELECT count(*) FROM product_unit_events WHERE code=$2 AND event_type='reprinted'),
		(SELECT count(*) FROM spncx_print_job_units),
		(SELECT printed_at FROM product_units WHERE code=$2),
		(SELECT last_printed_at FROM product_units WHERE code=$2)`, sku, c1).Scan(&total, &counter, &prints, &reprints, &jobUnits, &original, &latest); err != nil {
		t.Fatal(err)
	}
	if total != 3 || counter != 4 || prints != 2 || reprints != 1 || jobUnits != 4 || !original.Equal(first.CreatedAt) || !latest.Equal(reprint.CreatedAt) {
		t.Fatalf("unit identity or history changed: total=%d next=%d prints=%d reprints=%d jobs=%d times=%s/%s", total, counter, prints, reprints, jobUnits, original, latest)
	}
	neverIssued := SPNCXPrintRequest{ProductID: "p", VariantID: sku, Codes: []string{"A0C00002-X0099"}}
	if _, err := repo.PrintLabels(ctx, admin, "missing", neverIssued); !errors.Is(err, ErrSPNCXNotFound) {
		t.Fatalf("print minted or accepted an unknown unit: %v", err)
	}
}

func TestSPNCXPrintConcurrentFirstClaimIsUnique(t *testing.T) {
	_, schema, sku, _ := skuInventoryFixture(t)
	ctx := context.Background()
	scoped := skuScopedDB(t, schema)
	repo := NewSPNCXRepository(scoped)
	access := SPNCXSupplierAccess("supplier-staff", "s")
	req := SPNCXPrintRequest{ProductID: "p", VariantID: sku, Codes: []string{"A0C00001-X0001"}}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, key := range []string{"race-one", "race-two"} {
		wg.Add(1)
		go func(key string) { defer wg.Done(); _, err := repo.PrintLabels(ctx, access, key, req); results <- err }(key)
	}
	wg.Wait()
	close(results)
	ok, conflict := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else if errors.Is(err, ErrSPNCXPrintConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	var printed, jobs int
	if err := scoped.QueryRow(`SELECT print_count,(SELECT count(*) FROM spncx_print_jobs) FROM product_units WHERE code=$1`, req.Codes[0]).Scan(&printed, &jobs); err != nil || printed != 1 || jobs != 1 || ok != 1 || conflict != 1 {
		t.Fatalf("non-unique original print: units=%d jobs=%d ok=%d conflict=%d err=%v", printed, jobs, ok, conflict, err)
	}
}

func TestSPNCXLookupRequiresSupplierOwnershipOrConfirmedWarehouseCity(t *testing.T) {
	db, schema, sku, _ := skuInventoryFixture(t)
	ctx := context.Background()
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	here, other, unconfirmed := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN warehouse_city_confirmed boolean NOT NULL DEFAULT FALSE`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE warehouses SET city_name=CASE id WHEN 'w1' THEN 'here' ELSE 'elsewhere' END WHERE id IN ('w1','w2')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO users(id,warehouse_city,warehouse_city_confirmed) VALUES($1,'here',TRUE),($2,'elsewhere',TRUE),($3,'here',FALSE)`, here, other, unconfirmed); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE inventory_balances SET location_type='warehouse', warehouse_id='w1',supplier_warehouse_id=NULL WHERE variant_id=$1`, sku); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	scoped := skuScopedDB(t, schema)
	repo := NewSPNCXRepository(scoped)
	const code = "A0C00001-X0001"
	for _, access := range []SPNCXAccess{SPNCXAdminAccess("an-admin"), SPNCXSupplierAccess("supplier-staff", "s"), SPNCXWarehouseAccess(here)} {
		got, err := repo.LookupUnit(ctx, access, code)
		if err != nil || got.Code != code || got.VariantID != sku || len(got.History) == 0 || got.History[0].EventType != "minted" {
			t.Fatalf("authorized lookup %+v err=%v", got, err)
		}
	}
	for _, access := range []SPNCXAccess{{}, SPNCXSupplierAccess("a-foreign", "foreign"), SPNCXWarehouseAccess(other), SPNCXWarehouseAccess(unconfirmed)} {
		if _, err := repo.LookupUnit(ctx, access, code); !errors.Is(err, ErrSPNCXNotFound) {
			t.Fatalf("out-of-scope lookup leaked data: %v", err)
		}
	}
	if _, err := repo.PrintLabels(ctx, SPNCXSupplierAccess("supplier-staff", "s"), "no-custody", SPNCXPrintRequest{ProductID: "p", VariantID: sku, Codes: []string{code}}); !errors.Is(err, ErrSPNCXNotFound) {
		t.Fatalf("supplier printed a warehouse-held unit: %v", err)
	}
	order, item := "sold-by-warehouse", uuid.NewString()
	tx = skuTestTx(t, db, schema)
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO orders(id) VALUES($1)`, order); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,$2,'p',$3,1)`, item, order, sku); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE order_item_origins SET origin_type='warehouse',warehouse_id='w1',supplier_warehouse_id=NULL WHERE order_item_id=$1`, item); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, order); err != nil {
		t.Fatal(err)
	}
	// Sold-ledger location metadata is not authority; immutable order
	// allocation proves which warehouse handled this particular code.
	if _, err := tx.Exec(`UPDATE inventory_balances SET warehouse_id='w2'
		WHERE id=(SELECT balance_id FROM product_units WHERE code=$1)`, code); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := repo.LookupUnit(ctx, SPNCXWarehouseAccess(here), code)
	if err != nil || got.Status != "sold" || got.CurrentOrderID == nil || *got.CurrentOrderID != order {
		t.Fatalf("warehouse could not trace its exact sold code: %+v %v", got, err)
	}
	if _, err := repo.LookupUnit(ctx, SPNCXWarehouseAccess(other), code); !errors.Is(err, ErrSPNCXNotFound) {
		t.Fatalf("cross-city sold lookup leaked: %v", err)
	}
}

func TestSPNCXExternalIngressIssuesOnceBeforeAnyPrinting(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx := context.Background()
	warehouse, sku := "w-"+uuid.NewString(), uuid.NewString()
	spnc := "E" + strings.ToUpper(sku[:8])
	opsExec(t, db, `INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'p','c')`, warehouse)
	opsExec(t, db, `UPDATE products SET schema_version=2,product_type='variable',attribute_schema='{}' WHERE id=$1`, f.product)
	opsExec(t, db, `INSERT INTO product_variants(id,product_id,spnc,inventory_mode) VALUES($1,$2,$3,'tracked')`, sku, f.product, spnc)
	sup := NewSupplierRepository(db)
	source := CartonInboundSource{Kind: "external_replenishment", ExternalReference: "spncx-test-" + sku, Reason: "receipt idempotency fixture"}
	shipment, err := sup.CreateCartonShipmentFromSource(ctx, f.supplier, []CartonShipmentItem{{VariantID: sku, ExpectedQty: 1}}, source, warehouse, "spncx-receipt")
	if err != nil {
		t.Fatal(err)
	}
	if err = sup.ChangeCartonDispatch(ctx, f.supplier, shipment.ID, false); err != nil {
		t.Fatal(err)
	}
	assertUnitCount := func(want int) {
		t.Helper()
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM product_units WHERE variant_id=$1`, sku).Scan(&count); err != nil || count != want {
			t.Fatalf("units=%d want=%d err=%v", count, want, err)
		}
	}
	assertUnitCount(0) // dispatch is not physical receipt
	for i := 0; i < 2; i++ {
		if _, err := sup.ReceiveWarehouseShipment(ctx, shipment.ShipmentCode, []WarehouseReceiveItem{{VariantID: sku, ReceivedQty: 1, QCStatus: "pass"}}); err != nil {
			t.Fatal(err)
		}
	}
	assertUnitCount(1)
	repo := NewSPNCXRepository(db)
	access := SPNCXAdminAccess("receipt-audit")
	got, err := repo.LookupUnit(ctx, access, spnc+"-X0001")
	if err != nil || got.PrintedAt != nil || got.PrintCount != 0 || got.Status != "quarantined" || len(got.History) != 1 || got.History[0].EventType != "minted" {
		t.Fatalf("receipt did not persist one untouched code before printing: %+v %v", got, err)
	}
	req := SPNCXPrintRequest{ProductID: f.product, VariantID: sku, Codes: []string{got.Code}}
	if _, err := repo.PrintLabels(ctx, access, "before-QC", req); !errors.Is(err, ErrSPNCXPrintConflict) {
		t.Fatalf("printed before sellable entry: %v", err)
	}
	if err := NewWarehouseRepository(db).RecordQCPass(ctx, f.product, shipment.ID, f.user, 1); err != nil {
		t.Fatal(err)
	}
	inv09PlacePending(t, db, f, shipment.ID, warehouse)
	after, err := repo.LookupUnit(ctx, access, got.Code)
	if err != nil || after.Status != "in_stock" || after.PrintCount != 0 || !after.IssuedAt.Equal(got.IssuedAt) {
		t.Fatalf("putaway reminted an existing unit: %+v %v", after, err)
	}
	job, err := repo.PrintLabels(ctx, access, "after-putaway-"+sku, req)
	if err != nil || len(job.Codes) != 1 || job.Codes[0] != got.Code {
		t.Fatalf("first label lost issued code: %+v %v", job, err)
	}
	assertUnitCount(1)
}
