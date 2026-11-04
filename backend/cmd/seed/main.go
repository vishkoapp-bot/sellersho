package main

import (
	"context"
	"flag"
	"log"
	"strings"
	"time"

	"github.com/drup/platform/internal/config"
	"github.com/drup/platform/internal/database/seeder"
	"github.com/drup/platform/internal/repository"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("[FATAL] Configuration error: %v", err)
	}

	actionFlag := flag.String("action", "seed", "Seeding action: 'seed', 'bootstrap', 'clean', 'reset'")
	envFlag := flag.String("env", cfg.Environment, "Target environment: 'development', 'test', 'production'")
	dbFlag := flag.String("db", cfg.DatabaseURL, "PostgreSQL connection string")
	flag.Parse()

	env := strings.ToLower(strings.TrimSpace(*envFlag))
	action := strings.ToLower(strings.TrimSpace(*actionFlag))

	log.Printf("==================================================")
	log.Printf("Drup Platform — Database Seeder CLI")
	log.Printf("Target Environment: %s", env)
	log.Printf("Requested Action:   %s", action)
	log.Printf("==================================================")

	// Constraint 1, 9: Strict fail-closed environment guard
	if action == "clean" || action == "reset" || action == "seed" {
		if !seeder.IsDevEnvironment(env) {
			log.Fatalf("[FATAL] Security Guard: Action '%s' is strictly restricted to development/test environments (allowed: development, dev, test, local). Environment %q is rejected (fail-closed)!", action, env)
		}
	}

	db, err := repository.NewPostgresDB(*dbFlag)
	if err != nil {
		log.Fatalf("[FATAL] Could not connect to database: %v", err)
	}
	defer db.DB.Close()

	seederService := seeder.NewSeeder(db.DB, env)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch action {
	case "bootstrap":
		log.Println("[INFO] Running SeedBootstrap (system constants & taxonomy)...")
		if err := seederService.SeedBootstrap(ctx); err != nil {
			log.Fatalf("[ERROR] SeedBootstrap failed: %v", err)
		}
		log.Println("[SUCCESS] SeedBootstrap completed successfully.")

	case "clean":
		log.Println("[INFO] Running CleanDev (purging test fixtures)...")
		if err := seederService.CleanDev(ctx); err != nil {
			log.Fatalf("[ERROR] CleanDev failed: %v", err)
		}
		log.Println("[SUCCESS] CleanDev completed successfully.")

	case "reset":
		log.Println("[INFO] Running ResetDev (clean -> bootstrap -> dev seed)...")
		if err := seederService.ResetDev(ctx); err != nil {
			log.Fatalf("[ERROR] ResetDev failed: %v", err)
		}
		log.Println("[SUCCESS] ResetDev completed successfully.")

	case "seed":
		log.Println("[INFO] Running idempotent Seed (bootstrap + dev fixtures)...")
		if err := seederService.SeedBootstrap(ctx); err != nil {
			log.Fatalf("[ERROR] SeedBootstrap failed: %v", err)
		}
		if err := seederService.SeedDev(ctx); err != nil {
			log.Fatalf("[ERROR] SeedDev failed: %v", err)
		}
		log.Println("[SUCCESS] Idempotent Seeder completed successfully.")

	default:
		log.Fatalf("[FATAL] Unknown action '%s'. Valid actions are: seed, bootstrap, clean, reset", action)
	}
}
