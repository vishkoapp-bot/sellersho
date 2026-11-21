package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/drup/platform/internal/pricing"
)

// FIN-014 W5 — repair-batch proposal (audit §5.1).
//
// Gate: every certain defect gets exactly one operation, every
// ambiguous/unresolved case gets a held operation with a reason, and
// re-proposing the same report yields the same operation IDs.

func TestFIN014Proposal(t *testing.T) {
	db := fin014FixtureDB(t)
	unresolvedLot, _ := loadFIN014Fixture(t, db, "f14p-", "2026-10")
	ctx := context.Background()
	report := fin014ReportForFixture(t, db, "f14p-")
	assertFIN014ExpectedCounts(t, report)

	proposal, err := FIN014ProposeBatch(ctx, db, report, FIN014NewBatchID(), FIN014NewRunID())
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Operations) != len(report.Rows) {
		t.Fatalf("proposal operations=%d, want %d (one per report row)", len(proposal.Operations), len(report.Rows))
	}
	byRecord := map[string]FIN014Operation{}
	for _, op := range proposal.Operations {
		if _, dup := byRecord[op.RecordID]; dup {
			t.Errorf("record %s has two operations, want exactly one", op.RecordID)
		}
		byRecord[op.RecordID] = op
	}
	// Proposed (apply-eligible certain) vs held, per record.
	wantProposed := map[string]string{
		"f14p-payout-1":    "reject_payout_request",
		"f14p-order-c2m":   "create_cancellation_obligation",
		"f14p-parcel-1":    "post_parcel_reversal",
		"f14p-parcel-2":    "post_parcel_reversal",
		"f14p-parcel-3":    "post_parcel_reversal",
		"f14p-dispute-d4a": "release_dispute_profit",
		"f14p-coupon-2":    "correct_coupon_used_count",
		"f14p-order-d7":    "record_seller_profit_reversal",
	}
	wantHeld := map[string]string{
		"f14p-settle-1":        "Q4",
		"f14p-order-c2u":       "unknown",
		"f14p-dispute-d4b":     "review_provenance_missing",
		"f14p-coupon-1":        "ratify",
		"f14p-order-d7b":       "invisible",
		"f14p-store-a:2026-10": "opening",
		"f14p-order-c8a":       "FIN-010",
		"f14p-order-c8b":       "FIN-010",
		"f14p-order-c8c":       "FIN-010",
		"f14p-order-c8d":       "FIN-010",
	}
	for id, action := range wantProposed {
		op, ok := byRecord[id]
		if !ok {
			t.Errorf("proposed record %s missing", id)
			continue
		}
		if op.Status != "proposed" {
			t.Errorf("record %s status=%q, want proposed", id, op.Status)
		}
		if op.Effect["action"] != action {
			t.Errorf("record %s action=%v, want %s", id, op.Effect["action"], action)
		}
		if len(op.ExpectedBefore) == 0 {
			t.Errorf("record %s has no expected-before predicates", id)
		}
	}
	for id, reasonFrag := range wantHeld {
		op, ok := byRecord[id]
		if !ok {
			t.Errorf("held record %s missing", id)
			continue
		}
		if op.Status != "held" {
			t.Errorf("record %s status=%q, want held", id, op.Status)
		}
		if op.Effect["action"] != "none" {
			t.Errorf("record %s held with action=%v, want none", id, op.Effect["action"])
		}
		if !strings.Contains(op.HoldReason, reasonFrag) {
			t.Errorf("record %s hold reason %q does not mention %q", id, op.HoldReason, reasonFrag)
		}
	}
	// Spot-check the effects carry the right values.
	if op := byRecord["f14p-order-c2m"]; op.Effect["source_payment_id"] != "f14p-pay-c2m" || op.Effect["amount"] != int64(500000) {
		t.Errorf("c2m effect=%v, want source f14p-pay-c2m amount 500000", op.Effect)
	}
	if op := byRecord["f14p-coupon-2"]; op.Effect["used_count"] != int64(1) {
		t.Errorf("coupon-2 effect=%v, want used_count 1 (occupying)", op.Effect)
	}
	if op := byRecord["f14p-order-d7"]; op.Effect["obligation_id"] != "f14p-obl-d7" {
		t.Errorf("d7 effect=%v, want obligation f14p-obl-d7", op.Effect)
	}
	if op := byRecord["f14p-parcel-1"]; op.Effect["amount"] != int64(50000) {
		t.Errorf("parcel-1 effect=%v, want reversal amount 50000 (the duplicated debit)", op.Effect)
	}
	// The lot-scoped report proposes exactly one held storage op.
	lotReport := fin014ReportForFixture(t, db, unresolvedLot)
	lotProposal, err := FIN014ProposeBatch(ctx, db, lotReport, FIN014NewBatchID(), FIN014NewRunID())
	if err != nil {
		t.Fatal(err)
	}
	if len(lotProposal.Operations) != 1 || lotProposal.Operations[0].Status != "held" || lotProposal.Operations[0].Class != FIN014ClassStorageHistoryGap {
		t.Errorf("lot proposal=%+v, want one held storage_history_gap op", lotProposal.Operations)
	}

	// Re-proposing the same report yields the same operation IDs.
	again, err := FIN014ProposeBatch(ctx, db, report, FIN014NewBatchID(), FIN014NewRunID())
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Operations) != len(proposal.Operations) {
		t.Fatalf("re-proposal operations=%d, want %d", len(again.Operations), len(proposal.Operations))
	}
	for i := range proposal.Operations {
		if again.Operations[i].ID != proposal.Operations[i].ID {
			t.Errorf("re-proposal op %d id=%s, want %s", i, again.Operations[i].ID, proposal.Operations[i].ID)
		}
	}
	// Store once: OK. Store the same proposal again: duplicate IDs
	// reject the whole store (re-proposal must collide, not duplicate).
	if err := StoreFIN014Proposal(ctx, db, report, proposal, nil); err != nil {
		t.Fatalf("store proposal: %v", err)
	}
	if err := StoreFIN014Proposal(ctx, db, report, proposal, nil); err == nil {
		t.Errorf("storing the same proposal twice accepted, want duplicate operation ID rejection")
	}
	// A re-proposal under a new batch id also collides on store.
	if err := StoreFIN014Proposal(ctx, db, report, again, nil); err == nil {
		t.Errorf("storing a re-proposal accepted, want duplicate operation ID rejection")
	}

	// Validation: FIN-012 bound, predicate shape, cash gating.
	bad := *proposal
	bad.Operations = append([]FIN014Operation{}, proposal.Operations...)
	badOp := bad.Operations[0]
	badOp.Effect = map[string]any{"action": "reject_payout_request", "payout_request_id": badOp.RecordID, "amount": pricing.MaxTransportMoney + 1}
	bad.Operations[0] = badOp
	if err := ValidateFIN014Proposal(&bad); err == nil {
		t.Errorf("FIN-012 bound violation accepted, want rejection")
	}
	bad2 := *proposal
	bad2.Operations = append([]FIN014Operation{}, proposal.Operations...)
	held := bad2.Operations[0]
	held.Status = "held"
	held.HoldReason = ""
	bad2.Operations[0] = held
	if err := ValidateFIN014Proposal(&bad2); err == nil {
		t.Errorf("held operation without a reason accepted, want rejection")
	}
	bad3 := *proposal
	bad3.Operations = append([]FIN014Operation{}, proposal.Operations...)
	cash := bad3.Operations[0]
	cash.CashBasis = FIN014CashSimulated
	cash.Effect = map[string]any{"action": "refund_from_simulated", "amount": int64(1000)}
	bad3.Operations[0] = cash
	if err := ValidateFIN014Proposal(&bad3); err == nil {
		t.Errorf("unknown payout action for simulated cash accepted, want rejection")
	}
	bad4 := *proposal
	bad4.Operations = append([]FIN014Operation{}, proposal.Operations...)
	pred := bad4.Operations[0]
	pred.ExpectedBefore = []FIN014Predicate{{Kind: "equals", Table: "orders", ID: "x", Column: "nonexistent_column", Expected: "y"}}
	bad4.Operations[0] = pred
	if err := ValidateFIN014Proposal(&bad4); err == nil {
		t.Errorf("predicate on a non-allowlisted column accepted, want rejection")
	}
}
