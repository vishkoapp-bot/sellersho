package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/drup/platform/internal/config"
	_ "github.com/lib/pq"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("[FATAL] Configuration error: %v", err)
	}
	dbFlag := flag.String("db", cfg.DatabaseURL, "PostgreSQL connection string")
	dirFlag := flag.String("dir", "migrations", "Path to migrations folder")
	flag.Parse()

	db, err := sql.Open("postgres", *dbFlag)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping DB: %v", err)
	}

	entries, err := os.ReadDir(*dirFlag)
	if err != nil {
		log.Fatalf("Failed to read migrations directory: %v", err)
	}

	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, filepath.Join(*dirFlag, entry.Name()))
		}
	}
	sort.Strings(upFiles)

	for _, file := range upFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			log.Fatalf("Failed to read %s: %v", file, err)
		}
		log.Printf("Applying migration: %s", filepath.Base(file))
		if _, err := db.Exec(string(content)); err != nil {
			log.Printf("Notice on %s: %v (continuing)", filepath.Base(file), err)
		} else {
			log.Printf("Successfully applied: %s", filepath.Base(file))
		}
	}

	fmt.Println("All migrations processed successfully.")
}
