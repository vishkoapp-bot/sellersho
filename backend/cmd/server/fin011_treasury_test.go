package main

import (
	"context"
	"database/sql"
	"testing"
)

// FIN-011 treasury tests (real computeTreasuryCashPosition — D-05/G-05
// hold and Q-03/G-11 carve-out). The repository package cannot import
// cmd/server, so the shape-mirroring query test lives in
// internal/repository/fin011_referral_conservation_test.go; these are the
// real-function checks.
//
// Fixture economics (Toman): payable 10,000,000 = wholesale 1,000,000 +
// seller margin 8,500,000 + platform profit 500,000; referral
// commission 10,000 = 2% of platform profit.
func fin011TreasuryPosition(t *testing.T, db *sql.DB) *treasuryCashPosition {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	counting := &treasuryCountingQuerier{tx: tx}
	pos, err := computeTreasuryCashPosition(ctx, counting, 30)
	if err != nil {
		t.Fatalf("computeTreasuryCashPosition: %v", err)
	}
	return pos
}

func fin011TreasuryStores(t *testing.T, db *sql.DB, f *treasuryFixture) (referred, referrer string) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	owner := treasurySeedUser(t, tx, f, "seller")
	referred = treasurySeedStore(t, tx, f, owner)
	referrer = treasurySeedStore(t, tx, f, owner)
	if err := tx.Commit(); err != nil {
		t.Fatalf("seed stores commit: %v", err)
	}
	return referred, referrer
}

// D-05/G-05 (I-06): the unreleased referral commission joins the
// FIN-008 safeToSpend hold exactly like unreleased seller profit:
// hold = 8,500,000 profit + 10,000 pending commission = 8,510,000.
func TestFIN011TreasuryHoldIncludesPendingCommission(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("fin011h")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	referred, referrer := fin011TreasuryStores(t, db, f)
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: referred, Item: false,
		Payable: 10000000, Profit: 8500000, PlatformProfit: 500000,
		Status: "delivered", ProfitReleased: false,
		ReferrerStoreID: referrer, ReferralCommission: 10000,
	})
	pos := fin011TreasuryPosition(t, db)
	if pos.UnreleasedProfitHold != 8510000 {
		t.Fatalf("unreleasedProfitHold=%d want 8510000 (8,500,000 profit + 10,000 pending commission)", pos.UnreleasedProfitHold)
	}
	if pos.HeldInOrders != 8510000 {
		t.Fatalf("heldInOrders=%d want 8510000", pos.HeldInOrders)
	}
	if pos.HeldOrderCount != 1 {
		t.Fatalf("heldOrderCount=%d want 1", pos.HeldOrderCount)
	}
}

// G-11 (Q-03 carve-out): platformEarned nets the RELEASED commission
// (platform net = profit − commission) but NOT an unreleased one (that
// sits in the hold instead). Realised platform profit requires a
// gateway receipt, so both orders are gateway-funded.
func TestFIN011TreasuryPlatformEarnedNetsReleasedCommission(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("fin011e")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	referred, referrer := fin011TreasuryStores(t, db, f)
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: referred, Item: false,
		Payable: 10000000, GatewayAmount: 10000000, Funding: "gateway",
		Profit: 8500000, PlatformProfit: 500000,
		Status: "delivered", ProfitReleased: true,
		ReferrerStoreID: referrer, ReferralCommission: 10000, CommissionReleased: true,
	})
	if pos := fin011TreasuryPosition(t, db); pos.PlatformEarned != 490000 {
		t.Fatalf("platformEarned=%d want 490000 (500,000 realised profit − 10,000 released commission)", pos.PlatformEarned)
	}
	// A second order whose commission is NOT released: its 500,000
	// platform profit counts in full; only the first order's released
	// 10,000 is netted.
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: referred, Item: false,
		Payable: 10000000, GatewayAmount: 10000000, Funding: "gateway",
		Profit: 8500000, PlatformProfit: 500000,
		Status: "delivered", ProfitReleased: true,
		ReferrerStoreID: referrer, ReferralCommission: 10000, CommissionReleased: false,
	})
	if pos := fin011TreasuryPosition(t, db); pos.PlatformEarned != 990000 {
		t.Fatalf("platformEarned=%d want 990000 (1,000,000 realised profit − 10,000 released commission only)", pos.PlatformEarned)
	}
}
