package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// These fixtures exercise the shared helper on real PostgreSQL row locks, using
// two pinned physical connections. They are contract/adoption fixtures, not a
// claim that the existing domain entry points already follow the hierarchy.
func transactionContractConnections(t *testing.T) (*sql.Conn, *sql.Conn, string) {
	t.Helper()
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	schema := "contract_" + uuid.NewString()[:8]
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := db.ExecContext(cleanup, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("remove contract fixture schema: %v", err)
		}
	})
	for table := range transactionLockTables {
		if table == "order_item_units" {
			if _, err := db.ExecContext(ctx, `CREATE TABLE `+schema+`.order_item_units (order_item_id text, unit_id text, quantity integer NOT NULL DEFAULT 100, PRIMARY KEY (order_item_id, unit_id)); INSERT INTO `+schema+`.order_item_units (order_item_id,unit_id) VALUES ('a','a'),('b','a'),('a','b')`); err != nil {
				t.Fatal(err)
			}
			continue
		}
		column := transactionLockColumn(table)
		if _, err := db.ExecContext(ctx, `CREATE TABLE `+schema+`.`+table+` (`+column+` text PRIMARY KEY, quantity integer NOT NULL DEFAULT 100)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO `+schema+`.`+table+` (`+column+`) VALUES ('a'),('b')`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE `+schema+`.effects (event text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	a, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	b, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	var apid, bpid int
	if err := a.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&apid); err != nil {
		t.Fatal(err)
	}
	if err := b.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&bpid); err != nil || apid == bpid {
		t.Fatalf("fixture requires distinct PostgreSQL backends: %v", err)
	}
	return a, b, schema
}

func beginContractTx(t *testing.T, ctx context.Context, conn *sql.Conn, schema string) *sql.Tx {
	t.Helper()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	if _, err := tx.ExecContext(ctx, `SET LOCAL search_path TO `+schema+`, pg_catalog`); err != nil {
		t.Fatal(err)
	}
	return tx
}

func contractPlan(tables ...string) []TransactionRowLock {
	locks := make([]TransactionRowLock, 0, len(tables))
	for _, table := range tables {
		locks = append(locks, TransactionRowLock{Table: table, ID: "a"})
	}
	return locks
}

func TestTransactionContractAllowlistMatchesMigratedSchema(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for table := range transactionLockTables {
		want := []string{transactionLockColumn(table)}
		if table == "order_item_units" {
			want = []string{"order_item_id", "unit_id"}
		}
		var actual pq.StringArray
		err := db.QueryRowContext(ctx, `SELECT ARRAY(
            SELECT a.attname::text FROM pg_catalog.pg_class c
            JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
            JOIN pg_catalog.pg_constraint k ON k.conrelid=c.oid AND k.contype='p'
            CROSS JOIN LATERAL unnest(k.conkey) WITH ORDINALITY AS key(attnum, ord)
            JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid AND a.attnum=key.attnum
            WHERE n.nspname='public' AND c.relname=$1 AND NOT a.attisdropped
            ORDER BY key.ord)`, table).Scan(&actual)
		if err != nil || !reflect.DeepEqual([]string(actual), want) {
			t.Errorf("allowlisted guard %s primary key = %v, want exactly %v: %v", table, actual, want, err)
		}
	}
}

func TestTransactionContractTwoConnectionFlows(t *testing.T) {
	// Intentionally discover in opposite domain order. The helper must take the
	// same canonical locks despite parcel-first / position-first discovery.
	fixtures := []struct {
		name string
		a, b []TransactionRowLock
	}{
		{"dispatch_cancel", contractPlan("orders", "products", "supplier_shipments", "inventory_balances", "warehouse_storage_positions"), contractPlan("warehouse_storage_positions", "inventory_balances", "supplier_shipments", "products", "orders")},
		{"payment_return", contractPlan("orders", "payment_transactions", "stores", "suppliers", "products", "product_variants", "customer_returns", "return_items", "inventory_balances"), contractPlan("return_items", "customer_returns", "product_variants", "products", "suppliers", "stores", "payment_transactions", "orders")},
		{"transfer_capture", contractPlan("orders", "products", "product_variants", "inbound_shipments", "order_inventory_captures", "inventory_balances"), contractPlan("inventory_balances", "order_inventory_captures", "inbound_shipments", "product_variants", "products")},
		{"payout_refund", contractPlan("stores", "payout_requests"), contractPlan("orders", "payment_transactions", "stores", "customer_returns")},
		{"storage_assessment_departure", contractPlan("suppliers", "products", "warehouse_storage_positions"), contractPlan("warehouse_storage_positions", "putaway_capacity_reservations", "warehouse_bins", "inventory_balances", "products", "suppliers", "orders")},
		{"parcel_charge_adjustment", contractPlan("suppliers", "supplier_return_requests", "supplier_return_parcels"), contractPlan("supplier_return_parcels", "supplier_return_requests", "suppliers")},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			a, b, schema := transactionContractConnections(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			owner := beginContractTx(t, ctx, a, schema)
			if err := LockTransactionRowsTx(ctx, owner, &TransactionLockSet{}, fixture.a...); err != nil {
				t.Fatal(err)
			}
			if _, err := owner.ExecContext(ctx, `UPDATE `+fixture.a[0].Table+` SET quantity=91 WHERE `+transactionLockColumn(fixture.a[0].Table)+`='a'`); err != nil {
				t.Fatal(err)
			}
			waiter := beginContractTx(t, ctx, b, schema)
			var waiterPID int
			if err := waiter.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&waiterPID); err != nil {
				t.Fatal(err)
			}
			if _, err := waiter.ExecContext(ctx, `INSERT INTO effects VALUES ('failed-attempt')`); err != nil {
				t.Fatal(err)
			}
			if _, err := waiter.ExecContext(ctx, `SET LOCAL lock_timeout='400ms'`); err != nil {
				t.Fatal(err)
			}
			waitCtx, stop := context.WithTimeout(ctx, 2*time.Second)
			defer stop()
			result := make(chan error, 1)
			set := &TransactionLockSet{}
			started := time.Now()
			go func() { result <- LockTransactionRowsTx(waitCtx, waiter, set, fixture.b...) }()
			// Observe the actual database wait, not merely a scheduling delay.
			observedWait := false
			for time.Since(started) < 500*time.Millisecond {
				if err := owner.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, waiterPID).Scan(&observedWait); err != nil {
					t.Fatal(err)
				}
				if observedWait {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !observedWait {
				t.Fatal("second physical connection never waited on the shared guard")
			}
			var err error
			select {
			case err = <-result:
			case <-time.After(3 * time.Second):
				t.Fatal("guard acquisition did not return within the fixture bound")
			}
			var conflict *pq.Error
			if !errors.As(err, &conflict) || conflict.Code != "55P03" || time.Since(started) > 2*time.Second {
				t.Fatalf("wait must fail within its deadline, got %v after %s", err, time.Since(started))
			}
			if err := waiter.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err := owner.Rollback(); err != nil {
				t.Fatal(err)
			}
			var qty, effects int
			if err := a.QueryRowContext(ctx, `SELECT quantity FROM `+schema+`.`+fixture.a[0].Table+` WHERE `+transactionLockColumn(fixture.a[0].Table)+`='a'`).Scan(&qty); err != nil || qty != 100 {
				t.Fatalf("owner rollback retained mutation: quantity=%d, %v", qty, err)
			}
			if err := a.QueryRowContext(ctx, `SELECT count(*) FROM `+schema+`.effects`).Scan(&effects); err != nil || effects != 0 {
				t.Fatalf("failed waiter retained business event: count=%d, %v", effects, err)
			}
			// A fresh transaction on the same second physical connection can
			// acquire the whole plan and commit after the owner releases it.
			retry := beginContractTx(t, ctx, b, schema)
			if err := LockTransactionRowsTx(ctx, retry, &TransactionLockSet{}, fixture.b...); err != nil {
				t.Fatal(err)
			}
			if _, err := retry.ExecContext(ctx, `INSERT INTO effects VALUES ('successful-retry')`); err != nil {
				t.Fatal(err)
			}
			if err := retry.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := a.QueryRowContext(ctx, `SELECT count(*) FROM `+schema+`.effects`).Scan(&effects); err != nil || effects != 1 {
				t.Fatalf("successful retry effect count=%d, %v", effects, err)
			}
		})
	}
}

func TestTransactionContractIndependentAccounts(t *testing.T) {
	a, b, schema := transactionContractConnections(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	owner := beginContractTx(t, ctx, a, schema)
	if err := LockTransactionRowsTx(ctx, owner, &TransactionLockSet{}, TransactionRowLock{Table: "stores", ID: "a"}); err != nil {
		t.Fatal(err)
	}
	for _, account := range []TransactionRowLock{{Table: "stores", ID: "b"}, {Table: "suppliers", ID: "a"}, {Table: "users", ID: "a"}} {
		other := beginContractTx(t, ctx, b, schema)
		bounded, stop := context.WithTimeout(ctx, 500*time.Millisecond)
		err := LockTransactionRowsTx(bounded, other, &TransactionLockSet{}, account)
		stop()
		if err != nil {
			t.Fatalf("independent typed account blocked: %v", err)
		}
		if _, err := other.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET quantity=99 WHERE id=$1`, account.Table), account.ID); err != nil {
			t.Fatal(err)
		}
		if err := other.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTransactionContractRejectsLateEarlierLocks(t *testing.T) {
	a, b, schema := transactionContractConnections(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx := beginContractTx(t, ctx, a, schema)
	set := &TransactionLockSet{}
	if err := LockTransactionRowsTx(ctx, tx, set, contractPlan("products")...); err != nil {
		t.Fatal(err)
	}
	if err := LockTransactionRowsTx(ctx, tx, set, contractPlan("products")...); err != nil {
		t.Fatalf("reentrant preheld guard rejected: %v", err)
	}
	other := beginContractTx(t, ctx, b, schema)
	if err := LockTransactionRowsTx(ctx, other, set, contractPlan("products")...); !errors.Is(err, ErrTransactionLockOrder) {
		t.Fatalf("lock set reused across transactions: %v", err)
	}
	if err := LockTransactionRowsTx(ctx, tx, set, contractPlan("orders")...); !errors.Is(err, ErrTransactionLockOrder) {
		t.Fatalf("product-first transaction accepted a late order lock: %v", err)
	}
	if err := LockTransactionRowsTx(context.Background(), tx, set, contractPlan("products")...); !errors.Is(err, ErrTransactionLockOrder) {
		t.Fatalf("unbounded acquisition accepted: %v", err)
	}
	if err := LockTransactionRowsTx(ctx, tx, set, TransactionRowLock{Table: "products", ID: "missing"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing guard did not fail closed: %v", err)
	}
	if err := LockTransactionRowsTx(ctx, tx, set, contractPlan("products")...); !errors.Is(err, ErrTransactionLockOrder) {
		t.Fatalf("failed acquisition state reused: %v", err)
	}
}

func TestTransactionContractCompositeIdentityLocksExactRow(t *testing.T) {
	a, b, schema := transactionContractConnections(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	owner := beginContractTx(t, ctx, a, schema)
	lock := TransactionRowLock{Table: "order_item_units", ID: "a", SecondaryID: "a"}
	if err := LockTransactionRowsTx(ctx, owner, &TransactionLockSet{}, lock); err != nil {
		t.Fatal(err)
	}
	// Resale of the same unit and another unit on the same item are independent.
	for _, independent := range []TransactionRowLock{
		{Table: "order_item_units", ID: "b", SecondaryID: "a"},
		{Table: "order_item_units", ID: "a", SecondaryID: "b"},
	} {
		other := beginContractTx(t, ctx, b, schema)
		if _, err := other.ExecContext(ctx, `SET LOCAL lock_timeout='200ms'`); err != nil {
			t.Fatal(err)
		}
		if err := LockTransactionRowsTx(ctx, other, &TransactionLockSet{}, independent); err != nil {
			t.Fatalf("independent composite row blocked: %v", err)
		}
		if err := other.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
	other := beginContractTx(t, ctx, b, schema)
	if _, err := other.ExecContext(ctx, `SET LOCAL lock_timeout='200ms'`); err != nil {
		t.Fatal(err)
	}
	var conflict *pq.Error
	if err := LockTransactionRowsTx(ctx, other, &TransactionLockSet{}, lock); !errors.As(err, &conflict) || conflict.Code != "55P03" {
		t.Fatalf("same pair must be locked: %v", err)
	}
	if err := other.Rollback(); err != nil {
		t.Fatal(err)
	}
	missing := beginContractTx(t, ctx, b, schema)
	if err := LockTransactionRowsTx(ctx, missing, &TransactionLockSet{}, TransactionRowLock{Table: "order_item_units", ID: "b", SecondaryID: "b"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("nonexistent pair must fail closed: %v", err)
	}
}
