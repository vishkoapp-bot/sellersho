package repository

import (
	"context"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestFIN005ReturnEntitlementAndRelease(t *testing.T) {
	for _, mode := range []string{"verified", "historical", "active", "cancelled", "extra_reversal", "zero_components", "non_dropshipper"} {
		t.Run(mode, func(t *testing.T) {
			db, f, warehouse, order := fin003VerifiedPaidOrder(t)
			ctx := context.Background()
			repo := NewOrderRepository(db)
			opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',dropshipper_profit=9000000 WHERE id=$1`, order)
			if mode == "zero_components" {
				opsExec(t, db, `UPDATE order_items SET wholesale_cost=0,seller_margin=0 WHERE order_id=$1`, order)
			}
			if mode == "non_dropshipper" {
				opsExec(t, db, `UPDATE stores SET owner_type='central' WHERE id=$1`, f.store)
			}
			ret := fin003ReceivedReturn(t, db, f, warehouse, order)
			switch mode {
			case "historical":
				fin005ReturnFixtureExec(t, db, order, `UPDATE customer_returns SET status='refunded' WHERE id=$1`, ret)
			case "active":
			case "cancelled":
				fin005ReturnFixtureExec(t, db, order, `UPDATE customer_returns SET status='cancelled' WHERE id=$1`, ret)
			default:
				if _, _, err := repo.RefundCustomerReturn(ctx, ret, f.user); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "extra_reversal" {
				opsExec(t, db, `INSERT INTO customer_return_financial_events(id,return_id,return_item_id,order_id,supplier_id,component,operation,amount,ledger_entry_id,idempotency_key,reason,actor_user_id) SELECT $1,$2,ri.id,$3,$4,'product_cost','debit',1,NULL,$1,'bad extra evidence','fixture' FROM return_items ri WHERE ri.return_id=$2`, "CRF-"+uuid.NewString(), ret, order, f.supplier)
			}
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			released, err := repo.releaseClosedReturnWindow(ctx, order)
			if err != nil {
				t.Fatal(err)
			}
			want := mode == "verified" || mode == "cancelled" || mode == "zero_components" || mode == "non_dropshipper"
			if released != want {
				t.Fatalf("release=%v want=%v", released, want)
			}
			if again, err := repo.releaseClosedReturnWindow(ctx, order); err != nil || again {
				t.Fatalf("second release %v %v", again, err)
			}
			var n int
			if err = db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1 AND operator='system:delivery-lock-releaser'`, order).Scan(&n); err != nil || want && n != 2 || !want && n != 0 {
				t.Fatalf("release effects %d %v", n, err)
			}
		})
	}
}

func TestFIN005AdversarialCompletedReturnProof(t *testing.T) {
	for _, mode := range []string{"missing_obligation", "missing_reversal", "wrong_amount", "wrong_account", "wrong_ledger", "duplicate", "partial_principal", "pending", "failed", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			db, f, warehouse, order := fin003VerifiedPaidOrder(t)
			ctx := context.Background()
			ret := fin003ReceivedReturn(t, db, f, warehouse, order)
			repo := NewOrderRepository(db)
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err = lockOrderFinanceTx(ctx, tx, order, true); err != nil {
				t.Fatal(err)
			}
			if err = markDisputeWriterTx(ctx, tx); err != nil {
				t.Fatal(err)
			}
			amount := int64(10000000)
			if mode == "partial_principal" {
				amount = 5000000
			}
			reservation := RefundReservation{}
			if mode != "missing_obligation" {
				reservation, err = reserveOrderRefundTx(ctx, tx, order, "customer_return", ret, "wallet", f.user, amount, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode != "missing_reversal" {
				if mode == "wrong_amount" || mode == "wrong_account" || mode == "wrong_ledger" {
					var item string
					if err = tx.QueryRowContext(ctx, `SELECT id::text FROM return_items WHERE return_id=$1`, ret).Scan(&item); err != nil {
						t.Fatal(err)
					}
					reversalAmount, account := int64(1000000), f.supplier
					if mode == "wrong_amount" {
						reversalAmount++
					}
					if mode == "wrong_account" || mode == "wrong_ledger" {
						account = "unproven"
					}
					if err = insertReturnReversalTx(ctx, tx, ret, item, order, f.supplier, "", "product_cost", "supplier", account, reversalAmount, "bad-proof:"+ret, "fixture", f.user); err != nil {
						t.Fatal(err)
					}
				} else {
					if err = repo.recordReturnReversalsTx(ctx, tx, ret, order, f.user); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "duplicate" {
				if _, err = tx.ExecContext(ctx, `INSERT INTO customer_return_financial_events(id,return_id,return_item_id,order_id,supplier_id,store_id,component,operation,amount,ledger_entry_id,idempotency_key,reason,actor_user_id) SELECT $1,return_id,return_item_id,order_id,supplier_id,store_id,component,operation,amount,ledger_entry_id,$1,reason,actor_user_id FROM customer_return_financial_events WHERE return_id=$2 LIMIT 1`, "CRF-"+uuid.NewString(), ret); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "failed" || mode == "unknown" {
				for _, id := range reservation.IDs {
					if err = appendRefundOutcomeTx(ctx, tx, id, mode, "", "fixture outcome"); err != nil {
						t.Fatal(err)
					}
				}
			} else if mode != "pending" {
				if err = creditReservedWalletRefundsTx(ctx, tx, reservation.IDs); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = tx.ExecContext(ctx, `UPDATE customer_returns SET status='refunded' WHERE id=$1`, ret); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			tx, err = db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			proven, err := provenReturnedEntitlementTx(ctx, tx, order, ret)
			if err != nil || proven {
				t.Fatalf("invalid proof accepted %v %v", proven, err)
			}
		})
	}
}

func TestFIN005ReleaseStateMatrixAndHistory(t *testing.T) {
	for _, mode := range []string{"unexpired", "null_window", "invalid_window", "missing_confirmation", "requested", "approved", "shipped", "received", "rejected", "cancelled", "released_consistent", "released_inconsistent", "flag_ledger_mismatch"} {
		t.Run(mode, func(t *testing.T) {
			db, f, warehouse, order := fin003VerifiedPaidOrder(t)
			ctx := context.Background()
			opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',dropshipper_profit=7000 WHERE id=$1`, order)
			expected := "held"
			switch mode {
			case "unexpired":
				expected = "awaiting_window"
				opsExec(t, db, `UPDATE orders SET return_window_ends_at=NOW()+INTERVAL '1 hour' WHERE id=$1`, order)
			case "null_window":
				expected = "needs_review"
				opsExec(t, db, `UPDATE orders SET return_window_ends_at=NULL WHERE id=$1`, order)
			case "invalid_window":
				expected = "needs_review"
				opsExec(t, db, `UPDATE orders SET return_window_ends_at=confirmed_at-INTERVAL '1 hour' WHERE id=$1`, order)
			case "missing_confirmation":
				expected = "needs_review"
				opsExec(t, db, `UPDATE orders SET confirmed_at=NULL WHERE id=$1`, order)
			case "released_consistent", "released_inconsistent":
				expected = "released"
				if ok, err := NewOrderRepository(db).releaseClosedReturnWindow(ctx, order); err != nil || !ok {
					t.Fatalf("initial release %v %v", ok, err)
				}
				if mode == "released_inconsistent" {
					expected = "needs_review"
					opsExec(t, db, `INSERT INTO ledger_entries(id,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) SELECT $2,store_id,'seller','credit',1,'Staged profit release after return window closed','system:delivery-lock-releaser',id FROM orders WHERE id=$1`, order, "REL-"+uuid.NewString())
				}
			case "flag_ledger_mismatch":
				expected = "needs_review"
				fin005ReturnFixtureExec(t, db, order, `UPDATE orders SET profit_released=TRUE WHERE id=$1`, order)
			default:
				if mode == "rejected" || mode == "cancelled" {
					expected = "eligible"
				}
				ret := uuid.NewString()
				fin005ReturnFixtureExec(t, db, order, `INSERT INTO customer_returns(id,order_id,customer_id,status,reason,warehouse_id,prepaid) VALUES($1,$2,$3,$4,'fixture',$5,TRUE)`, ret, order, f.user, mode, warehouse)
			}
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			state, _, err := disputeReleaseStateTx(ctx, tx, order)
			if err != nil || state != expected {
				t.Fatalf("state %s want %s %v", state, expected, err)
			}
		})
	}
}

// Review-only physical/history rows must be filtered before LIMIT100 even when
// no dispute transition exists to quarantine them.
func TestFIN005ReviewOnlyCandidatesCannotStarveDueOrder(t *testing.T) {
	for _, mode := range []string{"invalid_window", "release_history"} {
		t.Run(mode, func(t *testing.T) {
			db := operationsDB(t)
			f := opsFixtureNew(t, db)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			var bad []string
			for i := 0; i < 100; i++ {
				order, _ := opsOrder(t, db, f, true)
				bad = append(bad, order)
				opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '2 days',dropshipper_profit=1 WHERE id=$1`, order)
				if mode == "invalid_window" {
					opsExec(t, db, `UPDATE orders SET confirmed_at=return_window_ends_at+INTERVAL '1 day' WHERE id=$1`, order)
				} else if i%2 == 0 {
					fin005ReturnFixtureExec(t, db, order, `UPDATE orders SET referral_commission_released=TRUE WHERE id=$1`, order)
				} else {
					opsExec(t, db, `INSERT INTO ledger_entries(id,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) VALUES($1,$2,'seller','credit',1,'Staged profit release after return window closed','fixture',$3)`, "REL-"+uuid.NewString(), f.store, order)
				}
			}
			eligible, _ := opsOrder(t, db, f, true)
			opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',dropshipper_profit=7000 WHERE id=$1`, eligible)
			repo := NewOrderRepository(db)
			if n, err := repo.ReleaseClosedReturnWindows(ctx); err != nil || n != 1 {
				t.Fatalf("100 earlier %s review rows starved eligible order: n=%d err=%v", mode, n, err)
			}
			if n, err := repo.ReleaseClosedReturnWindows(ctx); err != nil || n != 0 {
				t.Fatalf("repeated scan changed review-only rows: n=%d err=%v", n, err)
			}
			var effects int
			if err := db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1 AND operator='system:delivery-lock-releaser'`, eligible).Scan(&effects); err != nil || effects != 2 {
				t.Fatalf("eligible release pair %d %v", effects, err)
			}
			for _, order := range bad {
				var untouched bool
				if err := db.QueryRow(`SELECT NOT profit_released AND NOT EXISTS(SELECT 1 FROM dispute_order_transitions WHERE order_id=$1) AND NOT EXISTS(SELECT 1 FROM background_job_candidate_retry WHERE job='delivery-release' AND candidate=$1) AND NOT EXISTS(SELECT 1 FROM ledger_entries WHERE related_order_or_dispute=$1 AND operator='system:delivery-lock-releaser') FROM orders WHERE id=$1`, order).Scan(&untouched); err != nil || !untouched {
					t.Fatalf("review-only order changed or consumed retry budget %v %v", untouched, err)
				}
			}
		})
	}
}
