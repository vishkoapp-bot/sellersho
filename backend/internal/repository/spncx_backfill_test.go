package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestSpncxBackfillAllocation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		p            int
		stocks       []int
		defaultFirst bool
		ids          []string
		want         []int
	}{
		{"doubled", 100, []int{100, 40, 60}, true, []string{"2", "1", "0"}, []int{60, 40, 0}},
		{"equal", 9, []int{4, 5}, true, []string{"1", "0"}, []int{5, 4}},
		{"other", 75, []int{30, 50}, false, []string{"1", "0"}, []int{50, 25}},
		{"single", 5, []int{5}, true, []string{"0"}, []int{5}},
		{"tie", 8, []int{5, 5}, false, []string{"0", "1"}, []int{5, 3}},
		{"insufficient", 8, []int{5}, true, []string{"0"}, []int{5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var inputs []BackfillVariantInput
			var stock int
			for i, n := range tc.stocks {
				inputs = append(inputs, BackfillVariantInput{VariantID: fmt.Sprint(i), Stock: n, IsDefault: i == 0 && tc.defaultFirst, InventoryMode: "tracked", AlreadyMinted: i})
				stock += n
			}
			if tc.name == "tie" {
				inputs[0], inputs[1] = inputs[1], inputs[0]
			}
			got := AllocateBackfillVariants(inputs, tc.p)
			if len(got) != len(tc.want) {
				t.Fatalf("allocation length: %+v", got)
			}
			var total int
			for i, a := range got {
				if a.VariantID != tc.ids[i] || a.Alloc != tc.want[i] || fmt.Sprint(a.AlreadyMinted) != a.VariantID {
					t.Fatalf("allocation: %+v", got)
				}
				total += a.Alloc
			}
			if total != min(stock, tc.p) {
				t.Fatalf("allocated %d", total)
			}
		})
	}
}

type backfillTestFixture struct {
	t  *testing.T
	tx *sql.Tx
	db *sql.DB
}

func newBackfillTestFixture(t *testing.T) backfillTestFixture {
	t.Helper()
	db, schema, _, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	t.Cleanup(func() { _ = tx.Rollback() })
	f := backfillTestFixture{t, tx, skuScopedDB(t, schema)}
	// The shared fixture seeds a product for SKU tests. Remove it so the
	// catalogue dry run exercises the supplied full-catalogue expectations.
	f.exec(`DELETE FROM product_unit_events WHERE unit_id IN (SELECT id FROM product_units WHERE product_id='p')`)
	f.exec(`DELETE FROM product_units WHERE product_id='p'`)
	f.exec(`DELETE FROM product_variant_unit_counters WHERE variant_id IN (SELECT id FROM product_variants WHERE product_id='p')`)
	f.exec(`DELETE FROM inventory_movements WHERE product_id='p'`)
	f.exec(`DELETE FROM inventory_balances WHERE product_id='p'`)
	f.exec(`DELETE FROM product_variants WHERE product_id='p'`)
	f.exec(`DELETE FROM products WHERE id='p'`)
	return f
}

func (f backfillTestFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.tx.Exec(query, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f backfillTestFixture) commit() {
	f.t.Helper()
	if err := f.tx.Commit(); err != nil {
		f.t.Fatal(err)
	}
}

func (f backfillTestFixture) product(id string) {
	f.exec(`INSERT INTO products(id,supplier_id,title) VALUES($1,'s',$2)`, id, "Title "+id)
}

func (f backfillTestFixture) variant(product, spnc, mode string, stock int, def bool) string {
	id := uuid.NewString()
	f.exec(`INSERT INTO product_variants(id,product_id,spnc,stock,inventory_mode,is_default) VALUES($1,$2,$3,$4,$5,$6)`, id, product, spnc, stock, mode, def)
	return id
}

func (f backfillTestFixture) lot(id, product, variant string, quantity int, status string) {
	f.exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,supplier_warehouse_id,status,quantity,reconciled)
		VALUES($1,$2,'s',NULLIF($3,'')::uuid,'supplier','supplier',$4,'sw1',$5,$6,TRUE)`, id, product, variant, "supplier-warehouse:"+id, status, quantity)
}

func (f backfillTestFixture) stock(id string, p int) {
	// The balance trigger writes the projection during fixture setup.
	f.exec(`UPDATE products SET stock=$2 WHERE id=$1`, id, p)
}

func (f backfillTestFixture) run(id string) *BackfillProductResult {
	f.t.Helper()
	result, err := BackfillProduct(context.Background(), f.db, id, "test-actor", "test-hash")
	if err != nil || result == nil || result.Status != "completed" {
		f.t.Fatalf("backfill %s: %+v, %v", id, result, err)
	}
	return result
}

func backfillTestValue[T comparable](t *testing.T, db *sql.DB, want T, query string, args ...any) {
	t.Helper()
	var got T
	if err := db.QueryRow(query, args...).Scan(&got); err != nil || got != want {
		t.Fatalf("%s: got %v, want %v, err %v", query, got, want, err)
	}
}

type backfillTestCatalogue struct {
	backfillTestFixture
	def, large, small, equal, keep, trim string
}

func newBackfillTestCatalogue(t *testing.T) backfillTestCatalogue {
	t.Helper()
	f := newBackfillTestFixture(t)
	for _, id := range []string{"doubled", "equal", "other", "variant-less"} {
		f.product(id)
	}
	c := backfillTestCatalogue{backfillTestFixture: f}
	c.def = f.variant("doubled", "A0C00001", "legacy", 100, true)
	c.large = f.variant("doubled", "A0C00002", "legacy", 60, false)
	c.small = f.variant("doubled", "A0C00003", "legacy", 40, false)
	c.equal = f.variant("equal", "A0C00004", "legacy", 50, true)
	c.keep = f.variant("other", "A0C00005", "legacy", 50, false)
	c.trim = f.variant("other", "A0C00006", "legacy", 30, false)
	f.lot("double-a", "doubled", "", 70, "sellable")
	f.lot("double-b", "doubled", "", 30, "sellable")
	f.lot("double-damaged", "doubled", "", 2, "damaged")
	f.lot("equal-a", "equal", "", 50, "sellable")
	f.lot("other-a", "other", "", 75, "sellable")
	f.stock("doubled", 100)
	f.stock("equal", 50)
	f.stock("other", 75)
	f.stock("variant-less", 30)
	f.commit()
	return c
}

func TestSpncxBackfillPlanGroups(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	ctx := context.Background()
	exp := BackfillExpectations{ZeroAllocVariants: 1, GroupDoubled: 100, GroupEqual: 50, GroupOther: 75, GroupVariantLess: 30, TotalUnits: 255, ProductCount: 4}
	plan, err := ComputeBackfillPlan(ctx, f.db, nil, exp)
	if err != nil || plan == nil || !plan.AllPassed() {
		t.Fatalf("full catalogue: %+v, %v", plan, err)
	}
	if plan.GroupTotals != (BackfillGroupTotals{100, 50, 75, 30}) || plan.TotalUnits != 255 || plan.ProductCount != 4 {
		t.Fatalf("totals: %+v", plan)
	}
	d, e, o, v := plan.Products[0], plan.Products[1], plan.Products[2], plan.Products[3]
	wantAlloc := map[string]int{f.def: 0, f.large: 60, f.small: 40, f.equal: 50, f.keep: 50, f.trim: 25}
	zeros := 0
	for _, p := range plan.Products[:3] {
		for _, a := range p.Alloc {
			if n, ok := wantAlloc[a.VariantID]; !ok || n != a.Alloc {
				t.Fatalf("unexpected allocation: %+v", a)
			}
			delete(wantAlloc, a.VariantID)
			if a.Alloc == 0 {
				zeros++
			}
		}
	}
	if len(wantAlloc) != 0 || zeros != 1 || o.TrimTarget == nil || *o.TrimTarget != f.trim || len(v.NewVariants) != 1 || v.TopUp != 30 ||
		len(v.Alloc) != 1 || v.Alloc[0].Alloc != 30 || v.Alloc[0].VariantID != v.NewVariants[0].VariantID || v.NewVariants[0].Stock != 30 || v.Alloc[0].SPNC != "" {
		t.Fatalf("allocation or variant creation: %+v", plan.Products)
	}
	if _, err := uuid.Parse(v.NewVariants[0].VariantID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.LotsToReduce, []BackfillLotReduction{{"double-a", -70}, {"double-b", -30}}) ||
		!reflect.DeepEqual(d.LotsToCreate, []BackfillLotCreation{{f.large, 60, "supplier-warehouse:double-a"}, {f.small, 40, "supplier-warehouse:double-a"}}) ||
		len(e.LotsToCreate) != 1 || len(o.LotsToReduce) != 1 || len(v.LotsToReduce) != 0 || len(v.LotsToCreate) != 1 {
		t.Fatalf("lot plan: %+v", plan.Products)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"product_id", "title", "P", "alloc", "lots_to_create", "lots_to_reduce", "top_up", "excess", "excess_decision", "variant_stock_corrections", "new_variants", "order_history", "trim_target", "excluded", "excluded_reason", "flipped_variants"}
	if len(keys) != len(wantKeys) {
		t.Fatalf("plan keys: %s", raw)
	}
	for _, key := range wantKeys {
		if _, ok := keys[key]; !ok {
			t.Fatalf("missing %s: %s", key, raw)
		}
	}
	again, err := ComputeBackfillPlan(ctx, f.db, []string{"variant-less", "other", "equal", "doubled"}, ProdBackfillExpectations())
	if err != nil || !again.AllPassed() || again.PlanHash != plan.PlanHash || again.Products[3].NewVariants[0].VariantID == v.NewVariants[0].VariantID {
		t.Fatalf("repeat plan: %+v, %v", again, err)
	}
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_state`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units`)
	if _, err = f.db.Exec(`INSERT INTO spncx_backfill_state(product_id,status,units_target,excluded_reason) VALUES('equal','excluded',0,'test-veto')`); err != nil {
		t.Fatal(err)
	}
	veto, err := ComputeBackfillPlan(ctx, f.db, nil, exp)
	if err != nil || !veto.AllPassed() || !veto.Products[1].Excluded || veto.PlanHash == plan.PlanHash {
		t.Fatalf("veto plan: %+v, %v", veto, err)
	}
	mismatch, err := ComputeBackfillPlan(ctx, f.db, nil, ProdBackfillExpectations())
	if err != nil || mismatch.AllPassed() {
		t.Fatalf("expectation mismatch: %+v, %v", mismatch, err)
	}
	if _, err = ComputeBackfillPlan(ctx, f.db, []string{"missing"}, exp); !errors.Is(err, ErrBackfillProductNotFound) {
		t.Fatal(err)
	}
}

func TestSpncxBackfillProductDoubled(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	r := f.run("doubled")
	if r.UnitsMinted != 100 || r.UnitsTarget != 100 || len(r.FlippedVariants) != 3 || len(r.NewVariantIDs) != 0 {
		t.Fatalf("doubled result: %+v", r)
	}
	backfillTestValue(t, f.db, 100, `SELECT stock FROM products WHERE id='doubled'`)
	backfillTestValue(t, f.db, 0, `SELECT SUM(quantity) FROM inventory_balances WHERE product_id='doubled' AND variant_id IS NULL AND status='sellable'`)
	backfillTestValue(t, f.db, 2, `SELECT COUNT(*) FROM inventory_balances WHERE id IN ('double-a','double-b') AND quantity=0`)
	// Absorb movements record the absorbed magnitude (inventory_movements
	// quantity carries CHECK (quantity > 0) on the migrated schema); the
	// signed direction lives in metadata.
	backfillTestValue(t, f.db, 2, `SELECT COUNT(*) FROM inventory_movements WHERE reference_id='doubled' AND idempotency_key LIKE 'spncx-backfill:doubled:absorb:%' AND quantity>0 AND (metadata->>'quantity_delta')::int<0`)
	backfillTestValue(t, f.db, 100, `SELECT SUM(quantity) FROM inventory_movements WHERE reference_id='doubled' AND idempotency_key LIKE 'spncx-backfill:doubled:absorb:%'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units WHERE variant_id=$1`, f.def)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_variant_unit_counters WHERE variant_id=$1`, f.def)
	backfillTestValue(t, f.db, 0, `SELECT stock FROM product_variants WHERE id=$1`, f.def)
	for _, variant := range []struct {
		id, spnc string
		n        int
	}{{f.large, "A0C00002", 60}, {f.small, "A0C00003", 40}} {
		backfillTestValue(t, f.db, variant.n, `SELECT SUM(quantity) FROM inventory_balances WHERE variant_id=$1 AND status='sellable' AND unit_tracked AND supplier_warehouse_id='sw1'`, variant.id)
		backfillTestValue(t, f.db, variant.n, `SELECT COUNT(*) FROM product_units WHERE variant_id=$1 AND sequence BETWEEN 1 AND $2 AND code=$3||'-X'||LPAD(sequence::text,4,'0')`, variant.id, variant.n, variant.spnc)
	}
	backfillTestValue(t, f.db, true, `SELECT status='completed' AND units_minted=100 AND units_target=100
		AND plan_hash='test-hash' AND plan::text NOT LIKE '%plan_hash%' FROM spncx_backfill_state WHERE product_id='doubled'`)
	backfillTestValue(t, f.db, true, `SELECT snapshot->'product_stock'='100'::jsonb
		AND snapshot->'variant_stocks'=jsonb_build_object($1::text,100,$2::text,60,$3::text,40)
		AND snapshot->'balances' @> '[{"id":"double-a","quantity":70,"status":"sellable","variant_id":null,"location_key":"supplier-warehouse:double-a"},{"id":"double-b","quantity":30},{"id":"double-damaged","quantity":2,"status":"damaged"}]'
		AND jsonb_array_length(snapshot->'balances')=3 FROM spncx_backfill_snapshot WHERE product_id='doubled'`, f.def, f.large, f.small)
}

func (f backfillTestFixture) mint(product, variant, lot string, n int) {
	f.t.Helper()
	f.exec(`SELECT id FROM products WHERE id=$1 FOR UPDATE`, product)
	f.exec(`SELECT id FROM product_variants WHERE id=$1 FOR UPDATE`, variant)
	if _, err := mintUnitsTx(context.Background(), f.tx, variant, product, lot, n, "in_stock", "fixture", product, ""); err != nil {
		f.t.Fatal(err)
	}
}

func TestSpncxBackfillProductMintGap(t *testing.T) {
	f := newBackfillTestFixture(t)
	f.product("gap")
	v := f.variant("gap", "A0C00001", "legacy", 10, true)
	f.lot("gap-variant", "gap", v, 0, "sellable")
	f.mint("gap", v, "gap-variant", 3)
	f.lot("gap-unallocated", "gap", "", 7, "sellable")
	f.stock("gap", 10)
	f.commit()
	if r := f.run("gap"); r.UnitsMinted != 7 {
		t.Fatalf("mint gap: %+v", r)
	}
	backfillTestValue(t, f.db, 10, `SELECT COUNT(*) FROM product_units WHERE variant_id=$1 AND balance_id='gap-variant' AND sequence BETWEEN 1 AND 10 AND code='A0C00001-X'||LPAD(sequence::text,4,'0')`, v)
	backfillTestValue(t, f.db, 10, `SELECT quantity FROM inventory_balances WHERE id='gap-variant'`)
	backfillTestValue(t, f.db, 10, `SELECT stock FROM products WHERE id='gap'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM inventory_movements WHERE product_id='gap' AND reference_type='unit_coverage_gap'`)
}

func TestSpncxBackfillProductVariantLess(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	ctx := context.Background()
	plan, err := ComputeBackfillPlan(ctx, f.db, []string{"variant-less"}, BackfillExpectations{})
	if err != nil || !plan.AllPassed() {
		t.Fatalf("variant-less plan: %+v, %v", plan, err)
	}
	raw, err := json.Marshal(plan.Products[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO spncx_backfill_state(product_id,status,units_target,plan,plan_hash)
		VALUES('variant-less','pending',30,$1::jsonb,$2)`, string(raw), plan.PlanHash); err != nil {
		t.Fatal(err)
	}
	r, err := BackfillProduct(ctx, f.db, "variant-less", "test-actor", plan.PlanHash)
	if err != nil || r.Status != "completed" {
		t.Fatalf("variant-less run: %+v, %v", r, err)
	}
	if r.UnitsMinted != 30 || len(r.NewVariantIDs) != 1 || len(r.FlippedVariants) != 0 {
		t.Fatalf("variant-less: %+v", r)
	}
	id := r.NewVariantIDs[0]
	if id != plan.Products[0].NewVariants[0].VariantID {
		t.Fatalf("created variant %s differs from approved plan", id)
	}
	backfillTestValue(t, f.db, 1, `SELECT COUNT(*) FROM product_variants WHERE product_id='variant-less'`)
	backfillTestValue(t, f.db, true, `SELECT v.inventory_mode='tracked' AND v.is_default AND v.stock=30 AND v.variant_name=p.title
		AND v.variant_name<>'پیش‌فرض' AND p.schema_version<2 AND p.product_type<>'variable'
		FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.id=$1`, id)
	backfillTestValue(t, f.db, 30, `SELECT COUNT(*) FROM product_units WHERE variant_id=$1`, id)
	backfillTestValue(t, f.db, true, `SELECT plan->'new_variants'->0->>'variant_id'=v.id::text AND plan->'alloc'->0->>'spnc'=v.spnc
		FROM spncx_backfill_state s JOIN product_variants v ON v.product_id=s.product_id WHERE v.id=$1`, id)
	backfillTestValue(t, f.db, true, `SELECT quantity=30 AND from_status='external' AND to_status='sellable' AND metadata->>'reason'='ledger_top_up'
		FROM inventory_movements WHERE idempotency_key='spncx-backfill:variant-less:topup'`)
}

func TestSpncxBackfillProductExcess(t *testing.T) {
	f := newBackfillTestFixture(t)
	f.product("excess")
	f.variant("excess", "A0C00001", "legacy", 25, false)
	f.variant("excess", "A0C00002", "legacy", 15, false)
	f.lot("excess-a", "excess", "", 45, "sellable")
	f.lot("excess-b", "excess", "", 15, "sellable")
	f.stock("excess", 40)
	f.product("undecided")
	f.variant("undecided", "A0C00003", "legacy", 20, true)
	f.lot("undecided-a", "undecided", "", 30, "sellable")
	f.stock("undecided", 20)
	f.exec(`INSERT INTO spncx_backfill_state(product_id,status,units_target,excess_decision) VALUES('excess','pending',40,'write_off_accepted')`)
	f.commit()
	plan, err := ComputeBackfillPlan(context.Background(), f.db, []string{"excess"}, BackfillExpectations{})
	if err != nil || !plan.AllPassed() || plan.Products[0].ExcessDecision != nil ||
		!reflect.DeepEqual(plan.Products[0].LotsToReduce, []BackfillLotReduction{{"excess-a", -45}, {"excess-b", -15}}) {
		t.Fatalf("excess plan: %+v, %v", plan, err)
	}
	if r := f.run("excess"); r.Excess != 20 {
		t.Fatalf("excess: %+v", r)
	}
	backfillTestValue(t, f.db, true, `SELECT quantity=20 AND NOT unit_tracked AND NOT reconciled AND status='unreconciled'
		FROM inventory_balances WHERE location_key='spncx-excess:excess'`)
	backfillTestValue(t, f.db, 0, `SELECT SUM(quantity) FROM inventory_balances WHERE product_id='excess' AND status='sellable' AND variant_id IS NULL`)
	backfillTestValue(t, f.db, 40, `SELECT SUM(quantity) FROM inventory_balances WHERE product_id='excess' AND status='sellable' AND variant_id IS NOT NULL`)
	backfillTestValue(t, f.db, 40, `SELECT stock FROM products WHERE id='excess'`)
	backfillTestValue(t, f.db, 20, `SELECT excess_amount FROM spncx_backfill_state WHERE product_id='excess'`)
	backfillTestValue(t, f.db, true, `SELECT m.quantity=20 AND m.metadata=jsonb_build_object('reason','ledger_excess_held',
		'from_lot_ids',jsonb_build_array('excess-a','excess-b'),'from_quantity_delta',-20,'to_lot_id',b.id,'to_quantity_delta',20)
		FROM inventory_movements m JOIN inventory_balances b ON b.location_key=m.to_location_key
		WHERE m.idempotency_key='spncx-backfill:excess:excess'`)
	_, err = BackfillProduct(context.Background(), f.db, "undecided", "test-actor", "")
	if !errors.Is(err, ErrBackfillExcessUndecided) {
		t.Fatal(err)
	}
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_state WHERE product_id='undecided'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_snapshot WHERE product_id='undecided'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units WHERE product_id='undecided'`)
	backfillTestValue(t, f.db, 1, `SELECT COUNT(*) FROM inventory_balances WHERE product_id='undecided'`)
	backfillTestValue(t, f.db, 30, `SELECT quantity FROM inventory_balances WHERE id='undecided-a'`)
	if _, err = f.db.Exec(`INSERT INTO spncx_backfill_state(product_id,status,units_target,excess_decision)
		VALUES('undecided','pending',20,'physical_count_ordered')`); err != nil {
		t.Fatal(err)
	}
	if r := f.run("undecided"); r.Excess != 10 {
		t.Fatalf("decided: %+v", r)
	}
	backfillTestValue(t, f.db, 10, `SELECT quantity FROM inventory_balances WHERE location_key='spncx-excess:undecided'`)
}

func TestSpncxBackfillProductIdempotentRerun(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	f.run("equal")
	r, err := BackfillProduct(context.Background(), f.db, "equal", "test-actor", "second-hash")
	if err != nil || r.Status != "skipped_completed" || r.UnitsMinted != 0 {
		t.Fatalf("rerun: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, true, `SELECT COUNT(*)=50 AND COUNT(DISTINCT code)=50 FROM product_units WHERE product_id='equal'`)
	backfillTestValue(t, f.db, true, `SELECT status='completed' AND plan_hash='test-hash' FROM spncx_backfill_state WHERE product_id='equal'`)
}

func TestSpncxBackfillProductTracked(t *testing.T) {
	f := newBackfillTestFixture(t)
	f.product("tracked")
	v := f.variant("tracked", "A0C00001", "tracked", 5, true)
	f.lot("tracked-a", "tracked", v, 0, "sellable")
	f.mint("tracked", v, "tracked-a", 5)
	f.stock("tracked", 5)
	f.product("mismatch")
	bad := f.variant("mismatch", "A0C00002", "tracked", 4, true)
	f.lot("mismatch-a", "mismatch", bad, 3, "sellable")
	f.exec(`UPDATE product_variants SET stock=4 WHERE id=$1`, bad)
	f.stock("mismatch", 4)
	f.commit()
	r := f.run("tracked")
	if r.UnitsMinted != 0 || len(r.FlippedVariants) != 0 {
		t.Fatalf("tracked: %+v", r)
	}
	backfillTestValue(t, f.db, 1, `SELECT COUNT(*) FROM inventory_balances WHERE product_id='tracked'`)
	backfillTestValue(t, f.db, 5, `SELECT quantity FROM inventory_balances WHERE id='tracked-a' AND unit_tracked`)
	r, err := BackfillProduct(context.Background(), f.db, "mismatch", "test-actor", "")
	if !errors.Is(err, ErrBackfillPreflightFailed) || r.Status != "failed" {
		t.Fatalf("tracked mismatch: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, true, `SELECT status='failed' AND error IS NOT NULL FROM spncx_backfill_state WHERE product_id='mismatch'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units WHERE product_id='mismatch'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_snapshot WHERE product_id='mismatch'`)
}

func TestSpncxBackfillFlipZeroAlloc(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	f.run("doubled")
	backfillTestValue(t, f.db, 3, `SELECT COUNT(*) FROM product_variants WHERE product_id='doubled' AND inventory_mode='tracked'`)
	backfillTestValue(t, f.db, true, `SELECT plan->'flipped_variants' @> jsonb_build_array($1::text) FROM spncx_backfill_state WHERE product_id='doubled'`, f.def)
}

func TestSpncxBackfillNoDebit(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	for _, product := range []struct {
		id    string
		stock int
	}{{"doubled", 100}, {"equal", 50}, {"other", 75}, {"variant-less", 30}} {
		backfillTestValue(t, f.db, product.stock, `SELECT stock FROM products WHERE id=$1`, product.id)
		f.run(product.id)
		backfillTestValue(t, f.db, product.stock, `SELECT stock FROM products WHERE id=$1`, product.id)
		backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM inventory_movements WHERE product_id=$1 AND (reference_type='unit_coverage_gap' OR metadata ? 'unit_coverage_gap')`, product.id)
	}
}

func TestSpncxBackfillOrderHistoryExclusions(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	ctx := context.Background()
	if _, err := f.db.Exec(`INSERT INTO orders(id) VALUES('backfill-test-order')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO order_items(order_id,product_id,variant_id)
		VALUES('backfill-test-order','doubled',$1),('backfill-test-order','other',$2)`, f.def, f.trim); err != nil {
		t.Fatal(err)
	}
	plan, err := ComputeBackfillPlan(ctx, f.db, []string{"other", "doubled"}, BackfillExpectations{})
	if err != nil || !plan.AllPassed() {
		t.Fatalf("order history plan: %+v, %v", plan, err)
	}
	if plan.PreCutoverOrders.Count != 1 || plan.PreCutoverOrders.Items != 2 {
		t.Fatalf("order totals: %+v", plan.PreCutoverOrders)
	}
	for i, reason := range []string{"v0a_order_history_flag", "v0b_order_history_flag"} {
		p := plan.Products[i]
		if !p.Excluded || p.ExcludedReason == nil || *p.ExcludedReason != reason || p.OrderHistory.Orders != 1 {
			t.Fatalf("order history exclusion: %+v", p)
		}
		r, err := BackfillProduct(ctx, f.db, p.ProductID, "test-actor", plan.PlanHash)
		if !errors.Is(err, ErrBackfillExcluded) || r.Status != "excluded" {
			t.Fatalf("exclusion: %+v, %v", r, err)
		}
		backfillTestValue(t, f.db, reason, `SELECT excluded_reason FROM spncx_backfill_state WHERE product_id=$1 AND status='excluded'`, p.ProductID)
		backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_snapshot WHERE product_id=$1`, p.ProductID)
	}
}

func TestSpncxBackfillProductRollbackOnMovementError(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	if _, err := f.db.Exec(`INSERT INTO inventory_movements(id,product_id,quantity,reference_type,idempotency_key)
		VALUES('test-collision','doubled',1,'fixture','spncx-backfill:doubled:absorb:double-b')`); err != nil {
		t.Fatal(err)
	}
	r, err := BackfillProduct(context.Background(), f.db, "doubled", "test-actor", "")
	if err == nil || r.Status != "failed" {
		t.Fatalf("rollback: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units WHERE product_id='doubled'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_snapshot WHERE product_id='doubled'`)
	backfillTestValue(t, f.db, 100, `SELECT SUM(quantity) FROM inventory_balances WHERE product_id='doubled' AND status='sellable' AND variant_id IS NULL`)
	backfillTestValue(t, f.db, 3, `SELECT COUNT(*) FROM product_variants WHERE product_id='doubled' AND inventory_mode='legacy'`)
	backfillTestValue(t, f.db, true, `SELECT status='failed' AND error IS NOT NULL FROM spncx_backfill_state WHERE product_id='doubled'`)
	if _, err := f.db.Exec(`DELETE FROM inventory_movements WHERE id='test-collision'`); err != nil {
		t.Fatal(err)
	}
	f.run("doubled")
}

var backfillCatalogueExpectations = BackfillExpectations{
	ZeroAllocVariants: 1, GroupDoubled: 100, GroupEqual: 50, GroupOther: 75,
	GroupVariantLess: 30, TotalUnits: 255, ProductCount: 4,
}

func TestSpncxBackfillRunBatches(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	ctx := context.Background()
	opts := BackfillRunOptions{BatchSize: 2, Actor: "test-actor", Expectations: backfillCatalogueExpectations}
	for _, status := range []string{"completed", "skipped_completed"} {
		r, err := RunBackfill(ctx, f.db, opts)
		if err != nil {
			t.Fatalf("%s: %+v, %v", status, r, err)
		}
		if r.Completed+r.SkippedCompleted+r.Failed+r.ExcludedSkipped != len(r.Products) || len(r.Products) != 4 {
			t.Fatalf("summary: %+v", r)
		}
		completed, skipped, minted := 4, 0, 255
		if status == "skipped_completed" {
			completed, skipped, minted = 0, 4, 0
		}
		if r.Completed != completed || r.SkippedCompleted != skipped || r.TotalMinted != minted {
			t.Fatalf("%s: %+v", status, r)
		}
		for i, id := range []string{"doubled", "equal", "other", "variant-less"} {
			p := r.Products[i]
			if p.ProductID != id || p.Status != status {
				t.Fatalf("result %d: %+v", i, p)
			}
			minted -= p.UnitsMinted
		}
		if minted != 0 {
			t.Fatalf("unit counts diverge: %+v", r)
		}
		backfillTestValue(t, f.db, true, `SELECT COUNT(*)=255 AND COUNT(DISTINCT code)=255 FROM product_units`)
		backfillTestValue(t, f.db, 4, `SELECT COUNT(*) FROM spncx_backfill_state WHERE plan_hash=$1`, r.Plan.PlanHash)
	}
}

func TestSpncxBackfillRunRefusesPreflight(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	f.write(`UPDATE product_variants SET stock=49 WHERE id=$1`, f.equal)
	r, err := RunBackfill(context.Background(), f.db, BackfillRunOptions{Expectations: backfillCatalogueExpectations})
	if !errors.Is(err, ErrBackfillPreflightFailed) || r == nil || r.Plan == nil || r.Plan.AllPassed() || len(r.Products) != 0 {
		t.Fatalf("preflight: %+v, %v", r, err)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM product_units`, `SELECT COUNT(*) FROM spncx_backfill_state`,
		`SELECT COUNT(*) FROM spncx_backfill_snapshot`, `SELECT COUNT(*) FROM inventory_movements`,
	} {
		backfillTestValue(t, f.db, 0, query)
	}
}

func (f backfillTestFixture) write(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(query, args...); err != nil {
		f.t.Fatal(err)
	}
}

func TestSpncxBackfillRunSkipsExcluded(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	f.write(`INSERT INTO spncx_backfill_state(product_id,status,units_target,excluded_reason,excluded_by,excluded_at)
		VALUES('equal','excluded',0,'test-veto','test-owner',NOW())`)
	opts := BackfillRunOptions{Expectations: backfillCatalogueExpectations}
	r, err := RunBackfill(context.Background(), f.db, opts)
	if err != nil || r.Completed != 3 || r.ExcludedSkipped != 1 || r.Products[1].Status != "excluded_skipped" || r.TotalMinted != 205 {
		t.Fatalf("excluded run: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units WHERE product_id='equal'`)
	opts.Reinclude = map[string]string{"equal": "test owner approved"}
	opts.PlanHash = "approved-reinclude-hash"
	r, err = RunBackfill(context.Background(), f.db, opts)
	if err != nil || r.Completed != 1 || r.SkippedCompleted != 3 || r.Products[1].Status != "completed" || r.TotalMinted != 50 {
		t.Fatalf("reinclude run: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, true, `SELECT status='completed' AND plan_hash='approved-reinclude-hash'
		AND excluded_reason IS NULL AND excluded_by IS NULL AND excluded_at IS NULL
		FROM spncx_backfill_state WHERE product_id='equal'`)
}

func TestSpncxBackfillRunAdvisoryLock(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	ctx := context.Background()
	conn, err := f.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var locked bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(918273)`).Scan(&locked); err != nil || !locked {
		t.Fatalf("hold advisory lock: %t, %v", locked, err)
	}
	defer conn.ExecContext(ctx, `SELECT pg_advisory_unlock(918273)`)
	r, err := RunBackfill(ctx, f.db, BackfillRunOptions{Expectations: backfillCatalogueExpectations})
	if !errors.Is(err, ErrBackfillAlreadyRunning) || r == nil || len(r.Products) != 0 {
		t.Fatalf("concurrent run: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_state`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_snapshot`)
}

func TestSpncxBackfillRunContinuesAfterFailure(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	f.write(`INSERT INTO inventory_movements(id,product_id,quantity,reference_type,idempotency_key)
		VALUES('test-collision','doubled',1,'fixture','spncx-backfill:doubled:absorb:double-b')`)
	opts := BackfillRunOptions{BatchSize: 2, Expectations: backfillCatalogueExpectations}
	r, err := RunBackfill(context.Background(), f.db, opts)
	if err != nil || r.Failed != 1 || r.Completed != 3 || r.TotalMinted != 155 || r.Products[0].Status != "failed" {
		t.Fatalf("local failure: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, true, `SELECT status='failed' FROM spncx_backfill_state WHERE product_id='doubled'`)
	f.write(`DELETE FROM inventory_movements WHERE id='test-collision'`)
	r, err = RunBackfill(context.Background(), f.db, opts)
	if err != nil || r.Completed != 1 || r.SkippedCompleted != 3 || r.TotalMinted != 100 {
		t.Fatalf("resume after failure: %+v, %v", r, err)
	}
}

func TestSpncxBackfillRollback(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	// Sequences are per variant. Model previously issued sequences so the
	// smaller allocation (40) also starts above 100 on the second run.
	f.write(`INSERT INTO product_variant_unit_counters(variant_id,next_sequence) VALUES($1,61),($2,61)`, f.large, f.small)
	f.run("doubled")
	r, err := RollbackBackfill(context.Background(), f.db, "doubled", "test-actor", false)
	if err != nil || !r.RolledBack || !r.RestoredSnapshot || r.ProductID != "doubled" {
		t.Fatalf("rollback: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units WHERE product_id='doubled'`)
	backfillTestValue(t, f.db, 3, `SELECT COUNT(*) FROM inventory_balances WHERE product_id='doubled'`)
	backfillTestValue(t, f.db, true, `SELECT quantity=70 AND status='sellable' AND location_key='supplier-warehouse:double-a' FROM inventory_balances WHERE id='double-a'`)
	backfillTestValue(t, f.db, 30, `SELECT quantity FROM inventory_balances WHERE id='double-b'`)
	for id, stock := range map[string]int{f.def: 100, f.large: 60, f.small: 40} {
		backfillTestValue(t, f.db, stock, `SELECT stock FROM product_variants WHERE inventory_mode='legacy' AND id=$1`, id)
	}
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_state WHERE product_id='doubled'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_snapshot WHERE product_id='doubled'`)
	backfillTestValue(t, f.db, 100, `SELECT stock FROM products WHERE id='doubled'`)
	f.run("doubled")
	backfillTestValue(t, f.db, 101, `SELECT MIN(sequence) FROM product_units WHERE product_id='doubled'`)
	backfillTestValue(t, f.db, 121, `SELECT MIN(sequence) FROM product_units WHERE variant_id=$1`, f.large)
}

func TestSpncxBackfillRollbackRefusedAfterSale(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	f.run("doubled")
	f.write(`INSERT INTO orders(id) VALUES('test-sale')`)
	f.write(`INSERT INTO order_items(order_id,product_id,variant_id) VALUES('test-sale','doubled',$1)`, f.large)
	f.write(`INSERT INTO order_item_units(order_item_id,unit_id,code,order_id)
		SELECT oi.id,u.id,u.code,oi.order_id FROM order_items oi JOIN product_units u ON u.variant_id=oi.variant_id
		WHERE oi.order_id='test-sale' AND u.sequence=1`)
	f.write(`INSERT INTO product_unit_events(unit_id,code,event_type,reference_type,reference_id)
		SELECT unit_id,code,'sold','order',order_id FROM order_item_units WHERE order_id='test-sale'`)
	r, err := RollbackBackfill(context.Background(), f.db, "doubled", "test-actor", false)
	if !errors.Is(err, ErrBackfillRollbackRefused) || r.RolledBack {
		t.Fatalf("sale refusal: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, "completed", `SELECT status FROM spncx_backfill_state WHERE product_id='doubled'`)
	backfillTestValue(t, f.db, 100, `SELECT COUNT(*) FROM product_units WHERE product_id='doubled'`)
}

func TestSpncxBackfillRollbackPrinted(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	f.run("doubled")
	const code = "A0C00002-X0001"
	f.write(`UPDATE product_units SET printed_at=NOW(),print_count=1 WHERE code=$1`, code)
	f.write(`INSERT INTO product_unit_events(unit_id,code,event_type,reference_type,reference_id)
		SELECT id,code,'printed','label_print','test-print-job' FROM product_units WHERE code=$1`, code)
	f.write(`INSERT INTO spncx_print_jobs(idempotency_key,product_id,variant_id,unit_codes,payload)
		VALUES('test-print-job','doubled',$1,jsonb_build_array($2::text),jsonb_build_object('units',jsonb_build_array(jsonb_build_object('code',$2::text))))`, f.large, code)
	f.write(`INSERT INTO spncx_print_job_units(job_id,unit_id,code)
		SELECT j.id,u.id,u.code FROM spncx_print_jobs j CROSS JOIN product_units u
		WHERE j.idempotency_key='test-print-job' AND u.code=$1`, code)
	ctx := context.Background()
	r, err := RollbackBackfill(ctx, f.db, "doubled", "test-actor", false)
	if !errors.Is(err, ErrBackfillRollbackNeedsConfirmation) || r.RolledBack || !reflect.DeepEqual(r.PrintedCodesToDestroy, []string{code}) {
		t.Fatalf("printed refusal: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, 100, `SELECT COUNT(*) FROM product_units WHERE product_id='doubled'`)
	backfillTestValue(t, f.db, 1, `SELECT COUNT(*) FROM spncx_print_job_units`)
	r, err = RollbackBackfill(ctx, f.db, "doubled", "test-actor", true)
	if err != nil || !r.RolledBack || !reflect.DeepEqual(r.PrintedCodesToDestroy, []string{code}) {
		t.Fatalf("confirmed print destruction: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units WHERE product_id='doubled'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_print_job_units`)
	backfillTestValue(t, f.db, true, `SELECT unit_codes='[]'::jsonb AND payload->'units'='[]'::jsonb FROM spncx_print_jobs WHERE idempotency_key='test-print-job'`)
}

func TestSpncxBackfillRollbackAll(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	f.run("doubled")
	f.run("equal")
	r, err := RollbackBackfillAll(context.Background(), f.db, nil, "test-actor", false)
	if err != nil || len(r) != 2 || r[0].ProductID != "equal" || r[1].ProductID != "doubled" ||
		!r[0].RestoredSnapshot || !r[1].RestoredSnapshot || !r[0].RolledBack || !r[1].RolledBack {
		t.Fatalf("rollback all: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_state`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_snapshot`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units`)
}

func TestSpncxBackfillVerify(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	ctx := context.Background()
	if _, err := RunBackfill(ctx, f.db, BackfillRunOptions{Expectations: backfillCatalogueExpectations}); err != nil {
		t.Fatal(err)
	}
	ids := []string{"doubled", "equal", "other", "variant-less"}
	for _, tampered := range []bool{false, true} {
		if tampered {
			f.write(`UPDATE inventory_balances SET quantity=quantity+1 WHERE variant_id=$1 AND status='sellable'`, f.large)
		}
		for _, id := range ids {
			r, err := VerifyBackfillProduct(ctx, f.db, id)
			if err != nil || r.ProductID != id || len(r.Checks) != 11 {
				t.Fatalf("verification %s: %+v, %v", id, r, err)
			}
			failed := make(map[string]bool)
			for _, c := range r.Checks {
				if c.Passed != (c.RowCount == 0) {
					t.Fatalf("%s inconsistent check: %+v", id, c)
				}
				if id != "variant-less" && (c.ID == "V9" || c.ID == "V12") && c.Name != "new_variants (n/a)" {
					t.Fatalf("%s unexpected new-variant check: %+v", id, c)
				}
				if !c.Passed {
					failed[c.ID] = true
				}
			}
			if tampered && id == "doubled" {
				if !failed["V1"] && !failed["V2"] {
					t.Fatalf("tampered product passed stock/lot checks: %+v", r)
				}
			} else if len(failed) != 0 {
				t.Fatalf("%s unexpected failures: %+v", id, r)
			}
		}
	}
}

func TestSpncxBackfillVerifyNewVariants(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	ctx := context.Background()
	if _, err := VerifyBackfillProduct(ctx, f.db, "variant-less"); !errors.Is(err, ErrBackfillNotCompleted) {
		t.Fatalf("verify before completion: %v", err)
	}
	f.run("variant-less")
	r, err := VerifyBackfillProduct(ctx, f.db, "variant-less")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"V9": "new_variants", "V12": "variant_less_resolution"}
	for _, c := range r.Checks {
		if name, ok := want[c.ID]; ok {
			if c.Name != name || !c.Passed || c.RowCount != 0 {
				t.Fatalf("new variant check: %+v", c)
			}
			delete(want, c.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing checks: %v", want)
	}
}

func TestSpncxBackfillRollbackPreservesPreexistingUnits(t *testing.T) {
	f := newBackfillTestFixture(t)
	f.product("gap")
	v := f.variant("gap", "A0C00001", "legacy", 10, true)
	f.lot("gap-variant", "gap", v, 0, "sellable")
	f.mint("gap", v, "gap-variant", 3)
	f.lot("gap-unallocated", "gap", "", 7, "sellable")
	f.stock("gap", 10)
	f.commit()
	f.run("gap")
	f.write(`INSERT INTO product_unit_events(unit_id,code,event_type,reference_type)
		SELECT id,code,'printed','fixture' FROM product_units WHERE variant_id=$1 AND sequence=1`, v)
	r, err := RollbackBackfill(context.Background(), f.db, "gap", "test-actor", false)
	if err != nil || !r.RolledBack || len(r.PrintedCodesToDestroy) != 0 {
		t.Fatalf("rollback with preexisting units: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, true, `SELECT COUNT(*)=3 AND MAX(sequence)=3 FROM product_units WHERE variant_id=$1 AND balance_id='gap-variant'`, v)
	backfillTestValue(t, f.db, 3, `SELECT quantity FROM inventory_balances WHERE id='gap-variant'`)
	backfillTestValue(t, f.db, 4, `SELECT COUNT(*) FROM product_unit_events e JOIN product_units u ON u.id=e.unit_id WHERE u.variant_id=$1`, v)
	backfillTestValue(t, f.db, 1, `SELECT COUNT(*) FROM inventory_movements WHERE product_id='gap' AND reference_type='fixture'`)
}

func TestSpncxBackfillRollbackRefusesInconsistentSnapshotLot(t *testing.T) {
	f := newBackfillTestFixture(t)
	f.product("gap")
	v := f.variant("gap", "A0C00001", "legacy", 10, true)
	f.lot("gap-variant", "gap", v, 0, "sellable")
	f.mint("gap", v, "gap-variant", 3)
	f.lot("gap-unallocated", "gap", "", 7, "sellable")
	f.stock("gap", 10)
	f.commit()
	f.run("gap")
	f.write(`INSERT INTO inventory_balances(id,product_id,variant_id,supplier_id,owner_type,location_type,location_key,supplier_warehouse_id,status,quantity,unit_tracked)
		VALUES('sold-existing','gap',$1,'s','supplier','supplier','sold:existing','sw1','sold',1,TRUE)`, v)
	f.write(`UPDATE inventory_balances SET quantity=quantity-1 WHERE id='gap-variant'`)
	f.write(`UPDATE product_units SET status='sold',balance_id='sold-existing' WHERE variant_id=$1 AND sequence=1`, v)
	f.write(`INSERT INTO product_unit_events(unit_id,code,event_type,reference_type)
		SELECT id,code,'sold','fixture' FROM product_units WHERE variant_id=$1 AND sequence=1`, v)
	r, err := RollbackBackfill(context.Background(), f.db, "gap", "test-actor", false)
	if !errors.Is(err, ErrBackfillRollbackRefused) || r.RolledBack {
		t.Fatalf("rollback would overwrite live lot: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, 9, `SELECT quantity FROM inventory_balances WHERE id='gap-variant'`)
	backfillTestValue(t, f.db, 10, `SELECT COUNT(*) FROM product_units WHERE product_id='gap'`)
	backfillTestValue(t, f.db, "sold-existing", `SELECT balance_id FROM product_units WHERE variant_id=$1 AND sequence=1`, v)
}

func TestSpncxBackfillRollbackRemovesCreatedVariant(t *testing.T) {
	f := newBackfillTestFixture(t)
	f.product("variant-less")
	f.lot("variant-less-stock", "variant-less", "", 30, "sellable")
	f.commit()
	// Enforce the production mint-movement FK in this scratch fixture.
	f.write(`ALTER TABLE inventory_movements ADD FOREIGN KEY (variant_id) REFERENCES product_variants(id) ON DELETE RESTRICT`)
	p := f.run("variant-less")
	ctx := context.Background()
	r, err := RollbackBackfill(ctx, f.db, "variant-less", "test-actor", false)
	if err != nil || !r.RestoredSnapshot {
		t.Fatalf("rollback of created variant: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_variants WHERE product_id='variant-less'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_variant_unit_counters WHERE variant_id=$1`, p.NewVariantIDs[0])
	backfillTestValue(t, f.db, 30, `SELECT stock FROM products WHERE id='variant-less'`)
	backfillTestValue(t, f.db, 30, `SELECT quantity FROM inventory_balances WHERE id='variant-less-stock'`)
}

func TestSpncxBackfillRollbackAllContinuesAfterRefusal(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	f.run("doubled")
	f.run("equal")
	f.write(`DELETE FROM inventory_balances WHERE id='equal-a'`)
	r, err := RollbackBackfillAll(context.Background(), f.db, []string{"doubled", "other", "equal"}, "test-actor", false)
	if !errors.Is(err, ErrBackfillRollbackRefused) || !errors.Is(err, ErrBackfillNotCompleted) ||
		len(r) != 3 || r[0].ProductID != "other" || r[0].RolledBack || r[1].ProductID != "equal" ||
		r[1].RolledBack || r[2].ProductID != "doubled" || !r[2].RolledBack {
		t.Fatalf("rollback all with refusal: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, 50, `SELECT COUNT(*) FROM product_units WHERE product_id='equal'`)
	backfillTestValue(t, f.db, "completed", `SELECT status FROM spncx_backfill_state WHERE product_id='equal'`)
	backfillTestValue(t, f.db, 1, `SELECT COUNT(*) FROM spncx_backfill_snapshot WHERE product_id='equal'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units WHERE product_id='doubled'`)
}

func TestSpncxBackfillRollbackRefusesStrict(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	f.run("equal")
	previousStrict := spncxStrict
	t.Cleanup(func() { spncxStrict = previousStrict })
	spncxStrict = true
	r, err := RollbackBackfill(context.Background(), f.db, "equal", "test-actor", false)
	if !errors.Is(err, ErrBackfillRollbackRefused) || r.RolledBack {
		t.Fatalf("strict rollback: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, "completed", `SELECT status FROM spncx_backfill_state WHERE product_id='equal'`)
	backfillTestValue(t, f.db, 50, `SELECT COUNT(*) FROM product_units WHERE product_id='equal'`)
}

func TestSpncxBackfillRollbackTopUpOnly(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	f.run("variant-less")
	// This product began with positive stock but no balance rows. The created
	// lot rows are deleted (not decremented), so the stock projection's EXISTS
	// guard leaves products.stock at the snapshot value and the rollback
	// succeeds: 0 variants, 0 units, stock restored.
	r, err := RollbackBackfill(context.Background(), f.db, "variant-less", "test-actor", false)
	if err != nil || !r.RolledBack || !r.RestoredSnapshot {
		t.Fatalf("top-up-only rollback: %+v, %v", r, err)
	}
	backfillTestValue(t, f.db, 30, `SELECT stock FROM products WHERE id='variant-less'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units WHERE product_id='variant-less'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_variants WHERE product_id='variant-less'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_state WHERE product_id='variant-less'`)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM spncx_backfill_snapshot WHERE product_id='variant-less'`)
}
