package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	"testing"
)

func fin005Admitted(t *testing.T) (*FinanceRepository, string, string) {
	t.Helper()
	db, f, _, order := fin003VerifiedPaidOrder(t)
	opsExec(t, db, `UPDATE orders SET status='shipped' WHERE id=$1`, order)
	id := "FIN005-" + uuid.NewString()
	repo := NewFinanceRepository(db)
	if err := repo.CreateDispute(context.Background(), domain.Dispute{ID: id, OrderID: order, Type: "delivery_undelivered", ProductTitle: "fixture", ClaimAmount: 2000000, Reason: "fixture", StoreID: f.store}); err != nil {
		t.Fatal(err)
	}
	return repo, id, order
}
func TestFIN005SellerWinRestoresShipped(t *testing.T) {
	repo, id, order := fin005Admitted(t)
	if err := repo.ResolveDispute(context.Background(), id, "release_to_seller", "seller", "fixture", 2000000); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := repo.db.QueryRow(`SELECT status FROM orders WHERE id=$1`, order).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "shipped" {
		t.Fatalf("seller verdict invented/lost physical state: got %s want shipped", status)
	}
}
func TestFIN005IdenticalVerdictReplay(t *testing.T) {
	repo, id, _ := fin005Admitted(t)
	for i := 0; i < 2; i++ {
		if err := repo.ResolveDispute(context.Background(), id, "refund_supplier", "supplier", "fixture", 2000000); err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
	}
	var n int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("duplicate effects %d %v", n, err)
	}
}
func TestFIN005RejectsContradictoryVerdict(t *testing.T) {
	repo, id, _ := fin005Admitted(t)
	if err := repo.ResolveDispute(context.Background(), id, "refund_supplier", "carrier", "fixture", 2000000); err == nil {
		t.Fatal("contradictory action/party committed")
	}
}
func TestFIN005RejectsUnknownRefundAction(t *testing.T) {
	repo, id, _ := fin005Admitted(t)
	if err := repo.ResolveDispute(context.Background(), id, "refund_arbitrary", "carrier", "fixture", 2000000); err == nil {
		t.Fatal("unknown action committed")
	}
}

func TestFIN005ActionAndPartyAliases(t *testing.T) {
	aliases := map[string][]string{"supplier": {"supplier", "تأمین‌کننده", "تأمین‌کننده کالا"}, "seller": {"seller", "دراپ‌شیپر", "فروشنده", "سلر"}, "carrier": {"carrier", "شرکت حمل‌ونقل و باربری", "شرکت حمل", "باربری"}, "warehouse": {"warehouse", "انبار", "انبار مرکزی"}}
	for party, values := range aliases {
		for _, alias := range values {
			got, err := normalizeDisputeVerdict("refund_"+party, alias)
			if err != nil || got != party {
				t.Fatalf("alias %s %s %v", alias, got, err)
			}
		}
	}
	for _, action := range []string{"release_to_seller", "refund_supplier", "refund_seller", "refund_carrier", "refund_warehouse"} {
		if _, err := normalizeDisputeVerdict(action, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, pair := range [][2]string{{"release_to_seller", "supplier"}, {"refund_supplier", "platform"}, {"refund_seller", "arbitrary"}, {"refund_arbitrary", "carrier"}} {
		if _, err := normalizeDisputeVerdict(pair[0], pair[1]); err == nil {
			t.Fatalf("invalid pair %v", pair)
		}
	}
}
func TestFIN005AdmissionCapturesLockedFactsAndInvalidStateRollsBack(t *testing.T) {
	for _, status := range []string{"shipped", "delivered_unconfirmed", "delivered", "processing", "delivered_confirmed"} {
		t.Run(status, func(t *testing.T) {
			db, f, _, order := fin003VerifiedPaidOrder(t)
			opsExec(t, db, `UPDATE orders SET status=$2,shipped_at=NOW()-INTERVAL '3 days' WHERE id=$1`, order, status)
			id := "FIN005-" + uuid.NewString()
			repo := NewFinanceRepository(db)
			err := repo.CreateDispute(context.Background(), domain.Dispute{ID: id, OrderID: order, StoreID: f.store, Type: "delivery_undelivered", ProductTitle: "fixture", Reason: "fixture", ClaimAmount: 2000000})
			accepted := status != "processing" && status != "delivered_confirmed"
			if !accepted {
				if !errors.Is(err, ErrDisputeOrderNotDisputable) {
					t.Fatal(err)
				}
				var n int
				if err = db.QueryRow(`SELECT COUNT(*) FROM disputes WHERE id=$1`, id).Scan(&n); err != nil || n != 0 {
					t.Fatalf("invalid admission effects %d %v", n, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var captured bool
			if err = db.QueryRow(`SELECT e.prior_status=$3 AND e.order_id=o.id AND e.hold_at=o.disputed_at AND e.prior_disputed_at IS NULL AND e.prior_confirmed_at IS NULL AND e.prior_window_ends_at IS NULL AND NOT e.prior_profit_released AND e.actor_user_id=o.customer_user_id::text FROM dispute_order_evidence e JOIN orders o ON o.id=e.order_id WHERE e.dispute_id=$1 AND o.id=$2`, id, order, status).Scan(&captured); err != nil || !captured {
				t.Fatalf("admission facts %v %v", captured, err)
			}
			if _, err = repo.ResolveDisputeWithOutcome(context.Background(), id, DisputeResolutionInput{Action: "release_to_seller", ResponsibleParty: "seller", ActorUserID: f.user}); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow(`SELECT status=$2 AND confirmed_at IS NULL AND return_window_ends_at IS NULL AND NOT profit_released AND shipped_at IS NOT NULL FROM orders WHERE id=$1`, order, status).Scan(&captured); err != nil || !captured {
				t.Fatalf("verdict invented confirmation or lost timestamps %v %v", captured, err)
			}
		})
	}
}

func TestFIN005InvalidAdmissionMoneyRollsBack(t *testing.T) {
	for _, amount := range []int64{0, -1, 9007199254740992} {
		t.Run(fmt.Sprint(amount), func(t *testing.T) {
			db, _, _, order := fin003VerifiedPaidOrder(t)
			opsExec(t, db, `UPDATE orders SET status='shipped' WHERE id=$1`, order)
			id := "FIN005-" + uuid.NewString()
			err := NewFinanceRepository(db).CreateDispute(context.Background(), domain.Dispute{ID: id, OrderID: order, Type: "delivery_undelivered", ProductTitle: "fixture", ClaimAmount: amount, Reason: "fixture"})
			if !errors.Is(err, ErrDisputeInput) {
				t.Fatalf("invalid admission money %d accepted/error %v", amount, err)
			}
			var clean bool
			if err = db.QueryRow(`SELECT status='shipped' AND NOT EXISTS(SELECT 1 FROM disputes WHERE id=$2) FROM orders WHERE id=$1`, order, id).Scan(&clean); err != nil || !clean {
				t.Fatalf("invalid admission effects %v %v", clean, err)
			}
		})
	}
}

func TestFIN005ExtraVerifiedCaptureCannotBeSpentTwice(t *testing.T) {
	for _, claim := range []int64{15000000, 2000000} {
		t.Run(fmt.Sprint(claim), func(t *testing.T) {
			db, f, _, order := fin003VerifiedPaidOrder(t)
			ctx := context.Background()
			second := "second-" + uuid.NewString()[:12]
			opsExec(t, db, `INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES($1,$2,$2,$1,10000000,'failed')`, second, order)
			verifier := fin003VerifierFor(t, db)
			verification, err := verifier.BeginPaymentVerification(ctx, second, "gateway")
			if err != nil {
				t.Fatal(err)
			}
			if err = verifier.RecordPaymentVerification(ctx, verification, fin003Confirmation()); err != nil {
				t.Fatal(err)
			}
			opsExec(t, db, `UPDATE orders SET status='shipped' WHERE id=$1`, order)
			id := "FIN005-" + uuid.NewString()
			repo := NewFinanceRepository(db)
			if err = repo.CreateDispute(ctx, domain.Dispute{ID: id, OrderID: order, StoreID: f.store, Type: "delivery_undelivered", ProductTitle: "fixture", Reason: "fixture", ClaimAmount: claim}); err != nil {
				t.Fatal(err)
			}
			out, err := repo.ResolveDisputeWithOutcome(ctx, id, DisputeResolutionInput{Action: "refund_supplier", ResponsibleParty: "supplier", ActorUserID: f.user})
			if claim == 15000000 {
				if !errors.Is(err, ErrRefundPrincipalUnresolved) {
					t.Fatalf("spent already reserved extra capture %v", err)
				}
				var clean bool
				if err = db.QueryRow(`SELECT d.status='pending' AND NOT EXISTS(SELECT 1 FROM dispute_financial_decisions WHERE dispute_id=d.id) AND NOT EXISTS(SELECT 1 FROM order_refund_obligations WHERE business_id=d.id) AND NOT EXISTS(SELECT 1 FROM ledger_entries WHERE related_order_or_dispute=d.id) FROM disputes d WHERE d.id=$1`, id).Scan(&clean); err != nil || !clean {
					t.Fatalf("atomic rejection %v %v", clean, err)
				}
			} else {
				if err != nil || out.ClaimAmount != claim {
					t.Fatalf("within original available principal %+v %v", out, err)
				}
				replay, err := repo.ResolveDisputeWithOutcome(ctx, id, DisputeResolutionInput{Action: "refund_supplier", ResponsibleParty: "تأمین‌کننده کالا", ActorUserID: "retry"})
				if err != nil || !replay.Replay {
					t.Fatalf("canonical replay %+v %v", replay, err)
				}
				var original bool
				if err = db.QueryRow(`SELECT COUNT(*)=1 AND BOOL_AND(source_payment_id<>$2 AND amount_toman=$3 AND destination='original_payment') FROM order_refund_obligations WHERE purpose='dispute' AND business_id=$1`, id, second, claim).Scan(&original); err != nil || !original {
					t.Fatalf("claim spent compensation source %v %v", original, err)
				}
			}
			var captured, reserved, compensation int64
			if err = db.QueryRow(`SELECT (SELECT SUM(amount_toman) FROM order_payment_receipts WHERE order_id=$1),(SELECT SUM(amount_toman) FROM order_refund_obligations WHERE order_id=$1),(SELECT SUM(amount_toman) FROM order_refund_obligations WHERE order_id=$1 AND purpose='duplicate_payment')`, order).Scan(&captured, &reserved, &compensation); err != nil {
				t.Fatal(err)
			}
			expected := int64(10000000)
			if claim == 2000000 {
				expected += claim
			}
			if captured != 20000000 || reserved != expected || compensation != 10000000 || captured-reserved != 20000000-expected {
				t.Fatalf("captured/reserved/remaining %d/%d/%d compensation %d", captured, reserved, captured-reserved, compensation)
			}
		})
	}
}
