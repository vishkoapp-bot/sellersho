package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/payment"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func fin003PendingOrder(t *testing.T) (*sql.DB, opsFixture, string, string, string) {
	t.Helper()
	db, f, warehouse, _, _ := inv06Placed(t, false, 1)
	order, token := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE order_items SET wholesale_cost=1000000,seller_margin=9000000,final_unit_price=10000000 WHERE order_id=$1`, order)
	opsExec(t, db, `UPDATE orders SET total_amount=10000000,payable_amount=10000000,gateway_amount=10000000 WHERE id=$1`, order)
	opsExec(t, db, `UPDATE payment_transactions SET amount=10000000 WHERE order_id=$1`, order)
	seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
	return db, f, warehouse, order, token
}

func fin003PaidOrder(t *testing.T) (*sql.DB, opsFixture, string, string) {
	t.Helper()
	db, f, warehouse, order, token := fin003PendingOrder(t)
	if _, err := NewOrderRepository(db).MarkPaymentPaid(context.Background(), token, "verified-"+uuid.NewString()[:12], "mask"); err != nil {
		t.Fatal(err)
	}
	return db, f, warehouse, order
}

func TestFIN003PaidCancellationPreservesTenMillionLiability(t *testing.T) {
	db, f, _, order := fin003PaidOrder(t)
	repo := NewOrderRepository(db)
	ctx := context.Background()
	if err := repo.CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var status, payment, transaction string
	if err := db.QueryRow(`SELECT o.status,o.payment_status,p.status FROM orders o JOIN payment_transactions p ON p.order_id=o.id WHERE o.id=$1`, order).Scan(&status, &payment, &transaction); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || payment != "paid" || transaction != "paid" {
		t.Fatalf("cancellation erased captured payment facts: order=%s payment=%s transaction=%s", status, payment, transaction)
	}
	var available bool
	if err := db.QueryRow(`SELECT to_regclass('order_refund_obligations') IS NOT NULL`).Scan(&available); err != nil || !available {
		t.Fatalf("durable refund obligation is missing: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := repo.CancelOrder(ctx, order, f.user, false); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var amount int64
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&count, &amount); err != nil || count != 1 || amount != 10000000 {
		t.Fatalf("paid10m liability/replay count=%d amount=%d err=%v", count, amount, err)
	}
	var held, stock int
	if err := db.QueryRow(`SELECT SUM(qty) FROM warehouse_storage_positions WHERE product_id=$1`, f.product).Scan(&held); err != nil || held != 1 {
		t.Fatalf("financial cancellation changed physical custody %d %v", held, err)
	}
	if err := db.QueryRow(`SELECT stock FROM products WHERE id=$1`, f.product).Scan(&stock); err != nil || stock != 1 {
		t.Fatalf("cancelled captured unit not restored exactly once: %d %v", stock, err)
	}
}

func TestFIN003PriorRecordedPartialRefundReservesPrincipal(t *testing.T) {
	db, f, _, order := fin003PaidOrder(t)
	opsExec(t, db, `INSERT INTO ledger_entries(id,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) VALUES($1,$2,'platform','credit',2000000,'استرداد وجه سفارش: prior recorded refund',$3,$2)`, "old-refund-"+uuid.NewString(), order, f.user)
	if err := NewOrderRepository(db).CancelOrder(context.Background(), order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var cancellation, legacy, total int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount_toman) FILTER(WHERE purpose='cancellation'),0),COALESCE(SUM(amount_toman) FILTER(WHERE purpose='legacy_reservation'),0),COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&cancellation, &legacy, &total); err != nil || cancellation != 8000000 || legacy != 2000000 || total != 10000000 {
		t.Fatalf("prior refund spent twice: new=%d prior=%d total=%d %v", cancellation, legacy, total, err)
	}
}

func TestFIN003UnpaidCancellationCreatesNoRefund(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	order, _ := opsOrder(t, db, f, false)
	if err := NewOrderRepository(db).CancelOrder(context.Background(), order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unfunded cancellation invented refund %d %v", count, err)
	}
	var status, payment string
	if err := db.QueryRow(`SELECT status,payment_status FROM orders WHERE id=$1`, order).Scan(&status, &payment); err != nil || status != "cancelled" || payment != "pending" {
		t.Fatalf("unpaid facts rewritten %s/%s %v", status, payment, err)
	}
}

func TestFIN003ConcurrentCancellationReservesOnce(t *testing.T) {
	db, f, _, order := fin003PaidOrder(t)
	repo := NewOrderRepository(db)
	start := make(chan struct{})
	done := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; done <- repo.CancelOrder(context.Background(), order, f.user, false) }()
	}
	close(start)
	wg.Wait()
	close(done)
	for err := range done {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var amount int64
	if err := db.QueryRow(`SELECT COUNT(*),SUM(amount_toman) FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&count, &amount); err != nil || count != 1 || amount != 10000000 {
		t.Fatalf("concurrent liability %d/%d %v", count, amount, err)
	}
}

func TestFIN003SQLCannotRewriteOrOverreserveCancellation(t *testing.T) {
	db, f, _, order := fin003PaidOrder(t)
	if err := NewOrderRepository(db).CancelOrder(context.Background(), order, f.user, false); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		`UPDATE order_refund_obligations SET amount_toman=1 WHERE order_id=$1`,
		`DELETE FROM order_refund_obligations WHERE order_id=$1`,
		`UPDATE payment_transactions SET amount=amount+1 WHERE order_id=$1`,
		`UPDATE orders SET customer_user_id=NULL WHERE id=$1`,
		`INSERT INTO order_refund_obligations(id,order_id,source_payment_id,customer_id,source_amount_toman,amount_toman,currency,purpose,business_id,destination,policy_version,status,actor_user_id) SELECT id||'-extra',order_id,source_payment_id,customer_id,source_amount_toman,1,currency,purpose,business_id,destination,policy_version,status,actor_user_id FROM order_refund_obligations WHERE order_id=$1`,
	}
	for _, q := range cases {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(q, order)
		tx.Rollback()
		if err == nil {
			t.Fatalf("SQL bypassed preserved principal: %s", q)
		}
	}
	var unknown bool
	if err := db.QueryRow(`SELECT status='unknown' AND state_reason='funding_unverified' AND destination='original_payment' FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&unknown); err != nil || !unknown {
		t.Fatalf("recorded paid row pretended confirmed cash moved %v %v", unknown, err)
	}
}

func TestFIN003LegacyRefundedReturnNeedsReviewNotNewRefund(t *testing.T) {
	db, f, warehouse, order := fin003PaidOrder(t)
	ret := uuid.NewString()
	fin005ReturnFixtureExec(t, db, order, `INSERT INTO customer_returns(id,order_id,customer_id,status,reason,warehouse_id,prepaid) VALUES($1,$2,$3,'refunded','historical refund lacks immutable principal evidence',$4,TRUE)`, ret, order, f.user, warehouse)
	opsExec(t, db, `INSERT INTO return_items(id,return_id,order_item_id,qty,qc_status,received_at) SELECT $1,$2,id::text,1,'ok',NOW() FROM order_items WHERE order_id=$3`, uuid.NewString(), ret, order)
	if err := NewOrderRepository(db).CancelOrder(context.Background(), order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var fresh, review int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount_toman) FILTER(WHERE purpose='cancellation'),0),COALESCE(SUM(amount_toman) FILTER(WHERE purpose='legacy_reservation' AND status='unknown' AND destination='review'),0) FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&fresh, &review); err != nil || fresh != 0 || review != 10000000 {
		t.Fatalf("unproven old return became a new refund: fresh=%d review=%d %v", fresh, review, err)
	}
}

func TestFIN003AlreadyRefundedPaymentCannotCreateAnotherRefund(t *testing.T) {
	db, f, _, order := fin003PaidOrder(t)
	opsExec(t, db, `UPDATE payment_transactions SET status='refunded' WHERE order_id=$1`, order)
	opsExec(t, db, `UPDATE orders SET payment_status='refunded' WHERE id=$1`, order)
	if err := NewOrderRepository(db).CancelOrder(context.Background(), order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var fresh, review int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount_toman) FILTER(WHERE purpose='cancellation'),0),COALESCE(SUM(amount_toman) FILTER(WHERE purpose='legacy_reservation' AND status='unknown'),0) FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&fresh, &review); err != nil || fresh != 0 || review != 10000000 {
		t.Fatalf("old refunded marker reissued cash: fresh=%d review=%d %v", fresh, review, err)
	}
}

func TestFIN003MigrationPreservesHistoricalCancelledPaidRecords(t *testing.T) {
	db, f, _, order := fin003PaidOrder(t)
	opsExec(t, db, `UPDATE orders SET status='cancelled',payment_status='cancelled' WHERE id=$1`, order)
	up, err := os.ReadFile("../../migrations/202611150004_order_refund_principal.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/202611150004_order_refund_principal.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
	var unchanged bool
	if err = tx.QueryRow(`SELECT o.status='cancelled' AND o.payment_status='cancelled' AND p.status='paid' AND p.amount=10000000 AND NOT EXISTS(SELECT 1 FROM order_refund_obligations) AND NOT EXISTS(SELECT 1 FROM order_payment_receipts) AND NOT EXISTS(SELECT 1 FROM customer_wallets WHERE customer_id=$2::uuid AND balance<>0) FROM orders o JOIN payment_transactions p ON p.order_id=o.id WHERE o.id=$1`, order, f.user).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("migration invented cash/debt or rewrote old history %v %v", unchanged, err)
	}
	if _, err = tx.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
}

// Losing inventory admission must never erase a gateway's confirmed money fact.
func TestFIN003LatePaidCallbackAfterCancellationPreservesPrincipal(t *testing.T) {
	db, f, _, order, token := fin003PendingOrder(t)
	repo := NewOrderRepository(db)
	if err := repo.CancelOrder(context.Background(), order, f.user, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := repo.MarkPaymentPaid(context.Background(), token, "late-confirmation", "mask"); !errors.Is(err, ErrPaymentExpired) {
			t.Fatalf("late callback resurrected fulfilment: %v", err)
		}
	}
	fin003AssertCancelledMoney(t, db, f, order)
}
func TestFIN003CancellationPaidCallbackRaceConservesPrincipal(t *testing.T) {
	db, f, _, order, token := fin003PendingOrder(t)
	repo := NewOrderRepository(db)
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() { <-start; done <- repo.CancelOrder(context.Background(), order, f.user, false) }()
	go func() {
		<-start
		_, e := repo.MarkPaymentPaid(context.Background(), token, "concurrent-confirmation", "mask")
		if errors.Is(e, ErrPaymentExpired) {
			e = nil
		}
		done <- e
	}()
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	fin003AssertCancelledMoney(t, db, f, order)
}
func fin003AssertCancelledMoney(t *testing.T, db *sql.DB, f opsFixture, order string) {
	t.Helper()
	var status, payment, source string
	var captured bool
	if err := db.QueryRow(`SELECT o.status,o.payment_status,p.status,o.inventory_captured FROM orders o JOIN payment_transactions p ON p.order_id=o.id WHERE o.id=$1`, order).Scan(&status, &payment, &source, &captured); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || payment != "paid" || source != "paid" || captured {
		t.Fatalf("confirmed money lost or fulfilment resurrected: %s/%s/%s capture=%v", status, payment, source, captured)
	}
	var count, stock int
	var amount int64
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&count, &amount); err != nil || count != 1 || amount != 10000000 {
		t.Fatalf("late money liability %d/%d %v", count, amount, err)
	}
	if err := db.QueryRow(`SELECT stock FROM products WHERE id=$1`, f.product).Scan(&stock); err != nil || stock != 1 {
		t.Fatalf("callback/cancellation stock not conserved %d %v", stock, err)
	}
}

type fin003Verifier interface {
	BeginPaymentVerification(context.Context, string, string) (string, error)
	RecordPaymentVerification(context.Context, string, payment.VerificationResult) error
	RecordPaymentVerificationUnknown(context.Context, string, string) error
}

func fin003VerifierFor(t *testing.T, db *sql.DB) fin003Verifier {
	t.Helper()
	v, ok := any(NewOrderRepository(db)).(fin003Verifier)
	if !ok {
		t.Fatal("durable verified-funding boundary is missing")
	}
	return v
}
func fin003Confirmation() payment.VerificationResult {
	return payment.VerificationResult{Success: true, VerifiedAmount: 10000000, RRN: "provider-" + uuid.NewString()[:12], VerifiedAt: time.Now()}
}
func TestFIN003VerifiedReceiptAfterCancelIsAtomicAndReplaySafe(t *testing.T) {
	db, f, _, order, token := fin003PendingOrder(t)
	v := fin003VerifierFor(t, db)
	ctx := context.Background()
	id, err := v.BeginPaymentVerification(ctx, token, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	// A crash after the request is committed leaves a stable inquiry identity.
	replay, err := v.BeginPaymentVerification(ctx, token, "gateway")
	if err != nil || replay != id {
		t.Fatalf("inquiry identity changed %s %v", replay, err)
	}
	if err = NewOrderRepository(db).CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}
	result := fin003Confirmation()
	for i := 0; i < 3; i++ {
		if err = v.RecordPaymentVerification(ctx, id, result); err != nil {
			t.Fatal(err)
		}
	}
	fin003AssertCancelledMoney(t, db, f, order)
	var ready bool
	if err = db.QueryRow(`SELECT status='pending' AND state_reason='awaiting_original_provider' FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&ready); err != nil || !ready {
		t.Fatalf("verified gateway funding not reserved pending %v %v", ready, err)
	}
	result.RRN = "other-source"
	if err = v.RecordPaymentVerification(ctx, id, result); err == nil {
		t.Fatal("changed confirmation identity accepted")
	}
}
func TestFIN003VerificationSnapshotFreezesSourceAndKeepsUnknownInquiry(t *testing.T) {
	db, f, _, order, token := fin003PendingOrder(t)
	v := fin003VerifierFor(t, db)
	ctx := context.Background()
	id, err := v.BeginPaymentVerification(ctx, token, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE payment_transactions SET amount=1 WHERE order_id=$1`, `UPDATE payment_transactions SET gateway_token='other' WHERE order_id=$1`, `UPDATE orders SET customer_user_id=NULL WHERE id=$1`} {
		tx, e := db.Begin()
		if e != nil {
			t.Fatal(e)
		}
		_, e = tx.Exec(q, order)
		tx.Rollback()
		if e == nil {
			t.Fatalf("verification source changed while external request in flight: %s", q)
		}
	}
	if err = v.RecordPaymentVerificationUnknown(ctx, id, "provider_unavailable"); err != nil {
		t.Fatal(err)
	}
	if err = NewOrderRepository(db).CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.QueryRow(`SELECT COUNT(*) FROM order_payment_verifications WHERE id=$1`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("unknown verification lost %d %v", n, err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&n); err != nil || n != 0 {
		t.Fatalf("unknown inquiry invented captured money %d %v", n, err)
	}
	if err = v.RecordPaymentVerification(ctx, id, fin003Confirmation()); err != nil {
		t.Fatal(err)
	}
	fin003AssertCancelledMoney(t, db, f, order)
}
func TestFIN003VerificationAmountMismatchRetainsObservationWithoutInventedPrincipal(t *testing.T) {
	db, _, _, order, token := fin003PendingOrder(t)
	v := fin003VerifierFor(t, db)
	ctx := context.Background()
	id, err := v.BeginPaymentVerification(ctx, token, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	result := fin003Confirmation()
	result.VerifiedAmount = 9999999
	if err = v.RecordPaymentVerification(ctx, id, result); !errors.Is(err, payment.ErrAmountMismatch) {
		t.Fatalf("amount mismatch accepted %v", err)
	}
	var observations, receipts int
	if err = db.QueryRow(`SELECT (SELECT COUNT(*) FROM order_payment_verification_events WHERE verification_id=$1 AND status='amount_mismatch' AND observed_amount_toman=9999999),(SELECT COUNT(*) FROM order_payment_receipts WHERE order_id=$2)`, id, order).Scan(&observations, &receipts); err != nil || observations != 1 || receipts != 0 {
		t.Fatalf("mismatched money silently lost or certified: %d/%d %v", observations, receipts, err)
	}
}
func TestFIN003SimulatorReceiptCannotAuthorizeRealMoney(t *testing.T) {
	db, f, _, order, token := fin003PendingOrder(t)
	v := fin003VerifierFor(t, db)
	ctx := context.Background()
	id, err := v.BeginPaymentVerification(ctx, token, "simulated")
	if err != nil {
		t.Fatal(err)
	}
	if err = v.RecordPaymentVerification(ctx, id, fin003Confirmation()); err != nil {
		t.Fatal(err)
	}
	if err = NewOrderRepository(db).CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var uncertain bool
	if err = db.QueryRow(`SELECT status='unknown' AND state_reason='simulated_funding' FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&uncertain); err != nil || !uncertain {
		t.Fatalf("simulator pretended real captured cash %v %v", uncertain, err)
	}
}

func TestFIN003ExpiryPreservesPreviouslyConfirmedReceipt(t *testing.T) {
	db, f, _, order, token := fin003PendingOrder(t)
	v := fin003VerifierFor(t, db)
	ctx := context.Background()
	id, err := v.BeginPaymentVerification(ctx, token, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	if err = v.RecordPaymentVerification(ctx, id, fin003Confirmation()); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `UPDATE orders SET payment_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, order)
	expired, err := NewOrderRepository(db).ExpireUnpaidOrders(ctx)
	if err != nil || len(expired) != 1 {
		t.Fatalf("expiry %d %v", len(expired), err)
	}
	fin003AssertCancelledMoney(t, db, f, order)
}

type fin003AdmissionRejector interface {
	RejectVerifiedPaymentAdmission(context.Context, string) (bool, error)
}

func TestFIN003RejectedAdmissionCompensatesOnceButCannotCancelCapturedRetry(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(map[bool]string{false: "hold_lost", true: "already_captured"}[capture], func(t *testing.T) {
			db, f, _, order, token := fin003PendingOrder(t)
			v := fin003VerifierFor(t, db)
			ctx := context.Background()
			repo := NewOrderRepository(db)
			id, err := v.BeginPaymentVerification(ctx, token, "gateway")
			if err != nil {
				t.Fatal(err)
			}
			result := fin003Confirmation()
			if err = v.RecordPaymentVerification(ctx, id, result); err != nil {
				t.Fatal(err)
			}
			if capture {
				if _, err = repo.MarkPaymentPaid(ctx, token, result.RRN, "mask"); err != nil {
					t.Fatal(err)
				}
			}
			a, ok := any(repo).(fin003AdmissionRejector)
			if !ok {
				t.Fatal("verified admission compensation boundary missing")
			}
			for i := 0; i < 3; i++ {
				cancelled, e := a.RejectVerifiedPaymentAdmission(ctx, token)
				if e != nil || cancelled == capture {
					t.Fatalf("admission compensation cancelled=%v captured=%v %v", cancelled, capture, e)
				}
			}
			if !capture {
				fin003AssertCancelledMoney(t, db, f, order)
			} else {
				var valid bool
				if err = db.QueryRow(`SELECT status<>'cancelled' AND inventory_captured AND payment_status='paid' AND NOT EXISTS(SELECT 1 FROM order_refund_obligations WHERE order_id=$1) FROM orders WHERE id=$1`, order).Scan(&valid); err != nil || !valid {
					t.Fatalf("duplicate callback cancelled already captured order %v %v", valid, err)
				}
			}
		})
	}
}

func fin003VerifiedPaidOrder(t *testing.T) (*sql.DB, opsFixture, string, string) {
	t.Helper()
	db, f, warehouse, order, token := fin003PendingOrder(t)
	v := fin003VerifierFor(t, db)
	ctx := context.Background()
	id, err := v.BeginPaymentVerification(ctx, token, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	result := fin003Confirmation()
	if err = v.RecordPaymentVerification(ctx, id, result); err != nil {
		t.Fatal(err)
	}
	if _, err = NewOrderRepository(db).MarkPaymentPaid(ctx, token, result.RRN, "mask"); err != nil {
		t.Fatal(err)
	}
	return db, f, warehouse, order
}
func fin003ReceivedReturn(t *testing.T, db *sql.DB, f opsFixture, warehouse, order string) string {
	t.Helper()
	ret := uuid.NewString()
	fin005ReturnFixtureExec(t, db, order, `INSERT INTO customer_returns(id,order_id,customer_id,status,reason,warehouse_id,prepaid) VALUES($1,$2,$3,'received','received customer return',$4,TRUE)`, ret, order, f.user, warehouse)
	opsExec(t, db, `INSERT INTO return_items(id,return_id,order_item_id,qty,qc_status,received_at) SELECT $1,$2,id::text,1,'ok',NOW() FROM order_items WHERE order_id=$3`, uuid.NewString(), ret, order)
	return ret
}
func TestFIN003CancellationReservationBlocksCompetingWalletReturn(t *testing.T) {
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	repo := NewOrderRepository(db)
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if err := repo.CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.RefundCustomerReturn(ctx, ret, f.user); !errors.Is(err, ErrRefundPrincipalUnresolved) {
		t.Fatalf("return spent pending cancellation principal: %v", err)
	}
	var balance int64
	if err := db.QueryRow(`SELECT COALESCE((SELECT balance FROM customer_wallets WHERE customer_id=$1),0)`, f.user).Scan(&balance); err != nil || balance != 0 {
		t.Fatalf("competing route created wallet credit %d %v", balance, err)
	}
}
func TestFIN003WalletReturnConsumesPrincipalBeforeCancellation(t *testing.T) {
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	repo := NewOrderRepository(db)
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if amount, _, err := repo.RefundCustomerReturn(ctx, ret, f.user); err != nil || amount != 10000000 {
		t.Fatalf("wallet return %d %v", amount, err)
	}
	if err := repo.CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var wallet, returned, cancelled int64
	if err := db.QueryRow(`SELECT COALESCE((SELECT balance FROM customer_wallets WHERE customer_id=$1),0),COALESCE(SUM(amount_toman) FILTER(WHERE purpose='customer_return'),0),COALESCE(SUM(amount_toman) FILTER(WHERE purpose='cancellation'),0) FROM order_refund_obligations WHERE order_id=$2`, f.user, order).Scan(&wallet, &returned, &cancelled); err != nil || wallet != 10000000 || returned != 10000000 || cancelled != 0 {
		t.Fatalf("wallet source not conserved wallet=%d claim=%d cancellation=%d %v", wallet, returned, cancelled, err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM order_refund_events e JOIN order_refund_obligations o ON o.id=e.obligation_id WHERE o.order_id=$1 AND e.status='succeeded' AND e.destination='wallet' AND e.amount_toman=10000000`, order).Scan(&n); err != nil || n != 1 {
		t.Fatalf("wallet outcome not journaled exactly once %d %v", n, err)
	}
}
func TestFIN003UnverifiedHistoricalPaidRowCannotMintWalletMoney(t *testing.T) {
	db, f, warehouse, order := fin003PaidOrder(t)
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if _, _, err := NewOrderRepository(db).RefundCustomerReturn(context.Background(), ret, f.user); !errors.Is(err, ErrRefundPrincipalUnresolved) {
		t.Fatalf("unverified funding credited real wallet %v", err)
	}
}

func fin003Dispute(t *testing.T, db *sql.DB, order string) string {
	t.Helper()
	id := "fin-dispute-" + uuid.NewString()[:12]
	var previous string
	if err := db.QueryRow(`SELECT status FROM orders WHERE id=$1`, order).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `UPDATE orders SET status='shipped' WHERE id=$1`, order)
	if err := NewFinanceRepository(db).CreateDispute(context.Background(), domain.Dispute{ID: id, OrderID: order, Type: "delivery_undelivered", ProductTitle: "fixture", ClaimAmount: 2000000, Reason: "partial claim"}); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `UPDATE orders SET status=$2 WHERE id=$1`, order, previous)
	return id
}

func TestFIN003DisputeCannotSpendPendingCancellation(t *testing.T) {
	db, f, _, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	id := fin003Dispute(t, db, order)
	if err := NewOrderRepository(db).CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}
	if err := NewFinanceRepository(db).ResolveDispute(ctx, id, "refund_supplier", "supplier", f.user, 2000000); !errors.Is(err, ErrRefundPrincipalUnresolved) {
		t.Fatalf("dispute did not respect principal %v", err)
	}
	var valid bool
	if err := db.QueryRow(`SELECT status='pending' AND NOT EXISTS(SELECT 1 FROM ledger_entries WHERE related_order_or_dispute=$1) FROM disputes WHERE id=$1`, id).Scan(&valid); err != nil || !valid {
		t.Fatalf("rejected dispute partially committed %v %v", valid, err)
	}
}
func TestFIN003PartialDisputePreservesPaidFactsAndReservesExactPrincipal(t *testing.T) {
	db, f, _, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	id := fin003Dispute(t, db, order)
	repo := NewOrderRepository(db)
	if err := NewFinanceRepository(db).ResolveDispute(ctx, id, "refund_supplier", "supplier", f.user, 2000000); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if applied, err := repo.ApplyDisputeRefundEffect(ctx, id); err != nil || !applied {
			t.Fatalf("dispute effect %v %v", applied, err)
		}
	}
	var valid bool
	if err := db.QueryRow(`SELECT o.status<>'cancelled' AND o.payment_status='paid' AND p.status='paid' AND o.inventory_captured FROM orders o JOIN payment_transactions p ON p.order_id=o.id WHERE o.id=$1`, order).Scan(&valid); err != nil || !valid {
		t.Fatalf("partial adjudication pretended complete bank refund/fulfillment cancellation %v %v", valid, err)
	}
	if err := repo.CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var dispute, cancellation int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount_toman) FILTER(WHERE purpose='dispute'),0),COALESCE(SUM(amount_toman) FILTER(WHERE purpose='cancellation'),0) FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&dispute, &cancellation); err != nil || dispute != 2000000 || cancellation != 8000000 {
		t.Fatalf("partial dispute ignored %d/%d %v", dispute, cancellation, err)
	}
}

func TestFIN003MultipleConfirmedPaymentsPreserveEachActualCapture(t *testing.T) {
	fin003MultipleSources(t, false)
}
func TestFIN003LegacyRefundWithMultipleSourcesRequiresReview(t *testing.T) {
	fin003MultipleSources(t, true)
}
func fin003MultipleSources(t *testing.T, legacy bool) {
	t.Helper()
	db, f, _, order := fin003VerifiedPaidOrder(t)
	token := "second-" + uuid.NewString()[:12]
	opsExec(t, db, `INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES($1,$2,$2,$1,10000000,'failed')`, token, order)
	v := fin003VerifierFor(t, db)
	ctx := context.Background()
	id, err := v.BeginPaymentVerification(ctx, token, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	if legacy {
		opsExec(t, db, `INSERT INTO ledger_entries(id,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) VALUES($1,$2,'platform','credit',2000000,'استرداد وجه سفارش: source not recorded',$3,$2)`, "old-refund-"+uuid.NewString(), order, f.user)
	}
	if err = v.RecordPaymentVerification(ctx, id, fin003Confirmation()); err != nil {
		t.Fatal(err)
	}
	if err = NewOrderRepository(db).CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var fresh, review, total int64
	if err = db.QueryRow(`SELECT COALESCE(SUM(amount_toman) FILTER(WHERE purpose IN ('cancellation','duplicate_payment')),0),COALESCE(SUM(amount_toman) FILTER(WHERE purpose='legacy_reservation' AND destination='review'),0),SUM(amount_toman) FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&fresh, &review, &total); err != nil {
		t.Fatal(err)
	}
	expectedFresh, expectedReview := int64(20000000), int64(0)
	if legacy {
		expectedFresh, expectedReview = 0, 20000000
	}
	if fresh != expectedFresh || review != expectedReview || total != 20000000 {
		t.Fatalf("actual multiple captures lost or ambiguous history sent to arbitrary source fresh=%d review=%d total=%d", fresh, review, total)
	}
}
func TestFIN003VerificationInquiryCannotBeDowngradedToFailed(t *testing.T) {
	db, _, _, order, token := fin003PendingOrder(t)
	v := fin003VerifierFor(t, db)
	ctx := context.Background()
	if _, err := v.BeginPaymentVerification(ctx, token, "gateway"); err != nil {
		t.Fatal(err)
	}
	if err := NewOrderRepository(db).RecordPaymentFailure(ctx, token); err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err := db.QueryRow(`SELECT o.payment_status='pending' AND p.status='pending' FROM orders o JOIN payment_transactions p ON p.order_id=o.id WHERE o.id=$1`, order).Scan(&valid); err != nil || !valid {
		t.Fatalf("uncertain inquiry marked failed %v %v", valid, err)
	}
}

func TestFIN003LegacyPendingRefundTableClaimIsNotSpentAgain(t *testing.T) {
	fin003LegacyRefundTable(t, "pending", false)
}
func TestFIN003LegacyPSPRefundTableClaimIsNotSpentAgain(t *testing.T) {
	fin003LegacyRefundTable(t, "psp_refunded", false)
}
func TestFIN003LegacyCompletedRefundProjectionsAreNotDoubleCounted(t *testing.T) {
	fin003LegacyRefundTable(t, "completed", true)
}
func fin003LegacyRefundTable(t *testing.T, status string, projected bool) {
	t.Helper()
	db, f, _, order := fin003PaidOrder(t)
	down, err := os.ReadFile("../../migrations/202611150004_order_refund_principal.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../migrations/202611150004_order_refund_principal.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO refunds(id,order_id,idempotency_key,amount,status) VALUES($1,$2,$1,2000000,$3)`, "old-refund-"+uuid.NewString(), order, status); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if projected {
		opsExec(t, db, `INSERT INTO ledger_entries(id,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) VALUES($1,$2,'platform','credit',2000000,'استرداد وجه سفارش: same old claim projection',$3,$2)`, "old-ledger-"+uuid.NewString(), order, f.user)
	}
	if err = NewOrderRepository(db).CancelOrder(context.Background(), order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var fresh, review int64
	if err = db.QueryRow(`SELECT COALESCE(SUM(amount_toman) FILTER(WHERE purpose='cancellation'),0),COALESCE(SUM(amount_toman) FILTER(WHERE purpose='legacy_reservation' AND destination='review'),0) FROM order_refund_obligations WHERE order_id=$1`, order).Scan(&fresh, &review); err != nil || fresh != 0 || review != 10000000 {
		t.Fatalf("old PSP claim/projection spent again or double counted: fresh=%d review=%d %v", fresh, review, err)
	}
}
func TestFIN003ExtraVerifiedCaptureOnFulfilledLegacyOrderNeedsReview(t *testing.T) {
	db, _, _, order := fin003PaidOrder(t)
	token := "legacy-extra-" + uuid.NewString()[:12]
	opsExec(t, db, `UPDATE orders SET status='shipped',shipped_at=NOW() WHERE id=$1`, order)
	opsExec(t, db, `INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES($1,$2,$2,$1,10000000,'failed')`, token, order)
	v := fin003VerifierFor(t, db)
	ctx := context.Background()
	id, err := v.BeginPaymentVerification(ctx, token, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	if err = v.RecordPaymentVerification(ctx, id, fin003Confirmation()); err != nil {
		t.Fatal(err)
	}
	var count int
	var amount int64
	if err = db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE source_payment_id=$1 AND destination='review' AND status='unknown'`, token).Scan(&count, &amount); err != nil || count != 1 || amount != 10000000 {
		t.Fatalf("extra actual money on legacy fulfillment disappeared: %d/%d %v", count, amount, err)
	}
	var preserved bool
	if err = db.QueryRow(`SELECT status='shipped' AND inventory_captured FROM orders WHERE id=$1`, order).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("legacy goods resurrected or cancelled %v %v", preserved, err)
	}
}
func TestFIN003RejectedExpiryCannotStarveLaterConfirmedCompensation(t *testing.T) {
	db, f, _, order, token := fin003PendingOrder(t)
	v := fin003VerifierFor(t, db)
	ctx := context.Background()
	id, err := v.BeginPaymentVerification(ctx, token, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	if err = v.RecordPaymentVerification(ctx, id, fin003Confirmation()); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status,payment_expires_at) VALUES('000-fin-expiry-poison','000-fin-expiry-poison',$1,'fixture',1,1,'fixture','09000000000','x','x','x','0000000000','000-fin-expiry-poison',$2,'pending',NOW()-INTERVAL '1 second')`, f.store, f.user)
	opsExec(t, db, `INSERT INTO supplier_shipments(id,order_id,supplier_id,shipping_address,customer_phone,customer_name,status,shipped_at) VALUES('poison-leg','000-fin-expiry-poison',$1,'{}','09000000000','fixture','shipped',NOW())`, f.supplier)
	opsExec(t, db, `UPDATE orders SET payment_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, order)
	expired, err := NewOrderRepository(db).ExpireUnpaidOrders(ctx)
	if err == nil || len(expired) != 1 || expired[0].ID != order {
		t.Fatalf("poisoned expiry blocked independent money compensation: count=%d %v", len(expired), err)
	}
	fin003AssertCancelledMoney(t, db, f, order)
	if expired, err = NewOrderRepository(db).ExpireUnpaidOrders(ctx); err != nil || len(expired) != 0 {
		t.Fatalf("known rejected candidate immediately selected again %d %v", len(expired), err)
	}
}

func TestFIN003LateLegacyAttemptWithNewerPendingPreservesUnknownLiability(t *testing.T) {
	db, f, _, order, _ := fin003PendingOrder(t)
	old := "older-" + uuid.NewString()[:12]
	opsExec(t, db, `INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status,created_at) VALUES($1,$2,$2,$1,10000000,'failed',NOW()-INTERVAL '1 minute')`, old, order)
	repo := NewOrderRepository(db)
	ctx := context.Background()
	if err := repo.CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := repo.MarkPaymentPaid(ctx, old, "legacy-late-confirmed", "mask"); !errors.Is(err, ErrPaymentExpired) {
			t.Fatalf("late recorded source lost behind a newer pending attempt %v", err)
		}
	}
	var preserved bool
	if err := db.QueryRow(`SELECT o.status='cancelled' AND o.payment_status='paid' AND NOT o.inventory_captured AND p.status='paid' AND p.compensation_capture AND (SELECT COUNT(*) FROM payment_transactions WHERE order_id=$1 AND status='pending')=1 AND (SELECT SUM(amount_toman) FROM order_refund_obligations WHERE source_payment_id=$2 AND status='unknown' AND destination='original_payment')=10000000 FROM orders o JOIN payment_transactions p ON p.id=$2 WHERE o.id=$1`, order, old).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("legacy late capture fact/reservation not conserved %v %v", preserved, err)
	}
}
