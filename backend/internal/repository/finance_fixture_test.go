package repository

import (
	"database/sql"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"net/url"
	"os"
	"strings"
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
	// This schema deliberately has no public fallback. Install the actual new
	// retry table used by bounded release; never relax production missing-schema checks.
	migration, err := os.ReadFile("../../migrations/202611150007_background_jobs.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	ddl := string(migration)
	start := strings.Index(ddl, "CREATE TABLE background_job_candidate_retry (")
	if start < 0 {
		t.Fatal("missing candidate retry migration")
	}
	end := strings.Index(ddl[start:], ";")
	if end < 0 {
		t.Fatal("incomplete candidate retry migration")
	}
	if _, err = db.Exec(ddl[start : start+end+1]); err != nil {
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
