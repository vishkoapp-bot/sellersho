package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/drup/platform/internal/migrations"
	_ "github.com/lib/pq"
)

func main() {
	// Prefer environment credentials so they do not appear in process arguments.
	dbFlag := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection string (prefer DATABASE_URL)")
	dirFlag := flag.String("dir", "migrations", "Required migrations directory")
	adopt := flag.Bool("adopt-legacy", false, "Explicitly adopt legacy history matching the pinned historical manifest")
	timeout := flag.Duration("timeout", 10*time.Minute, "Overall migration deadline (deploy-level budget, not per-file)")
	lockTimeout := flag.Duration("lock-timeout", 30*time.Second, "Singleton advisory-lock wait deadline")
	// MIG-02 D-5: the old single per-migration knob (-migration-timeout,
	// 2 minutes for both) is split. Statement budget defaults to 10
	// minutes; the per-transaction lock wait defaults to 30 seconds.
	statementTimeout := flag.Duration("statement-timeout", 10*time.Minute, "Per-migration statement execution deadline")
	migrationLockTimeout := flag.Duration("migration-lock-timeout", 30*time.Second, "Per-migration DDL/data lock wait deadline")
	migrationTimeout := flag.Duration("migration-timeout", 0, "Deprecated alias for -statement-timeout (kept so existing deploy scripts keep parsing; -statement-timeout wins when both are set)")
	manifestFlag := flag.String("manifest", "", "Optional release manifest (release.json): per-migration lock/statement timeout overrides from migrations.timeouts (MIG-02 D-5). Unknown fields in an override entry, bad durations, or an override for a file not in -dir all fail the run.")
	flag.Parse()
	if *dbFlag == "" {
		log.Fatal("DATABASE_URL is required")
	}
	if *timeout <= 0 || *lockTimeout <= 0 || *statementTimeout <= 0 || *migrationLockTimeout <= 0 {
		log.Fatal("Migration timeouts must be positive")
	}
	statement := *statementTimeout
	if *migrationTimeout > 0 {
		log.Printf("WARNING: -migration-timeout is deprecated; use -statement-timeout (using %s as the statement timeout)", *migrationTimeout)
		statement = *migrationTimeout
	}
	var overrides map[string]migrations.MigrationTimeouts
	if *manifestFlag != "" {
		data, err := os.ReadFile(*manifestFlag)
		if err != nil {
			log.Fatalf("Cannot read release manifest %s: %v", *manifestFlag, err)
		}
		overrides, err = migrations.LoadTimeoutOverrides(data)
		if err != nil {
			log.Fatalf("Invalid release manifest timeouts: %v", err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	db, err := sql.Open("postgres", *dbFlag)
	if err != nil {
		log.Fatal("Failed to initialize migration database connection")
	}
	// PERF-03 G4: the migration pool used to be uncapped (sql.Open
	// defaults: unlimited open connections). Migrations run
	// sequentially, so 2 connections can never eat the deployment
	// budget while the backend holds its 25.
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	defer db.Close()
	if _, err := migrations.Run(ctx, db, migrations.Options{
		Dir: *dirFlag, AdoptLegacy: *adopt, LockTimeout: *lockTimeout,
		StatementTimeout: statement, MigrationLockTimeout: *migrationLockTimeout,
		TimeoutOverrides: overrides, Logf: log.Printf,
	}); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
	log.Println("Database schema verified and up-to-date")
}
