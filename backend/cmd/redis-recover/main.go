// Command redis-recover runs the PERF-04 manual Redis recovery paths
// (see docs/operations/redis-recovery.md). It is a maintenance binary,
// not part of the server image: build it from the same pushed commit as
// the deployed server and run it on the server host with the server's
// DATABASE_URL / REDIS_URL in the environment (never as arguments, so
// credentials never appear in the process list).
//
// Usage:
//
//	redis-recover rebuild
//	  Re-create Redis holds for live pending orders from PostgreSQL.
//	  Prints the rebuilt pool count; per-order failures are printed and
//	  do not stop the remaining orders.
//
//	redis-recover reconcile -product <product-id> [-variant <variant-id>]
//	  Recompute one pool's maintained aggregate from a full scan and
//	  store the exact sum. Prints the exact sum.
//
// Both commands are idempotent and safe to re-run. Neither resurrects
// holds for cancelled, expired, paid, or inventory-captured orders.
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"os"
	"time"

	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

func main() {
	log.SetFlags(log.LstdFlags)
	if len(os.Args) < 2 {
		log.Fatal("usage: redis-recover <rebuild|reconcile> [flags]")
	}
	mode := os.Args[1]
	fs := flag.NewFlagSet(mode, flag.ExitOnError)
	dbURL := fs.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection string (prefer DATABASE_URL)")
	redisURL := fs.String("redis", os.Getenv("REDIS_URL"), "Redis connection string (prefer REDIS_URL)")
	product := fs.String("product", "", "Product ID (reconcile only)")
	variant := fs.String("variant", "", "Variant ID (reconcile only)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		log.Fatalf("flag parse failed: %v", err)
	}
	if *dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	if *redisURL == "" {
		log.Fatal("REDIS_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	db, err := sql.Open("postgres", *dbURL)
	if err != nil {
		log.Fatal("Failed to initialize database connection")
	}
	// PERF-03 G4: cap the tool pool like the migration pool (was
	// uncapped sql.Open defaults). Recovery work is sequential.
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal("Database connection failed")
	}

	redisClient, err := repository.NewRedisClientContext(ctx, *redisURL)
	if err != nil {
		log.Fatal("Redis connection failed")
	}
	defer redisClient.Client.Close()

	switch mode {
	case "rebuild":
		rebuilt, err := repository.NewOrderRepository(db).RebuildInventoryHolds(ctx, redisClient)
		if err != nil {
			log.Printf("rebuild finished with errors: %v", err)
		}
		log.Printf("rebuild complete: %d hold pools rebuilt", rebuilt)
		if err != nil {
			os.Exit(1)
		}
	case "reconcile":
		if *product == "" {
			log.Fatal("reconcile requires -product")
		}
		sum, err := redisClient.ReconcileHeldAggregate(ctx, repository.InventoryHoldRef{ProductID: *product, VariantID: *variant})
		if err != nil {
			log.Fatalf("reconcile failed: %v", err)
		}
		log.Printf("reconcile complete: pool %s exact held quantity = %d", *product, sum)
	default:
		log.Fatalf("unknown mode %q: want rebuild or reconcile", mode)
	}
}
