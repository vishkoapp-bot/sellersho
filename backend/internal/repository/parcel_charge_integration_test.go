package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func parcelLegacyFixture(t *testing.T) *sql.DB {
	t.Helper()
	db := openFinanceBalanceDB(t)
	for _, q := range []string{
		`DROP TABLE IF EXISTS supplier_return_financial_events,supplier_return_parcels,supplier_return_requests CASCADE`,
		`CREATE TABLE supplier_return_requests(id TEXT PRIMARY KEY,supplier_id TEXT,status TEXT DEFAULT 'packed',
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			accepted_quantity integer NOT NULL DEFAULT 0,
			cancelled_by character varying(80),
			collection_method character varying(30),
			completed_at timestamp with time zone,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			failed_quantity integer NOT NULL DEFAULT 0,
			handover_at timestamp with time zone,
			idempotency_key character varying(150),
			inspected_at timestamp with time zone,
			inspection_status character varying(20),
			inventory_reference_id character varying(80),
			postal_charge bigint NOT NULL DEFAULT 0,
			product_id character varying(50),
			reason text,
			received_at timestamp with time zone,
			rejection_reason text,
			requested_quantity integer,
			reviewed_at timestamp with time zone,
			reviewed_by character varying(80),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			variant_id uuid,
			warehouse_bin_code character varying(30))`,
		`CREATE TABLE supplier_return_parcels(id TEXT PRIMARY KEY,return_request_id TEXT,final_charge BIGINT,payment_method TEXT,status TEXT DEFAULT 'draft'::character varying ,confirmed_by TEXT,updated_at TIMESTAMPTZ DEFAULT now() ,final_charge_source TEXT DEFAULT 'manual',final_charge_reference TEXT DEFAULT 'bill-1',
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			carrier_code character varying(50),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			created_by character varying(80),
			destination character varying(200),
			estimate_reference character varying(150),
			estimate_source character varying(20),
			estimated_charge bigint,
			handed_over_at timestamp with time zone,
			height_mm integer,
			idempotency_key character varying(150),
			length_mm integer,
			notes text,
			origin character varying(200),
			parcel_number integer,
			tracking_number character varying(150),
			weight_grams integer,
			width_mm integer)`,
		`CREATE TABLE supplier_return_financial_events(id TEXT PRIMARY KEY,return_request_id TEXT,parcel_id TEXT,supplier_id TEXT,operation TEXT,amount BIGINT,payment_method TEXT,ledger_entry_id TEXT,idempotency_key TEXT UNIQUE,reason TEXT,actor_user_id TEXT,event_identity TEXT UNIQUE,created_at TIMESTAMPTZ DEFAULT now(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			charge_component text)`,
		`INSERT INTO suppliers(id) VALUES('parcel-supplier')`,
		`INSERT INTO supplier_return_requests(id,supplier_id) VALUES('parcel-return','parcel-supplier')`,
		`INSERT INTO supplier_return_parcels(id,return_request_id,final_charge,payment_method,status) VALUES('parcel-one','parcel-return',120000,'sender_pays','draft')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func TestParcelChargeRejectsUnchargedStates(t *testing.T) {
	for _, state := range []string{"cancelled", "handed_over", "in_transit", "received", "confirmed"} {
		t.Run(state, func(t *testing.T) {
			db := parcelFixture(t)
			if _, err := db.Exec(`UPDATE supplier_return_parcels SET status=$1`, state); err != nil {
				t.Fatal(err)
			}
			err := NewSupplierRepository(db).ConfirmReturnParcelCharge(context.Background(), "parcel-return", "parcel-one", "actor", "key")
			var events int
			if e := db.QueryRow(`SELECT count(*) FROM supplier_return_financial_events`).Scan(&events); e != nil {
				t.Fatal(e)
			}
			if err == nil || events != 0 {
				t.Fatalf("uncharged %s accepted: err=%v debit events=%d", state, err, events)
			}
		})
	}
}
func TestParcelChargeReceiverAcceptedTerms(t *testing.T) {
	db := parcelFixture(t)
	if _, err := db.Exec(`UPDATE supplier_return_parcels SET payment_method='receiver_pays',final_charge=0`); err != nil {
		t.Fatal(err)
	}
	repo := NewSupplierRepository(db)
	if err := repo.ConfirmReturnParcelCharge(context.Background(), "parcel-return", "parcel-one", "actor", "key"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE supplier_return_parcels SET final_charge_reference='changed'`); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfirmReturnParcelCharge(context.Background(), "parcel-return", "parcel-one", "actor", "key"); err == nil {
		t.Fatal("receiver retry accepted changed bill reference without durable terms")
	}
}

func applyParcelMigration(t *testing.T, db *sql.DB) {
	t.Helper()
	// TEST-02 round 4: the standing supplier_return_financial_events
	// already carries charge_component (full demanded-set patch), so the
	// migration's bare ADD COLUMN fails with 42701. Drop it first (same
	// class as the checkout-attempt / sku-inventory / refund-fixture
	// pre-drops); the migration re-adds it with the same definition.
	if _, err := db.Exec(`ALTER TABLE supplier_return_financial_events DROP COLUMN IF EXISTS charge_component`); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../migrations/202611160001_parcel_charge_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(data)); err != nil {
		t.Fatal(err)
	}
}
func parcelCounts(t *testing.T, db *sql.DB, events, receipts int, amount int64) {
	t.Helper()
	var e, r int
	var bill, sum, ledger int64
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM supplier_return_financial_events),(SELECT count(*) FROM parcel_charge_receipts),final_charge,(SELECT COALESCE(sum(CASE WHEN operation='debit' THEN amount ELSE -amount END),0) FROM supplier_return_financial_events),(SELECT COALESCE(sum(CASE WHEN type='debit' THEN amount ELSE -amount END),0) FROM ledger_entries WHERE related_order_or_dispute='parcel-return') FROM supplier_return_parcels WHERE id='parcel-one'`).Scan(&e, &r, &bill, &sum, &ledger); err != nil {
		t.Fatal(err)
	}
	if e != events || r != receipts || bill != amount || sum != amount || ledger != amount {
		t.Fatalf("e=%d r=%d bill=%d eventsum=%d ledgersum=%d want %d/%d/%d", e, r, bill, sum, ledger, events, receipts, amount)
	}
	b, err := NewSupplierRepository(db).GetSettlementBalance(context.Background(), "parcel-supplier")
	if err != nil || b.Available != -amount {
		t.Fatalf("canonical balance=%+v err=%v want %d", b, err, -amount)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var locks TransactionLockSet
	if err = LockAccountsTx(context.Background(), tx, &locks, FinancialAccount{Type: "supplier", ID: "parcel-supplier"}); err != nil {
		t.Fatal(err)
	}
	projected, err := BalanceTx(context.Background(), tx, FinancialAccount{Type: "supplier", ID: "parcel-supplier"})
	if err != nil || projected != -amount {
		t.Fatalf("BalanceTx=%d err=%v want%d", projected, err, -amount)
	}

}
func TestParcelChargeCorrectionChain(t *testing.T) {
	db := parcelFixture(t)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "original"); err != nil {
		t.Fatal(err)
	}
	var original string
	if err := db.QueryRow(`SELECT row_to_json(e)::text FROM supplier_return_financial_events e`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	input := ParcelChargeCorrectionInput{ExpectedAmount: 120000, FinalAmount: 150000, Source: "api", Reference: "bill2", Reason: "carrier correction"}
	first, err := repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "correction1", input)
	if err != nil {
		t.Fatal(err)
	}
	secondInput := ParcelChargeCorrectionInput{ExpectedAmount: 150000, FinalAmount: 90000, Source: "manual", Reference: "bill3", Reason: "carrier rebate"}
	if _, err = repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "correction2", secondInput); err != nil {
		t.Fatal(err)
	}
	replay, err := repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "different actor", "correction1", input)
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatalf("immutable replay %+v %+v %v", first, replay, err)
	}
	if err = repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "confirm-new"); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "stale", input); !errors.Is(err, ErrFinanceCommandConflict) {
		t.Fatalf("stale %v", err)
	}
	input.Reference = "different"
	if _, err = repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "correction1", input); !errors.Is(err, ErrFinanceCommandConflict) {
		t.Fatalf("changed payload %v", err)
	}
	parcelCounts(t, db, 3, 3, 90000)
	var after string
	if err = db.QueryRow(`SELECT row_to_json(e)::text FROM supplier_return_financial_events e WHERE charge_component='original'`).Scan(&after); err != nil || original != after {
		t.Fatalf("original mutated %v", err)
	}

	for _, component := range []string{"'original'", "NULL"} {
		_, err = db.Exec(`INSERT INTO supplier_return_financial_events(id,return_request_id,parcel_id,supplier_id,operation,amount,payment_method,ledger_entry_id,idempotency_key,reason,actor_user_id,charge_component) VALUES('duplicate-original','parcel-return','parcel-one','parcel-supplier','debit',1,'sender_pays','new-leg','duplicate-key','duplicate','actor',` + component + `)`)
		if err == nil {
			t.Fatal("database accepted duplicate/unclassified original")
		}
	}
	for _, q := range []string{`UPDATE parcel_charge_receipts SET reason='tampered'`, `DELETE FROM parcel_charge_keys`, `UPDATE supplier_return_financial_events SET amount=1`, `DELETE FROM parcel_charge_receipts`} {
		if _, err = db.Exec(q); err == nil {
			t.Fatalf("evidence mutable: %s", q)
		}
	}
	if _, err = db.Exec(`UPDATE supplier_return_requests SET status='cancelled';UPDATE supplier_return_parcels SET status='cancelled'`); err != nil {
		t.Fatal(err)
	}
	if err = repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "original"); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "correction2", secondInput); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "cancel-new", ParcelChargeCorrectionInput{90000, 80000, "api", "bill4", "reason"}); err == nil {
		t.Fatal("new correction after cancellation")
	}
}
func TestParcelChargeConcurrent(t *testing.T) {
	db := parcelFixture(t)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", fmt.Sprintf("confirm-%d", i))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	parcelCounts(t, db, 1, 1, 120000)
	errs = make(chan error, 3)
	for _, amount := range []int64{130000, 140000} {
		wg.Add(1)
		go func(a int64) {
			defer wg.Done()
			_, err := repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", fmt.Sprintf("correct-%d", a), ParcelChargeCorrectionInput{120000, a, "manual", "bill2", "reason"})
			errs <- err
		}(amount)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "during-correction")
	}()
	wg.Wait()
	close(errs)
	conflicts := 0
	for err := range errs {
		if errors.Is(err, ErrFinanceCommandConflict) {
			conflicts++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if conflicts != 1 {
		t.Fatalf("conflicts %d", conflicts)
	}
	var amount int64
	if err := db.QueryRow(`SELECT final_charge FROM supplier_return_parcels`).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	parcelCounts(t, db, 2, 2, amount)
}
func TestParcelChargeAtomicFailures(t *testing.T) {
	for _, table := range []string{"ledger_entries", "supplier_return_financial_events", "supplier_return_parcels", "parcel_charge_receipts", "parcel_charge_keys"} {
		t.Run(table, func(t *testing.T) {
			db := parcelFixture(t)
			repo := NewSupplierRepository(db)
			ctx := context.Background()
			if _, err := db.Exec(`CREATE FUNCTION reject_charge() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure';END $$; CREATE TRIGGER reject_charge BEFORE INSERT OR UPDATE ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION reject_charge()`); err != nil {
				t.Fatal(err)
			}
			if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "original"); err == nil {
				t.Fatal("injected confirmation succeeded")
			}
			var n int
			var state string
			if err := db.QueryRow(`SELECT (SELECT count(*) FROM supplier_return_financial_events)+(SELECT count(*) FROM parcel_charge_receipts)+(SELECT count(*) FROM ledger_entries WHERE related_order_or_dispute='parcel-return'),status FROM supplier_return_parcels`).Scan(&n, &state); err != nil || n != 0 || state != "draft" {
				t.Fatalf("partial confirmation %d %s %v", n, state, err)
			}
			if _, err := db.Exec(`DROP TRIGGER reject_charge ON ` + table); err != nil {
				t.Fatal(err)
			}
			if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "original"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TRIGGER reject_charge BEFORE INSERT OR UPDATE ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION reject_charge()`); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "correction", ParcelChargeCorrectionInput{120000, 100000, "manual", "bill2", "reason"}); err == nil {
				t.Fatal("injected correction succeeded")
			}
			parcelCounts(t, db, 1, 1, 120000)
		})
	}
}
func TestParcelChargePrincipalRevalidation(t *testing.T) {
	db := parcelFixture(t)
	if _, err := db.Exec(`INSERT INTO suppliers(id) VALUES('changed')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var set TransactionLockSet
	if err = LockAccountsTx(context.Background(), tx, &set, FinancialAccount{Type: "supplier", ID: "parcel-supplier"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- NewSupplierRepository(db).ConfirmReturnParcelCharge(context.Background(), "parcel-return", "parcel-one", "actor", "key")
	}()
	waitFinanceAccountGuard(t, db, "suppliers")
	if _, err = db.Exec(`UPDATE supplier_return_requests SET supplier_id='changed'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if !errors.Is(err, ErrFinanceAccountChanged) {
			t.Fatal(err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("blocked")
	}
}

func parcelFixture(t *testing.T) *sql.DB {
	db := parcelLegacyFixture(t)
	applyParcelMigration(t, db)
	return db
}
func TestParcelChargeBoundaries(t *testing.T) {
	for _, mutation := range []string{"UPDATE supplier_return_requests SET status='cancelled'", "UPDATE supplier_return_requests SET status='rejected'", "UPDATE supplier_return_parcels SET final_charge=0", "UPDATE supplier_return_parcels SET final_charge=-1", "UPDATE supplier_return_parcels SET final_charge=9007199254740992", "UPDATE supplier_return_parcels SET final_charge=NULL"} {
		t.Run(mutation, func(t *testing.T) {
			db := parcelFixture(t)
			if _, err := db.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			if err := NewSupplierRepository(db).ConfirmReturnParcelCharge(context.Background(), "parcel-return", "parcel-one", "actor", "key"); err == nil {
				t.Fatal("invalid initial terms accepted")
			}
		})
	}
	for _, change := range []string{"final_charge=120001", "payment_method='receiver_pays'", "final_charge_source='api'", "final_charge_reference='changed'"} {
		t.Run(change, func(t *testing.T) {
			db := parcelFixture(t)
			repo := NewSupplierRepository(db)
			ctx := context.Background()
			if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "key"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE supplier_return_parcels SET ` + change); err != nil {
				t.Fatal(err)
			}
			if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "key2"); !errors.Is(err, ErrFinanceCommandConflict) {
				t.Fatalf("changed accepted terms %v", err)
			}
		})
	}
	for _, amount := range []string{"NULL", "0", "120000"} {
		t.Run("receiver-"+amount, func(t *testing.T) {
			db := parcelFixture(t)
			if _, err := db.Exec(`UPDATE supplier_return_parcels SET payment_method='receiver_pays',final_charge=` + amount); err != nil {
				t.Fatal(err)
			}
			repo := NewSupplierRepository(db)
			ctx := context.Background()
			for _, key := range []string{"key", "new-key"} {
				if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", key); err != nil {
					t.Fatal(err)
				}
			}
			var events, receipts int
			if err := db.QueryRow(`SELECT (SELECT count(*) FROM supplier_return_financial_events),(SELECT count(*) FROM parcel_charge_receipts)`).Scan(&events, &receipts); err != nil || events != 0 || receipts != 1 {
				t.Fatalf("receiver effects %d/%d %v", events, receipts, err)
			}
			if _, err := repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "correction", ParcelChargeCorrectionInput{0, 1, "manual", "bill2", "reason"}); err == nil {
				t.Fatal("receiver corrected")
			}
		})
	}
	db := parcelFixture(t)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "key"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO supplier_return_parcels(id,return_request_id,final_charge,payment_method,status) VALUES('parcel-two','parcel-return',120000,'sender_pays','draft')`); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "alias"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"key", "alias"} {
		if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-two", "actor", key); !errors.Is(err, ErrFinanceCommandConflict) {
			t.Fatalf("cross parcel key %v", err)
		}
	}
	for _, key := range []string{"", strings.Repeat("x", 129)} {
		if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-two", "actor", key); !errors.Is(err, ErrParcelChargeInput) {
			t.Fatal(err)
		}
	}
	if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-two", "", "valid"); !errors.Is(err, ErrParcelChargeInput) {
		t.Fatal(err)
	}
}
func TestParcelChargeMigration(t *testing.T) {
	up, err := os.ReadFile("../../migrations/202611160001_parcel_charge_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/202611160001_parcel_charge_identity.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("clean-down-up", func(t *testing.T) {
		db := parcelFixture(t)
		if _, err := db.Exec(string(down)); err != nil {
			t.Fatal(err)
		}
		applyParcelMigration(t, db)
	})
	for _, change := range []string{"", `UPDATE supplier_return_parcels SET status='handed_over'`, `UPDATE supplier_return_parcels SET status='draft'`, `UPDATE supplier_return_requests SET status='cancelled'`, `UPDATE supplier_return_parcels SET confirmed_by=NULL`, `UPDATE supplier_return_parcels SET final_charge=1`, `UPDATE ledger_entries SET amount=1`, `UPDATE ledger_entries SET target_account='other'`, `UPDATE supplier_return_financial_events SET supplier_id='other'`, `DELETE FROM ledger_entries`, `INSERT INTO supplier_return_financial_events SELECT 'duplicate',return_request_id,parcel_id,supplier_id,operation,amount,payment_method,ledger_entry_id,'duplicate-key',reason,actor_user_id,NULL,created_at FROM supplier_return_financial_events`} {
		t.Run("legacy-"+change, func(t *testing.T) {
			db := parcelLegacyFixture(t)
			if _, err := db.Exec(`UPDATE supplier_return_parcels SET status='confirmed',confirmed_by='actor'; INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) VALUES('legacy-ledger','2026-01-01','parcel-supplier','supplier','debit',120000,'carrier','actor','parcel-return'); INSERT INTO supplier_return_financial_events(id,return_request_id,parcel_id,supplier_id,operation,amount,payment_method,ledger_entry_id,idempotency_key,reason,actor_user_id,created_at) VALUES('legacy-event','parcel-return','parcel-one','parcel-supplier','debit',120000,'sender_pays','legacy-ledger','legacy-key','carrier','actor','2026-01-01')`); err != nil {
				t.Fatal(err)
			}
			if change != "" {
				if _, err := db.Exec(change); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			// TEST-02 round 4: the standing table already carries charge_component (full demanded-set patch); drop it first so the migration's bare ADD COLUMN does not fail with 42701 (same class as applyParcelMigration).
			if _, err = tx.Exec(`ALTER TABLE supplier_return_financial_events DROP COLUMN IF EXISTS charge_component`); err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(string(up))
			if change != "" {
				if err == nil {
					tx.Rollback()
					t.Fatal("ambiguous migration succeeded")
				}
				tx.Rollback()
				var exists bool
				if err = db.QueryRow(`SELECT to_regclass('parcel_charge_receipts') IS NOT NULL`).Scan(&exists); err != nil || exists {
					t.Fatalf("partial migration ddl %v %v", exists, err)
				}
				var id string
				if err = db.QueryRow(`SELECT id FROM supplier_return_financial_events WHERE id='legacy-event'`).Scan(&id); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var event, key string
			var created time.Time
			if err = db.QueryRow(`SELECT event_id,client_key,created_at FROM parcel_charge_receipts`).Scan(&event, &key, &created); err != nil || event != "legacy-event" || key != "legacy-key" || created.Year() != 2026 || created.Month() != 1 {
				t.Fatalf("legacy identity %s %s %v %v", event, key, created, err)
			}
			if err = NewSupplierRepository(db).ConfirmReturnParcelCharge(context.Background(), "parcel-return", "parcel-one", "actor", "replay"); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(string(down)); err == nil {
				t.Fatal("down removed evidence")
			}
		})
	}
}

func TestParcelChargeParentRevalidation(t *testing.T) {
	db := parcelFixture(t)
	if _, err := db.Exec(`INSERT INTO supplier_return_requests(id,supplier_id,status) VALUES('other-return','parcel-supplier','packed')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var set TransactionLockSet
	if err = LockAccountsTx(context.Background(), tx, &set, FinancialAccount{Type: "supplier", ID: "parcel-supplier"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- NewSupplierRepository(db).ConfirmReturnParcelCharge(context.Background(), "parcel-return", "parcel-one", "actor", "key")
	}()
	waitFinanceAccountGuard(t, db, "suppliers")
	if _, err = db.Exec(`UPDATE supplier_return_parcels SET return_request_id='other-return'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("changed parent admitted %v", err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM supplier_return_financial_events`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial effect", err, count)
	}
}
func TestParcelChargeSameCorrectionConcurrent(t *testing.T) {
	db := parcelFixture(t)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "key"); err != nil {
		t.Fatal(err)
	}
	input := ParcelChargeCorrectionInput{120000, 0, "manual", "bill2", "waived carrier charge"}
	var wg sync.WaitGroup
	results := make(chan *ParcelChargeReceipt, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "same-correction", input)
			results <- v
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first *ParcelChargeReceipt
	for v := range results {
		if first == nil {
			first = v
		} else if !reflect.DeepEqual(first, v) {
			t.Fatal("different replay receipts")
		}
	}
	parcelCounts(t, db, 2, 2, 0)
	if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "confirm-zero"); err != nil {
		t.Fatal(err)
	}
}
func TestParcelChargeRequiresOneStateTransition(t *testing.T) {
	db := parcelFixture(t)
	if _, err := db.Exec(`CREATE FUNCTION suppress_charge() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL;END $$;CREATE TRIGGER suppress_charge BEFORE UPDATE ON supplier_return_parcels FOR EACH ROW EXECUTE FUNCTION suppress_charge()`); err != nil {
		t.Fatal(err)
	}
	if err := NewSupplierRepository(db).ConfirmReturnParcelCharge(context.Background(), "parcel-return", "parcel-one", "actor", "key"); !errors.Is(err, ErrParcelChargeState) {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM supplier_return_financial_events)+(SELECT count(*) FROM ledger_entries WHERE related_order_or_dispute='parcel-return')+(SELECT count(*) FROM parcel_charge_receipts)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial effect", count, err)
	}
}
func TestParcelChargeAuditCompatibility(t *testing.T) {
	audit, err := os.ReadFile("../../../scripts/audit-parcel-charges.sql")
	if err != nil {
		t.Fatal(err)
	}
	db := parcelLegacyFixture(t)
	if _, err = db.Exec(string(audit)); err != nil {
		t.Fatal("pre-migration audit", err)
	}
	applyParcelMigration(t, db)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	if err = repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "original"); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "correction", ParcelChargeCorrectionInput{120000, 90000, "manual", "bill2", "reason"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(audit)); err != nil {
		t.Fatal("post-migration audit", err)
	}
}

func TestParcelChargeUnicodeReferenceBound(t *testing.T) {
	db := parcelFixture(t)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	if err := repo.ConfirmReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "original"); err != nil {
		t.Fatal(err)
	}
	input := ParcelChargeCorrectionInput{120000, 90000, "manual", strings.Repeat("ب", 150), "reason"}
	if _, err := repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "unicode", input); err != nil {
		t.Fatal(err)
	}
	input.ExpectedAmount = 90000
	input.FinalAmount = 80000
	input.Reference += "ب"
	if _, err := repo.CorrectReturnParcelCharge(ctx, "parcel-return", "parcel-one", "actor", "too-long", input); !errors.Is(err, ErrParcelChargeInput) {
		t.Fatal(err)
	}
}
