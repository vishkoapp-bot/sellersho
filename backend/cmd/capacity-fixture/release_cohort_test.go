package main

import (
	"context"
	"os"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
)

// TestCapacityReleaseCohort exercises the app's real release path
// (ReleaseClosedReturnWindows) on the seeded 100k-order floor. It is
// deliberately separate from TestCapacityDataFloor: the release path
// has a 300ms per-candidate budget in app code, and on a small shared
// VM its eligibility chain exceeds that budget, so candidates time out
// (0/100 on 2026-11-20, every candidate cancelled with 57014).
// Measured per-candidate cost on the disposable DB (all warm buffers):
// ledger_entries seq scan ~23ms (no index on related_order_or_dispute),
// order_item_origins cast join ~49ms (oi.id::text = o.order_item_id::text
// defeats the index), order_items by order_id seq scan ~21ms,
// payment_transactions by order_id seq scan ~9ms — plus the remaining
// eligibility queries, locks and ledger posting. That is an APP
// capacity finding (release cost scales with ledger/origins table
// size), not a harness defect. The cohort is therefore its own gate:
// the runners record its released count + status in the manifest and
// never let it block the data floor or the driver profiles.
func TestCapacityReleaseCohort(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	var seeded int
	if err := db.QueryRow("SELECT count(*) FROM orders WHERE id LIKE 'cap-order-%'").Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	if seeded < 100000 {
		t.Fatalf("release cohort needs the seeded floor: cap orders=%d want >= 100000 (run TestCapacityDataFloor first)", seeded)
	}
	ctx := context.Background()
	orders := repository.NewOrderRepository(db)
	released, err := orders.ReleaseClosedReturnWindows(ctx)
	status := "PASS"
	if err != nil || released != 100 {
		status = "FAILED"
	}
	t.Logf("CAPACITY_RELEASE_COHORT released=%d want=100 status=%s err=%v", released, status, err)
	if n, err := orders.ReleaseClosedReturnWindows(ctx); err != nil || n != 0 {
		t.Fatalf("release replay: count=%d err=%v", n, err)
	}
	// Paired posted release: every released cap order must have exactly
	// two balanced ledger entries (seller credit + platform debit).
	var bad int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT related_order_or_dispute FROM ledger_entries WHERE related_order_or_dispute LIKE 'cap-order-%' GROUP BY related_order_or_dispute HAVING sum(CASE WHEN type='credit' THEN amount ELSE -amount END)<>0 OR count(*)<>2) bad`).Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Fatalf("paired posted release: %d violations", bad)
	}
	if status == "FAILED" {
		t.Fatalf("release cohort: released=%d want=100 err=%v", released, err)
	}
}
