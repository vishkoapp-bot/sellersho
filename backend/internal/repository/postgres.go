package repository

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// PostgresDB manages the PostgreSQL database connection pool
type PostgresDB struct {
	DB *sql.DB
}

// NewPostgresDB initializes and tests connection to PostgreSQL
func NewPostgresDB(databaseURL string) (*PostgresDB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &PostgresDB{DB: db}, nil
}
