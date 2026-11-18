package main

import (
	"testing"

	"github.com/google/uuid"
)

// FIN-009 D-01 (treasury leg): a dispute/admin seller-profit reversal on
// an UNRELEASED order nets against the FIN-008 treasury hold (heldInOrders
// / unreleasedProfitHold), floored at 0 per order — exactly like the
// FIN-011 referral reversal. The canonical availability subtracts
// seller_profit_reversals only on RELEASED orders, so the hold is the
// only place an unreleased-order reversal is netted, exactly once.
// Delta-based: the position is read before and after the reversal row,
// so other tests' rows in the shared disposable DB cannot move the
// assertion.
func TestFIN009TreasuryHoldNetsDisputeSellerReversal(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("fin009h")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	referred, _ := fin011TreasuryStores(t, db, f)
	seeded := treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: referred, Item: false,
		Payable: 10000000, Profit: 8500000, PlatformProfit: 500000,
		Status: "delivered", ProfitReleased: false,
	})
	before := fin011TreasuryPosition(t, db)
	if before.HeldInOrders < 8500000 {
		t.Fatalf("setup: heldInOrders=%d, want >= 8500000 (seeded unreleased profit)", before.HeldInOrders)
	}
	reversalID := "SPR-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO seller_profit_reversals(id,order_id,store_id,amount,source,source_id,ledger_entry_id,idempotency_key,reason,actor_user_id) VALUES($1,$2,$3,8500000,'dispute',$4,'SPR-LED-test','spr:dispute:'||$4,'برگشت سود فروشنده — استرداد test','system:refund-execution')`, reversalID, seeded.ID, referred, "fin009-obligation"); err != nil {
		t.Fatalf("seed seller reversal: %v", err)
	}
	// Registered after the fixture cleanup, so it runs first (LIFO):
	// the reversal row is gone before the order is deleted.
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM seller_profit_reversals WHERE id=$1`, reversalID); err != nil {
			t.Logf("cleanup seller reversal (non-fatal): %v", err)
		}
	})
	after := fin011TreasuryPosition(t, db)
	if after.HeldInOrders != before.HeldInOrders-8500000 {
		t.Errorf("heldInOrders=%d, want %d (before %d − 8500000 dispute seller reversal)", after.HeldInOrders, before.HeldInOrders-8500000, before.HeldInOrders)
	}
	if after.UnreleasedProfitHold != before.UnreleasedProfitHold-8500000 {
		t.Errorf("unreleasedProfitHold=%d, want %d", after.UnreleasedProfitHold, before.UnreleasedProfitHold-8500000)
	}
	if after.HeldOrderCount != before.HeldOrderCount-1 {
		t.Errorf("heldOrderCount=%d, want %d (the fully reversed order drops out of the hold)", after.HeldOrderCount, before.HeldOrderCount-1)
	}
}
