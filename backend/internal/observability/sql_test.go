package observability

import (
	"bytes"
	"context"
	"database/sql"
	"github.com/drup/platform/internal/testdb"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSQLDriverCorrelationAndRedaction(t *testing.T) {
	guard, e := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	_ = guard
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	db, e := OpenPostgres(os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx := NewContext(context.Background())
	var v string
	if e = db.QueryRowContext(ctx, `SELECT $1::text`, "SECRET").Scan(&v); e != nil || v != "SECRET" {
		t.Fatal(e)
	}

	tx, e := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	stmt, e := tx.PrepareContext(ctx, `SELECT $1::bigint`)
	if e != nil {
		t.Fatal(e)
	}
	var n int64
	if e = stmt.QueryRowContext(ctx, int64(42)).Scan(&n); e != nil || n != 42 {
		t.Fatalf("prepared query %d %v", n, e)
	}
	stmt.Close()
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	tx, e = db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.ExecContext(ctx, `SELECT 1`); e != nil {
		t.Fatal(e)
	}
	if e = tx.Rollback(); e != nil {
		t.Fatal(e)
	}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, e = db.ExecContext(short, `SELECT pg_sleep(2)`); e == nil {
		t.Fatal("query cancellation lost")
	}
	if e = db.PingContext(ctx); e != nil {
		t.Fatal("pool failed after cancellation")
	}
	s := logs.String()
	if !strings.Contains(s, RequestID(ctx)) || !strings.Contains(s, "component=sql") || strings.Contains(s, "SECRET") || strings.Contains(s, "SELECT") {
		t.Fatal("missing correlation or leaked SQL")
	}
}
