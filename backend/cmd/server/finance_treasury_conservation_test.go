// FIN-008 treasury conservation / oracle integration tests.
//
// The tests seed through the real guarded writers wherever a gate
// requires it (payout request -> review -> confirm; early-withdrawal
// request -> review; customer-wallet withdrawal request -> review;
// finance adjustments through the wr69 handler) and through the
// verified-receipt fixture pattern (payment -> verification -> receipt,
// guarded by the same triggers the production writers use) for the
// order side. No status column is hand-edited except where a gate
// explicitly says so (G-11 fault injection; G-15 unevidenced inserts).
//
// Money unit: Toman everywhere. Every oracle asserts exact Toman
// amounts; a 1 Toman deviation fails.
//
// NOTE on test isolation: the cash position is a GLOBAL aggregate, so
// each test registers treasuryCleanup, which deletes exactly the rows
// its fixture created (tracked in treasuryFixture), and treasuryTestDB
// removes the three demo seed rows migration 000008 leaves behind
// (two pending payouts, one paid order with no receipt).

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var treasuryMobileSeq int64

func treasuryMobile() string {
	n := treasuryMobileSeq + 1
	treasuryMobileSeq = n
	return fmt.Sprintf("0912%07d", 1000000+n)
}

func treasuryShortID(prefix string) string { return prefix + "-" + uuid.NewString()[:8] }

// treasuryIBAN returns a valid IR IBAN (mod-97 check digits) for the
// payout / withdrawal writers, which validate the IBAN shape.
func treasuryIBAN() string {
	bban := "0170000000000000000000"
	n, _ := new(big.Int).SetString(bban+"182700", 10)
	check := 98 - new(big.Int).Mod(n, big.NewInt(97)).Int64()
	return fmt.Sprintf("IR%02d%s", check, bban)
}

func treasuryTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	// Migration 000008 seeds demo rows that would pollute the global
	// aggregates: two pending payout_requests and one paid order with
	// no receipt. Remove them; every other seed row has zero balances.
	for _, q := range []string{
		`DELETE FROM payout_requests WHERE id IN ('PAYOUT-PENDING-001','PAYOUT-PENDING-002')`,
		`DELETE FROM order_items WHERE order_id='ord-ready-pick-001'`,
		`DELETE FROM orders WHERE id='ord-ready-pick-001'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// treasuryFixture tracks every row a test creates so treasuryCleanup
// can delete exactly those rows afterwards (the cash position is a
// global aggregate, so a leftover row breaks every other test).
type treasuryFixture struct {
	prefix         string
	storeIDs       []string
	supplierIDs    []string
	productIDs     []string
	orderIDs       []string
	userIDs        []string
	customerIDs    []string
	openingCashIDs []string
}

func treasuryNewFixture(prefix string) *treasuryFixture {
	return &treasuryFixture{prefix: prefix}
}

func treasuryCleanup(t *testing.T, db *sql.DB, f *treasuryFixture) {
	t.Helper()
	if f == nil {
		return
	}
	orders := pq.Array(f.orderIDs)
	stores := pq.Array(f.storeIDs)
	suppliers := pq.Array(f.supplierIDs)
	products := pq.Array(f.productIDs)
	customers := pq.Array(f.customerIDs)
	users := pq.Array(f.userIDs)
	opening := pq.Array(f.openingCashIDs)
	qs := []struct {
		q    string
		args []interface{}
	}{
		{`DELETE FROM order_refund_events WHERE obligation_id IN (SELECT id FROM order_refund_obligations WHERE order_id = ANY($1))`, []interface{}{orders}},
		{`DELETE FROM order_refund_attempts WHERE obligation_id IN (SELECT id FROM order_refund_obligations WHERE order_id = ANY($1))`, []interface{}{orders}},
		{`DELETE FROM order_refund_wallet_credits WHERE obligation_id IN (SELECT id FROM order_refund_obligations WHERE order_id = ANY($1))`, []interface{}{orders}},
		{`DELETE FROM order_refund_obligations WHERE order_id = ANY($1)`, []interface{}{orders}},
		{`DELETE FROM order_payment_receipts WHERE order_id = ANY($1)`, []interface{}{orders}},
		{`DELETE FROM order_payment_verifications WHERE order_id = ANY($1)`, []interface{}{orders}},
		{`DELETE FROM payment_transactions WHERE order_id = ANY($1)`, []interface{}{orders}},
		{`DELETE FROM return_items WHERE return_id IN (SELECT id FROM customer_returns WHERE order_id = ANY($1))`, []interface{}{orders}},
		{`DELETE FROM customer_returns WHERE order_id = ANY($1)`, []interface{}{orders}},
		{`DELETE FROM order_item_origins WHERE order_id = ANY($1)`, []interface{}{orders}},
		{`DELETE FROM order_items WHERE order_id = ANY($1)`, []interface{}{orders}},
		{`DELETE FROM customer_withdrawal_requests WHERE customer_id = ANY($1::uuid[])`, []interface{}{customers}},
		{`DELETE FROM customer_wallets WHERE customer_id = ANY($1::uuid[])`, []interface{}{customers}},
		{`DELETE FROM seller_early_withdrawals WHERE store_id = ANY($1)`, []interface{}{stores}},
		{`DELETE FROM payout_requests WHERE store_id = ANY($1) OR supplier_id = ANY($2)`, []interface{}{stores, suppliers}},
		{`DELETE FROM ledger_entries WHERE reason ILIKE '%fin008%'`, nil},
		{`DELETE FROM finance_adjustments WHERE target_id = ANY($1) OR target_id = ANY($2)`, []interface{}{stores, suppliers}},
		{`DELETE FROM finance_command_requests WHERE principal_id = ANY($1) OR principal_id = ANY($2)`, []interface{}{stores, suppliers}},
		{`DELETE FROM treasury_opening_cash WHERE id = ANY($1)`, []interface{}{opening}},
		{`DELETE FROM orders WHERE id = ANY($1)`, []interface{}{orders}},
		{`DELETE FROM products WHERE id = ANY($1)`, []interface{}{products}},
		{`DELETE FROM stores WHERE id = ANY($1)`, []interface{}{stores}},
		{`DELETE FROM suppliers WHERE id = ANY($1)`, []interface{}{suppliers}},
		{`DELETE FROM users WHERE id = ANY($1::uuid[])`, []interface{}{users}},
	}
	for _, s := range qs {
		if _, err := db.Exec(s.q, s.args...); err != nil {
			t.Logf("treasury cleanup (non-fatal): %s: %v", s.q, err)
		}
	}
}

func treasurySeedUser(t *testing.T, tx *sql.Tx, f *treasuryFixture, role string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO users(id,mobile,full_name,role) VALUES($1::uuid,$2,'fin008 fixture',$3)`, id, treasuryMobile(), role); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	f.userIDs = append(f.userIDs, id)
	return id
}

func treasurySeedStore(t *testing.T, tx *sql.Tx, f *treasuryFixture, ownerID string) string {
	t.Helper()
	id := treasuryShortID(f.prefix)
	if _, err := tx.Exec(`INSERT INTO stores(id,name,slug,owner_type,owner_name,owner_user_id,active_plan_id,referral_code) VALUES($1,'fin008 fixture store',$1,'dropshipper','fin008 fixture',$2::uuid,'trial',$1)`, id, ownerID); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	f.storeIDs = append(f.storeIDs, id)
	return id
}

func treasurySeedSupplier(t *testing.T, tx *sql.Tx, f *treasuryFixture) string {
	t.Helper()
	id := treasuryShortID(f.prefix)
	if _, err := tx.Exec(`INSERT INTO suppliers(id,name) VALUES($1,'fin008 fixture supplier')`, id); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
	f.supplierIDs = append(f.supplierIDs, id)
	return id
}

type treasuryOrderSpec struct {
	StoreID        string // "" = no store
	SupplierID     string // "" = no item/origin (Item still possible)
	Item           bool
	Payable        int64
	WalletAmount   int64
	GatewayAmount  int64 // receipt/payment amount when Funding != ""
	Profit         int64 // dropshipper_profit
	PlatformProfit int64
	Wholesale      int64
	Status         string // default "delivered"
	ProfitReleased bool
	Funding        string // "gateway" | "simulated" | ""
}

type treasurySeededOrder struct {
	ID         string
	CustomerID string
	PaymentID  string
	ReceiptRef string
	ItemID     string
}

// treasurySeedOrder inserts one paid order with its payment row and,
// when Funding is set, the verification + receipt rows the receipt
// triggers require (payment amount == verification amount == receipt
// amount, customer == order customer, currency TOMAN). All inserts run
// in one transaction so concurrent readers never see a half-seeded
// order.
func treasurySeedOrder(t *testing.T, db *sql.DB, f *treasuryFixture, spec treasuryOrderSpec) treasurySeededOrder {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	customerID := treasurySeedUser(t, tx, f, "customer")
	f.customerIDs = append(f.customerIDs, customerID)
	orderID := treasuryShortID(f.prefix)
	status := spec.Status
	if status == "" {
		status = "delivered"
	}
	paymentAmount := spec.Payable
	if spec.Funding != "" {
		paymentAmount = spec.GatewayAmount
	}
	var storeID interface{}
	if spec.StoreID != "" {
		storeID = spec.StoreID
	}
	if _, err := tx.Exec(`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,payment_method,payment_status,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,status,tracking_code,dropshipper_profit,platform_profit,profit_released,customer_user_id,wallet_amount,gateway_amount)
		VALUES($1,$2,$3,'fin008 fixture',$4,$4,'cash','paid','fin008 fixture','09120000000','Qom','Qom','fin008 fixture','1234567890',$5,$12,$6,$7,$8,$9::uuid,$10,$11)`,
		orderID, "ORD-"+orderID, storeID, spec.Payable, status, spec.Profit, spec.PlatformProfit, spec.ProfitReleased, customerID, spec.WalletAmount, spec.GatewayAmount, "TRK-"+orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	f.orderIDs = append(f.orderIDs, orderID)
	seeded := treasurySeededOrder{ID: orderID, CustomerID: customerID}
	if spec.Item {
		productID := treasuryShortID(f.prefix)
		var supplierID interface{}
		if spec.SupplierID != "" {
			supplierID = spec.SupplierID
		}
		if _, err := tx.Exec(`INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock) VALUES($1,'fin008 fixture',$1,$2,$3,$4,10,100)`,
			productID, supplierID, spec.Payable, spec.Wholesale); err != nil {
			t.Fatalf("seed product: %v", err)
		}
		f.productIDs = append(f.productIDs, productID)
		itemID := uuid.NewString()
		if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price) VALUES($1::uuid,$2,$3,1,$4,0,0,$5)`,
			itemID, orderID, productID, spec.Wholesale, spec.Payable); err != nil {
			t.Fatalf("seed order item: %v", err)
		}
		seeded.ItemID = itemID
		if spec.SupplierID != "" {
			if _, err := tx.Exec(`INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,warehouse_id,origin_label,quantity) VALUES($1,$2,$3::uuid,0,'supplier',$4,NULL,'fixture',1)`,
				treasuryShortID(f.prefix), orderID, itemID, spec.SupplierID); err != nil {
				t.Fatalf("seed order origin: %v", err)
			}
		}
	}
	paymentID := treasuryShortID(f.prefix)
	if _, err := tx.Exec(`INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES($1,$2,$3,$4,$5,'paid')`,
		paymentID, orderID, "ORD-"+orderID, "gt-"+paymentID, paymentAmount); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	seeded.PaymentID = paymentID
	if spec.Funding != "" {
		verID := treasuryShortID(f.prefix)
		if _, err := tx.Exec(`INSERT INTO order_payment_verifications(id,payment_id,order_id,customer_id,amount_toman,currency,funding_kind) VALUES($1,$2,$3,$4::uuid,$5,'TOMAN',$6)`,
			verID, paymentID, orderID, customerID, spec.GatewayAmount, spec.Funding); err != nil {
			t.Fatalf("seed verification: %v", err)
		}
		ref := "ref-" + uuid.NewString()[:12]
		if _, err := tx.Exec(`INSERT INTO order_payment_receipts(id,verification_id,funding_kind,payment_id,order_id,customer_id,amount_toman,currency,provider_reference,verified_at) VALUES($1,$2,$3,$4,$5,$6::uuid,$7,'TOMAN',$8,NOW())`,
			treasuryShortID(f.prefix), verID, spec.Funding, paymentID, orderID, customerID, spec.GatewayAmount, ref); err != nil {
			t.Fatalf("seed receipt: %v", err)
		}
		seeded.ReceiptRef = ref
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("seed order commit: %v", err)
	}
	return seeded
}

func treasurySeedOpeningCash(t *testing.T, db *sql.DB, f *treasuryFixture, amount int64) string {
	t.Helper()
	id := treasuryShortID(f.prefix)
	if _, err := db.Exec(`INSERT INTO treasury_opening_cash(id,amount_toman,currency,evidence_kind,evidence_reference,verified_at) VALUES($1,$2,'TOMAN','bank_statement',$3,NOW())`, id, amount, "stmt-"+uuid.NewString()[:12]); err != nil {
		t.Fatalf("seed opening cash: %v", err)
	}
	f.openingCashIDs = append(f.openingCashIDs, id)
	return id
}

func treasurySeedWallet(t *testing.T, db *sql.DB, f *treasuryFixture, balance int64) string {
	t.Helper()
	customerID := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role) VALUES($1::uuid,$2,'fin008 fixture','customer')`, customerID, treasuryMobile()); err != nil {
		t.Fatalf("seed wallet user: %v", err)
	}
	f.userIDs = append(f.userIDs, customerID)
	f.customerIDs = append(f.customerIDs, customerID)
	if _, err := db.Exec(`INSERT INTO customer_wallets(customer_id,balance,locked_balance) VALUES($1::uuid,$2,0)`, customerID, balance); err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
	return customerID
}

func treasuryWallet(t *testing.T, db *sql.DB, customerID string) (balance, locked int64) {
	t.Helper()
	if err := db.QueryRow(`SELECT balance, locked_balance FROM customer_wallets WHERE customer_id=$1::uuid`, customerID).Scan(&balance, &locked); err != nil {
		t.Fatalf("read wallet: %v", err)
	}
	return balance, locked
}

// treasuryAdjust applies a signed finance adjustment through the real
// wr69 handler (Idempotency-Key required), so the ledger legs carry
// the production reason prefix.
func treasuryAdjust(t *testing.T, db *sql.DB, targetKind, targetID, direction string, amount int64) {
	t.Helper()
	body := fmt.Sprintf(`{"targetKind":%q,"targetId":%q,"direction":%q,"amount":%d,"reason":"fin008 fixture adjustment"}`, targetKind, targetID, direction, amount)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/finance/adjustments", strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "fin008-"+uuid.NewString())
	rec := httptest.NewRecorder()
	wr69FinanceAdjustments(rec, req, db)
	if rec.Code != http.StatusOK {
		t.Fatalf("adjustment %s %s %s %d: status %d: %s", targetKind, targetID, direction, amount, rec.Code, rec.Body.String())
	}
}

// treasuryCash mirrors the CashPositionDto the handler emits
// (nullable cash fields are pointers so a JSON null is visible).
type treasuryCash struct {
	Success                  bool             `json:"success"`
	Days                     int              `json:"days"`
	AllTimeIn                int64            `json:"allTimeIn"`
	AllTimeOut               int64            `json:"allTimeOut"`
	OverObligated            bool             `json:"overObligated"`
	OverObligatedAmount      int64            `json:"overObligatedAmount"`
	LiquidCash               *int64           `json:"liquidCash"`
	SafeToSpend              *int64           `json:"safeToSpend"`
	CashStatus               string           `json:"cashStatus"`
	ReconciliationRequired   bool             `json:"reconciliationRequired"`
	UnresolvedProviderCash   int64            `json:"unresolvedProviderCash"`
	UnverifiedPaidOrders     int64            `json:"unverifiedPaidOrders"`
	LegacySimulator          int64            `json:"legacySimulator"`
	Liability                int64            `json:"liability"`
	RecoveryReceivableStores int64            `json:"recoveryReceivableStores"`
	RecoveryReceivableSup    int64            `json:"recoveryReceivableSuppliers"`
	WalletLiability          int64            `json:"walletLiability"`
	RefundLiability          int64            `json:"refundLiability"`
	RefundLiabilityGateway   int64            `json:"refundLiabilityGateway"`
	RefundLiabilityWallet    int64            `json:"refundLiabilityWallet"`
	Holds                    int64            `json:"holds"`
	HoldsByStatus            map[string]int64 `json:"holdsByStatus"`
	HeldOrderCount           int64            `json:"heldOrderCount"`
	HeldInOrders             int64            `json:"heldInOrders"`
	UnreleasedProfitHold     int64            `json:"unreleasedProfitHold"`
	OwedToStores             int64            `json:"owedToStores"`
	OwedToSuppliers          int64            `json:"owedToSuppliers"`
	PendingRefundCount       int64            `json:"pendingRefundCount"`
	PendingRefunds           int64            `json:"pendingRefunds"`
	PendingCount             int64            `json:"pendingCount"`
	PendingPayouts           int64            `json:"pendingPayouts"`
	PlatformEarned           int64            `json:"platformEarned"`
	SellerInFlight           int64            `json:"sellerInFlight"`
	SupplierInFlight         int64            `json:"supplierInFlight"`
	Period                   struct {
		In    int64 `json:"in"`
		Out   int64 `json:"out"`
		Trend []struct {
			Date      string `json:"date"`
			In        int64  `json:"in"`
			Out       int64  `json:"out"`
			Available bool   `json:"available"`
		} `json:"trend"`
		TrendAvailable bool `json:"trendAvailable"`
	} `json:"period"`
}

func treasuryCall(t *testing.T, db *sql.DB, area string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/v1/finance/treasury/cash-position?days=30"
	if area != "cash-position" {
		path = "/api/v1/finance/treasury/" + area
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handleT01FinanceTreasury(rec, req, db, area)
	return rec
}

func treasuryReadOK(t *testing.T, db *sql.DB) *treasuryCash {
	t.Helper()
	rec := treasuryCall(t, db, "cash-position")
	if rec.Code != http.StatusOK {
		t.Fatalf("cash-position status=%d body=%s", rec.Code, rec.Body.String())
	}
	var c treasuryCash
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatalf("cash-position response is not JSON: %v: %s", err, rec.Body.String())
	}
	if !c.Success {
		t.Fatalf("cash-position success=false: %s", rec.Body.String())
	}
	return &c
}

// treasuryAssertIdentity checks the two identities every report must
// satisfy: liability == sum of its components, and safeToSpend ==
// liquidCash - liability (clamped to 0 when over-obligated).
func treasuryAssertIdentity(t *testing.T, c *treasuryCash) {
	t.Helper()
	sum := c.OwedToStores + c.OwedToSuppliers + c.WalletLiability + c.RefundLiability + c.Holds + c.UnreleasedProfitHold
	if sum != c.Liability {
		t.Fatalf("liability identity: components %d+%d+%d+%d+%d+%d=%d != liability %d",
			c.OwedToStores, c.OwedToSuppliers, c.WalletLiability, c.RefundLiability, c.Holds, c.UnreleasedProfitHold, sum, c.Liability)
	}
	if c.LiquidCash == nil || c.SafeToSpend == nil {
		t.Fatalf("identity check needs numeric cash: liquidCash=%v safeToSpend=%v", c.LiquidCash, c.SafeToSpend)
	}
	raw := *c.LiquidCash - c.Liability
	if c.OverObligated {
		if *c.SafeToSpend != 0 || c.OverObligatedAmount != raw {
			t.Fatalf("over-obligated: safeToSpend=%d overObligatedAmount=%d, want 0/%d", *c.SafeToSpend, c.OverObligatedAmount, raw)
		}
	} else if *c.SafeToSpend != raw {
		t.Fatalf("safeToSpend identity: %d != liquidCash %d - liability %d", *c.SafeToSpend, *c.LiquidCash, c.Liability)
	}
	wantStatus := "verified"
	if c.UnresolvedProviderCash > 0 {
		wantStatus = "partial"
	}
	if c.CashStatus != wantStatus {
		t.Fatalf("cashStatus=%q want %q (unresolved=%d)", c.CashStatus, wantStatus, c.UnresolvedProviderCash)
	}
	if c.ReconciliationRequired != (c.UnresolvedProviderCash > 0) {
		t.Fatalf("reconciliationRequired=%v inconsistent with unresolved=%d", c.ReconciliationRequired, c.UnresolvedProviderCash)
	}
}

func treasuryCashVal(t *testing.T, v *int64, what string) int64 {
	t.Helper()
	if v == nil {
		t.Fatalf("%s is null, want a number", what)
	}
	return *v
}

// treasuryOracleFixture seeds the G-01/G-02/G-03 oracle: one store with
// 10,000,000 released profit on a delivered paid order (verified
// gateway receipt 10,000,000) plus 10,000,000 evidenced opening cash
// = liquidCash 20,000,000; then a pending 2,000,000 payout via the
// real writer.
func treasuryOracleFixture(t *testing.T, db *sql.DB, f *treasuryFixture) (storeID, ownerID, payoutID string) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ownerID = treasurySeedUser(t, tx, f, "seller")
	storeID = treasurySeedStore(t, tx, f, ownerID)
	if err := tx.Commit(); err != nil {
		t.Fatalf("oracle fixture store commit: %v", err)
	}
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: storeID, Payable: 10000000, Profit: 10000000,
		ProfitReleased: true, Funding: "gateway", GatewayAmount: 10000000,
	})
	treasurySeedOpeningCash(t, db, f, 10000000)
	fin := repository.NewFinanceRepository(db)
	payout, err := fin.RequestOwnedStorePayout(ctx, ownerID, storeID, treasuryIBAN(), "bank", 2000000)
	if err != nil {
		t.Fatalf("oracle fixture payout request: %v", err)
	}
	return storeID, ownerID, payout.ID
}

// treasuryAssertOraclePending checks the pending/approved oracle:
// cash 20,000,000, available 8,000,000, hold 2,000,000, out 0,
// liability 10,000,000, safeToSpend 10,000,000.
func treasuryAssertOraclePending(t *testing.T, c *treasuryCash) {
	t.Helper()
	if treasuryCashVal(t, c.LiquidCash, "liquidCash") != 20000000 {
		t.Fatalf("liquidCash=%d want 20000000", *c.LiquidCash)
	}
	if c.OwedToStores != 8000000 {
		t.Fatalf("owedToStores=%d want 8000000", c.OwedToStores)
	}
	if c.Holds != 2000000 || c.HoldsByStatus["pending"]+c.HoldsByStatus["approved"] != 2000000 {
		t.Fatalf("holds=%d holdsByStatus=%v want 2000000", c.Holds, c.HoldsByStatus)
	}
	if c.AllTimeOut != 0 {
		t.Fatalf("allTimeOut=%d want 0", c.AllTimeOut)
	}
	if c.Liability != 10000000 {
		t.Fatalf("liability=%d want 10000000", c.Liability)
	}
	if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 10000000 {
		t.Fatalf("safeToSpend=%d want 10000000", *c.SafeToSpend)
	}
	treasuryAssertIdentity(t, c)
}

// treasuryInsertRefundObligation replicates the exact INSERT the
// reserveOrderRefundTx writer performs (same table, same trigger
// guards) — the writer itself is unexported in package repository.
func treasuryInsertRefundObligation(t *testing.T, db *sql.DB, f *treasuryFixture, order treasurySeededOrder, purpose, businessID, destination, policy string, sourceAmount, amount int64) string {
	t.Helper()
	id := treasuryShortID(f.prefix)
	if _, err := db.Exec(`INSERT INTO order_refund_obligations(id,order_id,source_payment_id,customer_id,source_amount_toman,amount_toman,currency,purpose,business_id,destination,policy_version,status,actor_user_id)
		VALUES($1,$2,$3,$4::uuid,$5,$6,'TOMAN',$7,$8,$9,$10,'unknown','fin008-test')`,
		id, order.ID, order.PaymentID, order.CustomerID, sourceAmount, amount, purpose, businessID, destination, policy); err != nil {
		t.Fatalf("insert refund obligation: %v", err)
	}
	return id
}

// treasuryInsertReturnReversal replicates the exact seller_profit
// event recordReturnReversalsTx writes for one refunded return item
// (same table, same columns, same amount arithmetic: unitMargin x
// returnQty). The order's item row is given the margin snapshot the
// writer reads (unitMargin per unit, orderQty units, so the order's
// dropshipper_profit must equal unitMargin x orderQty). The return
// row is inserted with the FIN005 writer flag, exactly as the repo
// writer does. Only the seller_profit event is written here — the
// supplier product_cost leg is out of scope for the F-02 fixture
// (the order has no supplier item).
func treasuryInsertReturnReversal(t *testing.T, db *sql.DB, f *treasuryFixture, order treasurySeededOrder, storeID string, orderQty, returnQty int, unitMargin int64) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE order_items SET quantity=$1, seller_margin=$2 WHERE order_id=$3`, orderQty, unitMargin, order.ID); err != nil {
		t.Fatalf("seed return margin snapshot: %v", err)
	}
	if _, err := tx.Exec(`SELECT set_config('sellersho.fin005_writer','v1',true)`); err != nil {
		t.Fatalf("set fin005 writer flag: %v", err)
	}
	returnID := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO customer_returns(id,order_id,customer_id,status,prepaid) VALUES($1::uuid,$2,$3::uuid,'refunded',false)`,
		returnID, order.ID, order.CustomerID); err != nil {
		t.Fatalf("seed return: %v", err)
	}
	itemID := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO return_items(id,return_id,order_item_id,qty,qc_status,received_at) VALUES($1::uuid,$2::uuid,$3,$4,'ok',NOW())`,
		itemID, returnID, order.ItemID, returnQty); err != nil {
		t.Fatalf("seed return item: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO customer_return_financial_events(id,return_id,return_item_id,order_id,supplier_id,store_id,component,operation,amount,ledger_entry_id,idempotency_key,reason,actor_user_id)
		VALUES($1,$2::uuid,$3::uuid,$4,NULL,$5,'seller_profit','debit',$6,NULL,$7,'fin008 fixture return reversal','fin008-test')`,
		"CRF-"+uuid.NewString(), returnID, itemID, order.ID, storeID, unitMargin*int64(returnQty), "fin008-crsp:"+returnID+":"+itemID); err != nil {
		t.Fatalf("seed return reversal event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("seed return reversal commit: %v", err)
	}
}

// ---------------------------------------------------------------- G-01
func TestTreasuryPendingToApprovedSafeToSpendUnchanged(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g01")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	fin := repository.NewFinanceRepository(db)
	_, _, payoutID := treasuryOracleFixture(t, db, f)

	before := treasuryReadOK(t, db)
	treasuryAssertOraclePending(t, before)
	if before.HoldsByStatus["pending"] != 2000000 {
		t.Fatalf("pending hold=%d want 2000000 (holdsByStatus=%v)", before.HoldsByStatus["pending"], before.HoldsByStatus)
	}

	if err := fin.ReviewSettlement(ctx, payoutID, "approved", "fin008-test", "finance-test"); err != nil {
		t.Fatalf("approve payout: %v", err)
	}
	after := treasuryReadOK(t, db)
	treasuryAssertOraclePending(t, after)
	if after.HoldsByStatus["approved"] != 2000000 {
		t.Fatalf("approved hold=%d want 2000000 (holdsByStatus=%v)", after.HoldsByStatus["approved"], after.HoldsByStatus)
	}
	if *after.SafeToSpend != *before.SafeToSpend || after.Liability != before.Liability || *after.LiquidCash != *before.LiquidCash {
		t.Fatalf("approve changed the oracle: before cash=%d liability=%d safe=%d, after cash=%d liability=%d safe=%d",
			*before.LiquidCash, before.Liability, *before.SafeToSpend, *after.LiquidCash, after.Liability, *after.SafeToSpend)
	}
}

// ---------------------------------------------------------------- G-02
func TestTreasuryTransferCompletionReducesCashAndLiabilityEqually(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g02")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	fin := repository.NewFinanceRepository(db)
	_, _, payoutID := treasuryOracleFixture(t, db, f)

	before := treasuryReadOK(t, db)
	treasuryAssertOraclePending(t, before)

	if err := fin.ReviewSettlement(ctx, payoutID, "approved", "fin008-test", "finance-test"); err != nil {
		t.Fatalf("approve payout: %v", err)
	}
	if err := fin.ConfirmManualTransfer(ctx, payoutID, "fin008-g02-ref", "finance-test"); err != nil {
		t.Fatalf("confirm transfer: %v", err)
	}
	after := treasuryReadOK(t, db)
	// Confirmed: available 8,000,000, hold 0, out 2,000,000,
	// liability 8,000,000, cash 18,000,000, safeToSpend 10,000,000.
	if treasuryCashVal(t, after.LiquidCash, "liquidCash") != 18000000 {
		t.Fatalf("liquidCash=%d want 18000000", *after.LiquidCash)
	}
	if after.OwedToStores != 8000000 {
		t.Fatalf("owedToStores=%d want 8000000", after.OwedToStores)
	}
	if after.Holds != 0 {
		t.Fatalf("holds=%d want 0", after.Holds)
	}
	if after.AllTimeOut != 2000000 {
		t.Fatalf("allTimeOut=%d want 2000000", after.AllTimeOut)
	}
	if after.Liability != 8000000 {
		t.Fatalf("liability=%d want 8000000", after.Liability)
	}
	if treasuryCashVal(t, after.SafeToSpend, "safeToSpend") != 10000000 {
		t.Fatalf("safeToSpend=%d want 10000000", *after.SafeToSpend)
	}
	if after.Period.Out != 2000000 {
		t.Fatalf("period.out=%d want 2000000", after.Period.Out)
	}
	// Cash and liability both fell by exactly 2,000,000; safeToSpend
	// is unchanged at 10,000,000.
	if *before.LiquidCash-*after.LiquidCash != 2000000 || before.Liability-after.Liability != 2000000 {
		t.Fatalf("completion did not reduce cash and liability equally: cash %d->%d, liability %d->%d",
			*before.LiquidCash, *after.LiquidCash, before.Liability, after.Liability)
	}
	treasuryAssertIdentity(t, after)
}

// ---------------------------------------------------------------- G-03
func TestTreasuryPayoutRejectionPreservesConservation(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g03a")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	fin := repository.NewFinanceRepository(db)
	_, _, payoutID := treasuryOracleFixture(t, db, f)

	if err := fin.ReviewSettlement(ctx, payoutID, "rejected", "fin008-test", "finance-test"); err != nil {
		t.Fatalf("reject payout: %v", err)
	}
	c := treasuryReadOK(t, db)
	// Rejected: the payout leaves the liability; the full
	// 10,000,000 stays available, cash stays 20,000,000.
	if treasuryCashVal(t, c.LiquidCash, "liquidCash") != 20000000 {
		t.Fatalf("liquidCash=%d want 20000000", *c.LiquidCash)
	}
	if c.OwedToStores != 10000000 {
		t.Fatalf("owedToStores=%d want 10000000", c.OwedToStores)
	}
	if c.Holds != 0 {
		t.Fatalf("holds=%d want 0", c.Holds)
	}
	if c.AllTimeOut != 0 {
		t.Fatalf("allTimeOut=%d want 0", c.AllTimeOut)
	}
	if c.Liability != 10000000 {
		t.Fatalf("liability=%d want 10000000", c.Liability)
	}
	if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 10000000 {
		t.Fatalf("safeToSpend=%d want 10000000", *c.SafeToSpend)
	}
	treasuryAssertIdentity(t, c)
}

func TestTreasuryApprovedPayoutRejectionPreservesConservation(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g03b")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	fin := repository.NewFinanceRepository(db)
	_, _, payoutID := treasuryOracleFixture(t, db, f)

	if err := fin.ReviewSettlement(ctx, payoutID, "approved", "fin008-test", "finance-test"); err != nil {
		t.Fatalf("approve payout: %v", err)
	}
	before := treasuryReadOK(t, db)
	treasuryAssertOraclePending(t, before)

	// ReviewSettlement only reviews pending rows, so an approved ->
	// rejected transition is not possible through the guarded writer.
	// The attempt must fail, and the treasury must be unchanged.
	// FINDING: there is no approved-reject writer path at all; if the
	// product needs one, it has to be built (with the hold released
	// and the fee restored) — this test records the gap.
	err := fin.ReviewSettlement(ctx, payoutID, "rejected", "fin008-test", "finance-test")
	if err == nil {
		t.Fatalf("approved payout was rejected by ReviewSettlement; the gate expects the writer to refuse non-pending rows")
	}
	t.Logf("approved-reject refused by writer as expected: %v", err)
	after := treasuryReadOK(t, db)
	treasuryAssertOraclePending(t, after)
	if *after.SafeToSpend != *before.SafeToSpend || after.Liability != before.Liability || *after.LiquidCash != *before.LiquidCash {
		t.Fatalf("refused approved-reject changed the oracle: before cash=%d liability=%d safe=%d, after cash=%d liability=%d safe=%d",
			*before.LiquidCash, before.Liability, *before.SafeToSpend, *after.LiquidCash, after.Liability, *after.SafeToSpend)
	}
}

// ---------------------------------------------------------------- G-04
func TestTreasuryWalletWithdrawalReservationIsNotDoubleCounted(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g04")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	treasurySeedOpeningCash(t, db, f, 20000000)
	customerID := treasurySeedWallet(t, db, f, 5000000)

	read := func() *treasuryCash {
		c := treasuryReadOK(t, db)
		if c.WalletLiability != 5000000 {
			t.Fatalf("walletLiability=%d want 5000000", c.WalletLiability)
		}
		if treasuryCashVal(t, c.LiquidCash, "liquidCash") != 20000000 {
			t.Fatalf("liquidCash=%d want 20000000", *c.LiquidCash)
		}
		if c.Liability != 5000000 {
			t.Fatalf("liability=%d want 5000000", c.Liability)
		}
		if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 15000000 {
			t.Fatalf("safeToSpend=%d want 15000000", *c.SafeToSpend)
		}
		treasuryAssertIdentity(t, c)
		return c
	}

	read() // no reservation yet

	// Request a 2,000,000 withdrawal through the real handler: the
	// locked amount is a subset of the wallet balance, never another
	// balance.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customer/wallet/withdrawals",
		strings.NewReader(fmt.Sprintf(`{"amount":2000000,"sheba":%q,"bankName":"bank","accountHolderName":"fin008 fixture"}`, treasuryIBAN())))
	req = req.WithContext(context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: customerID}))
	rec := httptest.NewRecorder()
	handleT01CustomerWithdrawals(rec, req, db)
	if rec.Code != http.StatusCreated {
		t.Fatalf("withdrawal request: status %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("withdrawal response has no id: %v: %s", err, rec.Body.String())
	}
	if bal, locked := treasuryWallet(t, db, customerID); bal != 5000000 || locked != 2000000 {
		t.Fatalf("wallet after request: balance=%d locked=%d want 5000000/2000000", bal, locked)
	}
	read() // locked 2,000,000: totals must be identical
}

// TestTreasuryWalletWithdrawalRejectionUnlocks is the second half of
// G-04: rejecting the pending withdrawal releases the lock and leaves
// every total unchanged.
func TestTreasuryWalletWithdrawalRejectionUnlocks(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g04b")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	treasurySeedOpeningCash(t, db, f, 20000000)
	customerID := treasurySeedWallet(t, db, f, 5000000)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/customer/wallet/withdrawals",
		strings.NewReader(fmt.Sprintf(`{"amount":2000000,"sheba":%q,"bankName":"bank","accountHolderName":"fin008 fixture"}`, treasuryIBAN())))
	req = req.WithContext(context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: customerID}))
	rec := httptest.NewRecorder()
	handleT01CustomerWithdrawals(rec, req, db)
	if rec.Code != http.StatusCreated {
		t.Fatalf("withdrawal request: status %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("withdrawal response has no id: %v: %s", err, rec.Body.String())
	}
	if _, locked := treasuryWallet(t, db, customerID); locked != 2000000 {
		t.Fatalf("locked=%d want 2000000", locked)
	}

	revReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/customer-withdrawals/"+created.ID+"/review",
		strings.NewReader(`{"status":"rejected","reason":"fin008 fixture"}`))
	revRec := httptest.NewRecorder()
	handleT01AdminCustomerWithdrawalReview(revRec, revReq, db, created.ID)
	if revRec.Code != http.StatusOK {
		t.Fatalf("withdrawal review: status %d: %s", revRec.Code, revRec.Body.String())
	}
	if bal, locked := treasuryWallet(t, db, customerID); bal != 5000000 || locked != 0 {
		t.Fatalf("wallet after rejection: balance=%d locked=%d want 5000000/0", bal, locked)
	}
	c := treasuryReadOK(t, db)
	if c.WalletLiability != 5000000 || c.Liability != 5000000 {
		t.Fatalf("walletLiability=%d liability=%d want 5000000/5000000", c.WalletLiability, c.Liability)
	}
	if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 15000000 {
		t.Fatalf("safeToSpend=%d want 15000000", *c.SafeToSpend)
	}
	treasuryAssertIdentity(t, c)
}

// ---------------------------------------------------------------- G-05
func TestTreasuryWalletRefundReplacesObligation(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g05")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	// Opening cash 17,000,000 + the order's verified gateway receipt
	// 3,000,000 = liquidCash 20,000,000 (the gate oracle).
	treasurySeedOpeningCash(t, db, f, 17000000)
	order := treasurySeedOrder(t, db, f, treasuryOrderSpec{
		Item: true, Payable: 3000000, Funding: "gateway", GatewayAmount: 3000000,
	})

	// A wallet-destination obligation only arises from a received
	// return (purpose customer_return): the obligation trigger checks
	// the return row and its qc-ok received items. customer_returns
	// INSERTs are fenced (FIN005): only the writer may write them,
	// gated on the sellersho.fin005_writer GUC — set it
	// transaction-locally, exactly as the repo writer does.
	returnID := uuid.NewString()
	retTx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retTx.Exec(`SELECT set_config('sellersho.fin005_writer','v1',true)`); err != nil {
		t.Fatalf("set fin005 writer flag: %v", err)
	}
	if _, err := retTx.Exec(`INSERT INTO customer_returns(id,order_id,customer_id,status,prepaid) VALUES($1::uuid,$2,$3::uuid,'received',false)`,
		returnID, order.ID, order.CustomerID); err != nil {
		t.Fatalf("seed return: %v", err)
	}
	if _, err := retTx.Exec(`INSERT INTO return_items(id,return_id,order_item_id,qty,qc_status,received_at) VALUES($1::uuid,$2::uuid,$3,1,'ok',NOW())`,
		uuid.NewString(), returnID, order.ItemID); err != nil {
		t.Fatalf("seed return item: %v", err)
	}
	if err := retTx.Commit(); err != nil {
		t.Fatalf("seed return commit: %v", err)
	}
	obgID := treasuryInsertRefundObligation(t, db, f, order, "customer_return", returnID, "wallet", "return_wallet_v1", 3000000, 3000000)

	check := func(wantRefund, wantWallet int64) {
		c := treasuryReadOK(t, db)
		if c.RefundLiability != wantRefund {
			t.Fatalf("refundLiability=%d want %d", c.RefundLiability, wantRefund)
		}
		if c.RefundLiabilityWallet != wantRefund {
			t.Fatalf("refundLiabilityWallet=%d want %d", c.RefundLiabilityWallet, wantRefund)
		}
		if c.RefundLiabilityGateway != 0 {
			t.Fatalf("refundLiabilityGateway=%d want 0", c.RefundLiabilityGateway)
		}
		if c.WalletLiability != wantWallet {
			t.Fatalf("walletLiability=%d want %d", c.WalletLiability, wantWallet)
		}
		if c.Liability != 3000000 {
			t.Fatalf("liability=%d want 3000000 (obligation and wallet credit are the same money)", c.Liability)
		}
		if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 17000000 {
			t.Fatalf("safeToSpend=%d want 17000000", *c.SafeToSpend)
		}
		if treasuryCashVal(t, c.LiquidCash, "liquidCash") != 20000000 {
			t.Fatalf("liquidCash=%d want 20000000", *c.LiquidCash)
		}
		treasuryAssertIdentity(t, c)
	}
	check(3000000, 0) // obligation outstanding, wallet still empty

	// Credit the wallet: replicate creditReservedWalletRefundsTx
	// (credit journal first, then the succeeded event — and both in
	// ONE transaction: the credit trigger requires the immutable
	// success outcome to commit together with the journal).
	creditTx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := creditTx.Exec(`INSERT INTO order_refund_wallet_credits(obligation_id,customer_id,amount_toman)
		SELECT id,customer_id,amount_toman FROM order_refund_obligations WHERE id=$1 AND destination='wallet'
		ON CONFLICT(obligation_id) DO NOTHING`, obgID); err != nil {
		t.Fatalf("wallet credit journal: %v", err)
	}
	if _, err := creditTx.Exec(`INSERT INTO order_refund_events(id,obligation_id,source_payment_id,amount_toman,currency,destination,status,operation_key,provider_reference,reason)
		SELECT 'wallet-success:'||id,id,source_payment_id,amount_toman,currency,destination,'succeeded','refund:'||id,'wallet:'||id,'wallet_credited'
		FROM order_refund_obligations WHERE id=$1 ON CONFLICT(id) DO NOTHING`, obgID); err != nil {
		t.Fatalf("wallet credit event: %v", err)
	}
	if err := creditTx.Commit(); err != nil {
		t.Fatalf("wallet credit commit: %v", err)
	}
	if bal, _ := treasuryWallet(t, db, order.CustomerID); bal != 3000000 {
		t.Fatalf("wallet balance=%d want 3000000 after credit", bal)
	}
	check(0, 3000000) // obligation replaced by the wallet balance
}

// ---------------------------------------------------------------- G-06
func TestTreasuryPendingGatewayRefundIsLiability(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g06")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	// No opening cash: the order's verified gateway receipt
	// 20,000,000 is the whole liquidCash (the gate oracle).
	order := treasurySeedOrder(t, db, f, treasuryOrderSpec{
		Payable: 20000000, Funding: "gateway", GatewayAmount: 20000000,
	})
	obgID := treasuryInsertRefundObligation(t, db, f, order, "admin_refund", "admin:"+order.ID+":fin008-g06", "original_payment", "original_payment_v1", 20000000, 4000000)

	pending := treasuryReadOK(t, db)
	if pending.RefundLiability != 4000000 || pending.RefundLiabilityGateway != 4000000 {
		t.Fatalf("pending refundLiability=%d gateway=%d want 4000000/4000000", pending.RefundLiability, pending.RefundLiabilityGateway)
	}
	if pending.Liability != 4000000 {
		t.Fatalf("pending liability=%d want 4000000", pending.Liability)
	}
	if treasuryCashVal(t, pending.SafeToSpend, "safeToSpend") != 16000000 {
		t.Fatalf("pending safeToSpend=%d want 16000000", *pending.SafeToSpend)
	}
	if treasuryCashVal(t, pending.LiquidCash, "liquidCash") != 20000000 {
		t.Fatalf("pending liquidCash=%d want 20000000", *pending.LiquidCash)
	}
	treasuryAssertIdentity(t, pending)

	// Mark the refund succeeded: replicate ProcessOrderRefund
	// (attempt first — the succeeded-event trigger requires it — then
	// the unknown started event, then the succeeded event).
	if _, err := db.Exec(`INSERT INTO order_refund_attempts(id,obligation_id,source_payment_id,amount_toman,currency,destination,operation_key,original_reference)
		VALUES($1,$2,$3,4000000,'TOMAN','original_payment',$4,$5)`,
		"fin008-att-"+obgID, obgID, order.PaymentID, "refund:"+obgID, order.ReceiptRef); err != nil {
		t.Fatalf("refund attempt: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO order_refund_events(id,obligation_id,source_payment_id,amount_toman,currency,destination,status,operation_key,provider_reference,reason)
		SELECT 'fin008-ev-u-'||id,id,source_payment_id,amount_toman,currency,destination,'unknown','refund:'||id,'','provider_request_started'
		FROM order_refund_obligations WHERE id=$1`, obgID); err != nil {
		t.Fatalf("refund unknown event: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO order_refund_events(id,obligation_id,source_payment_id,amount_toman,currency,destination,status,operation_key,provider_reference,reason)
		SELECT 'fin008-ev-s-'||id,id,source_payment_id,amount_toman,currency,destination,'succeeded','refund:'||id,$2,'provider_refund_confirmed'
		FROM order_refund_obligations WHERE id=$1`, obgID, order.ReceiptRef); err != nil {
		t.Fatalf("refund succeeded event: %v", err)
	}

	after := treasuryReadOK(t, db)
	if after.RefundLiability != 0 {
		t.Fatalf("succeeded refundLiability=%d want 0", after.RefundLiability)
	}
	if after.Liability != 0 {
		t.Fatalf("succeeded liability=%d want 0", after.Liability)
	}
	if treasuryCashVal(t, after.LiquidCash, "liquidCash") != 16000000 {
		t.Fatalf("succeeded liquidCash=%d want 16000000 (cash out)", *after.LiquidCash)
	}
	if treasuryCashVal(t, after.SafeToSpend, "safeToSpend") != 16000000 {
		t.Fatalf("succeeded safeToSpend=%d want 16000000", *after.SafeToSpend)
	}
	if after.AllTimeOut != 4000000 {
		t.Fatalf("allTimeOut=%d want 4000000", after.AllTimeOut)
	}
	treasuryAssertIdentity(t, after)
}

func TestTreasuryUnknownGatewayRefundStaysReserved(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g06b")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	treasurySeedOpeningCash(t, db, f, 20000000)
	// Paid order with NO receipt: the obligation's funding is
	// unverified, so the trigger must leave it 'unknown'.
	order := treasurySeedOrder(t, db, f, treasuryOrderSpec{Payable: 4000000})
	obgID := treasuryInsertRefundObligation(t, db, f, order, "admin_refund", "admin:"+order.ID+":fin008-g06b", "original_payment", "original_payment_v1", 4000000, 4000000)
	var status, stateReason string
	if err := db.QueryRow(`SELECT status, state_reason FROM order_refund_obligations WHERE id=$1`, obgID).Scan(&status, &stateReason); err != nil {
		t.Fatalf("read obligation: %v", err)
	}
	if status != "unknown" || stateReason != "funding_unverified" {
		t.Fatalf("obligation status=%q state_reason=%q want unknown/funding_unverified", status, stateReason)
	}
	c := treasuryReadOK(t, db)
	if c.RefundLiability != 4000000 || c.RefundLiabilityGateway != 4000000 {
		t.Fatalf("refundLiability=%d gateway=%d want 4000000/4000000", c.RefundLiability, c.RefundLiabilityGateway)
	}
	if c.Liability != 4000000 {
		t.Fatalf("liability=%d want 4000000", c.Liability)
	}
	if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 16000000 {
		t.Fatalf("safeToSpend=%d want 16000000", *c.SafeToSpend)
	}
	if c.UnresolvedProviderCash != 4000000 {
		t.Fatalf("unresolvedProviderCash=%d want 4000000", c.UnresolvedProviderCash)
	}
	if c.CashStatus != "partial" {
		t.Fatalf("cashStatus=%q want partial", c.CashStatus)
	}
	treasuryAssertIdentity(t, c)
}

// ---------------------------------------------------------------- F-01
func TestTreasuryFailedGatewayRefundStaysLiability(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("f01")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	// Same oracle as G-06: verified gateway receipt 20,000,000, one
	// gateway refund obligation of 4,000,000.
	order := treasurySeedOrder(t, db, f, treasuryOrderSpec{
		Payable: 20000000, Funding: "gateway", GatewayAmount: 20000000,
	})
	obgID := treasuryInsertRefundObligation(t, db, f, order, "admin_refund", "admin:"+order.ID+":fin008-f01", "original_payment", "original_payment_v1", 20000000, 4000000)

	pending := treasuryReadOK(t, db)
	if pending.RefundLiability != 4000000 || treasuryCashVal(t, pending.SafeToSpend, "safeToSpend") != 16000000 {
		t.Fatalf("pending refundLiability=%d safeToSpend=%d want 4000000/16000000", pending.RefundLiability, *pending.SafeToSpend)
	}
	treasuryAssertIdentity(t, pending)

	// The provider REJECTS the refund: a 'failed' event. A failed
	// event is a failed attempt, not a decision the customer is not
	// owed (FIN-003 keeps failed obligations reserved and retries
	// them) — the liability and safeToSpend must not move at all.
	if _, err := db.Exec(`INSERT INTO order_refund_attempts(id,obligation_id,source_payment_id,amount_toman,currency,destination,operation_key,original_reference)
		VALUES($1,$2,$3,4000000,'TOMAN','original_payment',$4,$5)`,
		"fin008-att-"+obgID, obgID, order.PaymentID, "refund:"+obgID, order.ReceiptRef); err != nil {
		t.Fatalf("refund attempt: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO order_refund_events(id,obligation_id,source_payment_id,amount_toman,currency,destination,status,operation_key,provider_reference,reason)
		SELECT 'fin008-ev-f-'||id,id,source_payment_id,amount_toman,currency,destination,'failed','refund:'||id,'','provider_refund_rejected'
		FROM order_refund_obligations WHERE id=$1`, obgID); err != nil {
		t.Fatalf("refund failed event: %v", err)
	}

	failed := treasuryReadOK(t, db)
	if failed.RefundLiability != 4000000 || failed.RefundLiabilityGateway != 4000000 {
		t.Fatalf("failed refundLiability=%d gateway=%d want 4000000/4000000 (a provider failure must not release the liability)", failed.RefundLiability, failed.RefundLiabilityGateway)
	}
	if failed.Liability != 4000000 {
		t.Fatalf("failed liability=%d want 4000000", failed.Liability)
	}
	if treasuryCashVal(t, failed.LiquidCash, "liquidCash") != 20000000 {
		t.Fatalf("failed liquidCash=%d want 20000000 (no money moved)", *failed.LiquidCash)
	}
	if treasuryCashVal(t, failed.SafeToSpend, "safeToSpend") != 16000000 {
		t.Fatalf("failed safeToSpend=%d want 16000000 (unchanged by the provider failure)", *failed.SafeToSpend)
	}
	treasuryAssertIdentity(t, failed)

	// The retry then succeeds: cash and liability fall together by
	// 4,000,000, safeToSpend unchanged.
	if _, err := db.Exec(`INSERT INTO order_refund_events(id,obligation_id,source_payment_id,amount_toman,currency,destination,status,operation_key,provider_reference,reason)
		SELECT 'fin008-ev-s-'||id,id,source_payment_id,amount_toman,currency,destination,'succeeded','refund:'||id,$2,'provider_refund_confirmed'
		FROM order_refund_obligations WHERE id=$1`, obgID, order.ReceiptRef); err != nil {
		t.Fatalf("refund succeeded event: %v", err)
	}
	after := treasuryReadOK(t, db)
	if after.RefundLiability != 0 || after.Liability != 0 {
		t.Fatalf("succeeded refundLiability=%d liability=%d want 0/0", after.RefundLiability, after.Liability)
	}
	if treasuryCashVal(t, after.LiquidCash, "liquidCash") != 16000000 {
		t.Fatalf("succeeded liquidCash=%d want 16000000", *after.LiquidCash)
	}
	if treasuryCashVal(t, after.SafeToSpend, "safeToSpend") != 16000000 {
		t.Fatalf("succeeded safeToSpend=%d want 16000000", *after.SafeToSpend)
	}
	treasuryAssertIdentity(t, after)
}

// ---------------------------------------------------------------- G-07
func TestTreasuryNegativeStoreBalanceIsNotNetted(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g07")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	ownerA := treasurySeedUser(t, tx, f, "seller")
	storeA := treasurySeedStore(t, tx, f, ownerA)
	ownerB := treasurySeedUser(t, tx, f, "seller")
	storeB := treasurySeedStore(t, tx, f, ownerB)
	supplierC := treasurySeedSupplier(t, tx, f)
	supplierD := treasurySeedSupplier(t, tx, f)
	if err := tx.Commit(); err != nil {
		t.Fatalf("g07 fixture commit: %v", err)
	}

	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: storeA, Payable: 10000000, Profit: 10000000,
		ProfitReleased: true, Funding: "gateway", GatewayAmount: 10000000,
	})
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		SupplierID: supplierC, Item: true, Payable: 6000000, Wholesale: 6000000,
		Funding: "gateway", GatewayAmount: 6000000,
	})
	treasurySeedOpeningCash(t, db, f, 14000000)
	// Negative balances via signed adjustments through the real writer.
	treasuryAdjust(t, db, "seller", storeB, "debit", 3000000)
	treasuryAdjust(t, db, "supplier", supplierD, "debit", 2000000)

	c := treasuryReadOK(t, db)
	if treasuryCashVal(t, c.LiquidCash, "liquidCash") != 30000000 {
		t.Fatalf("liquidCash=%d want 30000000", *c.LiquidCash)
	}
	if c.OwedToStores != 10000000 {
		t.Fatalf("owedToStores=%d want 10000000 (store B -3000000 must not reduce it)", c.OwedToStores)
	}
	if c.OwedToSuppliers != 6000000 {
		t.Fatalf("owedToSuppliers=%d want 6000000 (supplier D -2000000 must not reduce it)", c.OwedToSuppliers)
	}
	if c.RecoveryReceivableStores != 3000000 {
		t.Fatalf("recoveryReceivableStores=%d want 3000000", c.RecoveryReceivableStores)
	}
	if c.RecoveryReceivableSup != 2000000 {
		t.Fatalf("recoveryReceivableSuppliers=%d want 2000000", c.RecoveryReceivableSup)
	}
	if c.Liability != 16000000 {
		t.Fatalf("liability=%d want 16000000", c.Liability)
	}
	if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 14000000 {
		t.Fatalf("safeToSpend=%d want 14000000", *c.SafeToSpend)
	}
	treasuryAssertIdentity(t, c)
}

func TestTreasuryNegativeSupplierBalanceIsNotNetted(t *testing.T) {
	// Same population as the store test, asserted from the supplier
	// side: the supplier recovery debt must be reported separately
	// and must not offset the store liability either.
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g07b")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	ownerA := treasurySeedUser(t, tx, f, "seller")
	storeA := treasurySeedStore(t, tx, f, ownerA)
	supplierC := treasurySeedSupplier(t, tx, f)
	supplierD := treasurySeedSupplier(t, tx, f)
	if err := tx.Commit(); err != nil {
		t.Fatalf("g07b fixture commit: %v", err)
	}
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: storeA, Payable: 10000000, Profit: 10000000,
		ProfitReleased: true, Funding: "gateway", GatewayAmount: 10000000,
	})
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		SupplierID: supplierC, Item: true, Payable: 6000000, Wholesale: 6000000,
		Funding: "gateway", GatewayAmount: 6000000,
	})
	treasurySeedOpeningCash(t, db, f, 14000000)
	treasuryAdjust(t, db, "supplier", supplierD, "debit", 2000000)

	c := treasuryReadOK(t, db)
	if c.OwedToSuppliers != 6000000 {
		t.Fatalf("owedToSuppliers=%d want 6000000", c.OwedToSuppliers)
	}
	if c.RecoveryReceivableSup != 2000000 {
		t.Fatalf("recoveryReceivableSuppliers=%d want 2000000", c.RecoveryReceivableSup)
	}
	if c.OwedToStores != 10000000 {
		t.Fatalf("owedToStores=%d want 10000000", c.OwedToStores)
	}
	if c.Liability != 16000000 || treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 14000000 {
		t.Fatalf("liability=%d safeToSpend=%d want 16000000/14000000", c.Liability, *c.SafeToSpend)
	}
	treasuryAssertIdentity(t, c)
}

// ---------------------------------------------------------------- G-08
func TestTreasuryMixedLiabilitiesCoherentTotals(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g08")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := treasurySeedUser(t, tx, f, "seller")
	storeID := treasurySeedStore(t, tx, f, owner)
	supplierID := treasurySeedSupplier(t, tx, f)
	if err := tx.Commit(); err != nil {
		t.Fatalf("g08 fixture commit: %v", err)
	}

	// Released order: profit 10,000,000, verified receipt 10,000,000.
	released := treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: storeID, Payable: 10000000, Profit: 10000000,
		ProfitReleased: true, Funding: "gateway", GatewayAmount: 10000000,
	})
	// Delivered-but-unreleased order: profit 2,000,000, wholesale
	// 6,000,000, platform fee 2,000,000, payable 10,000,000, verified
	// receipt 10,000,000. Its wholesale slice lives inside the
	// supplier 6,000,000 and must not appear again as a hold.
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: storeID, SupplierID: supplierID, Item: true,
		Payable: 10000000, Profit: 2000000, PlatformProfit: 2000000, Wholesale: 6000000,
		Funding: "gateway", GatewayAmount: 10000000,
	})
	fin := repository.NewFinanceRepository(db)
	if _, err := fin.RequestOwnedStorePayout(ctx, owner, storeID, treasuryIBAN(), "bank", 2000000); err != nil {
		t.Fatalf("g08 payout request: %v", err)
	}
	treasurySeedWallet(t, db, f, 5000000)
	treasuryInsertRefundObligation(t, db, f, released, "admin_refund", "admin:"+released.ID+":fin008-g08", "original_payment", "original_payment_v1", 10000000, 2000000)
	treasurySeedOpeningCash(t, db, f, 10000000)

	c := treasuryReadOK(t, db)
	if treasuryCashVal(t, c.LiquidCash, "liquidCash") != 30000000 {
		t.Fatalf("liquidCash=%d want 30000000", *c.LiquidCash)
	}
	// NOTE on the gate's "available 10,000,000": the gate computes
	// earned 12,000,000 (released 10,000,000 + unreleased 2,000,000)
	// minus the 2,000,000 hold. The canonical FIN-001 available is
	// released profit only, so it is 10,000,000 - 2,000,000 =
	// 8,000,000; the unreleased 2,000,000 is a separate liability
	// term (G-09), not part of available. The seller contribution
	// the gate pins — 12,000,000 — is exactly
	// 8,000,000 (available) + 2,000,000 (payout hold) + 2,000,000
	// (unreleased profit), and every total below matches the gate.
	if c.OwedToStores != 8000000 {
		t.Fatalf("owedToStores=%d want 8000000 (released 10000000 - payout hold 2000000)", c.OwedToStores)
	}
	if c.Holds != 2000000 {
		t.Fatalf("holds=%d want 2000000", c.Holds)
	}
	if c.UnreleasedProfitHold != 2000000 {
		t.Fatalf("unreleasedProfitHold=%d want 2000000", c.UnreleasedProfitHold)
	}
	if c.OwedToStores+c.Holds+c.UnreleasedProfitHold != 12000000 {
		t.Fatalf("seller contribution=%d want 12000000", c.OwedToStores+c.Holds+c.UnreleasedProfitHold)
	}
	if c.OwedToSuppliers != 6000000 {
		t.Fatalf("owedToSuppliers=%d want 6000000", c.OwedToSuppliers)
	}
	if c.WalletLiability != 5000000 {
		t.Fatalf("walletLiability=%d want 5000000", c.WalletLiability)
	}
	if c.RefundLiability != 2000000 {
		t.Fatalf("refundLiability=%d want 2000000", c.RefundLiability)
	}
	if c.Liability != 25000000 {
		t.Fatalf("liability=%d want 25000000", c.Liability)
	}
	if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 5000000 {
		t.Fatalf("safeToSpend=%d want 5000000", *c.SafeToSpend)
	}
	if c.HeldOrderCount != 1 || c.HeldInOrders != 2000000 {
		t.Fatalf("heldOrderCount=%d heldInOrders=%d want 1/2000000", c.HeldOrderCount, c.HeldInOrders)
	}
	treasuryAssertIdentity(t, c)
}

// ---------------------------------------------------------------- G-09
func TestTreasuryHeldOrdersCountUnreleasedProfitOnly(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g09")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := treasurySeedUser(t, tx, f, "seller")
	storeID := treasurySeedStore(t, tx, f, owner)
	supplierID := treasurySeedSupplier(t, tx, f)
	if err := tx.Commit(); err != nil {
		t.Fatalf("g09 fixture commit: %v", err)
	}
	// payable 10,000,000 = wholesale 6,000,000 + profit 2,000,000 +
	// fee 2,000,000; delivered, unreleased; verified receipt.
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: storeID, SupplierID: supplierID, Item: true,
		Payable: 10000000, Profit: 2000000, PlatformProfit: 2000000, Wholesale: 6000000,
		Funding: "gateway", GatewayAmount: 10000000,
	})

	c := treasuryReadOK(t, db)
	if c.OwedToSuppliers != 6000000 {
		t.Fatalf("owedToSuppliers=%d want 6000000", c.OwedToSuppliers)
	}
	if c.OwedToStores != 0 {
		t.Fatalf("owedToStores=%d want 0 (profit is unreleased)", c.OwedToStores)
	}
	if c.UnreleasedProfitHold != 2000000 {
		t.Fatalf("unreleasedProfitHold=%d want 2000000", c.UnreleasedProfitHold)
	}
	if c.HeldInOrders != 2000000 {
		t.Fatalf("heldInOrders=%d want 2000000 (profit only, NOT the whole payable)", c.HeldInOrders)
	}
	if c.HeldOrderCount != 1 {
		t.Fatalf("heldOrderCount=%d want 1", c.HeldOrderCount)
	}
	if c.Liability != 8000000 {
		t.Fatalf("liability=%d want 8000000 (wholesale 6000000 + profit 2000000, once each)", c.Liability)
	}
	if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 2000000 {
		t.Fatalf("safeToSpend=%d want 2000000", *c.SafeToSpend)
	}
	if c.PlatformEarned != 2000000 {
		t.Fatalf("platformEarned=%d want 2000000", c.PlatformEarned)
	}
	treasuryAssertIdentity(t, c)
}

// ---------------------------------------------------------------- F-02
func TestTreasuryRefundedReturnProfitExcludedFromHeldOrders(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("f02")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := treasurySeedUser(t, tx, f, "seller")
	storeID := treasurySeedStore(t, tx, f, owner)
	if err := tx.Commit(); err != nil {
		t.Fatalf("f02 fixture commit: %v", err)
	}
	// Two delivered-but-unreleased orders on one store, each payable
	// 10,000,000 with profit 2,000,000 (2 units x 1,000,000 margin)
	// and a verified gateway receipt of 10,000,000.
	full := treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: storeID, Item: true,
		Payable: 10000000, Profit: 2000000,
		Funding: "gateway", GatewayAmount: 10000000,
	})
	partial := treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: storeID, Item: true,
		Payable: 10000000, Profit: 2000000,
		Funding: "gateway", GatewayAmount: 10000000,
	})
	// Order A is FULLY returned and refunded: its 2,000,000 profit
	// reverses in full. Order B is PARTLY returned: 1 of 2 units,
	// so 1,000,000 of its profit reverses.
	treasuryInsertReturnReversal(t, db, f, full, storeID, 2, 2, 1000000)
	treasuryInsertReturnReversal(t, db, f, partial, storeID, 2, 1, 1000000)

	c := treasuryReadOK(t, db)
	if treasuryCashVal(t, c.LiquidCash, "liquidCash") != 20000000 {
		t.Fatalf("liquidCash=%d want 20000000", *c.LiquidCash)
	}
	// The fully-returned order contributes 0 to the hold; the
	// partly-returned one contributes only its unreversed 1,000,000.
	if c.HeldInOrders != 1000000 {
		t.Fatalf("heldInOrders=%d want 1000000 (fully-returned profit excluded, partial profit netted once)", c.HeldInOrders)
	}
	if c.UnreleasedProfitHold != 1000000 {
		t.Fatalf("unreleasedProfitHold=%d want 1000000", c.UnreleasedProfitHold)
	}
	if c.HeldOrderCount != 1 {
		t.Fatalf("heldOrderCount=%d want 1 (only the partly-returned order still holds profit)", c.HeldOrderCount)
	}
	// The reversals must not manufacture a recovery receivable out
	// of profit the seller never received, and the store owes nothing
	// else: no recovery, no store liability.
	if c.OwedToStores != 0 {
		t.Fatalf("owedToStores=%d want 0", c.OwedToStores)
	}
	if c.RecoveryReceivableStores != 0 {
		t.Fatalf("recoveryReceivableStores=%d want 0 (reversed unreleased profit is not a debt)", c.RecoveryReceivableStores)
	}
	if c.Liability != 1000000 {
		t.Fatalf("liability=%d want 1000000", c.Liability)
	}
	if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 19000000 {
		t.Fatalf("safeToSpend=%d want 19000000", *c.SafeToSpend)
	}
	treasuryAssertIdentity(t, c)
}

// ---------------------------------------------------------------- G-10
func TestTreasuryEarlyWithdrawalPendingApprovedRejected(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g10")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	fin := repository.NewFinanceRepository(db)

	// The early-withdrawal fee percentage is a platform setting; the
	// gate pins it at 10%. Restore the (NULL) original afterwards.
	if _, err := db.Exec(`UPDATE platform_settings SET early_withdrawal_fee_pct=10 WHERE id=1`); err != nil {
		t.Fatalf("set early withdrawal fee pct: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`UPDATE platform_settings SET early_withdrawal_fee_pct=NULL WHERE id=1`)
	})
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := treasurySeedUser(t, tx, f, "seller")
	storeID := treasurySeedStore(t, tx, f, owner)
	if err := tx.Commit(); err != nil {
		t.Fatalf("g10 fixture commit: %v", err)
	}
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: storeID, Payable: 10000000, Profit: 10000000,
		ProfitReleased: true, Funding: "gateway", GatewayAmount: 10000000,
	})
	treasurySeedOpeningCash(t, db, f, 10000000)

	// Pending: gross 1,000,000 held, available 9,000,000.
	wd, err := fin.RequestEarlyWithdrawal(ctx, owner, storeID, 1000000, treasuryIBAN(), "bank")
	if err != nil {
		t.Fatalf("request early withdrawal: %v", err)
	}
	pending := treasuryReadOK(t, db)
	if pending.OwedToStores != 9000000 {
		t.Fatalf("pending owedToStores=%d want 9000000", pending.OwedToStores)
	}
	if pending.Holds != 1000000 || pending.HoldsByStatus["earlyWithdrawal"] != 1000000 {
		t.Fatalf("pending holds=%d holdsByStatus=%v want 1000000 earlyWithdrawal", pending.Holds, pending.HoldsByStatus)
	}
	if pending.Liability != 10000000 {
		t.Fatalf("pending liability=%d want 10000000", pending.Liability)
	}
	if treasuryCashVal(t, pending.SafeToSpend, "safeToSpend") != 10000000 {
		t.Fatalf("pending safeToSpend=%d want 10000000", *pending.SafeToSpend)
	}
	treasuryAssertIdentity(t, pending)

	// Approved: fee 100,000 + net payout 900,000 (status approved);
	// available stays 9,000,000, hold becomes the 900,000 payout.
	revReq := httptest.NewRequest(http.MethodPost, "/api/v1/finance/early-withdrawals/"+wd.ID+"/review",
		strings.NewReader(`{"status":"approved"}`))
	revRec := httptest.NewRecorder()
	handleT01EarlyWithdrawalReview(revRec, revReq, db, wd.ID)
	if revRec.Code != http.StatusOK {
		t.Fatalf("early withdrawal review: status %d: %s", revRec.Code, revRec.Body.String())
	}
	approved := treasuryReadOK(t, db)
	if approved.OwedToStores != 9000000 {
		t.Fatalf("approved owedToStores=%d want 9000000", approved.OwedToStores)
	}
	if approved.Holds != 900000 || approved.HoldsByStatus["approved"] != 900000 {
		t.Fatalf("approved holds=%d holdsByStatus=%v want 900000 approved", approved.Holds, approved.HoldsByStatus)
	}
	if approved.Liability != 9900000 {
		t.Fatalf("approved liability=%d want 9900000", approved.Liability)
	}
	if treasuryCashVal(t, approved.SafeToSpend, "safeToSpend") != 10100000 {
		t.Fatalf("approved safeToSpend=%d want 10100000", *approved.SafeToSpend)
	}
	treasuryAssertIdentity(t, approved)

	// A second request of 1,000,000, then rejected: the totals must
	// return to exactly the approved numbers.
	wd2, err := fin.RequestEarlyWithdrawal(ctx, owner, storeID, 1000000, treasuryIBAN(), "bank")
	if err != nil {
		t.Fatalf("request second early withdrawal: %v", err)
	}
	revReq2 := httptest.NewRequest(http.MethodPost, "/api/v1/finance/early-withdrawals/"+wd2.ID+"/review",
		strings.NewReader(`{"status":"rejected","reason":"fin008 fixture"}`))
	revRec2 := httptest.NewRecorder()
	handleT01EarlyWithdrawalReview(revRec2, revReq2, db, wd2.ID)
	if revRec2.Code != http.StatusOK {
		t.Fatalf("second early withdrawal review: status %d: %s", revRec2.Code, revRec2.Body.String())
	}
	after := treasuryReadOK(t, db)
	if after.OwedToStores != 9000000 || after.Holds != 900000 || after.Liability != 9900000 {
		t.Fatalf("after rejection: owedToStores=%d holds=%d liability=%d want 9000000/900000/9900000",
			after.OwedToStores, after.Holds, after.Liability)
	}
	if treasuryCashVal(t, after.SafeToSpend, "safeToSpend") != 10100000 {
		t.Fatalf("after rejection safeToSpend=%d want 10100000", *after.SafeToSpend)
	}
	treasuryAssertIdentity(t, after)
}

// ---------------------------------------------------------------- G-11
func TestTreasuryFailedReadFailsReport(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g11")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	treasurySeedOpeningCash(t, db, f, 1000000)
	// A healthy report first.
	treasuryReadOK(t, db)

	// Rename each table the report reads, one at a time; the report
	// must fail (non-200) and must not leak any figures. The table
	// is renamed back immediately after the call, before any
	// assertion, so a failure here cannot poison later tests.
	groups := []string{
		"orders", "payout_requests", "suppliers", "stores",
		"customer_wallets", "order_refund_obligations",
		"seller_early_withdrawals", "order_payment_receipts",
		"treasury_opening_cash", "customer_withdrawal_requests",
	}
	for _, table := range groups {
		fault := table + "_fin008_fault"
		if _, err := db.Exec(`ALTER TABLE ` + table + ` RENAME TO ` + fault); err != nil {
			t.Fatalf("rename %s: %v", table, err)
		}
		rec := treasuryCall(t, db, "cash-position")
		if _, err := db.Exec(`ALTER TABLE ` + fault + ` RENAME TO ` + table); err != nil {
			t.Fatalf("rename back %s: %v", table, err)
		}
		if rec.Code == http.StatusOK {
			t.Fatalf("cash-position with %s unavailable returned 200: %s", table, rec.Body.String())
		}
		body := rec.Body.String()
		if strings.Contains(body, `"safeToSpend"`) || strings.Contains(body, `"success":true`) {
			t.Fatalf("cash-position with %s unavailable leaked figures: %s", table, body)
		}
	}

	// A payout row in the legacy 'paid' status (no writer sets it)
	// must fail the report, not silently pass.
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := treasurySeedUser(t, tx, f, "seller")
	storeID := treasurySeedStore(t, tx, f, owner)
	if err := tx.Commit(); err != nil {
		t.Fatalf("g11 paid-status fixture commit: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,available_balance,status) VALUES($1,$2,'seller','fin008 fixture',$3,'bank',1000000,0,'paid')`,
		treasuryShortID(f.prefix), storeID, treasuryIBAN()); err != nil {
		t.Fatalf("insert paid payout row: %v", err)
	}
	rec := treasuryCall(t, db, "cash-position")
	if rec.Code == http.StatusOK {
		t.Fatalf("cash-position with a 'paid' payout row returned 200: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "reconciliationRequired") {
		t.Fatalf("paid-payout failure must carry reconciliationRequired: %s", rec.Body.String())
	}
}

func TestTreasuryProfitLocksUncomputableIsNotZero(t *testing.T) {
	db := treasuryTestDB(t)
	// Drop the profit-lock view, then the column it needs; restore
	// both in cleanup so later tests are unaffected.
	if _, err := db.Exec(`DROP VIEW IF EXISTS v_fin_store_profit_lock`); err != nil {
		t.Fatalf("drop profit-lock view: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`ALTER TABLE orders ADD COLUMN IF NOT EXISTS return_window_ends_at TIMESTAMPTZ`); err != nil {
			t.Logf("restore return_window_ends_at: %v", err)
			return
		}
		b, err := os.ReadFile("../../migrations/202610190004_profit_lock_view.up.sql")
		if err != nil {
			t.Logf("read profit-lock view migration: %v", err)
			return
		}
		if _, err := db.Exec(string(b)); err != nil {
			t.Logf("restore profit-lock view: %v", err)
		}
	})
	if _, err := db.Exec(`ALTER TABLE orders DROP COLUMN IF EXISTS return_window_ends_at`); err != nil {
		t.Fatalf("drop return_window_ends_at: %v", err)
	}
	rec := treasuryCall(t, db, "profit-locks")
	if rec.Code == http.StatusOK {
		t.Fatalf("uncomputable profit locks returned 200: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"success":true`) || strings.Contains(rec.Body.String(), `"locked":0`) {
		t.Fatalf("uncomputable profit locks leaked a zero answer: %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------- G-12
func TestTreasuryConcurrentApprovalYieldsCoherentSnapshot(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g12")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	fin := repository.NewFinanceRepository(db)
	storeID, _, payoutID := treasuryOracleFixture(t, db, f)

	// Pre-change snapshot: cash 20,000,000, liability 10,000,000,
	// safeToSpend 10,000,000. The approve itself changes no number
	// (G-01). Post-change snapshot (after the new verified order
	// lands): cash 25,000,000, liability 15,000,000, safeToSpend
	// 10,000,000. Every concurrent read must equal one of the two.
	type snapshot struct{ cash, liability, safe int64 }
	pre := snapshot{20000000, 10000000, 10000000}
	post := snapshot{25000000, 15000000, 10000000}

	type readResult struct {
		code int
		body string
	}
	var mu sync.Mutex
	var results []readResult
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/finance/treasury/cash-position?days=30", nil)
			rec := httptest.NewRecorder()
			handleT01FinanceTreasury(rec, req, db, "cash-position")
			mu.Lock()
			results = append(results, readResult{code: rec.Code, body: rec.Body.String()})
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	time.Sleep(50 * time.Millisecond)
	if err := fin.ReviewSettlement(ctx, payoutID, "approved", "fin008-test", "finance-test"); err != nil {
		t.Fatalf("concurrent approve: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: storeID, Payable: 5000000, Profit: 5000000,
		ProfitReleased: true, Funding: "gateway", GatewayAmount: 5000000,
	})
	time.Sleep(1 * time.Second)
	close(stop)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(results) == 0 {
		t.Fatal("no concurrent reads happened")
	}
	for i, r := range results {
		if r.code != http.StatusOK {
			t.Fatalf("concurrent read %d: status=%d body=%s", i, r.code, r.body)
		}
		var c treasuryCash
		if err := json.Unmarshal([]byte(r.body), &c); err != nil {
			t.Fatalf("concurrent read %d not JSON: %v: %s", i, err, r.body)
		}
		treasuryAssertIdentity(t, &c)
		got := snapshot{*c.LiquidCash, c.Liability, *c.SafeToSpend}
		if got != pre && got != post {
			t.Fatalf("concurrent read %d is neither the pre- nor the post-change snapshot: cash=%d liability=%d safe=%d (pre %+v, post %+v)",
				i, got.cash, got.liability, got.safe, pre, post)
		}
	}
	// The final read, after both writers finished, must be the
	// post-change snapshot.
	final := treasuryReadOK(t, db)
	if got := (snapshot{*final.LiquidCash, final.Liability, *final.SafeToSpend}); got != post {
		t.Fatalf("final read %+v want post-change %+v", got, post)
	}
}

// ---------------------------------------------------------------- G-13
func TestTreasuryReportExposesUnresolvedProviderCash(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g13")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	// (a) verified gateway receipt 5,000,000.
	treasurySeedOrder(t, db, f, treasuryOrderSpec{Payable: 5000000, Funding: "gateway", GatewayAmount: 5000000})
	// (b) paid order with no receipt at all: 5,000,000 unverified.
	treasurySeedOrder(t, db, f, treasuryOrderSpec{Payable: 5000000})
	// (c) legacy simulator receipt 5,000,000.
	treasurySeedOrder(t, db, f, treasuryOrderSpec{Payable: 5000000, Funding: "simulated", GatewayAmount: 5000000})

	c := treasuryReadOK(t, db)
	if treasuryCashVal(t, c.LiquidCash, "liquidCash") != 5000000 {
		t.Fatalf("liquidCash=%d want 5000000 (only the verified gateway receipt)", *c.LiquidCash)
	}
	if c.UnverifiedPaidOrders != 5000000 {
		t.Fatalf("unverifiedPaidOrders=%d want 5000000", c.UnverifiedPaidOrders)
	}
	if c.LegacySimulator != 5000000 {
		t.Fatalf("legacySimulator=%d want 5000000", c.LegacySimulator)
	}
	if c.UnresolvedProviderCash != 10000000 {
		t.Fatalf("unresolvedProviderCash=%d want 10000000", c.UnresolvedProviderCash)
	}
	if c.AllTimeIn != 15000000 {
		t.Fatalf("allTimeIn=%d want 15000000 (paid-order payable sum is NOT bank cash)", c.AllTimeIn)
	}
	if c.CashStatus != "partial" {
		t.Fatalf("cashStatus=%q want partial", c.CashStatus)
	}
	if !c.ReconciliationRequired {
		t.Fatal("reconciliationRequired=false want true")
	}
	if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 5000000 {
		t.Fatalf("safeToSpend=%d want 5000000 (cash minus no liability)", *c.SafeToSpend)
	}
	if c.Liability != 0 {
		t.Fatalf("liability=%d want 0", c.Liability)
	}
	treasuryAssertIdentity(t, c)
}

// ---------------------------------------------------------------- F-03
func TestTreasuryNegativeLiquidCashIsUnknownNotNegative(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("f03")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := treasurySeedUser(t, tx, f, "seller")
	storeID := treasurySeedStore(t, tx, f, owner)
	if err := tx.Commit(); err != nil {
		t.Fatalf("f03 fixture commit: %v", err)
	}
	// Verified receipt 5,000,000 on order A (the only verified cash).
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		Payable: 5000000, Profit: 0, ProfitReleased: true,
		Funding: "gateway", GatewayAmount: 5000000,
	})
	// A store earning 10,000,000 on an UNVERIFIED paid order: the
	// payout gate reads canonical available (which derives from paid
	// orders regardless of receipts), so a 10,000,000 payout can be
	// confirmed against cash the bank never verified.
	treasurySeedOrder(t, db, f, treasuryOrderSpec{
		StoreID: storeID, Payable: 10000000, Profit: 10000000,
		ProfitReleased: true,
	})
	fin := repository.NewFinanceRepository(db)
	payout, err := fin.RequestOwnedStorePayout(ctx, owner, storeID, treasuryIBAN(), "bank", 10000000)
	if err != nil {
		t.Fatalf("f03 payout request: %v", err)
	}
	if err := fin.ReviewSettlement(ctx, payout.ID, "approved", "fin008-test", "finance-test"); err != nil {
		t.Fatalf("f03 approve payout: %v", err)
	}
	if err := fin.ConfirmManualTransfer(ctx, payout.ID, "fin008-f03-ref", "finance-test"); err != nil {
		t.Fatalf("f03 confirm transfer: %v", err)
	}

	// Computed liquidCash would be 5,000,000 verified in minus
	// 10,000,000 confirmed out = -5,000,000. A negative verified
	// cash figure is impossible on a complete evidence set — its
	// negativity proves the outflows were funded by unverified cash
	// — so the report must render it UNKNOWN (null), never as a
	// negative verified number.
	rec := treasuryCall(t, db, "cash-position")
	if rec.Code != http.StatusOK {
		t.Fatalf("cash-position status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"liquidCash":null`) || !strings.Contains(body, `"safeToSpend":null`) {
		t.Fatalf("negative computed cash must be JSON null, never a negative number: %s", body)
	}
	var c treasuryCash
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatalf("cash-position response is not JSON: %v", err)
	}
	if c.LiquidCash != nil || c.SafeToSpend != nil {
		t.Fatalf("liquidCash=%v safeToSpend=%v want both null", c.LiquidCash, c.SafeToSpend)
	}
	if c.CashStatus != "unknown" {
		t.Fatalf("cashStatus=%q want unknown", c.CashStatus)
	}
	if !c.ReconciliationRequired {
		t.Fatal("reconciliationRequired=false want true")
	}
	if c.UnresolvedProviderCash != 10000000 {
		t.Fatalf("unresolvedProviderCash=%d want 10000000", c.UnresolvedProviderCash)
	}
	if c.AllTimeOut != 10000000 {
		t.Fatalf("allTimeOut=%d want 10000000", c.AllTimeOut)
	}
	if c.Liability != 0 {
		t.Fatalf("liability=%d want 0 (the payout is confirmed money-out, not a hold)", c.Liability)
	}
}

// ---------------------------------------------------------------- G-14
func TestTreasuryProductionHttpUnknownCash(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g14")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	// One paid order with no receipt and no other evidence at all.
	treasurySeedOrder(t, db, f, treasuryOrderSpec{Payable: 50000000})

	rec := treasuryCall(t, db, "cash-position")
	if rec.Code != http.StatusOK {
		t.Fatalf("cash-position status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"liquidCash":null`) || !strings.Contains(body, `"safeToSpend":null`) {
		t.Fatalf("unknown cash must be JSON null, not 0: %s", body)
	}
	var c treasuryCash
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatalf("cash-position response is not JSON: %v", err)
	}
	if c.LiquidCash != nil || c.SafeToSpend != nil {
		t.Fatalf("liquidCash=%v safeToSpend=%v want both null", c.LiquidCash, c.SafeToSpend)
	}
	if c.CashStatus != "unknown" {
		t.Fatalf("cashStatus=%q want unknown", c.CashStatus)
	}
	if !c.ReconciliationRequired {
		t.Fatal("reconciliationRequired=false want true")
	}
	if c.UnresolvedProviderCash != 50000000 {
		t.Fatalf("unresolvedProviderCash=%d want 50000000", c.UnresolvedProviderCash)
	}
	if c.AllTimeIn != 50000000 {
		t.Fatalf("allTimeIn=%d want 50000000", c.AllTimeIn)
	}
}

// ---------------------------------------------------------------- G-15
func TestTreasuryNoGuessedOpeningBalance(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g15")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })

	// An opening balance without evidence is rejected by the table
	// CHECK — there is no way to write one, guessed or otherwise.
	if _, err := db.Exec(`INSERT INTO treasury_opening_cash(id,amount_toman,currency,evidence_kind,evidence_reference) VALUES('g15-no-evidence',7000000,'TOMAN','bank_statement','')`); err == nil {
		t.Fatal("opening cash with an empty evidence reference was accepted")
	}
	if _, err := db.Exec(`INSERT INTO treasury_opening_cash(id,amount_toman,currency,evidence_kind,evidence_reference) VALUES('g15-null-evidence',7000000,'TOMAN','bank_statement',NULL)`); err == nil {
		t.Fatal("opening cash with a NULL evidence reference was accepted")
	}
	if _, err := db.Exec(`INSERT INTO treasury_opening_cash(id,amount_toman,currency,evidence_kind,evidence_reference) VALUES('g15-no-kind',7000000,'TOMAN',NULL,'stmt-x')`); err == nil {
		t.Fatal("opening cash with a NULL evidence kind was accepted")
	}

	// Evidenced opening cash is real cash.
	treasurySeedOpeningCash(t, db, f, 7000000)
	c := treasuryReadOK(t, db)
	if treasuryCashVal(t, c.LiquidCash, "liquidCash") != 7000000 {
		t.Fatalf("liquidCash=%d want 7000000", *c.LiquidCash)
	}
	if c.CashStatus != "verified" {
		t.Fatalf("cashStatus=%q want verified", c.CashStatus)
	}
	if c.ReconciliationRequired {
		t.Fatal("reconciliationRequired=true want false (no unresolved cash, no other rows)")
	}
	if treasuryCashVal(t, c.SafeToSpend, "safeToSpend") != 7000000 {
		t.Fatalf("safeToSpend=%d want 7000000", *c.SafeToSpend)
	}
	treasuryAssertIdentity(t, c)
}

// ---------------------------------------------------------------- F-12
// TestTreasuryOpeningCashNoWriterInNonTestGo pins the STATIC half of
// G-15 as an executed test: no non-test Go file may write
// treasury_opening_cash — the table is read (SELECT) only, so no
// config key, settings column, env var, or import endpoint in the
// tree can set opening cash without evidence. A future writer would
// silently reintroduce guessed cash; this test fails first.
func TestTreasuryOpeningCashNoWriterInNonTestGo(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate this test file")
	}
	root := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(root, "backend", "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("could not locate the repo root (backend/go.mod) from the test file")
		}
		root = parent
	}
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, "treasury_opening_cash") {
				continue
			}
			upper := strings.ToUpper(line)
			if strings.Contains(upper, "INSERT") || strings.Contains(upper, "UPDATE") || strings.Contains(upper, "DELETE") || !strings.Contains(upper, "SELECT") {
				offenders = append(offenders, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo tree: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("non-test Go code references treasury_opening_cash outside a SELECT (a writer would silently reintroduce guessed cash):\n%s", strings.Join(offenders, "\n"))
	}
}

// ---------------------------------------------------------------- G-16
func TestTreasuryCheckedArithmeticRejectsOverflow(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g16")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	supA := treasurySeedSupplier(t, tx, f)
	supB := treasurySeedSupplier(t, tx, f)
	if err := tx.Commit(); err != nil {
		t.Fatalf("g16 fixture commit: %v", err)
	}
	// Two suppliers at 6,000,000,000,000,000 each: the total
	// 12,000,000,000,000,000 exceeds MaxTransportMoney
	// (9,007,199,254,740,991), so the report must fail, never clamp
	// or wrap.
	for _, sup := range []string{supA, supB} {
		if _, err := db.Exec(`INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator)
			VALUES($1,NOW(),'supplier_payable:'||$2,'supplier','credit',6000000000000000,'سند تعدیل fin008-g16 overflow fixture','test')`,
			uuid.NewString(), sup); err != nil {
			t.Fatalf("seed overflow ledger entry: %v", err)
		}
	}
	rec := treasuryCall(t, db, "cash-position")
	if rec.Code == http.StatusOK {
		t.Fatalf("overflow report returned 200: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"safeToSpend"`) || strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("overflow report leaked figures: %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------- G-17
type treasuryCountingQuerier struct {
	tx      *sql.Tx
	queries []string
}

func (c *treasuryCountingQuerier) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	c.queries = append(c.queries, query)
	return c.tx.QueryRowContext(ctx, query, args...)
}

func (c *treasuryCountingQuerier) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	c.queries = append(c.queries, query)
	return c.tx.QueryContext(ctx, query, args...)
}

// treasurySeedG17Stores seeds 50 stores: 40 with a +1,000,000 credit
// ledger entry, 3 with a -1,000,000 debit entry, 7 with no entries.
func treasurySeedG17Stores(t *testing.T, db *sql.DB, f *treasuryFixture) []string {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var ids []string
	for i := 0; i < 50; i++ {
		owner := treasurySeedUser(t, tx, f, "seller")
		ids = append(ids, treasurySeedStore(t, tx, f, owner))
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("g17 fixture commit: %v", err)
	}
	for i, id := range ids {
		var typ string
		switch {
		case i < 40:
			typ = "credit"
		case i < 43:
			typ = "debit"
		default:
			continue
		}
		if _, err := db.Exec(`INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator)
			VALUES($1,NOW(),'store_payable:'||$2,'seller',$3,1000000,'سند تعدیل fin008-g17 store fixture','test')`,
			uuid.NewString(), id, typ); err != nil {
			t.Fatalf("seed g17 ledger entry: %v", err)
		}
	}
	return ids
}

func TestTreasuryStoreAggregationIsSetBased(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g17")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	treasurySeedG17Stores(t, db, f)

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
	storeQueries := 0
	for _, q := range counting.queries {
		if strings.Contains(q, "FROM stores s") {
			storeQueries++
		}
	}
	if storeQueries != 1 {
		t.Fatalf("store-balance queries=%d want exactly 1 (set-based aggregation, no per-store loop)", storeQueries)
	}
	if pos.OwedToStores != 40000000 {
		t.Fatalf("owedToStores=%d want 40000000", pos.OwedToStores)
	}
	if pos.RecoveryReceivableStores != 3000000 {
		t.Fatalf("recoveryReceivableStores=%d want 3000000", pos.RecoveryReceivableStores)
	}
}

func TestTreasuryStoreAggregationMatchesCanonicalBalances(t *testing.T) {
	db := treasuryTestDB(t)
	f := treasuryNewFixture("g17b")
	t.Cleanup(func() { treasuryCleanup(t, db, f) })
	ids := treasurySeedG17Stores(t, db, f)

	// The independent per-store computation: the canonical balance
	// (FIN-001) for each store, summed the same way (positives to
	// the liability, negatives to the recovery receivable).
	ctx := context.Background()
	fin := repository.NewFinanceRepository(db)
	var wantPositive, wantRecovery int64
	for _, id := range ids {
		bal, err := fin.GetStoreBalance(ctx, id)
		if err != nil {
			t.Fatalf("GetStoreBalance %s: %v", id, err)
		}
		if bal.AvailableAmount >= 0 {
			wantPositive += bal.AvailableAmount
		} else {
			wantRecovery += -bal.AvailableAmount
		}
	}
	if wantPositive != 40000000 || wantRecovery != 3000000 {
		t.Fatalf("canonical per-store sums: positive=%d recovery=%d want 40000000/3000000", wantPositive, wantRecovery)
	}
	c := treasuryReadOK(t, db)
	if c.OwedToStores != wantPositive {
		t.Fatalf("owedToStores=%d want canonical %d", c.OwedToStores, wantPositive)
	}
	if c.RecoveryReceivableStores != wantRecovery {
		t.Fatalf("recoveryReceivableStores=%d want canonical %d", c.RecoveryReceivableStores, wantRecovery)
	}
	if c.Liability != 40000000 {
		t.Fatalf("liability=%d want 40000000", c.Liability)
	}
	treasuryAssertIdentity(t, c)
}
