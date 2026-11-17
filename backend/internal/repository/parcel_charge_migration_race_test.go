package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"
)

// Poll server-side lock ownership, never elapsed sleep as evidence that preflight
// has run. The PID is our dedicated migration connection, blocked by our writer.
func waitParcelMigrationBlock(t *testing.T, db *sql.DB, migrationPID, writerPID int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		var relation, mode string
		err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted) AND $2=ANY(pg_blocking_pids($1)),COALESCE((SELECT relation::regclass::text FROM pg_locks WHERE pid=$1 AND NOT granted AND relation IS NOT NULL LIMIT 1),''),COALESCE((SELECT mode FROM pg_locks WHERE pid=$1 AND NOT granted LIMIT 1),'')`, migrationPID, writerPID).Scan(&blocked, &relation, &mode)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			t.Logf("owned barrier: migration blocked by writer on %s %s", relation, mode)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("migration did not reach owned writer lock barrier")
}

func seedLegacyParcelMoney(t *testing.T, tx *sql.Tx) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) VALUES('race-ledger',now(),'parcel-supplier','supplier','debit',120000,'carrier','actor','parcel-return')`,
		`INSERT INTO supplier_return_financial_events(id,return_request_id,parcel_id,supplier_id,operation,amount,payment_method,ledger_entry_id,idempotency_key,reason,actor_user_id) VALUES('race-event','parcel-return','parcel-one','parcel-supplier','debit',120000,'sender_pays','race-ledger','race-key','carrier','actor')`,
	} {
		if _, err := tx.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParcelChargeMigrationLiveUpgradeRace(t *testing.T) {
	up, err := os.ReadFile("../../migrations/202611160001_parcel_charge_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"old-invalid-state-debit", "old-discovery-before-account", "parent-cancellation", "parcel-handover"} {
		t.Run(scenario, func(t *testing.T) {
			db := parcelLegacyFixture(t)
			if strings.HasPrefix(scenario, "old-") {
				if _, err := db.Exec(`UPDATE supplier_return_parcels SET status='cancelled'`); err != nil {
					t.Fatal(err)
				}
			} else {
				setup, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer setup.Rollback()
				if _, err = setup.Exec(`UPDATE supplier_return_parcels SET status='confirmed',confirmed_by='actor'`); err != nil {
					t.Fatal(err)
				}
				seedLegacyParcelMoney(t, setup)
				if err = setup.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			writer, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback()
			var writerPID int
			if err = writer.QueryRow(`SELECT pg_backend_pid()`).Scan(&writerPID); err != nil {
				t.Fatal(err)
			}
			if scenario == "old-discovery-before-account" {
				// Old writer has retained the discovery read while migration
				// reserves supplier DDL mode and drains the return table.
				if _, err = writer.Exec(`SELECT supplier_id FROM supplier_return_requests WHERE id='parcel-return'`); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "old-invalid-state-debit" {
				// Exact old writer prefix and writes, including the defective zero-row UPDATE.
				for _, q := range []string{`SELECT id FROM suppliers WHERE id='parcel-supplier' FOR UPDATE`, `SELECT id FROM supplier_return_requests WHERE id='parcel-return' FOR UPDATE`, `SELECT id FROM supplier_return_parcels WHERE id='parcel-one' FOR UPDATE`} {
					if _, err = writer.Exec(q); err != nil {
						t.Fatal(err)
					}
				}
				seedLegacyParcelMoney(t, writer)
				result, err := writer.Exec(`UPDATE supplier_return_parcels SET status='confirmed',confirmed_by='actor' WHERE id='parcel-one' AND status IN ('draft','confirmed')`)
				if err != nil {
					t.Fatal(err)
				}
				rows, err := result.RowsAffected()
				if err != nil || rows != 0 {
					t.Fatalf("old writer affected=%d err=%v", rows, err)
				}
			} else {
				q := `UPDATE supplier_return_requests SET status='cancelled' WHERE id='parcel-return'`
				if scenario == "parcel-handover" {
					q = `UPDATE supplier_return_parcels SET status='handed_over' WHERE id='parcel-one'`
				}
				if _, err = writer.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			var migrationPID int
			if err = conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&migrationPID); err != nil {
				t.Fatal(err)
			}
			migration, err := conn.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer migration.Rollback()
			if _, err = migration.Exec(`SET LOCAL lock_timeout='8s';SET LOCAL statement_timeout='10s'`); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, e := migration.ExecContext(ctx, string(up))
				if e == nil {
					e = migration.Commit()
				} else {
					_ = migration.Rollback()
				}
				done <- e
			}()
			waitParcelMigrationBlock(t, db, migrationPID, writerPID)

			if scenario == "old-discovery-before-account" {
				for _, q := range []string{`SELECT id FROM suppliers WHERE id='parcel-supplier' FOR UPDATE`, `SELECT id FROM supplier_return_requests WHERE id='parcel-return' FOR UPDATE`, `SELECT id FROM supplier_return_parcels WHERE id='parcel-one' FOR UPDATE`} {
					if _, err = writer.ExecContext(ctx, q); err != nil {
						t.Fatal(err)
					}
				}
				seedLegacyParcelMoney(t, writer)
				if _, err = writer.Exec(`UPDATE supplier_return_parcels SET status='confirmed',confirmed_by='actor' WHERE id='parcel-one' AND status IN ('draft','confirmed')`); err != nil {
					t.Fatal(err)
				}
			}
			if err = writer.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
			case <-ctx.Done():
				t.Fatal("migration completion deadline")
			}
			if err == nil {
				t.Fatal("migration legitimized unchecked concurrent legacy evidence")
			}
			if !strings.Contains(err.Error(), "parcel charge reconciliation required") {
				t.Fatalf("expected evidence rejection, got %v", err)
			}
			var receiptTable, component bool
			if err = db.QueryRow(`SELECT to_regclass('parcel_charge_receipts') IS NOT NULL,EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='supplier_return_financial_events' AND column_name='charge_component')`).Scan(&receiptTable, &component); err != nil {
				t.Fatal(err)
			}
			if receiptTable || component {
				t.Fatalf("partial migration after rejection receipts=%v component=%v", receiptTable, component)
			}
			var events, ledger int
			if err = db.QueryRow(`SELECT (SELECT count(*) FROM supplier_return_financial_events WHERE id='race-event'),(SELECT count(*) FROM ledger_entries WHERE id='race-ledger')`).Scan(&events, &ledger); err != nil || events != 1 || ledger != 1 {
				t.Fatalf("old writer evidence changed: %d/%d %v", events, ledger, err)
			}
		})
	}
}
func TestParcelChargeMigrationRetainsEvidenceLocksThroughBackfill(t *testing.T) {
	up, err := os.ReadFile("../../migrations/202611160001_parcel_charge_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	split := strings.Index(string(up), "INSERT INTO parcel_charge_receipts(")
	if split < 0 {
		t.Fatal("missing real migration backfill")
	}
	for _, relation := range []string{"supplier_return_requests", "supplier_return_parcels"} {
		t.Run(relation, func(t *testing.T) {
			db := parcelLegacyFixture(t)
			setup, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer setup.Rollback()
			if _, err = setup.Exec(`UPDATE supplier_return_parcels SET status='confirmed',confirmed_by='actor'`); err != nil {
				t.Fatal(err)
			}
			seedLegacyParcelMoney(t, setup)
			if err = setup.Commit(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			migration, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer migration.Rollback()
			var migrationPID int
			if err = migration.QueryRow(`SELECT pg_backend_pid()`).Scan(&migrationPID); err != nil {
				t.Fatal(err)
			}
			if _, err = migration.ExecContext(ctx, string(up[:split])); err != nil {
				t.Fatal(err)
			}
			for _, lock := range []struct{ table, mode string }{{"suppliers", "ShareRowExclusiveLock"}, {"supplier_return_requests", "AccessExclusiveLock"}, {"supplier_return_parcels", "AccessExclusiveLock"}, {"ledger_entries", "AccessExclusiveLock"}, {"supplier_return_financial_events", "AccessExclusiveLock"}} {
				var held bool
				if err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation=to_regclass($2) AND mode=$3 AND granted)`, migrationPID, lock.table, lock.mode).Scan(&held); err != nil || !held {
					t.Fatalf("missing retained %s %s: %v", lock.table, lock.mode, err)
				}
			}
			writer, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback()
			var writerPID int
			if err = writer.QueryRow(`SELECT pg_backend_pid()`).Scan(&writerPID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				q := `UPDATE supplier_return_requests SET status='cancelled' WHERE id='parcel-return'`
				if relation == "supplier_return_parcels" {
					q = `UPDATE supplier_return_parcels SET status='handed_over' WHERE id='parcel-one'`
				}
				_, e := writer.ExecContext(ctx, q)
				if e == nil {
					e = writer.Commit()
				}
				done <- e
			}()
			waitParcelMigrationBlock(t, db, writerPID, migrationPID)
			if _, err = migration.ExecContext(ctx, string(up[split:])); err != nil {
				t.Fatal(err)
			}
			if err = migration.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("queued lifecycle writer deadline")
			}
			var count int
			if err = db.QueryRow(`SELECT count(*) FROM parcel_charge_receipts WHERE event_id='race-event' AND actor_user_id='actor' AND final_amount=120000`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("backfill evidence missing %d %v", count, err)
			}
		})
	}
}
