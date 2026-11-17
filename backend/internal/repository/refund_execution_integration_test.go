package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/payment"
)

type fin003Processor interface {
	ProcessOrderRefund(context.Context, string, payment.RefundProvider) (string, error)
}

type fin003LocalRefundProvider struct {
	mu             sync.Mutex
	sends, lookups int
	lost           bool
	status         string
	wrongAmount    bool
	onSend         func()
}

func (f *fin003LocalRefundProvider) outcome(q payment.RefundRequest) *payment.RefundOutcome {
	n := q.AmountToman
	if f.wrongAmount {
		n++
	}
	return &payment.RefundOutcome{OperationKey: q.OperationKey, PaymentID: q.PaymentID, AmountToman: n, Currency: q.Currency, Destination: q.Destination, Status: f.status, ProviderReference: "local-refund-confirmation"}
}
func (f *fin003LocalRefundProvider) Refund(ctx context.Context, q payment.RefundRequest) (*payment.RefundOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends++
	if f.onSend != nil {
		f.onSend()
	}
	if f.lost {
		return nil, errors.New("local fake: response lost after external success")
	}
	return f.outcome(q), nil
}
func (f *fin003LocalRefundProvider) LookupRefund(ctx context.Context, q payment.RefundRequest) (*payment.RefundOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	return f.outcome(q), nil
}

func fin003ReadyRefund(t *testing.T) (*sql.DB, string, string, fin003Processor) {
	t.Helper()
	db, f, _, order := fin003VerifiedPaidOrder(t)
	if err := NewOrderRepository(db).CancelOrder(context.Background(), order, f.user, false); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.QueryRow(`SELECT id FROM order_refund_obligations WHERE order_id=$1 AND purpose='cancellation'`, order).Scan(&id); err != nil {
		t.Fatal(err)
	}
	processor, ok := any(NewOrderRepository(db)).(fin003Processor)
	if !ok {
		t.Fatal("durable refund attempt/reconciliation boundary is missing")
	}
	return db, order, id, processor
}

func TestFIN003UnavailableOriginalProviderKeepsPending(t *testing.T) {
	db, _, id, p := fin003ReadyRefund(t)
	for i := 0; i < 3; i++ {
		if state, err := p.ProcessOrderRefund(context.Background(), id, nil); err != nil || state != "pending" {
			t.Fatalf("missing adapter pretended outcome %s %v", state, err)
		}
	}
	var attempts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM order_refund_attempts WHERE obligation_id=$1`, id).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("unavailable provider generated attempts %d %v", attempts, err)
	}
}

func TestFIN003LostRefundResponseReconcilesWithoutSecondTransfer(t *testing.T) {
	db, order, id, p := fin003ReadyRefund(t)
	ctx := context.Background()
	fake := &fin003LocalRefundProvider{lost: true, status: "succeeded"}
	// Provider I/O must not hold an order/account transaction open.
	fake.onSend = func() {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err = tx.Exec(`SELECT id FROM orders WHERE id=$1 FOR UPDATE NOWAIT`, order); err != nil {
			t.Fatalf("provider I/O holds the database guard: %v", err)
		}
	}
	if state, err := p.ProcessOrderRefund(ctx, id, fake); err != nil || state != "unknown" {
		t.Fatalf("lost response was not reserved unknown %s %v", state, err)
	}
	for i := 0; i < 3; i++ {
		if state, err := p.ProcessOrderRefund(ctx, id, fake); err != nil || state != "succeeded" {
			t.Fatalf("reconciliation %s %v", state, err)
		}
	}
	if fake.sends != 1 || fake.lookups != 1 {
		t.Fatalf("unknown attempt resent or completed result re-polled: sends=%d inquiries=%d", fake.sends, fake.lookups)
	}
	var n int
	var amount int64
	if err := db.QueryRow(`SELECT COUNT(*),SUM(amount_toman) FROM order_refund_events WHERE obligation_id=$1 AND status='succeeded'`, id).Scan(&n, &amount); err != nil || n != 1 || amount != 10000000 {
		t.Fatalf("confirmed outcome duplicated %d/%d %v", n, amount, err)
	}
}

func TestFIN003FailedOrMismatchedRefundCannotReleasePrincipal(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed", true: "mismatch"}[mismatch], func(t *testing.T) {
			db, order, id, processor := fin003ReadyRefund(t)
			fake := &fin003LocalRefundProvider{status: "failed", wrongAmount: mismatch}
			state, err := processor.ProcessOrderRefund(context.Background(), id, fake)
			if mismatch {
				if state != "unknown" || !errors.Is(err, ErrPaymentVerificationIdentity) {
					t.Fatalf("mismatched result accepted %s %v", state, err)
				}
			} else if state != "failed" || err != nil {
				t.Fatalf("explicit failed outcome lost %s %v", state, err)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err = lockOrderFinanceTx(context.Background(), tx, order, true); err != nil {
				t.Fatal(err)
			}
			if _, err = ReserveAdminPaymentRefundTx(context.Background(), tx, order, "", "new-refund", "test", 1); !errors.Is(err, ErrRefundPrincipalUnresolved) {
				t.Fatalf("failure/mismatch released principal %v", err)
			}
		})
	}
}

func TestFIN003PopulatedMigrationRollbackRefusesEvidenceLoss(t *testing.T) {
	db, _, _, _ := fin003ReadyRefund(t)
	down, err := os.ReadFile("../../migrations/202611150004_order_refund_principal.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(string(down)); err == nil {
		t.Fatal("populated migration discarded money evidence")
	}
}

func TestFIN003BoundedReconciliationCannotStarveBehindUnknownRefund(t *testing.T) {
	db, _, _, processor := fin003ReadyRefund(t)
	// The runner isolates top-level tests, so both claims share this database.
	_, _, second, _ := fin003ReadyRefund(t)
	r := NewOrderRepository(db)
	fake := &fin003LocalRefundProvider{status: "unknown"}
	for i := 0; i < 2; i++ {
		if _, err := r.ReconcileOrderRefunds(context.Background(), fake, 1); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM order_refund_attempts`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("first unknown claim starved later outbox work: attempts=%d %v", n, err)
	}
	_ = processor
	_ = second
}
func TestFIN003ConcurrentRefundWorkersSendAtMostOnce(t *testing.T) {
	db, _, id, p := fin003ReadyRefund(t)
	fake := &fin003LocalRefundProvider{status: "succeeded"}
	start := make(chan struct{})
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			state, e := p.ProcessOrderRefund(context.Background(), id, fake)
			if e == nil && state != "succeeded" {
				e = errors.New("unexpected state " + state)
			}
			done <- e
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if fake.sends != 1 {
		t.Fatalf("concurrent transfers %d", fake.sends)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM order_refund_events WHERE obligation_id=$1 AND status='succeeded'`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("success evidence duplicated %d %v", n, err)
	}
}
func TestFIN003SQLRejectsUnprovedAndMismatchedRefundSuccess(t *testing.T) {
	db, _, id, _ := fin003ReadyRefund(t)
	for _, statement := range []string{
		`INSERT INTO order_refund_events(id,obligation_id,source_payment_id,amount_toman,currency,destination,status,operation_key,provider_reference,reason) SELECT 'forged',id,source_payment_id,amount_toman,currency,destination,'succeeded','refund:'||id,'invented','forged' FROM order_refund_obligations WHERE id=$1`,
		`INSERT INTO order_refund_attempts(id,obligation_id,source_payment_id,amount_toman,currency,destination,operation_key,original_reference) SELECT 'bad-attempt',id,source_payment_id,amount_toman+1,currency,destination,'refund:'||id,'wrong' FROM order_refund_obligations WHERE id=$1`,
		`INSERT INTO order_refund_wallet_credits(obligation_id,customer_id,amount_toman) SELECT id,customer_id,amount_toman FROM order_refund_obligations WHERE id=$1`,
	} {
		tx, e := db.Begin()
		if e != nil {
			t.Fatal(e)
		}
		_, e = tx.Exec(statement, id)
		tx.Rollback()
		if e == nil {
			t.Fatalf("SQL certified unsupported money movement: %s", statement)
		}
	}
}
func TestFIN003WalletJournalRollsBackWithAllRefundEffects(t *testing.T) {
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	ctx := context.Background()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = lockOrderFinanceTx(ctx, tx, order, true); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	r, err := reserveOrderRefundTx(ctx, tx, order, "customer_return", ret, "wallet", f.user, 10000000, "")
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = creditReservedWalletRefundsTx(ctx, tx, r.IDs); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	tx.Rollback()
	var valid bool
	if err = db.QueryRow(`SELECT NOT EXISTS(SELECT 1 FROM order_refund_obligations WHERE order_id=$1) AND NOT EXISTS(SELECT 1 FROM order_refund_wallet_credits WHERE customer_id=$2) AND COALESCE((SELECT balance FROM customer_wallets WHERE customer_id=$2),0)=0`, order, f.user).Scan(&valid); err != nil || !valid {
		t.Fatalf("refund rollback leaked money %v %v", valid, err)
	}
}

func TestFIN003FailedRefundProjectionRemainsExplicitlyFailed(t *testing.T) {
	db, order, id, p := fin003ReadyRefund(t)
	if _, err := p.ProcessOrderRefund(context.Background(), id, &fin003LocalRefundProvider{status: "failed"}); err != nil {
		t.Fatal(err)
	}
	rows := []domain.Order{{ID: order}}
	if err := NewOrderRepository(db).attachRefundSummaries(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].Refund == nil || rows[0].Refund.Status != "failed" || !rows[0].Refund.NeedsReview || rows[0].Refund.FailedToman != 10000000 {
		t.Fatalf("failed outcome erased in customer projection %+v", rows[0].Refund)
	}
}

func TestFIN003SQLWalletCreditCannotCommitWithoutMatchingOutcome(t *testing.T) {
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	ctx := context.Background()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = lockOrderFinanceTx(ctx, tx, order, true); err != nil {
		t.Fatal(err)
	}
	reservation, err := reserveOrderRefundTx(ctx, tx, order, "customer_return", ret, "wallet", f.user, 10000000, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO order_refund_wallet_credits(obligation_id,customer_id,amount_toman) SELECT id,customer_id,amount_toman FROM order_refund_obligations WHERE id=$1`, reservation.IDs[0]); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err == nil {
		t.Fatal("wallet effect committed without a matching immutable success outcome")
	}
	var balance int64
	if err = db.QueryRow(`SELECT COALESCE((SELECT balance FROM customer_wallets WHERE customer_id=$1),0)`, f.user).Scan(&balance); err != nil || balance != 0 {
		t.Fatalf("incomplete wallet effect leaked %d %v", balance, err)
	}
}

func TestFIN003HistoricalAuditIsReadOnlyAndFindsUnverifiedCancelledMoney(t *testing.T) {
	db, _, _, order := fin003PaidOrder(t)
	opsExec(t, db, `UPDATE orders SET status='cancelled' WHERE id=$1`, order)
	script, err := os.ReadFile("../../../scripts/audit-order-refund-principal.sql")
	if err != nil {
		t.Fatal(err)
	}
	query := strings.Replace(string(script), "BEGIN READ ONLY;", "", 1)
	query = strings.Replace(query, "COMMIT;", "", 1)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN READ ONLY`); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, `ROLLBACK`)
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		t.Fatal(err)
	}
	found := false
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err = rows.Scan(dest...); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		for i, name := range columns {
			if name == "order_id" && values[i] == order {
				found = true
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || !found {
		t.Fatalf("historical candidate missing %v %v", found, err)
	}
	if _, err = conn.ExecContext(ctx, `UPDATE orders SET status='processing' WHERE id=$1`, order); err == nil {
		t.Fatal("audit transaction permitted a data mutation")
	}
	conn.ExecContext(ctx, `ROLLBACK`)
	var preserved bool
	if err = db.QueryRow(`SELECT o.status='cancelled' AND p.status='paid' AND p.amount=10000000 AND NOT EXISTS(SELECT 1 FROM order_refund_obligations WHERE order_id=$1) FROM orders o JOIN payment_transactions p ON p.order_id=o.id WHERE o.id=$1`, order).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("read-only audit changed historical money %v %v", preserved, err)
	}
}
