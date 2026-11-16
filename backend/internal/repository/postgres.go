package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/drup/platform/internal/observability"
)

// PostgresDB manages the PostgreSQL database connection pool
type PostgresDB struct {
	DB *sql.DB
}

// NewPostgresDB initializes and tests connection to PostgreSQL
func NewPostgresDB(databaseURL string) (*PostgresDB, error) {
	return NewPostgresDBContext(context.Background(), databaseURL)
}

func NewPostgresDBContext(parent context.Context, databaseURL string) (*PostgresDB, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	db, err := observability.OpenPostgres(databaseURL)
	if err != nil {
		return nil, errors.New("DATABASE_URL initialization failed; configuration value withheld")
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("PostgreSQL connection failed; dependency details withheld")
	}

	return &PostgresDB{DB: db}, nil
}
