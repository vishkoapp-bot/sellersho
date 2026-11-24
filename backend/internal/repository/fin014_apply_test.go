package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// FIN-014 W6 — repair-batch apply (audit §5.2 #4, the rerun proof).
//
// report -> apply -> report -> apply: the second report has zero
// remaining certain defects among the apply-eligible classes, and the
// second apply writes zero new rows (counts + hashes of the affected
// tables before/after). Held-certain defects persist with documented
// reasons (settlement item: Q4; coupon-1: cap ratification; the D4
// dispute release, if the release path refuses it).

func TestFIN014Apply(t *testing.T) {
	db := fin014FixtureDB(t)
	ctx := context.Background()
	loadFIN014Fixture(t, db, "f14a-", "2026-10")
	report := fin014ReportForFixture(t, db, "f14a-")
	assertFIN014ExpectedCounts(t, report)
	proposal, err := FIN014ProposeBatch(ctx, db, report, FIN014NewBatchID(), FIN014NewRunID())
	if err != nil {
		t.Fatal(err)
	}
	if err := StoreFIN014Proposal(ctx, db, report, proposal, nil); err != nil {
		t.Fatalf("store proposal: %v", err)
	}
	approveBatch(t, db, proposal.BatchID)

	// First apply: the 8 apply-eligible certain defects, except the
	// D4 dispute release if the release path refuses it (then it is
	// recorded as failed and stays reported — see below).
	result, err := FIN014ApplyBatch(ctx, db, proposal.BatchID, 25)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	d4aFailed := result.Applied == 7
	if result.Applied != 8 && result.Applied != 7 {
		t.Logf("first apply failed ops: %v", result.FailedOps)
		t.Fatalf("first apply applied=%d failed=%d, want 8 applied (or 7 + the D4 release failed)", result.Applied, result.Failed)
	}
	if result.Applied+result.Failed != 8 {
		t.Fatalf("first apply applied+failed=%d, want 8 (the proposed operations)", result.Applied+result.Failed)
	}
	if d4aFailed {
		if result.Failed != 1 {
			t.Fatalf("first apply applied=7 but failed=%d, want 1 (the D4 release)", result.Failed)
		}
		var failedOp string
		if err := db.QueryRow(`SELECT record_id FROM fin014_repair_operations WHERE batch_id=$1 AND status='failed'`, proposal.BatchID).Scan(&failedOp); err != nil {
			t.Fatal(err)
		}
		if failedOp != "f14a-dispute-d4a" {
			t.Fatalf("failed operation record=%s, want f14a-dispute-d4a (the release path refusing is the only expected failure)", failedOp)
		}
		t.Logf("apply: D4 dispute release refused by the release path; the defect stays reported (documented held-certain)")
	}
	if result.Done && result.Failed != 0 {
		t.Errorf("apply done=true with %d failed operations, want done=false while a failure stands", result.Failed)
	}
	if !result.Done && result.Failed == 0 {
		t.Errorf("apply done=false with zero failures, want done=true (nothing proposed remains)")
	}

	// Second report: zero remaining certain defects among the
	// apply-eligible classes. The only certain rows that may remain
	// are the held ones: settlement (Q4), coupon-1 (cap), and the D4
	// dispute if its release was refused.
	second := fin014ReportForFixture(t, db, "f14a-")
	allowedCertain := map[string]bool{
		"f14a-settle-1": true, "f14a-coupon-1": true,
	}
	if result.Applied == 7 {
		allowedCertain["f14a-dispute-d4a"] = true
	}
	for _, r := range second.Rows {
		if r.Confidence == FIN014ConfidenceCertain && !allowedCertain[r.RecordID] {
			t.Errorf("second report still has certain defect %s/%s (apply-eligible, should be repaired)", r.Class, r.RecordID)
		}
	}
	for _, id := range []string{"f14a-payout-1", "f14a-parcel-1", "f14a-parcel-2", "f14a-parcel-3", "f14a-coupon-2", "f14a-order-d7"} {
		for _, r := range second.Rows {
			if r.RecordID == id {
				t.Errorf("second report still lists repaired record %s", id)
			}
		}
	}
	// c2m is the honest exception: creating the obligation moves the
	// defect from "missing obligation" (certain) to "obligation with
	// unknown outcome" (unresolved) — the refund itself needs a
	// provider attempt + gateway receipt, which the dry-run apply
	// cannot fabricate. It must no longer be certain.
	for _, r := range second.Rows {
		if r.RecordID == "f14a-order-c2m" && r.Confidence == FIN014ConfidenceCertain {
			t.Errorf("second report still has c2m as certain; the created obligation should have moved it to unresolved")
		}
	}
	// The repairs are compensating events, not rewrites: the original
	// rows are untouched (obligation created, not edited; reversal
	// event posted alongside the original debits).
	var c2mObligations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM order_refund_obligations WHERE order_id='f14a-order-c2m' AND purpose='cancellation'`).Scan(&c2mObligations); err != nil || c2mObligations != 1 {
		t.Errorf("c2m obligations=%d, want 1 (created, not edited)", c2mObligations)
	}
	var d7Reversals int
	if err := db.QueryRow(`SELECT COUNT(*) FROM seller_profit_reversals WHERE order_id='f14a-order-d7'`).Scan(&d7Reversals); err != nil || d7Reversals != 1 {
		t.Errorf("d7 seller-profit reversals=%d, want 1", d7Reversals)
	}

	// Second apply: zero new rows (counts + hashes of every table the
	// report touches, before/after).
	before := fin014TableFingerprints(t, db)
	secondResult, err := FIN014ApplyBatch(ctx, db, proposal.BatchID, 25)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if secondResult.Applied != 0 || secondResult.Failed != 0 {
		t.Errorf("second apply applied=%d failed=%d, want 0/0", secondResult.Applied, secondResult.Failed)
	}
	if !secondResult.Done {
		t.Errorf("second apply done=false, want true")
	}
	after := fin014TableFingerprints(t, db)
	for tbl, h := range before {
		if after[tbl] != h {
			t.Errorf("table %s changed across the second (no-op) apply: before=%s after=%s", tbl, h, after[tbl])
		}
	}

	// Approval gate (Q8): an unapproved batch refuses to apply.
	// Second copy, a different cycle period (the settlement cycle is
	// keyed by period, so a same-period copy would collide).
	loadFIN014Fixture(t, db, "f14b-", "2026-11")
	reportB := fin014ReportForFixture(t, db, "f14b-")
	proposalB, err := FIN014ProposeBatch(ctx, db, reportB, FIN014NewBatchID(), FIN014NewRunID())
	if err != nil {
		t.Fatal(err)
	}
	if err := StoreFIN014Proposal(ctx, db, reportB, proposalB, nil); err != nil {
		t.Fatalf("store proposal B: %v", err)
	}
	if _, err := FIN014ApplyBatch(ctx, db, proposalB.BatchID, 25); err == nil {
		t.Errorf("unapproved batch applied, want refusal (Q8: owner has not named the approver)")
	} else if !strings.Contains(err.Error(), "Q8") && !strings.Contains(err.Error(), "approv") {
		t.Errorf("unapproved batch refusal %q does not name the approval gap", err.Error())
	}

	// Broken predicate: mutate a source row after proposing, then
	// apply. That operation aborts (failed, with the reason) and the
	// batch stays consistent: the other operations apply, the broken
	// one writes no partial effect.
	approveBatch(t, db, proposalB.BatchID)
	if _, err := db.Exec(`UPDATE payout_requests SET amount=999 WHERE id='f14b-payout-1'`); err != nil {
		t.Fatal(err)
	}
	broken, err := FIN014ApplyBatch(ctx, db, proposalB.BatchID, 25)
	if err != nil {
		t.Fatalf("broken-predicate apply: %v", err)
	}
	if broken.Failed != 1 {
		t.Fatalf("broken-predicate apply failed=%d, want 1", broken.Failed)
	}
	if broken.Applied != 7 && broken.Applied != 6 {
		t.Fatalf("broken-predicate apply applied=%d, want 7 (or 6 if the D4 release also refused)", broken.Applied)
	}
	if broken.Done {
		t.Errorf("broken-predicate apply done=true with a failed operation, want false (resumable)")
	}
	var payoutStatus string
	var payoutAmount int64
	if err := db.QueryRow(`SELECT status, amount FROM payout_requests WHERE id='f14b-payout-1'`).Scan(&payoutStatus, &payoutAmount); err != nil {
		t.Fatal(err)
	}
	if payoutStatus != "pending" || payoutAmount != 999 {
		t.Errorf("broken payout request status=%s amount=%d, want pending/999 (no partial effect)", payoutStatus, payoutAmount)
	}
	var failedReason string
	if err := db.QueryRow(`SELECT hold_reason FROM fin014_repair_operations WHERE batch_id=$1 AND status='failed'`, proposalB.BatchID).Scan(&failedReason); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(failedReason, "predicate mismatch") {
		t.Errorf("failed operation reason %q does not name the predicate mismatch", failedReason)
	}
	// The failed operation can be re-proposed with fresh predicates
	// once the data is fixed (W6 resumability): restore the amount,
	// re-propose, and store — the same operation id is refreshed,
	// not duplicated.
	if _, err := db.Exec(`UPDATE payout_requests SET amount=1500000 WHERE id='f14b-payout-1'`); err != nil {
		t.Fatal(err)
	}
	reReport := fin014ReportForFixture(t, db, "f14b-")
	reProposal, err := FIN014ProposeBatch(ctx, db, reReport, FIN014NewBatchID(), FIN014NewRunID())
	if err != nil {
		t.Fatal(err)
	}
	// Re-propose ONLY the failed operation: the re-report also
	// regenerates the held operations, which stay stored in their
	// original batch — a re-store of those must still collide.
	var payoutOp *FIN014Operation
	for i := range reProposal.Operations {
		if reProposal.Operations[i].RecordID == "f14b-payout-1" {
			payoutOp = &reProposal.Operations[i]
		}
	}
	if payoutOp == nil {
		t.Fatal("re-proposal has no payout-1 operation")
	}
	reProposal.Operations = []FIN014Operation{*payoutOp}
	if err := StoreFIN014Proposal(ctx, db, reReport, reProposal, nil); err != nil {
		t.Fatalf("re-proposing the failed operation: %v", err)
	}
	var reStatus string
	if err := db.QueryRow(`SELECT status FROM fin014_repair_operations WHERE batch_id=$1 AND record_id='f14b-payout-1'`, reProposal.BatchID).Scan(&reStatus); err != nil || reStatus != "proposed" {
		t.Errorf("re-proposed payout operation status=%q err=%v, want proposed", reStatus, err)
	}
}

// approveBatch records the Q8 approval on a batch (test-only stand-in
// for the owner naming the approver; the apply command never does
// this itself).
func approveBatch(t *testing.T, db *sql.DB, batchID string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE fin014_repair_batches SET status='approved', approved_by='fin014-test-approver', approved_at=NOW() WHERE id=$1`, batchID); err != nil {
		t.Fatalf("approve batch %s: %v", batchID, err)
	}
}

func TestFIN014ApplyDisposableGuard(t *testing.T) {
	db := fin014FixtureDB(t)
	if err := FIN014RequireDisposableDB(context.Background(), db); err != nil {
		t.Fatalf("disposable guard refused the test database: %v", err)
	}
	// Simulate the missing-marker rejection in the verified disposable DB,
	// never by opening a second connection to the maintenance database.
	var marker string
	if err := db.QueryRow(`DELETE FROM public.sellersho_test_guard WHERE singleton = TRUE RETURNING database_name`).Scan(&marker); err != nil {
		t.Fatalf("remove disposable guard marker: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`INSERT INTO public.sellersho_test_guard (singleton, database_name) VALUES (TRUE, $1)`, marker); err != nil {
			t.Errorf("restore disposable guard marker: %v", err)
		}
	})
	if err := FIN014RequireDisposableDB(context.Background(), db); err == nil {
		t.Errorf("disposable guard accepted a database with no guard marker, want refusal")
	}
}
