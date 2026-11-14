package repository

import (
	"database/sql"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"net/url"
	"testing"
)

// Guard the database first, then use an exclusive schema with no public fallback.
func openPrivateFinanceFixture(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	admin, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "finance_fixture_" + uuid.NewString()[:8]
	if _, err := admin.Exec(`CREATE SCHEMA ` + pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := testdb.Open(t, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(`DROP SCHEMA ` + pq.QuoteIdentifier(schema) + ` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	return db
}
