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
	timeout := flag.Duration("timeout", 10*time.Minute, "Overall migration deadline")
	lockTimeout := flag.Duration("lock-timeout", 30*time.Second, "Singleton lock wait deadline")
	migrationTimeout := flag.Duration("migration-timeout", 2*time.Minute, "Per-migration execution/DDL lock deadline")
	flag.Parse()
	if *dbFlag == "" {
		log.Fatal("DATABASE_URL is required")
	}
	if *timeout <= 0 || *lockTimeout <= 0 || *migrationTimeout <= 0 {
		log.Fatal("Migration timeouts must be positive")
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
		MigrationTimeout: *migrationTimeout, Logf: log.Printf,
	}); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
	log.Println("Database schema verified and up-to-date")
}
