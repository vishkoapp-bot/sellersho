package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"strings"
	"testing"
	"time"
)

func fin005Fault(t *testing.T, db *sql.DB, table, condition string) {
	t.Helper()
	name := "fin005_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	q := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF %s THEN RAISE EXCEPTION 'FIN005 injected fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER %s BEFORE INSERT OR UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION %s()`, name, condition, name, table, name)
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(fmt.Sprintf(`DROP TRIGGER %s ON %s; DROP FUNCTION %s()`, name, table, name)); err != nil {
			t.Error(err)
		}
	})
}
func TestFIN005AtomicResolutionFaultBoundaries(t *testing.T) {
	for _, boundary := range []string{"provisional", "obligation", "adjustment", "decision", "compatibility", "restoration", "transition"} {
		t.Run(boundary, func(t *testing.T) {
			repo, id, order := fin005Admitted(t)
			action, party := "refund_supplier", "supplier"
			table, condition := "disputes", `NEW.id=`+pq.QuoteLiteral(id)+` AND NEW.status='pending' AND NEW.resolution_action IS NOT NULL`
			switch boundary {
			case "obligation":
				table, condition = "order_refund_obligations", `NEW.business_id=`+pq.QuoteLiteral(id)
			case "adjustment":
				table, condition = "ledger_entries", `NEW.related_order_or_dispute=`+pq.QuoteLiteral(id)
			case "decision":
				table, condition = "dispute_financial_decisions", `NEW.dispute_id=`+pq.QuoteLiteral(id)
			case "compatibility":
				condition = `NEW.id=` + pq.QuoteLiteral(id) + ` AND NEW.status='resolved'`
			case "restoration":
				action, party = "release_to_seller", "seller"
				table, condition = "orders", `NEW.id=`+pq.QuoteLiteral(order)+` AND NEW.status='shipped'`
			case "transition":
				action, party = "release_to_seller", "seller"
				table, condition = "dispute_order_transitions", `NEW.dispute_id=`+pq.QuoteLiteral(id)
			}
			fin005Fault(t, repo.db, table, condition)
			_, err := repo.ResolveDisputeWithOutcome(context.Background(), id, DisputeResolutionInput{Action: action, ResponsibleParty: party, ActorUserID: "fixture"})
			if err == nil || !strings.Contains(err.Error(), "FIN005 injected fault") {
				t.Fatalf("boundary did not fail: %v", err)
			}
			var clean bool
			err = repo.db.QueryRow(`SELECT d.status='pending' AND d.resolution_action IS NULL AND o.status='delivery_disputed' AND NOT o.profit_released AND NOT EXISTS(SELECT 1 FROM dispute_financial_decisions WHERE dispute_id=d.id) AND NOT EXISTS(SELECT 1 FROM order_refund_obligations WHERE business_id=d.id) AND NOT EXISTS(SELECT 1 FROM ledger_entries WHERE related_order_or_dispute=d.id) FROM disputes d JOIN orders o ON o.id=d.order_id WHERE d.id=$1`, id).Scan(&clean)
			if err != nil || !clean {
				t.Fatalf("partial commit clean=%v %v", clean, err)
			}
		})
	}
}
func TestFIN005StoredClaimExpectationAndConflictingReplay(t *testing.T) {
	repo, id, _ := fin005Admitted(t)
	for _, amount := range []int64{0, -1, 1, 9007199254740992} {
		if _, err := repo.ResolveDisputeWithOutcome(context.Background(), id, DisputeResolutionInput{Action: "refund_supplier", ResponsibleParty: "supplier", ActorUserID: "actor", ExpectedClaimAmount: &amount}); !errors.Is(err, ErrDisputeInput) {
			t.Fatalf("expectation %d: %v", amount, err)
		}
	}
	out, err := repo.ResolveDisputeWithOutcome(context.Background(), id, DisputeResolutionInput{Action: "refund_supplier", ResponsibleParty: "تأمین‌کننده کالا", ActorUserID: "actor"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ClaimAmount != 2000000 || out.ActorUserID != "actor" || out.Refund.AmountToman != 2000000 || out.Refund.Status == "succeeded" {
		t.Fatalf("untruthful projection %+v", out)
	}
	var adjustmentTarget, adjustmentType string
	if err = repo.db.QueryRow(`SELECT target_account,target_type FROM ledger_entries WHERE related_order_or_dispute=$1`, id).Scan(&adjustmentTarget, &adjustmentType); err != nil || adjustmentTarget != "تأمین‌کننده کالا" || adjustmentType != "supplier" {
		t.Fatalf("compatibility target %q %q %v", adjustmentTarget, adjustmentType, err)
	}
	replay, err := repo.ResolveDisputeWithOutcome(context.Background(), id, DisputeResolutionInput{Action: "refund_supplier", ResponsibleParty: "supplier", ActorUserID: "retry-actor"})
	if err != nil || !replay.Replay || replay.ActorUserID != "actor" {
		t.Fatalf("replay %+v %v", replay, err)
	}
	if _, err = repo.ResolveDisputeWithOutcome(context.Background(), id, DisputeResolutionInput{Action: "release_to_seller", ResponsibleParty: "seller", ActorUserID: "actor"}); !errors.Is(err, ErrDisputeConflict) {
		t.Fatalf("conflict %v", err)
	}
}
func TestFIN005OldWritersAndImmutableEvidence(t *testing.T) {
	repo, id, order := fin005Admitted(t)
	for _, q := range []string{
		`UPDATE disputes SET status='resolved',resolution_action='release_to_seller' WHERE id=` + pq.QuoteLiteral(id),
		`DELETE FROM disputes WHERE id=` + pq.QuoteLiteral(id),
		`UPDATE orders SET profit_released=TRUE WHERE id=` + pq.QuoteLiteral(order),
		`UPDATE dispute_order_evidence SET prior_status='delivered_confirmed' WHERE dispute_id=` + pq.QuoteLiteral(id),
		`DELETE FROM dispute_order_evidence WHERE dispute_id=` + pq.QuoteLiteral(id),
	} {
		if _, err := repo.db.Exec(q); err == nil {
			t.Fatalf("unmarked mutation accepted: %s", q)
		}
	}
	tx, err := repo.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = markDisputeWriterTx(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.db.Exec(`UPDATE disputes SET resolution_action='refund_supplier' WHERE id=$1`, id); err == nil {
		t.Fatal("transaction marker leaked")
	}
	if _, err = repo.db.Exec(`UPDATE disputes SET reason='unrelated note' WHERE id=$1`, id); err != nil {
		t.Fatalf("unrelated note fenced %v", err)
	}
	tx, err = repo.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = markDisputeWriterTx(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE disputes SET status='resolved',resolution_action='release_to_seller',responsible_party='seller',resolution_date=NOW() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err == nil {
		t.Fatal("marker authorized decisionless verdict")
	}
}
func TestFIN005ConcurrentVerdictsSerialize(t *testing.T) {
	for _, different := range []bool{false, true} {
		t.Run(fmt.Sprint(different), func(t *testing.T) {
			repo, id, order := fin005Admitted(t)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			blocker, err := repo.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			if _, err = blocker.ExecContext(ctx, `SELECT id FROM orders WHERE id=$1 FOR UPDATE`, order); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 2)
			for i := 0; i < 2; i++ {
				action, party := "refund_supplier", "supplier"
				if different && i == 1 {
					action, party = "refund_carrier", "carrier"
				}
				go func() {
					_, e := repo.ResolveDisputeWithOutcome(ctx, id, DisputeResolutionInput{Action: action, ResponsibleParty: party, ActorUserID: "actor"})
					result <- e
				}()
			}
			// pg_stat_activity is an observable barrier: both actual writers queued.
			for {
				var n int
				if err = repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT id FROM orders WHERE id=$1 FOR UPDATE%'`).Scan(&n); err != nil {
					t.Fatal(err)
				}
				if n >= 2 {
					break
				}
				if ctx.Err() != nil {
					t.Fatal(ctx.Err())
				}
			}
			if err = blocker.Commit(); err != nil {
				t.Fatal(err)
			}
			success, conflict := 0, 0
			for i := 0; i < 2; i++ {
				err = <-result
				if err == nil {
					success++
				} else if errors.Is(err, ErrDisputeConflict) {
					conflict++
				} else {
					t.Fatal(err)
				}
			}
			if different && (success != 1 || conflict != 1) || !different && success != 2 {
				t.Fatalf("results success=%d conflict=%d", success, conflict)
			}
			var n int
			if err = repo.db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1`, id).Scan(&n); err != nil || n != 1 {
				t.Fatalf("multiple money effects %d %v", n, err)
			}
		})
	}
}

func TestFIN005ReleaseFaultBoundariesRollbackTogether(t *testing.T) {
	for _, boundary := range []string{"release_flag", "credit", "debit", "referral", "transition"} {
		t.Run(boundary, func(t *testing.T) {
			repo, id, order := fin005Admitted(t)
			opsExec(t, repo.db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',dropshipper_profit=7000 WHERE id=$1`, order)
			var store string
			if err := repo.db.QueryRow(`SELECT store_id FROM orders WHERE id=$1`, order).Scan(&store); err != nil {
				t.Fatal(err)
			}
			opsExec(t, repo.db, `INSERT INTO referral_bonuses(id,referrer_store_id,referred_store_id,amount,status) VALUES($1,$2,$2,1000,'locked')`, uuid.NewString(), store)
			table, condition := "orders", `NEW.id=`+pq.QuoteLiteral(order)+` AND NEW.profit_released`
			switch boundary {
			case "credit", "debit":
				table, condition = "ledger_entries", `NEW.related_order_or_dispute=`+pq.QuoteLiteral(order)+` AND NEW.type=`+pq.QuoteLiteral(boundary)
			case "referral":
				table, condition = "referral_bonuses", `NEW.referred_store_id=`+pq.QuoteLiteral(store)+` AND NEW.status='released'`
			case "transition":
				table, condition = "dispute_order_transitions", `NEW.dispute_id=`+pq.QuoteLiteral(id)+` AND NEW.reason='released'`
			}
			fin005Fault(t, repo.db, table, condition)
			_, err := repo.ResolveDisputeWithOutcome(context.Background(), id, DisputeResolutionInput{Action: "release_to_seller", ResponsibleParty: "seller", ActorUserID: "fixture"})
			if err == nil || !strings.Contains(err.Error(), "FIN005 injected fault") {
				t.Fatalf("release boundary did not fail %v", err)
			}
			var clean bool
			err = repo.db.QueryRow(`SELECT d.status='pending' AND NOT o.profit_released AND NOT o.referral_commission_released AND NOT EXISTS(SELECT 1 FROM ledger_entries l WHERE l.related_order_or_dispute=o.id) AND NOT EXISTS(SELECT 1 FROM dispute_financial_decisions f WHERE f.dispute_id=d.id) AND EXISTS(SELECT 1 FROM referral_bonuses b WHERE b.referred_store_id=o.store_id AND b.status='locked') FROM disputes d JOIN orders o ON o.id=d.order_id WHERE d.id=$1`, id).Scan(&clean)
			if err != nil || !clean {
				t.Fatalf("partial release commit clean=%v %v", clean, err)
			}
		})
	}
}
func TestFIN005AdmissionFaultsRollbackEvidenceAndHold(t *testing.T) {
	for _, boundary := range []string{"evidence", "hold"} {
		t.Run(boundary, func(t *testing.T) {
			db, f, _, order := fin003VerifiedPaidOrder(t)
			opsExec(t, db, `UPDATE orders SET status='shipped' WHERE id=$1`, order)
			id := "FIN005-" + uuid.NewString()
			table, condition := "dispute_order_evidence", `NEW.dispute_id=`+pq.QuoteLiteral(id)
			if boundary == "hold" {
				table, condition = "orders", `NEW.id=`+pq.QuoteLiteral(order)+` AND NEW.status='delivery_disputed'`
			}
			fin005Fault(t, db, table, condition)
			err := NewFinanceRepository(db).CreateDispute(context.Background(), domain.Dispute{ID: id, OrderID: order, Type: "delivery_undelivered", ProductTitle: "fixture", ClaimAmount: 100, Reason: "fixture", StoreID: f.store})
			if err == nil || !strings.Contains(err.Error(), "FIN005 injected fault") {
				t.Fatal(err)
			}
			var clean bool
			if err = db.QueryRow(`SELECT status='shipped' AND NOT EXISTS(SELECT 1 FROM disputes WHERE id=$2) AND NOT EXISTS(SELECT 1 FROM dispute_order_evidence WHERE dispute_id=$2) FROM orders WHERE id=$1`, order, id).Scan(&clean); err != nil || !clean {
				t.Fatalf("partial admission %v %v", clean, err)
			}
		})
	}
}

func TestFIN005FinancialEvidenceIsAppendOnly(t *testing.T) {
	repo, id, _ := fin005Admitted(t)
	if _, err := repo.ResolveDisputeWithOutcome(context.Background(), id, DisputeResolutionInput{Action: "release_to_seller", ResponsibleParty: "seller", ActorUserID: "actor"}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"dispute_order_evidence", "dispute_financial_decisions", "dispute_order_transitions"} {
		for _, op := range []string{"UPDATE", "DELETE"} {
			q := `DELETE FROM ` + table + ` WHERE dispute_id=$1`
			if op == "UPDATE" {
				q = `UPDATE ` + table + ` SET dispute_id=dispute_id WHERE dispute_id=$1`
			}
			if _, err := repo.db.Exec(q, id); err == nil || !strings.Contains(err.Error(), "immutable") {
				t.Fatalf("%s %s accepted/not immutable %v", op, table, err)
			}
		}
	}
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if _, _, err := NewOrderRepository(db).RefundCustomerReturn(context.Background(), ret, f.user); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE customer_return_financial_events SET amount=amount+1 WHERE return_id=$1`, `DELETE FROM customer_return_financial_events WHERE return_id=$1`, `UPDATE ledger_entries SET amount=amount+1 WHERE id IN (SELECT ledger_entry_id FROM customer_return_financial_events WHERE return_id=$1)`, `DELETE FROM ledger_entries WHERE id IN (SELECT ledger_entry_id FROM customer_return_financial_events WHERE return_id=$1)`} {
		if _, err := db.Exec(q, ret); err == nil {
			t.Fatalf("financial proof mutation accepted %s", q)
		}
	}
}
