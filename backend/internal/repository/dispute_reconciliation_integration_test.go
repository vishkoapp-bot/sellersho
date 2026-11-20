package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
	"time"
)

// FIN-013 §1 d — synchronization note (P5, FIN-005 probe
// TestAuditFinanceRejectedDisputeReleasesSeller): sequential
// disposable-DB assertions; verdict + release eligibility are
// reconciled in one transaction (reconcileDisputeEligibilityTx), so
// no wait or barrier is needed — determinism comes from the atomic
// verdict+release, not timing.

func TestAuditFinanceRejectedDisputeReleasesSeller(t *testing.T)  { fin005ReviewedRecovery(t, false) }
func TestFIN005ReviewedLegacyRecoveryAndDuplicate(t *testing.T)   { fin005ReviewedRecovery(t, false) }
func TestFIN005MissingReviewedProvenanceRemainsHeld(t *testing.T) { fin005ReviewedRecovery(t, true) }
func fin005ReviewedRecovery(t *testing.T, ambiguous bool) {
	t.Helper()
	db := notifyWritersTestDB(t)
	customer, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "review-order", "REVIEW", customer, "delivery_disputed", "paid")
	opsExec(t, db, `CREATE TABLE audit_logs(id TEXT PRIMARY KEY,timestamp TIMESTAMPTZ DEFAULT NOW(),operator TEXT,action TEXT,category TEXT,details TEXT,created_at TIMESTAMPTZ DEFAULT NOW())`)
	opsExec(t, db, `UPDATE orders SET confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',disputed_at=NOW()-INTERVAL '2 days',dropshipper_profit=7000 WHERE id='review-order'`)
	opsExec(t, db, `INSERT INTO disputes(id,order_id,order_number,type,customer_name,product_title,claim_amount,reason,status,resolution_action,responsible_party,resolution_date) VALUES('review-dispute','review-order','REVIEW','delivery_undelivered','fixture','fixture',500000,'reviewed old verdict','resolved','release_to_seller','seller',NOW()-INTERVAL '1 day')`)
	var confirmed, ends, hold time.Time
	if err := db.QueryRow(`SELECT confirmed_at,return_window_ends_at,disputed_at FROM orders WHERE id='review-order'`).Scan(&confirmed, &ends, &hold); err != nil {
		t.Fatal(err)
	}
	installFIN005PrivateFixture(t, db)
	review := legacyDisputeReview{DisputeID: "review-dispute", OrderID: "review-order", PriorStatus: "delivered_confirmed", ConfirmedAt: confirmed, WindowEndsAt: ends, HoldAt: hold, SourceRecordIDs: []string{"retained-confirmation-window-hold"}}
	if !ambiguous {
		opsExec(t, db, `INSERT INTO audit_logs(id,operator,action,category,details) VALUES('retained-confirmation-window-hold','historical-auditor','retained physical facts','dispute','authoritative confirmed state, recorded original window and hold linkage')`)
	}
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `INSERT INTO audit_logs(id,operator,action,category,details) VALUES('approved-review','reviewer','FIN005 reviewed legacy evidence','dispute',$1)`, string(raw))
	repo := NewFinanceRepository(db)
	for i := 0; i < 2; i++ {
		out, err := repo.ReconcileReviewedDispute(context.Background(), "review-dispute", "approved-review", "reviewer")
		if err != nil {
			t.Fatal(err)
		}
		if ambiguous && out.SellerReleaseState != "needs_review" || !ambiguous && out.SellerReleaseState != "released" {
			t.Fatalf("review state %+v", out)
		}
	}
	var actualConfirmed, actualEnds time.Time
	var released bool
	var n int
	if err = db.QueryRow(`SELECT confirmed_at,return_window_ends_at,profit_released FROM orders WHERE id='review-order'`).Scan(&actualConfirmed, &actualEnds, &released); err != nil {
		t.Fatal(err)
	}
	if !actualConfirmed.Equal(confirmed) || !actualEnds.Equal(ends) || released == ambiguous {
		t.Fatal("review rewrote historical window/confirmation or minted release")
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute='review-order' AND operator='system:delivery-lock-releaser'`).Scan(&n); err != nil || !ambiguous && n != 2 || ambiguous && n != 0 {
		t.Fatalf("review effects %d %v", n, err)
	}
	if ambiguous {
		if err = db.QueryRow(`SELECT COUNT(*) FROM dispute_order_transitions WHERE reason='review_provenance_missing' AND result='needs_review'`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("review hold not durable %d %v", n, err)
		}
	}
}
func TestFIN005LateReturnClosureWorkerRecovery(t *testing.T) {
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',dropshipper_profit=7000 WHERE id=$1`, order)
	ret := uuid.NewString()
	fin005ReturnFixtureExec(t, db, order, `INSERT INTO customer_returns(id,order_id,customer_id,status,reason,warehouse_id,prepaid) VALUES($1,$2,$3,'approved','claim',$4,TRUE)`, ret, order, f.user, warehouse)
	repo := NewOrderRepository(db)
	if n, err := repo.ReleaseClosedReturnWindows(ctx); err != nil || n != 0 {
		t.Fatalf("held return released %d %v", n, err)
	}
	if err := repo.CancelCustomerReturn(ctx, ret); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.ReleaseClosedReturnWindows(ctx); err != nil || n != 1 {
		t.Fatalf("closed return did not recover %d %v", n, err)
	}
	if n, err := repo.ReleaseClosedReturnWindows(ctx); err != nil || n != 0 {
		t.Fatalf("restart duplicate %d %v", n, err)
	}
}

func TestFIN005TwoLegacyClaimsCloseWithoutDuplicateRelease(t *testing.T) {
	db := notifyWritersTestDB(t)
	customer, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "two-order", "TWO", customer, "delivery_disputed", "paid")
	opsExec(t, db, `CREATE TABLE audit_logs(id TEXT PRIMARY KEY,operator TEXT,action TEXT,category TEXT,details TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			timestamp timestamp with time zone NOT NULL DEFAULT now())`)
	opsExec(t, db, `UPDATE orders SET confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',disputed_at=NOW()-INTERVAL '2 days',dropshipper_profit=7000 WHERE id='two-order'`)
	opsExec(t, db, `INSERT INTO disputes(id,order_id,order_number,type,claim_amount,status,reason) VALUES('claim-one','two-order','TWO','delivery_undelivered',1000,'pending','legacy'),('claim-two','two-order','TWO','delivery_undelivered',1000,'pending','legacy')`)
	var confirmed, ends, hold time.Time
	if err := db.QueryRow(`SELECT confirmed_at,return_window_ends_at,disputed_at FROM orders WHERE id='two-order'`).Scan(&confirmed, &ends, &hold); err != nil {
		t.Fatal(err)
	}
	installFIN005PrivateFixture(t, db)
	opsExec(t, db, `INSERT INTO audit_logs VALUES('source','historical','retained physical state','dispute','confirmed state/window/hold authoritative record')`)
	repo := NewFinanceRepository(db)
	ctx := context.Background()
	for _, id := range []string{"claim-one", "claim-two"} {
		raw, err := json.Marshal(legacyDisputeReview{DisputeID: id, OrderID: "two-order", PriorStatus: "delivered_confirmed", ConfirmedAt: confirmed, WindowEndsAt: ends, HoldAt: hold, SourceRecordIDs: []string{"source"}})
		if err != nil {
			t.Fatal(err)
		}
		opsExec(t, db, `INSERT INTO audit_logs VALUES($1,'reviewer','FIN005 reviewed legacy evidence','dispute',$2)`, "review:"+id, string(raw))
		if _, err = repo.ReconcileReviewedDispute(ctx, id, "review:"+id, "reviewer"); err != nil {
			t.Fatal(err)
		}
	}
	first, err := repo.ResolveDisputeWithOutcome(ctx, "claim-one", DisputeResolutionInput{Action: "release_to_seller", ResponsibleParty: "seller", ActorUserID: "reviewer"})
	if err != nil || first.OrderStatus != "delivery_disputed" || first.SellerReleaseState != "held" {
		t.Fatalf("first erased second claim %+v %v", first, err)
	}
	if n, err := NewOrderRepository(db).ReleaseClosedReturnWindows(ctx); err != nil || n != 0 {
		t.Fatalf("worker erased second claim %d %v", n, err)
	}
	last, err := repo.ResolveDisputeWithOutcome(ctx, "claim-two", DisputeResolutionInput{Action: "release_to_seller", ResponsibleParty: "seller", ActorUserID: "reviewer"})
	if err != nil || last.SellerReleaseState != "released" {
		t.Fatalf("final claim failed recovery %+v %v", last, err)
	}
	var n int
	if err = db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute='two-order' AND operator='system:delivery-lock-releaser'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("duplicate release %d %v", n, err)
	}
}

func TestFIN005HeldCandidatesCannotStarveDueOrder(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for i := 0; i < 100; i++ {
		order, _ := opsOrder(t, db, f, true)
		opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '2 days',dropshipper_profit=1 WHERE id=$1`, order)
		fin005ReturnFixtureExec(t, db, order, `INSERT INTO customer_returns(id,order_id,customer_id,status,reason) VALUES($1,$2,$3,'requested','held fixture')`, uuid.NewString(), order, f.user)
	}
	eligible, _ := opsOrder(t, db, f, true)
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',dropshipper_profit=7000 WHERE id=$1`, eligible)
	n, err := NewOrderRepository(db).ReleaseClosedReturnWindows(ctx)
	if err != nil || n != 1 {
		t.Fatalf("100 earlier held rows starved eligible row n=%d %v", n, err)
	}
	var released bool
	if err = db.QueryRow(`SELECT profit_released FROM orders WHERE id=$1`, eligible).Scan(&released); err != nil || !released {
		t.Fatalf("eligible did not progress %v %v", released, err)
	}
}

func fin005UnexpiredReviewedFixture(t *testing.T) (*sql.DB, *FinanceRepository, context.Context, time.Time, time.Time) {
	t.Helper()
	db := notifyWritersTestDB(t)
	customer, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "replay-order", "REPLAY", customer, "delivery_disputed", "paid")
	opsExec(t, db, `CREATE TABLE audit_logs(id TEXT PRIMARY KEY,operator TEXT,action TEXT,category TEXT,details TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			timestamp timestamp with time zone NOT NULL DEFAULT now())`)
	opsExec(t, db, `UPDATE orders SET confirmed_at=NOW()-INTERVAL '8 days',disputed_at=NOW()-INTERVAL '1 day',dropshipper_profit=7000 WHERE id='replay-order'`)
	opsExec(t, db, `INSERT INTO disputes(id,order_id,order_number,type,claim_amount,status,resolution_action,responsible_party,resolution_date) VALUES('replay-claim','replay-order','REPLAY','delivery_undelivered',100,'resolved','release_to_seller','seller',NOW())`)
	installFIN005PrivateFixture(t, db)
	// Actual recorded original end; advance real DB clock to due, never rewrite it.
	opsExec(t, db, `UPDATE orders SET return_window_ends_at=clock_timestamp()+INTERVAL '3 seconds' WHERE id='replay-order'`)
	var confirmed, ends, hold time.Time
	if err := db.QueryRow(`SELECT confirmed_at,return_window_ends_at,disputed_at FROM orders WHERE id='replay-order'`).Scan(&confirmed, &ends, &hold); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(legacyDisputeReview{DisputeID: "replay-claim", OrderID: "replay-order", PriorStatus: "delivered_confirmed", ConfirmedAt: confirmed, WindowEndsAt: ends, HoldAt: hold, SourceRecordIDs: []string{"replay-source"}})
	opsExec(t, db, `INSERT INTO audit_logs VALUES('replay-source','historical','retained facts','dispute',$1),('replay-review','reviewer','FIN005 reviewed legacy evidence','dispute',$1)`, string(raw))
	repo := NewFinanceRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return db, repo, ctx, confirmed, ends
}
func TestFIN005ReviewedUnexpiredReplayRetainsWorkerEligibility(t *testing.T) {
	db, repo, ctx, confirmed, ends := fin005UnexpiredReviewedFixture(t)
	for i := 0; i < 2; i++ {
		out, err := repo.ReconcileReviewedDispute(ctx, "replay-claim", "replay-review", "reviewer")
		if err != nil || out.SellerReleaseState != "awaiting_window" {
			t.Fatalf("unexpired review %d: %+v %v", i, out, err)
		}
	}
	var quarantined int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dispute_order_transitions WHERE order_id='replay-order' AND reason='review_current_state_conflict'`).Scan(&quarantined); err != nil || quarantined != 0 {
		t.Fatalf("identical review replay quarantined restored order: %d %v", quarantined, err)
	}
	for {
		var due bool
		if err := db.QueryRowContext(ctx, `SELECT clock_timestamp()>=$1`, ends).Scan(&due); err != nil {
			t.Fatal(err)
		}
		if due {
			break
		}
	}
	for i := 0; i < 2; i++ {
		want := int64(1)
		if i == 1 {
			want = 0
		}
		if n, err := NewOrderRepository(db).ReleaseClosedReturnWindows(ctx); err != nil || n != want {
			t.Fatalf("expiry recovery %d n=%d want=%d %v", i, n, want, err)
		}
	}
	var exact bool
	if err := db.QueryRow(`SELECT confirmed_at=$2 AND return_window_ends_at=$3 AND profit_released AND (SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute='replay-order' AND operator='system:delivery-lock-releaser')=2 FROM orders WHERE id=$1`, "replay-order", confirmed, ends).Scan(&exact); err != nil || !exact {
		t.Fatalf("replay changed original facts/release pair %v %v", exact, err)
	}
}

func TestFIN005ReviewedReplayPreservesGenuineNewerConflicts(t *testing.T) {
	for _, mode := range []string{"newer_hold", "changed_window", "different_review"} {
		t.Run(mode, func(t *testing.T) {
			db, repo, ctx, confirmed, ends := fin005UnexpiredReviewedFixture(t)
			out, err := repo.ReconcileReviewedDispute(ctx, "replay-claim", "replay-review", "reviewer")
			if err != nil || out.SellerReleaseState != "awaiting_window" {
				t.Fatalf("initial review %+v %v", out, err)
			}
			reference := "replay-review"
			switch mode {
			case "newer_hold":
				fin005ReturnFixtureExec(t, db, "replay-order", `UPDATE orders SET status='delivery_disputed',disputed_at=clock_timestamp() WHERE id='replay-order'`)
			case "changed_window":
				opsExec(t, db, `UPDATE orders SET return_window_ends_at=return_window_ends_at+INTERVAL '1 day' WHERE id='replay-order'`)
			case "different_review":
				reference = "different-review"
				opsExec(t, db, `INSERT INTO audit_logs SELECT 'different-review',operator,action,category,details FROM audit_logs WHERE id='replay-review'`)
			}
			out, err = repo.ReconcileReviewedDispute(ctx, "replay-claim", reference, "reviewer")
			if err != nil || out.SellerReleaseState != "needs_review" {
				t.Fatalf("genuine conflict mistaken for replay %+v %v", out, err)
			}
			var protected bool
			if err = db.QueryRow(`SELECT confirmed_at=$1 AND NOT profit_released AND EXISTS(SELECT 1 FROM dispute_order_transitions WHERE order_id='replay-order' AND reason='review_current_state_conflict' AND result='needs_review') FROM orders WHERE id='replay-order'`, confirmed).Scan(&protected); err != nil || !protected {
				t.Fatalf("newer state conflict lost %v %v", protected, err)
			}
			if mode != "changed_window" {
				if err = db.QueryRow(`SELECT return_window_ends_at=$1 FROM orders WHERE id='replay-order'`, ends).Scan(&protected); err != nil || !protected {
					t.Fatal("replay rewrote original window", err)
				}
			}
		})
	}
}
