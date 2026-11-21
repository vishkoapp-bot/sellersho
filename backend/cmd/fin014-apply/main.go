// Command fin014-apply applies one approved FIN-014 repair batch
// (bounded, resumable, predicate-checked).
//
// Usage:
//
//	FIN014_DATABASE_URL=postgres://.../sellersho_test_<hex> \
//	  go run ./cmd/fin014-apply -batch fin014-batch-<uuid> [-limit 25]
//
// The batch must already carry its Q8 approval record (approved_by +
// approved_at, status approved/applying). The command never approves
// its own batch: if the owner has not named the approver, it refuses
// to apply and says so. Settlement-item repairs are excluded from
// apply (report-only, audit §8 Q4); storage merged lots and
// unknown-PSP cases stay held. The command refuses any database
// without the disposable guard marker (same rule as internal/testdb).
// It never touches production data, never sends OTPs, and never
// rewrites history — repairs are compensating events through the
// existing domain writers where they exist (three actions use
// documented guarded direct writes because no writer exists for them;
// see fin014_apply.go).
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
	batch := flag.String("batch", "", "Repair batch id to apply (required)")
	limit := flag.Int("limit", repository.FIN014ApplyMaxOps, "Operations per run (max 25)")
	dsn := flag.String("db", os.Getenv("FIN014_DATABASE_URL"), "PostgreSQL DSN (prefer FIN014_DATABASE_URL)")
	flag.Parse()
	if *batch == "" {
		log.Fatal("-batch is required")
	}
	if *dsn == "" {
		log.Fatal("FIN014_DATABASE_URL is required")
	}
	u, err := url.Parse(*dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Fragment != "" {
		log.Fatal("refusing target: DSN must be an explicit PostgreSQL URL")
	}
	name := u.Path
	if len(name) < 2 || !disposableName.MatchString(name[1:]) {
		log.Fatal("refusing target: database name must be sellersho_test_*")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := sql.Open("postgres", *dsn)
	if err != nil {
		log.Fatal("cannot open database connection")
	}
	defer db.Close()
	if err := repository.FIN014RequireDisposableDB(ctx, db); err != nil {
		log.Fatalf("refusing target: %v", err)
	}
	result, err := repository.FIN014ApplyBatch(ctx, db, *batch, *limit)
	if err != nil {
		log.Fatalf("fin014 apply: %v", err)
	}
	fmt.Printf("batch=%s applied=%d failed=%d done=%v\n", result.BatchID, result.Applied, result.Failed, result.Done)
	for id, reason := range result.FailedOps {
		fmt.Printf("failed: %s: %s\n", id, reason)
	}
}
