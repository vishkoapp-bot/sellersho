// Command fin014-report emits one FIN-014 dry-run finance report as
// JSON on stdout.
//
// Usage:
//
//	FIN014_DATABASE_URL=postgres://.../sellersho_test_<hex> \
//	  go run ./cmd/fin014-report -prefix f14f- -limit 100
//
// The command is read-only (the report runs in a read-only,
// repeatable-read transaction) and refuses any database without the
// disposable guard marker (same rule as internal/testdb: the database
// name must be sellersho_test_* and the public.sellersho_test_guard
// row must name it). It never touches production data, never sends
// OTPs, and never writes. FIN-011 stays pinned in the output; the
// report records the expected check-7 failure, never hides it.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"time"

	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

var disposableName = regexp.MustCompile(`^sellersho_test_[a-z0-9_]+$`)

func main() {
	log.SetFlags(0)
	prefix := flag.String("prefix", "", "ID prefix filter (\"\" matches everything)")
	limit := flag.Int("limit", 100, "Page size (1-100)")
	cursor := flag.String("cursor", "", "Paging cursor from a previous run (empty for the first page)")
	dsn := flag.String("db", os.Getenv("FIN014_DATABASE_URL"), "PostgreSQL DSN (prefer FIN014_DATABASE_URL)")
	flag.Parse()
	if *dsn == "" {
		log.Fatal("FIN014_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", *dsn)
	if err != nil {
		log.Fatal("cannot open database connection")
	}
	defer db.Close()
	// Disposable guard (mirrors internal/testdb.Identity + Open, but
	// usable outside tests): refuse anything that is not a marked
	// sellersho_test_* database. Errors deliberately omit the DSN.
	name, err := parseDSN(*dsn)
	if err != nil {
		log.Fatalf("refusing target: %v", err)
	}
	if !disposableName.MatchString(name) {
		log.Fatalf("refusing target: database name must be sellersho_test_*")
	}
	if err := repository.FIN014RequireDisposableDB(ctx, db); err != nil {
		log.Fatalf("refusing target: %v", err)
	}
	report, err := repository.GetFIN014Report(ctx, db, repository.FIN014ReportOptions{
		Limit:    *limit,
		Cursor:   *cursor,
		IDPrefix: *prefix,
	})
	if err != nil {
		log.Fatalf("fin014 report: %v", err)
	}
	out, err := repository.FIN014ReportJSON(report)
	if err != nil {
		log.Fatalf("fin014 report JSON: %v", err)
	}
	fmt.Println(string(out))
}

// parseDSN extracts the database name from a postgres:// DSN (same
// shape testdb.Identity accepts: postgres/postgresql scheme, no
// fragment, no overriding session parameters).
func parseDSN(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Fragment != "" {
		return "", fmt.Errorf("DSN must be an explicit PostgreSQL URL")
	}
	name := u.Path
	if len(name) < 2 {
		return "", fmt.Errorf("DSN has no database name")
	}
	return name[1:], nil
}
