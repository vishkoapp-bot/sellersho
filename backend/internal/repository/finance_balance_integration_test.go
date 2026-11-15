package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// openGapMoneyDB enters only through testdb.Open's disposable marker guard.
func openFinanceBalanceDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openGapMoneyDB(t)
	for _, q := range []string{
		`ALTER TABLE stores ADD COLUMN owner_user_id TEXT, ADD COLUMN active_plan_id TEXT, ADD COLUMN plan_expires_at TIMESTAMPTZ, ADD COLUMN updated_at TIMESTAMPTZ`,
		`ALTER TABLE seller_service_purchases ALTER COLUMN id SET DEFAULT md5(random()::text), ADD COLUMN service_type TEXT`,
		`ALTER TABLE platform_settings ADD COLUMN early_withdrawal_fee_pct NUMERIC; UPDATE platform_settings SET early_withdrawal_fee_pct=10`,
		`ALTER TABLE seller_early_withdrawals ADD COLUMN fee_pct NUMERIC, ADD COLUMN net_amount BIGINT, ADD COLUMN shaba TEXT, ADD COLUMN bank_name TEXT, ADD COLUMN rejection_reason TEXT, ADD COLUMN payout_request_id TEXT, ADD COLUMN created_at TIMESTAMPTZ, ADD COLUMN processed_at TIMESTAMPTZ`,
		`DROP TABLE IF EXISTS users CASCADE; CREATE TABLE users (id UUID PRIMARY KEY, mobile TEXT,full_name TEXT,role TEXT,is_active BOOLEAN DEFAULT false)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func waitFinanceAccountGuard(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE $1)`, "%SELECT id FROM "+table+" WHERE id=$1 FOR UPDATE%").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("financial writer did not wait on the actual account row guard")
}

func TestFinanceBalanceChangedPrincipalPlanRollsBack(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "changed-plan", 10000000)
	if _, err := db.Exec(`INSERT INTO suppliers(id) VALUES('new-principal'); UPDATE order_items SET wholesale_cost=1000000,final_unit_price=11000000 WHERE order_id='order-changed-plan'`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var set TransactionLockSet
	if err := LockAccountsTx(ctx, tx, &set, FinancialAccount{Type: "seller", ID: "changed-plan"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, "test"); done <- err }()
	waitFinanceAccountGuard(t, db, "stores")
	if _, err := db.Exec(`UPDATE products SET supplier_id='new-principal' WHERE id='product-changed-plan'`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrFinanceAccountChanged) {
		t.Fatal("refund admitted a newly discovered unguarded financial principal")
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM customer_return_financial_events WHERE return_id=$1`, ret).Scan(&events); err != nil || events != 0 {
		t.Fatalf("changed-plan effects=%d err=%v", events, err)
	}
}

func TestFinanceBalanceTransactionOwnEffects(t *testing.T) {
	db := openFinanceBalanceDB(t)
	seedFinanceBalance(t, db, "own-effects", 10000000)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var set TransactionLockSet
	account := FinancialAccount{Type: "seller", ID: "own-effects"}
	if err := LockAccountsTx(ctx, tx, &set, account); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO payout_requests(id,store_id,amount,status) VALUES('uncommitted-payout','own-effects',2000000,'pending'); INSERT INTO ledger_entries(id,target_account,target_type,type,amount,reason,operator) VALUES('uncommitted-adjustment','store_payable:own-effects','seller','debit',3000000,'سند تعدیل own effects','test')`); err != nil {
		t.Fatal(err)
	}
	available, err := BalanceTx(ctx, tx, account)
	if err != nil || available != 5000000 {
		t.Fatalf("transaction available=%d err=%v", available, err)
	}
	outside, err := NewFinanceRepository(db).GetStoreBalance(ctx, "own-effects")
	if err != nil || outside.AvailableAmount != 10000000 {
		t.Fatalf("uncommitted effects escaped: %+v err=%v", outside, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	outside, err = NewFinanceRepository(db).GetStoreBalance(ctx, "own-effects")
	if err != nil || outside.AvailableAmount != 10000000 {
		t.Fatalf("rollback effects: %+v err=%v", outside, err)
	}
}

func TestFinanceBalanceRefundUsesImmutableSupplierOrigin(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "immutable-party", 10000000)
	if _, err := db.Exec(`INSERT INTO suppliers(id) VALUES('catalog-new-party'); UPDATE order_items SET wholesale_cost=1000000,final_unit_price=11000000 WHERE order_id='order-immutable-party'; UPDATE products SET supplier_id='catalog-new-party' WHERE id='product-immutable-party'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewOrderRepository(db).RefundCustomerReturn(context.Background(), ret, "test"); err != nil {
		t.Fatal(err)
	}
	var supplier string
	if err := db.QueryRow(`SELECT supplier_id FROM customer_return_financial_events WHERE return_id=$1 AND component='product_cost'`, ret).Scan(&supplier); err != nil || supplier != "supplier-immutable-party" {
		t.Fatalf("refund supplier=%q err=%v", supplier, err)
	}
}

func TestFinanceBalanceAmbiguousLegacySupplierFailsClosed(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "unknown-party", 10000000)
	if _, err := db.Exec(`DELETE FROM order_item_origins WHERE order_id='order-unknown-party'; UPDATE order_items SET wholesale_cost=1000000,final_unit_price=11000000 WHERE order_id='order-unknown-party'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewOrderRepository(db).RefundCustomerReturn(context.Background(), ret, "test"); err == nil {
		t.Fatal("legacy refund guessed today's catalog supplier without immutable provenance")
	}
	var effects int
	if err := db.QueryRow(`SELECT COUNT(*) FROM customer_return_financial_events WHERE return_id=$1`, ret).Scan(&effects); err != nil || effects != 0 {
		t.Fatalf("ambiguous effects=%d err=%v", effects, err)
	}
}

func TestFinanceBalanceShippingChargeUsesImmutableSupplierOrigin(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "immutable-shipping", 10000000)
	if _, err := db.Exec(`INSERT INTO suppliers(id) VALUES('shipping-new-party'); UPDATE products SET supplier_id='shipping-new-party' WHERE id='product-immutable-shipping'; UPDATE customer_returns SET status='approved' WHERE order_id='order-immutable-shipping'`); err != nil {
		t.Fatal(err)
	}
	var item string
	if err := db.QueryRow(`SELECT id::text FROM return_items WHERE return_id=$1`, ret).Scan(&item); err != nil {
		t.Fatal(err)
	}
	if _, err := NewOrderRepository(db).ReceiveCustomerReturn(context.Background(), ret, []CustomerReturnItemQC{{ReturnItemID: item, QCStatus: "defective"}}, "test"); err != nil {
		t.Fatal(err)
	}
	var supplier string
	if err := db.QueryRow(`SELECT supplier_id FROM customer_return_shipping_charges WHERE return_id=$1 AND bearer='supplier'`, ret).Scan(&supplier); err != nil || supplier != "supplier-immutable-shipping" {
		t.Fatalf("shipping charge supplier=%q err=%v", supplier, err)
	}
}

func TestFinanceBalancePayoutStateAndCrash(t *testing.T) {
	db := openFinanceBalanceDB(t)
	seedFinanceBalance(t, db, "states", 10000000)
	repo := NewFinanceRepository(db)
	ctx := context.Background()
	p, err := repo.RequestOwnedStorePayout(ctx, "00000000-0000-4000-8000-000000000001", "states", "bank", "bank", 2000000)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewSettlement(ctx, p.ID, "completed", "", "test"); err == nil {
		t.Fatal("pending payout accepted invalid completed transition")
	}
	if err := repo.ReviewSettlement(ctx, p.ID, "approved", "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewSettlement(ctx, p.ID, "approved", "", "test"); err == nil {
		t.Fatal("review replay accepted")
	}
	if err := repo.ConfirmManualTransfer(ctx, p.ID, "ref", "test"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfirmManualTransfer(ctx, p.ID, "ref", "test"); err == nil {
		t.Fatal("transfer replay accepted")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1`, p.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("transfer legs=%d err=%v", n, err)
	}
	bal, err := repo.GetStoreBalance(ctx, "states")
	if err != nil || bal.AvailableAmount != 8000000 {
		t.Fatalf("balance=%+v err=%v", bal, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var locks TransactionLockSet
	if err := LockAccountsTx(ctx, tx, &locks, FinancialAccount{Type: "seller", ID: "states"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO payout_requests(id,store_id,amount,status) VALUES('crash','states',8000000,'pending')`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RequestOwnedStorePayout(ctx, "00000000-0000-4000-8000-000000000001", "states", "bank", "bank", 8000000); err != nil {
		t.Fatalf("rolled back reservation persisted: %v", err)
	}
}

func TestFinanceBalanceRefundSerializesAndIndependentAccount(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "locked", 10000000)
	seedFinanceBalance(t, db, "other", 10000000)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var locks TransactionLockSet
	if err := LockAccountsTx(ctx, tx, &locks, FinancialAccount{Type: "seller", ID: "locked"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, "test"); done <- err }()
	waitFinanceAccountGuard(t, db, "stores")
	select {
	case err := <-done:
		t.Fatalf("refund bypassed seller guard: %v", err)
	default:
	}
	if _, err := NewFinanceRepository(db).RequestStorePayout(ctx, "other", "bank", "bank", 2000000); err != nil {
		t.Fatalf("independent blocked: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := NewFinanceRepository(db).RequestStorePayout(ctx, "locked", "bank", "bank", 1); err == nil {
		t.Fatal("refund did not fence spend")
	}
}

func seedFinanceBalance(t *testing.T, db *sql.DB, store string, profit int64) (string, string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO users(id) VALUES('00000000-0000-4000-8000-000000000001'::uuid) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	customer := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id) VALUES($1::uuid) ON CONFLICT DO NOTHING`, customer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO stores(id,owner_type,owner_user_id) VALUES($1,'dropshipper','00000000-0000-4000-8000-000000000001')`, store); err != nil {
		t.Fatal(err)
	}
	gmOrder(t, db, "order-"+store, customer, store, false, true, profit)
	gmProduct(t, db, "product-"+store, "supplier-"+store, 0)
	item := gmItem(t, db, "order-"+store, "product-"+store, 1, 0, profit, profit, "")
	ret := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO customer_returns(id,order_id,customer_id,status,prepaid) VALUES($1::uuid,$2,$3::uuid,'received',true)`, ret, "order-"+store, customer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO return_items(id,return_id,order_item_id,qty,qc_status,received_at) VALUES($1::uuid,$2::uuid,$3,1,'ok',NOW())`, uuid.NewString(), ret, item); err != nil {
		t.Fatal(err)
	}
	return ret, customer
}

func TestAuditFinanceRefundCannotBeWithdrawn(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "refund", 10000000)
	ctx := context.Background()
	if amount, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, "test"); err != nil || amount != 10000000 {
		t.Fatalf("refund amount=%d err=%v", amount, err)
	}
	bal, err := NewFinanceRepository(db).GetStoreBalance(ctx, "refund")
	if err != nil || bal.AvailableAmount != 0 {
		t.Fatalf("balance=%+v err=%v", bal, err)
	}
	if _, err := NewFinanceRepository(db).RequestOwnedStorePayout(ctx, "00000000-0000-4000-8000-000000000001", "refund", "bank", "bank", 2000000); err == nil {
		t.Fatal("refunded profit was admitted for withdrawal")
	}
}

func TestFinanceBalanceSignedDebtAndAdjustments(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "debt", 10000000)
	repo := NewFinanceRepository(db)
	ctx := context.Background()
	if _, err := repo.RequestOwnedStorePayout(ctx, "00000000-0000-4000-8000-000000000001", "debt", "bank", "bank", 2000000); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, "test"); err != nil {
		t.Fatal(err)
	}
	bal, err := repo.GetStoreBalance(ctx, "debt")
	if err != nil || bal.AvailableAmount != -2000000 {
		t.Fatalf("signed debt=%+v err=%v", bal, err)
	}
	if _, err := repo.RequestStorePayout(ctx, "debt", "bank", "bank", 1); err == nil {
		t.Fatal("debt admitted new spend")
	}
	for _, direction := range []string{"credit", "debit"} {
		if _, err := db.Exec(`INSERT INTO ledger_entries(id,target_account,target_type,type,amount,reason,operator) VALUES($1,'store_payable:debt','seller',$2,3000000,'سند تعدیل test','test')`, uuid.NewString(), direction); err != nil {
			t.Fatal(err)
		}
		want := int64(1000000)
		if direction == "debit" {
			want = -2000000
		}
		bal, err := repo.GetStoreBalance(ctx, "debt")
		if err != nil || bal.AvailableAmount != want {
			t.Fatalf("%s balance=%+v err=%v", direction, bal, err)
		}
		var available int64
		if err := db.QueryRow(sellerAvailableSQL, "debt").Scan(&available); err != nil || available != want {
			t.Fatalf("%s authorization=%d want=%d err=%v", direction, available, want, err)
		}
	}
}

func TestFinanceBalanceConcurrentDebits(t *testing.T) {
	db := openFinanceBalanceDB(t)
	seedFinanceBalance(t, db, "race", 10000000)
	seedFinanceBalance(t, db, "independent", 10000000)
	repo := NewFinanceRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 18; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			switch i % 3 {
			case 0:
				_, _ = repo.RequestStorePayout(ctx, "race", "bank", "bank", 2000000)
			case 1:
				_ = repo.PurchaseSellerService(ctx, "00000000-0000-4000-8000-000000000001", "race", "standard")
			case 2:
				_, _ = repo.RequestEarlyWithdrawal(ctx, "00000000-0000-4000-8000-000000000001", "race", 2000000, "bank", "bank")
			}
		}(i)
	}
	close(start)
	wg.Wait()
	bal, err := repo.GetStoreBalance(ctx, "race")
	if err != nil || bal.AvailableAmount < 0 {
		t.Fatalf("overspend balance=%+v err=%v", bal, err)
	}
	var debits int64
	if err := db.QueryRow(`SELECT COALESCE((SELECT SUM(amount) FROM payout_requests WHERE store_id='race'),0)+COALESCE((SELECT SUM(amount) FROM seller_service_purchases WHERE store_id='race'),0)+COALESCE((SELECT SUM(gross_amount) FROM seller_early_withdrawals WHERE store_id='race'),0)`).Scan(&debits); err != nil {
		t.Fatal(err)
	}
	if debits <= 0 || debits > 10000000 || bal.AvailableAmount != 10000000-debits {
		t.Fatalf("debits=%d balance=%+v", debits, bal)
	}
}

func TestFinanceBalanceStorageWriterUsesAccountGuard(t *testing.T) {
	db, f, warehouse := fin006Fixture(t)
	fin006KnownReceipt(t, db, f, warehouse, 1, tehranDay(time.Now()).AddDate(0, 0, -35))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var set TransactionLockSet
	if err := LockAccountsTx(ctx, tx, &set, FinancialAccount{Type: "supplier", ID: f.supplier}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := NewSupplierRepository(db).RunStorageFeeAssessment(ctx); done <- err }()
	waitFinanceAccountGuard(t, db, "suppliers")
	select {
	case err := <-done:
		t.Fatalf("storage debit bypassed account guard: %v", err)
	default:
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if storageChargeCount(t, db) != 5 {
		t.Fatalf("charges=%d want5", storageChargeCount(t, db))
	}
}

func TestFinanceBalanceAggregateOverflowFailsClosed(t *testing.T) {
	db := openFinanceBalanceDB(t)
	seedFinanceBalance(t, db, "overflow", 9223372036854775807)
	if _, err := db.Exec(`INSERT INTO referral_bonuses(id,referrer_store_id,amount,status) VALUES('overflow-bonus','overflow',1,'locked')`); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFinanceRepository(db).GetStoreBalance(context.Background(), "overflow"); err == nil {
		t.Fatal("balance aggregate overflow was accepted")
	}
}

func TestFinanceBalanceConcurrentRefundAndIndependentDebits(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "refund-race", 10000000)
	seedFinanceBalance(t, db, "refund-independent", 10000000)
	repo := NewFinanceRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if _, err := repo.RequestStorePayout(ctx, "refund-race", "bank", "bank", 2000000); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	refundDone := make(chan error, 1)
	independentDone := make(chan error, 1)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			switch i % 3 {
			case 0:
				_, _ = repo.RequestStorePayout(ctx, "refund-race", "bank", "bank", 2000000)
			case 1:
				_ = repo.PurchaseSellerService(ctx, "00000000-0000-4000-8000-000000000001", "refund-race", "standard")
			case 2:
				_, _ = repo.RequestEarlyWithdrawal(ctx, "00000000-0000-4000-8000-000000000001", "refund-race", 2000000, "bank", "bank")
			}
		}(i)
	}
	go func() {
		<-start
		_, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, "test")
		refundDone <- err
	}()
	go func() {
		<-start
		_, err := repo.RequestStorePayout(ctx, "refund-independent", "bank", "bank", 10000000)
		independentDone <- err
	}()
	close(start)
	wg.Wait()
	if err := <-refundDone; err != nil {
		t.Fatal(err)
	}
	if err := <-independentDone; err != nil {
		t.Fatal(err)
	}
	var spent int64
	if err := db.QueryRow(`SELECT COALESCE((SELECT SUM(amount) FROM payout_requests WHERE store_id='refund-race'),0)+COALESCE((SELECT SUM(amount) FROM seller_service_purchases WHERE store_id='refund-race'),0)+COALESCE((SELECT SUM(gross_amount) FROM seller_early_withdrawals WHERE store_id='refund-race'),0)`).Scan(&spent); err != nil {
		t.Fatal(err)
	}
	b, err := repo.GetStoreBalance(ctx, "refund-race")
	if err != nil || spent < 2000000 || spent > 10000000 || b.AvailableAmount != -spent {
		t.Fatalf("spent=%d balance=%+v err=%v", spent, b, err)
	}
	if _, err := repo.RequestStorePayout(ctx, "refund-race", "bank", "bank", 1); err == nil {
		t.Fatal("tracked debt allowed spend")
	}
	if _, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, "test"); err == nil {
		t.Fatal("refund replay accepted")
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM customer_return_financial_events WHERE return_id=$1`, ret).Scan(&events); err != nil || events != 1 {
		t.Fatalf("refund effects=%d err=%v", events, err)
	}
}

func TestFinanceBalanceSupplierSignedParity(t *testing.T) {
	db := openFinanceBalanceDB(t)
	seedFinanceBalance(t, db, "supplier-parity", 10000000)
	for _, direction := range []string{"credit", "debit"} {
		amount := int64(3000000)
		if direction == "debit" {
			amount = 5000000
		}
		if _, err := db.Exec(`INSERT INTO ledger_entries(id,target_account,target_type,type,amount,reason,operator) VALUES($1,'supplier_payable:supplier-supplier-parity','supplier',$2,$3,'سند تعدیل parity','test')`, uuid.NewString(), direction, amount); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	read, err := NewSupplierRepository(db).GetSettlementBalance(ctx, "supplier-supplier-parity")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var set TransactionLockSet
	if err := LockAccountsTx(ctx, tx, &set, FinancialAccount{Type: "supplier", ID: "supplier-supplier-parity"}); err != nil {
		t.Fatal(err)
	}
	available, err := BalanceTx(ctx, tx, FinancialAccount{Type: "supplier", ID: "supplier-supplier-parity"})
	if err != nil || available != -2000000 || read.Available != available {
		t.Fatalf("read=%+v tx=%d err=%v", read, available, err)
	}
	total, err := NewSupplierRepository(db).SumAllSuppliersAvailable(ctx)
	if err != nil || total != available {
		t.Fatalf("aggregate=%d err=%v", total, err)
	}
}

func TestFinanceBalanceParcelBusinessEventOnce(t *testing.T) {
	db := openFinanceBalanceDB(t)
	for _, q := range []string{
		`DROP TABLE IF EXISTS supplier_return_financial_events,supplier_return_parcels,supplier_return_requests CASCADE`,
		`CREATE TABLE supplier_return_requests(id TEXT PRIMARY KEY,supplier_id TEXT)`,
		`CREATE TABLE supplier_return_parcels(id TEXT PRIMARY KEY,return_request_id TEXT,final_charge BIGINT,payment_method TEXT,status TEXT,confirmed_by TEXT,updated_at TIMESTAMPTZ)`,
		`CREATE TABLE supplier_return_financial_events(id TEXT PRIMARY KEY,return_request_id TEXT,parcel_id TEXT,supplier_id TEXT,operation TEXT,amount BIGINT,payment_method TEXT,ledger_entry_id TEXT,idempotency_key TEXT UNIQUE,reason TEXT,actor_user_id TEXT,event_identity TEXT UNIQUE)`,
		`INSERT INTO suppliers(id) VALUES('parcel-supplier')`,
		`INSERT INTO supplier_return_requests(id,supplier_id) VALUES('parcel-return','parcel-supplier')`,
		`INSERT INTO supplier_return_parcels(id,return_request_id,final_charge,payment_method,status) VALUES('parcel-one','parcel-return',125000,'sender_pays','draft')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "test", "first-key"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "test", "different-key"); err != nil {
		t.Fatal(err)
	}
	b, err := repo.GetSettlementBalance(ctx, "parcel-supplier")
	if err != nil || b.Available != -125000 {
		t.Fatalf("one parcel charged balance=%+v err=%v", b, err)
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM supplier_return_financial_events`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("effects=%d err=%v", events, err)
	}
}

func TestFinanceBalanceSupplierOriginReadAuthorizationParity(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "origin-parity", 10000000)
	if _, err := db.Exec(`INSERT INTO suppliers(id) VALUES('origin-catalog-new'); UPDATE products SET supplier_id='origin-catalog-new' WHERE id='product-origin-parity'; UPDATE order_items SET wholesale_cost=1000000,final_unit_price=11000000 WHERE order_id='order-origin-parity'`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	supplierRepo := NewSupplierRepository(db)
	if _, err := supplierRepo.RequestSettlement(ctx, "origin-catalog-new", 1, "bank", "bank", "holder", "wrong-owner-attempt"); err == nil {
		t.Fatal("today's catalog owner gained spend authority from a different immutable supplier")
	}
	old, err := supplierRepo.GetSettlementBalance(ctx, "supplier-origin-parity")
	if err != nil || old.Earned != 1000000 || old.Available != 1000000 {
		t.Fatalf("original balance=%+v err=%v", old, err)
	}
	current, err := supplierRepo.GetSettlementBalance(ctx, "origin-catalog-new")
	if err != nil || current.Earned != 0 || current.Available != 0 {
		t.Fatalf("catalog balance=%+v err=%v", current, err)
	}
	if _, err := supplierRepo.RequestSettlement(ctx, "supplier-origin-parity", 500000, "bank", "bank", "holder", "original-owner-payout"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, "test"); err != nil {
		t.Fatal(err)
	}
	old, err = supplierRepo.GetSettlementBalance(ctx, "supplier-origin-parity")
	if err != nil || old.Available != -500000 {
		t.Fatalf("original debt=%+v err=%v", old, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var set TransactionLockSet
	account := FinancialAccount{Type: "supplier", ID: "supplier-origin-parity"}
	if err := LockAccountsTx(ctx, tx, &set, account); err != nil {
		t.Fatal(err)
	}
	available, err := BalanceTx(ctx, tx, account)
	if err != nil || available != old.Available {
		t.Fatalf("tx=%d panel=%d err=%v", available, old.Available, err)
	}
	total, err := supplierRepo.SumAllSuppliersAvailable(ctx)
	if err != nil || total != available {
		t.Fatalf("treasury=%d tx=%d err=%v", total, available, err)
	}
}

func TestFinanceBalanceUnprovenSupplierCannotAuthorizeOrDisappear(t *testing.T) {
	db := openFinanceBalanceDB(t)
	seedFinanceBalance(t, db, "unproven", 10000000)
	if _, err := db.Exec(`UPDATE order_items SET wholesale_cost=1000000 WHERE order_id='order-unproven';DELETE FROM order_item_origins WHERE order_id='order-unproven'`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	supplier := NewSupplierRepository(db)
	if _, err := supplier.GetSettlementBalance(ctx, "supplier-unproven"); !errors.Is(err, ErrFinancePrincipalUnresolved) {
		t.Fatalf("ambiguous panel err=%v", err)
	}
	if _, err := supplier.SumAllSuppliersAvailable(ctx); !errors.Is(err, ErrFinancePrincipalUnresolved) {
		t.Fatalf("ambiguous treasury err=%v", err)
	}
	if _, err := supplier.RequestSettlement(ctx, "supplier-unproven", 1, "bank", "bank", "holder", "unproven-attempt"); !errors.Is(err, ErrFinancePrincipalUnresolved) {
		t.Fatalf("ambiguous authorization err=%v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payout_requests WHERE supplier_id='supplier-unproven'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("ambiguous effects%d err%v", n, err)
	}
}

func TestFinanceBalanceSameSupplierOriginSplitConservesEarned(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "split-origin", 10000000)
	if _, err := db.Exec(`UPDATE order_items SET quantity=2,wholesale_cost=1000000,seller_margin=5000000,final_unit_price=6000000 WHERE order_id='order-split-origin';INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,warehouse_id,origin_label,quantity) SELECT 'second-same-party',order_id,id,1,'warehouse','supplier-split-origin','wh-2','Second warehouse',1 FROM order_items WHERE order_id='order-split-origin'`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sup := NewSupplierRepository(db)
	b, err := sup.GetSettlementBalance(ctx, "supplier-split-origin")
	if err != nil || b.Earned != 2000000 {
		t.Fatalf("split earned=%+v err=%v", b, err)
	}
	total, err := sup.SumAllSuppliersAvailable(ctx)
	if err != nil || total != 2000000 {
		t.Fatalf("split treasury%d err%v", total, err)
	}
	if _, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, "test"); err != nil {
		t.Fatal(err)
	}
	b, err = sup.GetSettlementBalance(ctx, "supplier-split-origin")
	if err != nil || b.Earned != 2000000 || b.ReturnClawbacks != 1000000 || b.Available != 1000000 {
		t.Fatalf("partial split refund=%+v err=%v", b, err)
	}
}

func TestFinanceBalanceInvalidOriginCoverageRequiresReconciliation(t *testing.T) {
	for name, mutation := range map[string]string{
		"missing":                          `DELETE FROM order_item_origins WHERE order_id='order-invalid-origin'`,
		"too many units":                   `UPDATE order_item_origins SET quantity=2 WHERE order_id='order-invalid-origin'`,
		"nonpositive units":                `UPDATE order_item_origins SET quantity=0 WHERE order_id='order-invalid-origin'`,
		"warehouse financial party absent": `UPDATE order_item_origins SET supplier_id=NULL WHERE order_id='order-invalid-origin'`,
		"wrong order":                      `UPDATE order_item_origins SET order_id='another-order' WHERE order_id='order-invalid-origin'`,
		"conflicting conserved split":      `INSERT INTO suppliers(id) VALUES('split-other-party'); UPDATE order_items SET quantity=2 WHERE order_id='order-invalid-origin'; INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,warehouse_id,origin_label,quantity) SELECT 'conflicting-second-party',order_id,id,1,'warehouse','split-other-party','wh-2','Other',1 FROM order_items WHERE order_id='order-invalid-origin'`,
	} {
		t.Run(name, func(t *testing.T) {
			db := openFinanceBalanceDB(t)
			ret, _ := seedFinanceBalance(t, db, "invalid-origin", 10000000)
			if _, err := db.Exec(`UPDATE order_items SET wholesale_cost=1000000,final_unit_price=11000000 WHERE order_id='order-invalid-origin'`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if _, err := NewSupplierRepository(db).GetSettlementBalance(ctx, "supplier-invalid-origin"); !errors.Is(err, ErrFinancePrincipalUnresolved) {
				t.Fatalf("unresolved read err%v", err)
			}
			if _, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, "test"); !errors.Is(err, ErrFinancePrincipalUnresolved) {
				t.Fatalf("unresolved refund err%v", err)
			}
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM customer_return_financial_events WHERE return_id=$1`, ret).Scan(&n); err != nil || n != 0 {
				t.Fatalf("unresolved effects%d err%v", n, err)
			}
		})
	}
}

func TestFinanceReviewBlankSupplierCannotReceive(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "blank-receive", 10000000)
	if _, err := db.Exec(`UPDATE orders SET inventory_captured=true WHERE store_id='blank-receive'; UPDATE order_items SET wholesale_cost=1000000 WHERE order_id='order-blank-receive'; UPDATE order_item_origins SET supplier_id=NULL WHERE order_id='order-blank-receive'; UPDATE products SET supplier_id=NULL WHERE id='product-blank-receive'; UPDATE customer_returns SET status='approved' WHERE id='` + ret + `'`); err != nil {
		t.Fatal(err)
	}
	var item string
	_ = db.QueryRow(`SELECT id::text FROM return_items WHERE return_id=$1`, ret).Scan(&item)
	if _, err := NewOrderRepository(db).ReceiveCustomerReturn(context.Background(), ret, []CustomerReturnItemQC{{ReturnItemID: item, QCStatus: "defective"}}, "test"); !errors.Is(err, ErrFinancePrincipalUnresolved) {
		t.Fatalf("blank receive err=%v", err)
	}
	var status string
	_ = db.QueryRow(`SELECT status FROM customer_returns WHERE id=$1`, ret).Scan(&status)
	if status != "approved" {
		t.Fatalf("committed receive %s", status)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM customer_return_shipping_charges WHERE return_id=$1`, ret).Scan(&n)
	if n != 0 {
		t.Fatal("committed shipping effects")
	}
	for _, table := range []string{"customer_return_financial_events", "ledger_entries", "inventory_movements", "inventory_balances"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("committed %s effects%d err%v", table, count, err)
		}
	}
	var stock int
	if err := db.QueryRow(`SELECT stock FROM products WHERE id='product-blank-receive'`).Scan(&stock); err != nil || stock != 0 {
		t.Fatalf("stock%d err%v", stock, err)
	}

}

func TestFinanceReviewStatementRefundAndAdjustment(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ret, _ := seedFinanceBalance(t, db, "statement-review", 10000000)
	for _, q := range []string{`ALTER TABLE seller_service_purchases ADD COLUMN created_at TIMESTAMPTZ DEFAULT NOW()`, `UPDATE orders SET profit_released_at=NOW() WHERE store_id='statement-review'`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := NewOrderRepository(db).RefundCustomerReturn(context.Background(), ret, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator) VALUES('statement-credit',NOW(),'store_payable:statement-review','seller','credit',3000000,'سند تعدیل review','test'),('statement-debit',NOW(),'store_payable:statement-review','seller','debit',1000000,'سند تعدیل review','test')`); err != nil {
		t.Fatal(err)
	}
	st, err := NewFinanceRepository(db).GetStoreStatement(context.Background(), "statement-review", time.Now().UTC().Format("2006-01"))
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalCredit != 13000000 || st.TotalDebit != 11000000 || st.NetBalance != 2000000 {
		t.Fatalf("statement=%+v", st)
	}
	if st.OpeningAvailable != 0 || st.ClosingAvailable != 2000000 || st.CurrentAvailable != 2000000 {
		t.Fatalf("unreconciled closing %+v", st)
	}
	if _, err := db.Exec(`INSERT INTO payout_requests(id,store_id,amount,status,created_at) VALUES('statement-reservation','statement-review',1000000,'pending',NOW()); UPDATE orders SET referrer_seller_id='statement-review',referral_commission=500000,referral_commission_released=true WHERE store_id='statement-review'; INSERT INTO seller_early_withdrawals(id,store_id,gross_amount,fee_amount,status,created_at,processed_at) VALUES('statement-early','statement-review',2000000,200000,'approved',NOW(),NOW()); INSERT INTO payout_requests(id,store_id,amount,status,created_at) VALUES('statement-early-net','statement-review',1800000,'approved',NOW())`); err != nil {
		t.Fatal(err)
	}
	st, err = NewFinanceRepository(db).GetStoreStatement(context.Background(), "statement-review", time.Now().UTC().Format("2006-01"))
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalCredit != 13500000 || st.TotalDebit != 11200000 || st.ClosingReserved != 2800000 || st.ClosingAvailable != -500000 || st.CurrentAvailable != st.ClosingAvailable || st.ClosingAvailable-st.OpeningAvailable != st.NetBalance-(st.ClosingReserved-st.OpeningReserved) {
		t.Fatalf("reservation/posted reconciliation %+v", st)
	}

}

func TestFinanceReviewFixturePreservesPublicCapture(t *testing.T) {
	db := openFinanceBalanceDB(t)
	ctx := context.Background()
	a, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for _, connection := range []*sql.Conn{a, b} {
		var schema, path string
		if err := connection.QueryRowContext(ctx, `SELECT current_schema(),current_setting('search_path')`).Scan(&schema, &path); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(schema, "finance_fixture_") || strings.Contains(path, "public") {
			t.Fatalf("not schema-only: %s %s", schema, path)
		}
		var primary, foreign int
		if err := connection.QueryRowContext(ctx, `SELECT COUNT(*) FILTER(WHERE contype='p'),COUNT(*) FILTER(WHERE contype='f') FROM pg_constraint WHERE conrelid IN ('public.order_inventory_captures'::regclass,'public.order_inventory_returns'::regclass)`).Scan(&primary, &foreign); err != nil || primary != 2 || foreign < 2 {
			t.Fatalf("public capture constraints lost: pk%d fk%d err%v", primary, foreign, err)
		}
	}
}

func TestFinanceReviewServiceRecoveryDoesNotRequoteRetiredOffer(t *testing.T) {
	db := openFinanceBalanceDB(t)
	seedFinanceBalance(t, db, "retired-offer", 10000000)
	ctx := context.Background()
	original := SellerServiceAcknowledgment{ID: "retired-offer", ActivePlanID: "retired-plan", Amount: 12300, DurationDays: 7, PlanExpiresAt: time.Now().UTC().AddDate(0, 0, 7)}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var set TransactionLockSet
	account := FinancialAccount{Type: "seller", ID: "retired-offer"}
	if err := LockAccountsTx(ctx, tx, &set, account); err != nil {
		t.Fatal(err)
	}
	c, err := newFinanceCommand(account, "purchase-service-v1", []string{"retired-recovery"}, []interface{}{"00000000-0000-4000-8000-000000000001", "retired-plan"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO seller_service_purchases(store_id,service_type,amount) VALUES('retired-offer','retired-plan',12300)`); err != nil {
		t.Fatal(err)
	}
	if err := c.recordTx(ctx, tx, original); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE stores SET active_plan_id='vip',plan_expires_at=NOW()+INTERVAL '900 days' WHERE id='retired-offer'`); err != nil {
		t.Fatal(err)
	}
	ack, err := NewFinanceRepository(db).PurchaseSellerServiceAcknowledgment(ctx, "00000000-0000-4000-8000-000000000001", "retired-offer", "retired-plan", "retired-recovery")
	if err != nil {
		t.Fatal(err)
	}
	if ack.Amount != original.Amount || ack.DurationDays != original.DurationDays || ack.ActivePlanID != original.ActivePlanID || !ack.PlanExpiresAt.Equal(original.PlanExpiresAt) {
		t.Fatalf("requoted %+v original%+v", ack, original)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM seller_service_purchases WHERE store_id='retired-offer'`).Scan(&n)
	if n != 1 {
		t.Fatalf("purchases%d", n)
	}
}

func TestFinanceReviewStatementPeriodReservationsAreNotPostedTwice(t *testing.T) {
	db := openFinanceBalanceDB(t)
	seedFinanceBalance(t, db, "statement-period", 2000000)
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	previous := start.AddDate(0, -1, 0)
	created := previous.Add(time.Hour)
	if _, err := db.Exec(`ALTER TABLE seller_service_purchases ADD COLUMN created_at TIMESTAMPTZ DEFAULT NOW()`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE orders SET created_at=$1,profit_released_at=$1 WHERE store_id='statement-period'`, created); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO payout_requests(id,store_id,amount,status,created_at,processed_at) VALUES('period-reserved','statement-period',500000,'approved',$1,$2),('period-early-net','statement-period',450000,'approved',$2,$2)`, created, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO seller_early_withdrawals(id,store_id,gross_amount,fee_amount,status,created_at,processed_at) VALUES('period-early','statement-period',500000,50000,'approved',$1,$2)`, created, now); err != nil {
		t.Fatal(err)
	}
	repo := NewFinanceRepository(db)
	old, err := repo.GetStoreStatement(context.Background(), "statement-period", previous.Format("2006-01"))
	if err != nil {
		t.Fatal(err)
	}
	if old.TotalCredit != 2000000 || old.TotalDebit != 0 || old.ClosingReserved != 1000000 || old.ClosingAvailable != 1000000 {
		t.Fatalf("old period %+v", old)
	}
	current, err := repo.GetStoreStatement(context.Background(), "statement-period", start.Format("2006-01"))
	if err != nil {
		t.Fatal(err)
	}
	if current.TotalCredit != 0 || current.TotalDebit != 50000 || current.OpeningAvailable != 1000000 || current.ClosingAvailable != 1000000 || current.OpeningReserved != 1000000 || current.ClosingReserved != 950000 {
		t.Fatalf("current period %+v", current)
	}
}
