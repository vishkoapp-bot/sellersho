package repository

import (
	"database/sql"
	"errors"

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
		return nil, errors.New("DATABASE_URL initialization failed; configuration value withheld")
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, errors.New("PostgreSQL connection failed; dependency details withheld")
	}

	return &PostgresDB{DB: db}, nil
}
