package repository

import (
	"context"
	"fmt"
	"testing"
)

// FIN-014 W7 — unresolved-exclusion gate (audit §5.2 #5).
//
// A seeded unresolved case must not flow into an automatic
// payout/refund decision: the gate refuses it, and the queue still
// lists it afterwards. A clean control passes the gate. The gate is
// read-only — consulting it writes nothing.
//
// This test specifies the check; it does NOT wire the gate into any
// live payout handler (that touches a money path and needs supervisor
// + finance-reviewer approval). The attempted decision below is the
// decision point as the caller would use it: consult the gate, refuse
// when it blocks, proceed only when it clears.

func TestFIN014UnresolvedExcluded(t *testing.T) {
	db := fin014FixtureDB(t)
	ctx := context.Background()
	loadFIN014Fixture(t, db, "f14g-", "2026-10")

	// attemptAutoDecision is the decision point: an automatic
	// payout/refund for a record consults the gate first and refuses
	// when the gate blocks. It performs no money movement itself.
	attemptAutoDecision := func(recordID string) error {
		blocking, err := FIN014UnresolvedGate(ctx, db, "f14g-", recordID)
		if err != nil {
			return err
		}
		if len(blocking) > 0 {
			return fmt.Errorf("refused: unresolved FIN-014 case %s blocks the automatic decision (%d blocking row(s), first: %s/%s)", recordID, len(blocking), blocking[0].Class, blocking[0].RecordID)
		}
		return nil
	}

	before := fin014TableFingerprints(t, db)

	// Unresolved: paid order cancelled, obligation outcome unknown.
	if err := attemptAutoDecision("f14g-order-c2u"); err == nil {
		t.Errorf("automatic decision for the unresolved c2u order was allowed, want refusal")
	}
	// Unresolved: paid order with no payment record at all.
	if err := attemptAutoDecision("f14g-order-c8a"); err == nil {
		t.Errorf("automatic decision for the unresolved c8a order was allowed, want refusal")
	}
	// Unresolved: payment exists, no verification, no receipt.
	if err := attemptAutoDecision("f14g-order-c8b"); err == nil {
		t.Errorf("automatic decision for the unresolved c8b order was allowed, want refusal")
	}
	// Negative controls: exactly one cancellation obligation (c2c)
	// and a resolved-and-released dispute (d4c) clear the gate.
	if err := attemptAutoDecision("f14g-order-c2c"); err != nil {
		t.Errorf("automatic decision for the clean c2c order refused, want it to clear the gate: %v", err)
	}
	if err := attemptAutoDecision("f14g-order-d4c"); err != nil {
		t.Errorf("automatic decision for the clean d4c order refused, want it to clear the gate: %v", err)
	}

	// The gate is read-only, and the queue still lists the refused
	// cases afterwards — the refusal did not resolve or drop them.
	after := fin014TableFingerprints(t, db)
	for tbl, h := range before {
		if after[tbl] != h {
			t.Errorf("table %s changed by consulting the gate: before=%s after=%s", tbl, h, after[tbl])
		}
	}
	report := fin014ReportForFixture(t, db, "f14g-")
	for _, id := range []string{"f14g-order-c2u", "f14g-order-c8a", "f14g-order-c8b"} {
		found := false
		for _, r := range report.Rows {
			if r.RecordID == id && r.Confidence == FIN014ConfidenceUnresolved {
				found = true
			}
		}
		if !found {
			t.Errorf("unresolved queue no longer lists %s after the refused decision, want it still listed", id)
		}
	}
}
