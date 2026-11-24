package repository

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSpncxBackfillAdminPersist(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	ctx := context.Background()
	persist := func() *BackfillPlan {
		t.Helper()
		plan, err := ComputeBackfillPlan(ctx, f.db, nil, backfillCatalogueExpectations)
		if err != nil {
			t.Fatal(err)
		}
		if err := PersistBackfillDryRunPlan(ctx, f.db, plan); err != nil {
			t.Fatal(err)
		}
		return plan
	}
	f.write(`UPDATE inventory_balances SET quantity=90 WHERE id='other-a'`)
	f.write(`UPDATE products SET stock=75 WHERE id='other'`)
	first := persist()
	if first.DiffVsPreviousDryRun != nil {
		t.Fatalf("first dry run diff: %+v", first.DiffVsPreviousDryRun)
	}
	backfillTestValue(t, f.db, 4, `SELECT COUNT(*) FROM spncx_backfill_state WHERE status='pending'
		AND plan_hash=$1 AND plan::text NOT LIKE '%plan_hash%'`, first.PlanHash)
	backfillTestValue(t, f.db, 0, `SELECT COUNT(*) FROM product_units`)
	if _, err := RecordBackfillExcessDecision(ctx, f.db, "other", "physical_count_ordered", "owner"); err != nil {
		t.Fatal(err)
	}
	f.run("equal")
	var completedPlan string
	if err := f.db.QueryRow(`SELECT plan::text FROM spncx_backfill_state WHERE product_id='equal'`).Scan(&completedPlan); err != nil {
		t.Fatal(err)
	}
	f.write(`UPDATE products SET stock=74 WHERE id='other'`)
	f.write(`UPDATE spncx_backfill_state SET status='failed',error='retry me' WHERE product_id='other'`)
	next := persist()
	diff, ok := next.DiffVsPreviousDryRun.(*BackfillPlanDiff)
	if !ok || len(diff.ChangedProducts) != 1 || diff.ChangedProducts[0] != (BackfillProductDiff{
		ProductID: "other", AllocChanged: true, ExcessChanged: true,
	}) {
		t.Fatalf("unexpected diff: %+v", next.DiffVsPreviousDryRun)
	}
	backfillTestValue(t, f.db, true, `SELECT status='pending' AND error IS NULL
		AND excess_decision='physical_count_ordered' AND units_target=74 AND excess_amount=16
		FROM spncx_backfill_state WHERE product_id='other'`)
	backfillTestValue(t, f.db, true, `SELECT status='completed' AND plan::text=$1
		FROM spncx_backfill_state WHERE product_id='equal'`, completedPlan)
	backfillTestValue(t, f.db, 50, `SELECT COUNT(*) FROM product_units`)
}

func TestSpncxBackfillAdminZeroAllocAssertion(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	ctx := context.Background()
	prod, err := ComputeBackfillPlan(ctx, f.db, nil, ProdBackfillExpectations())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range prod.GlobalAssertions {
		if a.Name == "zero_alloc_count" {
			found = !a.Passed && a.Details == "got 1, expected 40"
		}
	}
	if !found || prod.AllPassed() {
		t.Fatalf("production assertion missing: %+v", prod.GlobalAssertions)
	}
	exp := BackfillExpectations{ZeroAllocVariants: 1, GroupDoubled: 100, GroupEqual: 50, GroupOther: 75, GroupVariantLess: 30, TotalUnits: 255, ProductCount: 4}
	plan, err := ComputeBackfillPlan(ctx, f.db, nil, exp)
	if err != nil || !plan.AllPassed() {
		t.Fatalf("fixture assertions: %+v, %v", plan, err)
	}
}

func TestSpncxBackfillAdminExcessAndStatus(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	ctx := context.Background()
	decide := func(id, decision, actor string) (*BackfillExcessDecision, error) {
		return RecordBackfillExcessDecision(ctx, f.db, id, decision, actor)
	}
	for _, tc := range []struct {
		id, decision string
		want         error
	}{
		{"missing", "bad", ErrBackfillInvalidExcessDecision},
		{"missing", "write_off_accepted", ErrBackfillExcessProductNotFound},
		{"other", "write_off_accepted", ErrBackfillNoExcessToDecide},
	} {
		if _, err := decide(tc.id, tc.decision, "actor"); !errors.Is(err, tc.want) {
			t.Fatalf("decide %s: got %v, want %v", tc.id, err, tc.want)
		}
	}
	f.write(`UPDATE inventory_balances SET quantity=90 WHERE id='other-a'`)
	f.write(`UPDATE products SET stock=75 WHERE id='other'`)
	plan, err := ComputeBackfillPlan(ctx, f.db, nil, backfillCatalogueExpectations)
	if err != nil {
		t.Fatal(err)
	}
	if err := PersistBackfillDryRunPlan(ctx, f.db, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := decide("doubled", "write_off_accepted", "actor"); !errors.Is(err, ErrBackfillNoExcessToDecide) {
		t.Fatal(err)
	}
	first, err := decide("other", "write_off_accepted", "first-actor")
	if err != nil || first.ReviewBy != nil || first.Amount != 15 || first.DecidedBy != "first-actor" {
		t.Fatalf("initial decision: %+v, %v", first, err)
	}
	f.run("other")
	second, err := decide("other", "physical_count_ordered", "second-actor")
	if err != nil || second.ReviewBy == nil || !second.DecidedAt.After(first.DecidedAt) || second.DecidedBy != "second-actor" {
		t.Fatalf("replacement decision: %+v, %v", second, err)
	}
	f.write(`UPDATE spncx_backfill_state SET status='excluded',excluded_reason='veto',
		excluded_by='owner',excluded_at=NOW() WHERE product_id='equal'`)
	status, err := ReadBackfillStatus(ctx, f.db)
	if err != nil || status.Running || len(status.Products) != 4 ||
		status.Summary.Completed != 1 || status.Summary.Excluded != 1 ||
		status.Summary.Pending != 2 || status.Summary.TotalUnitsMinted != 75 {
		t.Fatalf("status: %+v, %v", status, err)
	}
	excluded, completed := status.Products[1], status.Products[2]
	if excluded.ProductID != "equal" || excluded.Status != "excluded" || excluded.ExcludedAt == nil ||
		excluded.ExcludedReason == nil || *excluded.ExcludedReason != "veto" ||
		excluded.ExcludedBy == nil || *excluded.ExcludedBy != "owner" {
		t.Fatalf("exclusion: %+v", excluded)
	}
	if completed.CompletedAt == nil || completed.ExcessHeld != 15 || completed.ExcessReviewBy == nil ||
		!second.ReviewBy.Equal(completed.CompletedAt.Add(30*24*time.Hour)) ||
		!completed.ExcessReviewBy.Equal(*second.ReviewBy) {
		t.Fatalf("completed product: %+v, decision: %+v", completed, second)
	}
	backfillTestValue(t, f.db, true, `SELECT excess_decision='physical_count_ordered'
		AND excess_decided_by='second-actor' AND excess_decided_at=$1
		FROM spncx_backfill_state WHERE product_id='other'`, second.DecidedAt)
}

func TestSpncxBackfillAdminReconciliation(t *testing.T) {
	f := newBackfillTestCatalogue(t)
	ctx := context.Background()
	f.write(`UPDATE inventory_balances SET quantity=90 WHERE id='other-a'`)
	f.write(`UPDATE products SET stock=75 WHERE id='other'`)
	f.write(`INSERT INTO spncx_backfill_state(product_id,status,units_target,excess_amount,excess_decision)
		VALUES('other','pending',75,15,'write_off_accepted')`)
	result, err := RunBackfill(ctx, f.db, BackfillRunOptions{Expectations: backfillCatalogueExpectations, Actor: "actor"})
	if err != nil || result.Completed != 4 {
		t.Fatalf("run: %+v, %v", result, err)
	}
	read := func() *ReconciliationReport {
		t.Helper()
		report, err := RunReconciliationReport(ctx, f.db)
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	report := read()
	var counters int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM spncx_backfill_state s,
		jsonb_array_elements(s.plan->'alloc') a WHERE s.status='completed' AND (a->>'alloc')::int>0`).Scan(&counters); err != nil {
		t.Fatal(err)
	}
	for _, check := range report.Checks {
		switch check := check.(type) {
		case reconciliationRowsCheck:
			if !check.Passed {
				t.Errorf("unexpected failure: %+v", check)
			}
			if check.ID == "V6b" && check.RowCount != 1 {
				t.Errorf("missing V6b decision row: %+v", check)
			}
		case reconciliationTotalsCheck:
			if !check.Passed || check.Expected.Counters != counters || check.Totals.Counters != counters {
				t.Errorf("V4: %+v", check)
			}
		case reconciliationCompletionCheck:
			if !check.Passed {
				t.Errorf("V5: %+v", check)
			}
		case reconciliationPhysicalCheck:
			if check.Passed || len(check.SampledProducts) != 0 || len(check.Mismatches) != 0 {
				t.Errorf("V10: %+v", check)
			}
		default:
			t.Errorf("unexpected check type: %T", check)
		}
	}
	if len(report.HeldLots) != 1 {
		t.Fatalf("expected held excess: %+v", report.HeldLots)
	}
	f.write(`INSERT INTO orders(id) VALUES('b6-reconciliation-order')`)
	f.write(`INSERT INTO order_items(order_id,product_id,variant_id)
		VALUES('b6-reconciliation-order','other',$1),('b6-reconciliation-order','other',$2)`, f.keep, f.trim)
	if check := read().Checks[0].(reconciliationRowsCheck); !check.Passed || check.RowCount != 0 {
		t.Fatalf("equal V0b evidence should pass: %+v", check)
	}
	f.write(`INSERT INTO order_items(order_id,product_id,variant_id) VALUES('b6-reconciliation-order','other',$1)`, f.trim)
	f.write(`DELETE FROM product_unit_events WHERE unit_id=(SELECT id FROM product_units WHERE status='in_stock' ORDER BY id LIMIT 1)`)
	f.write(`DELETE FROM product_units WHERE id=(SELECT id FROM product_units WHERE status='in_stock' ORDER BY id LIMIT 1)`)
	f.write(`UPDATE spncx_backfill_state SET excess_decision=NULL,completed_at=NOW()-INTERVAL '31 days'
		WHERE product_id='other'`)
	report = read()
	for _, i := range []int{0, 1, 9} {
		check := report.Checks[i].(reconciliationRowsCheck)
		if check.Passed || check.RowCount == 0 {
			t.Errorf("expected failed check: %+v", check)
		}
	}
}
