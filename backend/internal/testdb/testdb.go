// Package testdb is the only entry point for PostgreSQL integration tests.
// It never reads application configuration. Provisioning a marker is an explicit,
// separate action performed by the disposable-environment runner, never by Open.
package testdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/url"
	"regexp"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

var databaseName = regexp.MustCompile(`^sellersho_test_[a-z0-9_]+$`)

// NoConnections supplies a pool for unit tests that exercise database/sql
// settings or metrics without opening a network connection.
func NoConnections(t testing.TB) *sql.DB {
	t.Helper()
	db := sql.OpenDB(noConnector{})
	t.Cleanup(func() { db.Close() })
	return db
}

type noConnector struct{}

func (noConnector) Driver() driver.Driver { return noConnector{} }
func (noConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("unit test pool refuses all connections")
}
func (c noConnector) Open(string) (driver.Conn, error) { return c.Connect(context.Background()) }

// Identity refuses implicit libpq defaults and parameters that can override the
// parsed host/database or inject session options. Errors deliberately omit DSNs.
func Identity(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Fragment != "" {
		return "", errors.New("test database requires an explicit PostgreSQL URL")
	}
	name := u.Path
	if len(name) < 2 || !databaseName.MatchString(name[1:]) {
		return "", errors.New("test database name must start with sellersho_test_ and contain a unique suffix")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", errors.New("invalid test database parameters")
	}
	for key, values := range query {
		switch key {
		case "sslmode", "connect_timeout", "search_path", "application_name":
		default:
			return "", errors.New("unsupported test database parameter")
		}
		if len(values) != 1 {
			return "", errors.New("duplicate test database parameter")
		}
	}
	return name[1:], nil
}

// Open skips only absent opt-in configuration. Invalid, unreachable or unmarked
// explicit targets fail the test, so integration failures cannot masquerade as skips.
// Cleanup is registered before any connection or fixture writes.
func Open(t testing.TB, dsn string) (*sql.DB, error) {
	t.Helper()
	if dsn == "" {
		t.Skip("integration test requires an explicit disposable test database URL")
	}
	name, err := Identity(dsn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("cannot initialize disposable test connection")
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var actual, marker string
	err = db.QueryRowContext(ctx, `SELECT current_database(), database_name FROM public.sellersho_test_guard WHERE singleton = TRUE`).Scan(&actual, &marker)
	if err != nil || actual != name || marker != name {
		t.Fatal("refusing test target: disposable database identity/marker verification failed")
	}
	return db, nil
}
