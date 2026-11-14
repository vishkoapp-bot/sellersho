package repository

import (
	"database/sql"
	"fmt"

	"github.com/drup/platform/internal/observability"
)

// PostgresDB manages the PostgreSQL database connection pool
type PostgresDB struct {
	DB *sql.DB
}

// NewPostgresDB initializes and tests connection to PostgreSQL
func NewPostgresDB(databaseURL string) (*PostgresDB, error) {
	db, err := observability.OpenPostgres(databaseURL)
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
