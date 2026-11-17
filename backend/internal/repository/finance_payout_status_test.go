package repository

// Fix69 A1 regression tests.
//
// The DB test runs ONLY when FINANCE_PAYOUT_TEST_DATABASE_URL points at
// a DISPOSABLE Postgres (never live/staging); otherwise it skips, like
// the other opt-in repository tests. It walks one payout through
// pending -> approved -> transfer_confirmed -> completed and asserts
// the store available balance never jumps back up at transfer_confirmed
// (the pre-fix bug: transfer_confirmed was missing from both deducted
// status lists, so confirming a manual transfer restored the full amount
// and the same profit could be paid out twice). A rejected payout must
// NOT deduct: rejection returns the amount to available.
// The party-mapping and SQL-string tests need no database and always run.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/testdb"

	_ "github.com/lib/pq"
)

func TestDisputePartyLedgerType(t *testing.T) {
	cases := []struct {
		party, action, want string
		ok                  bool
	}{
		{"supplier", "refund_supplier", "supplier", true},
		{"تأمین‌کننده کالا", "refund_supplier", "supplier", true},
		{"تأمین‌کننده", "refund_supplier", "supplier", true},
		{"seller", "refund_seller", "seller", true},
		{"دراپ‌شیپر", "refund_seller", "seller", true},
		{"carrier", "refund_carrier", "carrier", true},
		{"شرکت حمل‌ونقل و باربری", "refund_carrier", "carrier", true},
		{"warehouse", "refund_warehouse", "warehouse", true},
		{"انبار", "refund_warehouse", "warehouse", true},
		{"انبار مرکزی", "refund_warehouse", "warehouse", true},
		// Action fallback: an unmapped label must still follow the action,
		// never silently default to carrier.
		{"some unknown label", "refund_warehouse", "warehouse", true},
		{"some unknown label", "refund_supplier", "supplier", true},
		{"some unknown label", "refund_seller", "seller", true},
		{"some unknown label", "refund_carrier", "carrier", true},
		{"platform", "refund_supplier", "platform", true},
		{"پلتفرم", "refund_supplier", "platform", true},
		{"some unknown label", "release_to_seller", "", false},
		{"", "refund_carrier", "carrier", true},
	}
	for _, c := range cases {
		got, ok := disputePartyLedgerType(c.party, c.action)
		if ok != c.ok || got != c.want {
			t.Errorf("disputePartyLedgerType(%q, %q) = (%q, %v), want (%q, %v)",
				c.party, c.action, got, ok, c.want, c.ok)
		}
	}
}

func TestSellerAvailableSQLDeductsTransferConfirmed(t *testing.T) {
	// No-DB guard: the seller-services available-balance SQL must list
	// transfer_confirmed among the deducted payout statuses.
	if !strings.Contains(sellerAvailableSQL, "'transfer_confirmed'") {
		t.Errorf("sellerAvailableSQL does not deduct transfer_confirmed payouts:\n%s", sellerAvailableSQL)
	}
	for _, status := range []string{"'pending'", "'approved'", "'paid'", "'processing'", "'completed'"} {
		if !strings.Contains(sellerAvailableSQL, status) {
			t.Errorf("sellerAvailableSQL lost deducted status %s", status)
		}
	}
}

func TestPayoutStatusAvailableNeverJumpsBack(t *testing.T) {
	dsn := os.Getenv("FINANCE_PAYOUT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("FINANCE_PAYOUT_TEST_DATABASE_URL not set; skipping disposable-DB integration test")
	}
	ctx := context.Background()
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	ddl := []string{
		`DROP TABLE IF EXISTS payout_requests, seller_service_purchases, referral_bonuses, orders, stores, customer_return_financial_events, ledger_entries, seller_early_withdrawals CASCADE`,
		`CREATE TABLE stores (id VARCHAR(50) PRIMARY KEY, referrer_seller_id VARCHAR(50))`,
		`CREATE TABLE orders (id VARCHAR(50) PRIMARY KEY, store_id VARCHAR(50), status VARCHAR(30), payable_amount BIGINT, dropshipper_profit BIGINT, profit_released BOOLEAN, referral_commission BIGINT, referrer_seller_id VARCHAR(50), referral_commission_released BOOLEAN)`,
		`CREATE TABLE referral_bonuses (id VARCHAR(50) PRIMARY KEY, referrer_store_id VARCHAR(50), amount BIGINT, status VARCHAR(30), released_at TIMESTAMPTZ)`,
		`CREATE TABLE payout_requests (id VARCHAR(50) PRIMARY KEY, store_id VARCHAR(50), amount BIGINT, status VARCHAR(30))`,
		`CREATE TABLE seller_service_purchases (id VARCHAR(50) PRIMARY KEY, store_id VARCHAR(50), amount BIGINT)`,
		// Tables GetStoreBalance reads since the gap-6/15 balance terms
		// (and the D31 early-withdrawal terms): empty here, so every new
		// term contributes 0 and the payout-status expectations below
		// are unchanged.
		`CREATE TABLE customer_return_financial_events (id VARCHAR(80) PRIMARY KEY, store_id TEXT, component VARCHAR(20), operation VARCHAR(20), amount BIGINT)`,
		`CREATE TABLE ledger_entries (id VARCHAR(80) PRIMARY KEY, target_account TEXT, target_type TEXT, type TEXT, amount BIGINT, reason TEXT)`,
		`CREATE TABLE seller_early_withdrawals (id VARCHAR(80) PRIMARY KEY, store_id VARCHAR(50), status VARCHAR(30), fee_amount BIGINT, gross_amount BIGINT)`,
	}
	for _, q := range ddl {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("ddl %q: %v", q, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS payout_requests, seller_service_purchases, referral_bonuses, orders, stores, customer_return_financial_events, ledger_entries, seller_early_withdrawals CASCADE`)
	})

	storeID := "test-store-a1"
	if _, err := db.ExecContext(ctx, `INSERT INTO stores (id) VALUES ($1)`, storeID); err != nil {
		t.Fatalf("insert store: %v", err)
	}
	// Released profit of 1,000,000 on a delivered order.
	if _, err := db.ExecContext(ctx, `INSERT INTO orders (id, store_id, status, payable_amount, dropshipper_profit, profit_released, referral_commission, referral_commission_released) VALUES ('ord-a1', $1, 'delivered', 5000000, 1000000, TRUE, 0, FALSE)`, storeID); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO payout_requests (id, store_id, amount, status) VALUES ('pay-a1', $1, 400000, 'pending')`, storeID); err != nil {
		t.Fatalf("insert payout: %v", err)
	}

	repo := NewFinanceRepository(db)
	check := func(want int64) {
		t.Helper()
		bal, err := repo.GetStoreBalance(ctx, storeID)
		if err != nil {
			t.Fatalf("GetStoreBalance: %v", err)
		}
		if bal.AvailableAmount != want {
			t.Errorf("GetStoreBalance available = %d, want %d", bal.AvailableAmount, want)
		}
		var avail int64
		if err := db.QueryRowContext(ctx, sellerAvailableSQL, storeID).Scan(&avail); err != nil {
			t.Fatalf("sellerAvailableSQL: %v", err)
		}
		if avail != want {
			t.Errorf("sellerAvailableSQL available = %d, want %d", avail, want)
		}
	}
	setStatus := func(status string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `UPDATE payout_requests SET status = $1 WHERE id = 'pay-a1'`, status); err != nil {
			t.Fatalf("set status %s: %v", status, err)
		}
	}

	check(600000) // pending deducts
	setStatus("approved")
	check(600000) // approved deducts
	setStatus("transfer_confirmed")
	check(600000) // REGRESSION: pre-fix this returned 1000000 (double-spend)
	setStatus("completed")
	check(600000) // completed still deducts
	setStatus("rejected")
	check(1000000) // rejected must NOT deduct
}
