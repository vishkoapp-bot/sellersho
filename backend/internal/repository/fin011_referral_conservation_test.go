// FIN-011 referral reversal + release conservation.
//
// Implementation tests for the FIN-011 fix (owner policy Nov 18 ~23:33/23:35
// Tehran: Q-01 retained-value, Q-02 retained-value bonus, Q-03 carve-out,
// Q-05 referrer immutable). Each TestFIN011* case drives the REAL repository
// methods against the FULL migrated schema in a disposable DB (provisioner:
// test-isolated-native.sh pattern, OPERATIONS_TEST_DATABASE_URL).
//
// Fixture economics (all amounts Toman, internally consistent):
//
//	payable 10,000,000 = wholesale 1,000,000 + seller margin 8,500,000
//	                     + platform profit 500,000
//	referral commission = 2% of platform profit = 10,000 (live default,
//	migration 000001: referral_commission_percent DEFAULT 2.00)
//	referral bonus = 100,000 flat (ReferralBonusAmount, store_repo.go:281)
//
// Cases assert the CONSERVED value. G-01/G-02/G-04/G-08/G-09/G-10 assert the
// Q-01 retained-value reversal math; G-03 asserts the Q-02/I-01 bonus
// unlock (window close + retained value > 0); G-05 asserts the D-05
// treasury hold shape (the real-function check lives in
// cmd/server/fin011_treasury_test.go; the query here mirrors
// finance_treasury.go exactly). G-06/G-07 are the regression guards.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
)

// fin011ReferralOrder builds a verified-paid order shaped for referral
// conservation: referrer store R, referred store f.store, locked 100,000
// bonus, 10,000 commission snapshot. When twoUnits is true the single
// order item carries quantity 2 at half unit prices, so a qty-1 return
// is a 50% partial return.
func fin011ReferralOrder(t *testing.T, twoUnits bool) (*sql.DB, opsFixture, string, string, string) {
	t.Helper()
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()

	refUser := uuid.NewString()
	refStore := "fin011-ref-" + uuid.NewString()[:8]
	opsExec(t, db, `INSERT INTO users(id,mobile,full_name,role) VALUES($1,$2,'fin011 referrer','seller')`, refUser, fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9))
	opsExec(t, db, `INSERT INTO stores(id,name,slug,owner_name,owner_user_id,active_plan_id,status) VALUES($1,'fin011 referrer',$2,'ref',$3,'standard','active')`, refStore, "fin011-ref-"+uuid.NewString()[:8], refUser)
	opsExec(t, db, `UPDATE stores SET referrer_seller_id=$1 WHERE id=$2`, refStore, f.store)
	opsExec(t, db, `INSERT INTO referral_bonuses(id,referrer_store_id,referred_store_id,amount,status,created_at) VALUES($1,$2,$3,100000,'locked',NOW())`, "bon-"+uuid.NewString(), refStore, f.store)

	if twoUnits {
		opsExec(t, db, `UPDATE order_items SET quantity=2,wholesale_cost=500000,seller_margin=4250000,final_unit_price=5000000 WHERE order_id=$1`, order)
		opsExec(t, db, `UPDATE order_item_origins SET quantity=2 WHERE order_id=$1`, order)
	} else {
		opsExec(t, db, `UPDATE order_items SET wholesale_cost=1000000,seller_margin=8500000,final_unit_price=10000000 WHERE order_id=$1`, order)
	}
	opsExec(t, db, `UPDATE orders SET dropshipper_profit=8500000,platform_profit=500000,referral_commission=10000,referrer_seller_id=$1 WHERE id=$2`, refStore, order)
	_ = ctx
	return db, f, warehouse, order, refStore
}

func fin011Confirm(t *testing.T, db *sql.DB, order, customer string) {
	t.Helper()
	opsExec(t, db, `UPDATE orders SET status='shipped',shipped_at=NOW() WHERE id=$1`, order)
	if err := NewOrderRepository(db).ConfirmOrderDelivery(context.Background(), order, customer); err != nil {
		t.Fatalf("ConfirmOrderDelivery: %v", err)
	}
}

func fin011CloseWindow(t *testing.T, db *sql.DB, order string) {
	t.Helper()
	opsExec(t, db, `UPDATE orders SET confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, order)
}

func fin011Release(t *testing.T, db *sql.DB, order string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	released, err := NewOrderRepository(db).releaseClosedReturnWindow(ctx, order)
	if err != nil {
		t.Fatalf("releaseClosedReturnWindow: %v", err)
	}
	return released
}

func fin011ReferrerBalance(t *testing.T, db *sql.DB, refStore string) (bonusAvailable, commissionEarned, available int64) {
	t.Helper()
	bal, err := NewFinanceRepository(db).GetStoreBalance(context.Background(), refStore)
	if err != nil {
		t.Fatalf("GetStoreBalance(%s): %v", refStore, err)
	}
	return bal.BonusAvailable, bal.ReferralCommissionEarned, bal.AvailableAmount
}

func fin011CommissionFlag(t *testing.T, db *sql.DB, order string) bool {
	t.Helper()
	var released bool
	if err := db.QueryRow(`SELECT referral_commission_released FROM orders WHERE id=$1`, order).Scan(&released); err != nil {
		t.Fatalf("read referral flag: %v", err)
	}
	return released
}

func fin011BonusStatus(t *testing.T, db *sql.DB, refStore, referred string) string {
	t.Helper()
	var status string
	if err := db.QueryRow(`SELECT status FROM referral_bonuses WHERE referrer_store_id=$1 AND referred_store_id=$2`, refStore, referred).Scan(&status); err != nil {
		t.Fatalf("read bonus status: %v", err)
	}
	return status
}

// D-02: full refund BEFORE release — the whole order value was returned,
// so the retained platform profit (and therefore the 2% commission on it)
// is zero. The release still flips the full 10,000 snapshot commission.
func TestFIN011CommissionFullReturnBeforeRelease(t *testing.T) {
	db, f, warehouse, order, refStore := fin011ReferralOrder(t, false)
	repo := NewOrderRepository(db)
	fin011Confirm(t, db, order, f.user)
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	amount, _, err := repo.RefundCustomerReturn(context.Background(), ret, f.user)
	if err != nil || amount != 10000000 {
		t.Fatalf("full refund amount=%d err=%v", amount, err)
	}
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("release did not run after proven full refund")
	}
	if !fin011CommissionFlag(t, db, order) {
		t.Fatalf("commission flag not flipped at release")
	}
	_, earned, _ := fin011ReferrerBalance(t, db, refStore)
	if earned != 0 {
		t.Errorf("CONSERVATION BREAK (D-02): full refund before release, referrer commission earned=%d Toman, conserved=0", earned)
	}
}

// D-01: 50% partial refund BEFORE release — retained platform profit is
// 250,000, so the conserved commission is 2% of it = 5,000. The release
// flips the full 10,000 order-time snapshot.
func TestFIN011CommissionPartialReturnBeforeRelease(t *testing.T) {
	db, f, warehouse, order, refStore := fin011ReferralOrder(t, true)
	repo := NewOrderRepository(db)
	fin011Confirm(t, db, order, f.user)
	ret := fin003ReceivedReturn(t, db, f, warehouse, order) // qty 1 of 2 = 50%
	amount, _, err := repo.RefundCustomerReturn(context.Background(), ret, f.user)
	if err != nil || amount != 5000000 {
		t.Fatalf("partial refund amount=%d err=%v", amount, err)
	}
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("release did not run after proven partial refund")
	}
	_, earned, _ := fin011ReferrerBalance(t, db, refStore)
	if earned != 5000 {
		t.Errorf("CONSERVATION BREAK (D-01): 50%% refund before release, referrer commission earned=%d Toman, conserved=5000 (2%% of retained 250,000 platform profit)", earned)
	}
}

// D-03 / G-03 (Q-02 retained-value, I-01/I-02/I-03): the 100,000 one-time
// bonus is a RETAINED-VALUE reward, not an activation reward. It must NOT
// release at delivery confirmation (the old D-03 defect); it releases only
// at window close, and only when the referred store has an order that
// closed its window with retained value > 0. A fully returned first order
// keeps it locked; a LATER order closing with retained value > 0 unlocks
// it (flat 100,000 — a partial return with retained value > 0 still
// unlocks; the bonus is flat, not pro-rata).
func TestFIN011BonusFullReturnAfterConfirm(t *testing.T) {
	db, f, warehouse, order, refStore := fin011ReferralOrder(t, false)
	repo := NewOrderRepository(db)
	fin011Confirm(t, db, order, f.user)
	// Confirmation alone must NOT release the bonus anymore (D-03 fix).
	if got := fin011BonusStatus(t, db, refStore, f.store); got != "locked" {
		t.Fatalf("bonus released at confirmation (D-03 still live): %s", got)
	}
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if amount, _, err := repo.RefundCustomerReturn(context.Background(), ret, f.user); err != nil || amount != 10000000 {
		t.Fatalf("full refund amount=%d err=%v", amount, err)
	}
	// Fully returned first order: retained value 0, bonus stays locked.
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("release did not run after proven full refund")
	}
	if got := fin011BonusStatus(t, db, refStore, f.store); got != "locked" {
		t.Fatalf("fully returned first order released the bonus: %s", got)
	}
	if bonus, _, _ := fin011ReferrerBalance(t, db, refStore); bonus != 0 {
		t.Errorf("fully returned first order, referrer bonus available=%d Toman, conserved=0", bonus)
	}
	// A LATER order of the same referred store, closing its window with
	// retained value > 0, unlocks the flat bonus.
	order2 := fin011SecondOrder(t, db, f, order, refStore)
	fin011ConfirmSystem(t, db, order2)
	fin011CloseWindow(t, db, order2)
	if released := fin011Release(t, db, order2); !released {
		t.Fatalf("second-order release did not run")
	}
	if got := fin011BonusStatus(t, db, refStore, f.store); got != "released" {
		t.Fatalf("later successful order did not unlock the bonus: %s", got)
	}
	if bonus, earned, _ := fin011ReferrerBalance(t, db, refStore); bonus != 100000 || earned != 10000 {
		t.Errorf("after later successful order: bonus=%d commission=%d, want 100000/10000 (first order fully reversed, second order released)", bonus, earned)
	}
}

// G-03b (I-02): a PARTIALLY returned first order with retained value > 0
// still unlocks the flat 100,000 bonus at window close.
func TestFIN011BonusPartialReturnStillUnlocks(t *testing.T) {
	db, f, warehouse, order, refStore := fin011ReferralOrder(t, true)
	repo := NewOrderRepository(db)
	fin011Confirm(t, db, order, f.user)
	if got := fin011BonusStatus(t, db, refStore, f.store); got != "locked" {
		t.Fatalf("bonus released at confirmation: %s", got)
	}
	ret := fin003ReceivedReturn(t, db, f, warehouse, order) // qty 1 of 2 = 50%
	if amount, _, err := repo.RefundCustomerReturn(context.Background(), ret, f.user); err != nil || amount != 5000000 {
		t.Fatalf("partial refund amount=%d err=%v", amount, err)
	}
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("release did not run after proven partial refund")
	}
	if got := fin011BonusStatus(t, db, refStore, f.store); got != "released" {
		t.Fatalf("partially retained first order did not unlock the bonus: %s", got)
	}
	if bonus, _, _ := fin011ReferrerBalance(t, db, refStore); bonus != 100000 {
		t.Errorf("partially retained first order, referrer bonus available=%d Toman, want 100000", bonus)
	}
}

// D-04: refund AFTER release — commission was already released at window
// close; the refund reverses supplier cost + seller margin only, so the
// referrer keeps the full commission on refunded value.
func TestFIN011CommissionRefundAfterRelease(t *testing.T) {
	db, f, warehouse, order, refStore := fin011ReferralOrder(t, false)
	repo := NewOrderRepository(db)
	fin011Confirm(t, db, order, f.user)
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("release did not run")
	}
	if _, earned, _ := fin011ReferrerBalance(t, db, refStore); earned != 10000 {
		t.Fatalf("setup: released commission earned=%d, want 10000", earned)
	}
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if amount, _, err := repo.RefundCustomerReturn(context.Background(), ret, f.user); err != nil || amount != 10000000 {
		t.Fatalf("full refund after release amount=%d err=%v", amount, err)
	}
	_, earned, _ := fin011ReferrerBalance(t, db, refStore)
	if earned != 0 {
		t.Errorf("CONSERVATION BREAK (D-04): full refund after release, referrer commission earned=%d Toman, conserved=0", earned)
	}
	// Compensating event only: the released flag and the snapshot must
	// NOT be rewritten by the reversal.
	if !fin011CommissionFlag(t, db, order) {
		t.Errorf("refund after release mutated the released flag (history rewritten)")
	}
	if got := fin011ReversalSum(t, db, order); got != 10000 {
		t.Errorf("post-release reversal event missing: referral_commission_reversals sum=%d, want 10000", got)
	}
}

// CONTROL (passes on current tree): the order snapshots its referrer at
// creation. Changing the store's referrer afterwards must not move
// already-earned commission to the new referrer.
func TestFIN011ReferrerSnapshotImmuneToStoreChange(t *testing.T) {
	db, f, warehouse, order, refStore := fin011ReferralOrder(t, false)
	otherUser := uuid.NewString()
	otherStore := "fin011-other-" + uuid.NewString()[:8]
	opsExec(t, db, `INSERT INTO users(id,mobile,full_name,role) VALUES($1,$2,'fin011 other','seller')`, otherUser, fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9))
	opsExec(t, db, `INSERT INTO stores(id,name,slug,owner_name,owner_user_id,active_plan_id,status) VALUES($1,'fin011 other',$2,'o',$3,'standard','active')`, otherStore, "fin011-o-"+uuid.NewString()[:8], otherUser)
	fin011Confirm(t, db, order, f.user)
	opsExec(t, db, `UPDATE stores SET referrer_seller_id=$1 WHERE id=$2`, otherStore, f.store)
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("release did not run")
	}
	var snapshot string
	if err := db.QueryRow(`SELECT referrer_seller_id FROM orders WHERE id=$1`, order).Scan(&snapshot); err != nil || snapshot != refStore {
		t.Fatalf("order referrer snapshot moved: %q err=%v", snapshot, err)
	}
	if _, earned, _ := fin011ReferrerBalance(t, db, refStore); earned != 10000 {
		t.Fatalf("original referrer earned=%d, want 10000", earned)
	}
	if _, earned, _ := fin011ReferrerBalance(t, db, otherStore); earned != 0 {
		t.Fatalf("new referrer earned=%d, want 0", earned)
	}
	_ = warehouse
}

// CONTROL (passes on current tree) + structural observation D-06: with no
// return, the full commission and bonus release — but the commission has
// NO ledger leg: the referrer's balance credits it straight from the
// orders row while ledger_entries carries nothing for it.
func TestFIN011NoReturnHappyPathReleasesBoth(t *testing.T) {
	db, f, _, order, refStore := fin011ReferralOrder(t, false)
	fin011Confirm(t, db, order, f.user)
	// FIN-011 (Q-02): confirmation no longer releases the bonus; it
	// releases only at window close below.
	if got := fin011BonusStatus(t, db, refStore, f.store); got != "locked" {
		t.Fatalf("bonus released at confirmation: %s", got)
	}
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("release did not run")
	}
	if got := fin011BonusStatus(t, db, refStore, f.store); got != "released" {
		t.Fatalf("bonus not released at window close: %s", got)
	}
	bonus, earned, _ := fin011ReferrerBalance(t, db, refStore)
	if bonus != 100000 || earned != 10000 {
		t.Fatalf("happy path balance bonus=%d commission=%d, want 100000/10000", bonus, earned)
	}
	// G-11 (Q-03 carve-out): the commission release writes a ledger leg
	// (platform debit / referrer credit) for the retained commission, so
	// ledger_entries reconciles referral without reading the orders row.
	// (The audit's control observed 0 referral rows here — that WAS D-06.)
	credit, debit := fin011ReferralLedgerLeg(t, db, order)
	if credit != 10000 || debit != 10000 {
		t.Fatalf("commission release ledger leg credit=%d debit=%d, want 10000/10000", credit, debit)
	}
	var pair int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1 AND reason='Staged profit release after return window closed'`, order).Scan(&pair); err != nil || pair != 2 {
		t.Fatalf("profit release ledger pair=%d err=%v (want 2)", pair, err)
	}
}

// D-05 / G-05 (I-06): the FIN-008 safeToSpend unreleased-profit hold
// (finance_treasury.go) must count the pending referral commission exactly
// like unreleased seller profit. NOTE: this test mirrors the hold query
// verbatim so the shape is pinned here; the REAL function is exercised by
// TestFIN011TreasuryHoldIncludesPendingCommission in
// cmd/server/fin011_treasury_test.go (computeTreasuryCashPosition).
func TestFIN011UnreleasedCommissionMissingFromTreasuryHold(t *testing.T) {
	db, _, _, order, _ := fin011ReferralOrder(t, false)
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed' WHERE id=$1`, order)
	// Conserved hold = unreleased seller profit 8,500,000 + pending
	// referral commission 10,000 (payable at window close).
	if held := fin011TreasuryHold(t, db, order); held != 8510000 {
		t.Errorf("CONSERVATION BREAK (D-05): treasury unreleased hold=%d Toman, conserved=8510000 (8,500,000 profit + 10,000 pending commission)", held)
	}
}

// G-05b (I-06 / FIN-008 F-02): a reversal on an UNRELEASED order nets
// against the hold, not against released availability. The 50% refund
// reverses half the margin (FIN-005 pro-rata: 8,500,000 * 5,000,000 /
// 10,000,000 = 4,250,000) and half the commission (5,000), so the hold
// for this order is (8,500,000 - 4,250,000) + (10,000 - 5,000) =
// 4,255,000.
func TestFIN011UnreleasedReversalNetsAgainstHold(t *testing.T) {
	db, f, warehouse, order, _ := fin011ReferralOrder(t, true)
	repo := NewOrderRepository(db)
	fin011Confirm(t, db, order, f.user)
	ret := fin003ReceivedReturn(t, db, f, warehouse, order) // qty 1 of 2 = 50%
	if amount, _, err := repo.RefundCustomerReturn(context.Background(), ret, f.user); err != nil || amount != 5000000 {
		t.Fatalf("partial refund amount=%d err=%v", amount, err)
	}
	var sellerRev int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN operation='debit' THEN amount ELSE -amount END),0) FROM customer_return_financial_events WHERE order_id=$1 AND component='seller_profit'`, order).Scan(&sellerRev); err != nil {
		t.Fatal(err)
	}
	commRev := fin011ReversalSum(t, db, order)
	if held := fin011TreasuryHold(t, db, order); held != 8500000-sellerRev+10000-commRev {
		t.Errorf("unreleased reversal did not net against the hold: hold=%d Toman, want %d (8,500,000 - %d seller reversal + 10,000 - %d commission reversal)", held, 8500000-sellerRev+10000-commRev, sellerRev, commRev)
	}
	if held := fin011TreasuryHold(t, db, order); held != 4255000 {
		t.Errorf("unreleased reversal did not net against the hold: hold=%d Toman, want 4255000 (4,250,000 profit + 5,000 pending commission)", held)
	}
}

// fin011TreasuryHold runs the hold query exactly as finance_treasury.go
// computes heldInOrders (unreleased delivered paid orders, per-order
// floors at 0), scoped to one order: the disposable DB is shared by
// every test in the package and earlier tests leave unreleased orders
// behind, so a global sum would count their holds too. Keep in sync
// with cmd/server/finance_treasury.go.
func fin011TreasuryHold(t *testing.T, db *sql.DB, order string) int64 {
	t.Helper()
	var held int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(net),0) FROM (
		SELECT GREATEST(COALESCE(o.dropshipper_profit,0) - COALESCE((SELECT SUM(CASE WHEN operation='debit' THEN amount ELSE -amount END) FROM customer_return_financial_events WHERE order_id=o.id AND component='seller_profit'),0), 0) + GREATEST(COALESCE(o.referral_commission,0) - COALESCE((SELECT SUM(amount) FROM referral_commission_reversals WHERE order_id=o.id),0), 0) AS net
		FROM orders o WHERE o.id=$1 AND o.payment_status='paid' AND COALESCE(o.profit_released,FALSE)=FALSE AND o.status IN ('delivered','delivered_confirmed')
	) s`, order).Scan(&held); err != nil {
		t.Fatalf("hold query: %v", err)
	}
	return held
}

// fin011ConfirmSystem confirms delivery via the system path (no customer
// match needed). After the FIN-011 fix it must NOT release the bonus.
func fin011ConfirmSystem(t *testing.T, db *sql.DB, order string) {
	t.Helper()
	if err := NewOrderRepository(db).ConfirmOrderDeliverySystem(context.Background(), order); err != nil {
		t.Fatalf("ConfirmOrderDeliverySystem: %v", err)
	}
}

// fin011SecondOrder inserts a second paid order for the SAME referred
// store (f.store), copying the first order's item row so the economics
// match (10,000,000 payable, 10,000 commission, same referrer). Used by
// the G-03 later-order-unlocks case.
func fin011SecondOrder(t *testing.T, db *sql.DB, f opsFixture, firstOrder, refStore string) string {
	t.Helper()
	id := "fin011-2nd-" + uuid.NewString()[:12]
	opsExec(t, db, `INSERT INTO orders (id, order_number, store_id, store_name, total_amount, payable_amount, customer_full_name, customer_phone, customer_province, customer_city, customer_address, customer_postal_code, status, tracking_code, customer_user_id, payment_status, dropshipper_profit, platform_profit, referral_commission, referrer_seller_id) VALUES ($1, $2, $3, 'fin011 second', 10000000, 10000000, 'fin011', '09120000000', 'Tehran', 'Tehran', 'Street 1', '1234567890', 'shipped', $4, $5::uuid, 'paid', 8500000, 500000, 10000, $6)`,
		id, "ORD-2ND-"+uuid.NewString()[:8], f.store, "TRK-2ND-"+uuid.NewString()[:8], f.user, refStore)
	opsExec(t, db, `INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) SELECT $1::uuid, $2, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price FROM order_items WHERE order_id=$3`, uuid.NewString(), id, firstOrder)
	return id
}

// fin011Dispute files a dispute with the given claim amount, mirroring
// fin003Dispute (temporary 'shipped' status for admission, then restore).
func fin011Dispute(t *testing.T, db *sql.DB, order string, claim int64) string {
	t.Helper()
	id := "fin011-dispute-" + uuid.NewString()[:12]
	var previous string
	if err := db.QueryRow(`SELECT status FROM orders WHERE id=$1`, order).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `UPDATE orders SET status='shipped' WHERE id=$1`, order)
	if err := NewFinanceRepository(db).CreateDispute(context.Background(), domain.Dispute{ID: id, OrderID: order, Type: "delivery_undelivered", ProductTitle: "fixture", ClaimAmount: claim, Reason: "fin011 dispute refund"}); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `UPDATE orders SET status=$2 WHERE id=$1`, order, previous)
	return id
}

// fin011ReversalSum is the recorded referral-commission reversal total
// for one order (the compensating events).
func fin011ReversalSum(t *testing.T, db *sql.DB, order string) int64 {
	t.Helper()
	var sum int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM referral_commission_reversals WHERE order_id=$1`, order).Scan(&sum); err != nil {
		t.Fatalf("reversal sum: %v", err)
	}
	return sum
}

// fin011ReferralLedgerLeg is the release-time commission ledger leg
// (credit to the referrer / debit to the platform) for one order.
func fin011ReferralLedgerLeg(t *testing.T, db *sql.DB, order string) (credit, debit int64) {
	t.Helper()
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount) FILTER (WHERE type='credit'),0), COALESCE(SUM(amount) FILTER (WHERE type='debit'),0) FROM ledger_entries WHERE related_order_or_dispute=$1 AND reason='Referral commission release after return window closed'`, order).Scan(&credit, &debit); err != nil {
		t.Fatalf("referral ledger leg: %v", err)
	}
	return credit, debit
}

// fin011ReleaseState reads the canonical release state (same package).
func fin011ReleaseState(t *testing.T, db *sql.DB, order string) string {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	state, _, err := disputeReleaseStateTx(context.Background(), tx, order)
	if err != nil {
		t.Fatalf("disputeReleaseStateTx: %v", err)
	}
	return state
}

// fin011ResolveFullDispute resolves a full-claim dispute refund and drives
// the obligation to 'succeeded' through the real provider path (fake
// local provider, the established FIN-003 pattern).
func fin011ResolveFullDispute(t *testing.T, db *sql.DB, f opsFixture, order string) string {
	t.Helper()
	ctx := context.Background()
	disputeID := fin011Dispute(t, db, order, 10000000)
	if err := NewFinanceRepository(db).ResolveDispute(ctx, disputeID, "refund_seller", "seller", f.user, 10000000); err != nil {
		t.Fatalf("ResolveDispute: %v", err)
	}
	var obligationID string
	if err := db.QueryRow(`SELECT id FROM order_refund_obligations WHERE order_id=$1 AND purpose='dispute'`, order).Scan(&obligationID); err != nil {
		t.Fatalf("dispute obligation: %v", err)
	}
	// Distinct provider reference per test: order_refund_events has a
	// UNIQUE index on provider_reference for succeeded original_payment
	// refunds, so the constant "local-refund-confirmation" can only be
	// used once per disposable DB.
	if state, err := NewOrderRepository(db).ProcessOrderRefund(ctx, obligationID, &fin003LocalRefundProvider{status: "succeeded", reference: "fin011-" + uuid.NewString()[:12]}); err != nil || state != "succeeded" {
		t.Fatalf("dispute refund execution state=%s err=%v", state, err)
	}
	if applied, err := NewOrderRepository(db).ApplyDisputeRefundEffect(ctx, disputeID); err != nil || !applied {
		t.Fatalf("ApplyDisputeRefundEffect applied=%v err=%v", applied, err)
	}
	return disputeID
}

// G-08: partial (50%) refund AFTER release — the compensating reversal
// takes the earned commission from 10,000 to 5,000; the reversal is
// visible as an event, the released flag is untouched, and a negative
// signed balance is allowed (debt visible, never floored).
func TestFIN011CommissionPartialRefundAfterRelease(t *testing.T) {
	db, f, warehouse, order, refStore := fin011ReferralOrder(t, true)
	repo := NewOrderRepository(db)
	fin011Confirm(t, db, order, f.user)
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("release did not run")
	}
	if _, earned, _ := fin011ReferrerBalance(t, db, refStore); earned != 10000 {
		t.Fatalf("setup: released commission earned=%d, want 10000", earned)
	}
	ret := fin003ReceivedReturn(t, db, f, warehouse, order) // qty 1 of 2 = 50%
	if amount, _, err := repo.RefundCustomerReturn(context.Background(), ret, f.user); err != nil || amount != 5000000 {
		t.Fatalf("partial refund after release amount=%d err=%v", amount, err)
	}
	_, earned, available := fin011ReferrerBalance(t, db, refStore)
	if earned != 5000 {
		t.Errorf("G-08: partial refund after release, referrer commission earned=%d Toman, conserved=5000 (2%% of retained 250,000 platform profit)", earned)
	}
	if got := fin011ReversalSum(t, db, order); got != 5000 {
		t.Errorf("G-08: compensating reversal event missing: sum=%d, want 5000", got)
	}
	if !fin011CommissionFlag(t, db, order) {
		t.Errorf("G-08: refund after release mutated the released flag (history rewritten)")
	}
	// Available = released bonus 100,000 + retained commission 5,000.
	if available != 105000 {
		t.Errorf("G-08: referrer available=%d, want 105000 (100000 bonus + 5000 retained commission)", available)
	}
}

// G-09 (before release): a FULL dispute refund before release reverses
// the whole commission at refund-success time. The release gate holds
// dispute-refunded orders for review (pre-existing conservative rule:
// ANY dispute obligation blocks automatic release), so the release must
// NOT run — and nothing is earned either way.
func TestFIN011DisputeRefundFullBeforeRelease(t *testing.T) {
	db, f, _, order, refStore := fin011ReferralOrder(t, false)
	fin011Confirm(t, db, order, f.user)
	fin011ResolveFullDispute(t, db, f, order)
	if got := fin011ReversalSum(t, db, order); got != 10000 {
		t.Fatalf("dispute reversal event missing: sum=%d, want 10000", got)
	}
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); released {
		t.Fatalf("release ran on a dispute-refunded order (must stay held for review)")
	}
	if state := fin011ReleaseState(t, db, order); state != "needs_review" {
		t.Fatalf("release state=%q, want needs_review", state)
	}
	if _, earned, _ := fin011ReferrerBalance(t, db, refStore); earned != 0 {
		t.Errorf("G-09 before release: referrer commission earned=%d, conserved=0", earned)
	}
}

// G-09 (after release): a FULL dispute refund after release reverses the
// whole commission via a compensating event; the released flag stays set.
func TestFIN011DisputeRefundFullAfterRelease(t *testing.T) {
	db, f, _, order, refStore := fin011ReferralOrder(t, false)
	fin011Confirm(t, db, order, f.user)
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("release did not run")
	}
	if _, earned, _ := fin011ReferrerBalance(t, db, refStore); earned != 10000 {
		t.Fatalf("setup: released commission earned=%d, want 10000", earned)
	}
	fin011ResolveFullDispute(t, db, f, order)
	if _, earned, _ := fin011ReferrerBalance(t, db, refStore); earned != 0 {
		t.Errorf("G-09 after release: referrer commission earned=%d, conserved=0", earned)
	}
	if got := fin011ReversalSum(t, db, order); got != 10000 {
		t.Errorf("G-09: compensating reversal event missing: sum=%d, want 10000", got)
	}
	if !fin011CommissionFlag(t, db, order) {
		t.Errorf("G-09: dispute refund mutated the released flag (history rewritten)")
	}
}

// G-10: the referrer requests + completes payout of the released referral
// money (bonus 100,000 + commission 10,000), THEN a full return+refund
// lands. The signed balance must go NEGATIVE by exactly the reversed
// commission (10,000) — debt visible in GetStoreBalance and in the
// statement, never floored — and a second payout must be rejected.
// (The bonus is NOT reversed: it unlocked at window close with retained
// value > 0, per Q-02/I-01.)
func TestFIN011PayoutThenReturnGoesNegative(t *testing.T) {
	db, f, warehouse, order, refStore := fin011ReferralOrder(t, false)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	repo := NewOrderRepository(db)
	fin011Confirm(t, db, order, f.user)
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("release did not run")
	}
	var refOwner string
	if err := db.QueryRow(`SELECT owner_user_id FROM stores WHERE id=$1`, refStore).Scan(&refOwner); err != nil {
		t.Fatalf("referrer owner: %v", err)
	}
	req, err := finRepo.RequestOwnedStorePayout(ctx, refOwner, refStore, "IR820170000000123456789012", "بانک ملی", 110000)
	if err != nil {
		t.Fatalf("payout request: %v", err)
	}
	if err := finRepo.ReviewSettlement(ctx, req.ID, "approved", "fin011 fixture", "admin"); err != nil {
		t.Fatalf("payout review: %v", err)
	}
	if err := finRepo.ConfirmManualTransfer(ctx, req.ID, "TRF-fin011", "admin"); err != nil {
		t.Fatalf("payout confirm: %v", err)
	}
	// THEN a full return + refund lands (late return, fixture path).
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if amount, _, err := repo.RefundCustomerReturn(ctx, ret, f.user); err != nil || amount != 10000000 {
		t.Fatalf("full refund after payout amount=%d err=%v", amount, err)
	}
	bonus, earned, available := fin011ReferrerBalance(t, db, refStore)
	if bonus != 100000 {
		t.Errorf("G-10: bonus available=%d, want 100000 (bonus is not reversed by a post-release return)", bonus)
	}
	if earned != 0 {
		t.Errorf("G-10: commission earned=%d, want 0", earned)
	}
	if available != -10000 {
		t.Errorf("G-10: referrer signed balance=%d, want -10000 (debt visible, never floored)", available)
	}
	if _, err := finRepo.RequestOwnedStorePayout(ctx, refOwner, refStore, "IR820170000000123456789012", "بانک ملی", 1); err == nil {
		t.Errorf("G-10: second payout succeeded on a negative balance (must be rejected)")
	}
	stmt, err := finRepo.GetStoreStatement(ctx, refStore, time.Now().Format("2006-01"))
	if err != nil {
		t.Fatalf("GetStoreStatement: %v", err)
	}
	if stmt.NetBalance != -10000 || stmt.CurrentAvailable != -10000 {
		t.Errorf("G-10: statement net=%d current=%d, want -10000/-10000", stmt.NetBalance, stmt.CurrentAvailable)
	}
	var reversalEntries int
	for _, e := range stmt.Entries {
		if e.Reason == "برگشت کمیسیون معرفی" && e.Type == "debit" && e.Amount == 10000 {
			reversalEntries++
		}
	}
	if reversalEntries != 1 {
		t.Errorf("G-10: statement reversal entries=%d, want 1", reversalEntries)
	}
}

// G-07b / I-05 (Q-05): the referrer can NEVER change. Behaviour is
// covered by TestFIN011ReferrerSnapshotImmuneToStoreChange; this is the
// structural guard: no runtime code may assign stores.referrer_seller_id.
// Grandfathered seed data only: migration 000002's demo referral chain
// and the dev seeder that re-applies it (both predate the policy and
// touch no live store — Q-07 probe: 0 stores have a referrer in prod).
func TestFIN011ReferrerCannotChange(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	backend := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	assign := regexp.MustCompile(`(?i)\bSET\s+referrer_seller_id\s*=`)
	allowed := map[string]bool{
		filepath.Join(backend, "migrations", "000002_seed_data.up.sql"):           true,
		filepath.Join(backend, "internal", "database", "seeder", "seeder_dev.go"): true,
	}
	var violations []string
	err := filepath.WalkDir(backend, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "migrations.previous":
				return fs.SkipDir
			}
			if path == filepath.Join(backend, "docs", "fa") || path == filepath.Join(backend, "internal", "networkreward") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".sql") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if assign.Match(body) && !allowed[path] {
			violations = append(violations, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("tree scan: %v", err)
	}
	if len(violations) > 0 {
		t.Errorf("Q-05 violated: referrer_seller_id is assigned outside grandfathered seed data in: %v", violations)
	}
}
