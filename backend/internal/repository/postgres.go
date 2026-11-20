package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/drup/platform/internal/observability"
)

// PostgresDB manages the PostgreSQL database connection pool
type PostgresDB struct {
	DB *sql.DB
}

// PoolSettings bounds one sql.DB pool (PERF-03 G4). Zero lifetimes
// mean "reuse connections indefinitely" — the historical behaviour,
// kept as the default so existing deployments do not change.
type PoolSettings struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultPoolSettings preserves the historical 25/10 pool.
func DefaultPoolSettings() PoolSettings {
	return PoolSettings{MaxOpenConns: 25, MaxIdleConns: 10}
}

// Validate rejects nonsense limits at boot: max-open outside the
// whole-deployment budget (1..100 PostgreSQL connections), idle above
// open, or a lifetime/idle time outside 0..24h.
func (s PoolSettings) Validate() error {
	if s.MaxOpenConns < 1 || s.MaxOpenConns > 100 {
		return fmt.Errorf("pool max open connections must be in 1..100 (got %d)", s.MaxOpenConns)
	}
	if s.MaxIdleConns < 0 || s.MaxIdleConns > s.MaxOpenConns {
		return fmt.Errorf("pool max idle connections must be in 0..%d (got %d)", s.MaxOpenConns, s.MaxIdleConns)
	}
	if s.ConnMaxLifetime < 0 || s.ConnMaxLifetime > 24*time.Hour {
		return fmt.Errorf("pool connection max lifetime must be in 0..24h (got %s)", s.ConnMaxLifetime)
	}
	if s.ConnMaxIdleTime < 0 || s.ConnMaxIdleTime > 24*time.Hour {
		return fmt.Errorf("pool connection max idle time must be in 0..24h (got %s)", s.ConnMaxIdleTime)
	}
	return nil
}

// ApplyPoolSettings applies s to an already-opened *sql.DB.
func ApplyPoolSettings(db *sql.DB, s PoolSettings) {
	db.SetMaxOpenConns(s.MaxOpenConns)
	db.SetMaxIdleConns(s.MaxIdleConns)
	db.SetConnMaxLifetime(s.ConnMaxLifetime)
	db.SetConnMaxIdleTime(s.ConnMaxIdleTime)
}

// NewPostgresDB initializes and tests connection to PostgreSQL
func NewPostgresDB(databaseURL string) (*PostgresDB, error) {
	return NewPostgresDBContext(context.Background(), databaseURL)
}

func NewPostgresDBContext(parent context.Context, databaseURL string) (*PostgresDB, error) {
	return NewPostgresDBWithPool(parent, databaseURL, DefaultPoolSettings())
}

// NewPostgresDBWithPool is NewPostgresDBContext with explicit, validated
// pool bounds (PERF-03 G4). Invalid bounds fail before any connection
// is opened.
func NewPostgresDBWithPool(parent context.Context, databaseURL string, pool PoolSettings) (*PostgresDB, error) {
	if err := pool.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	db, err := observability.OpenPostgres(databaseURL)
	if err != nil {
		return nil, errors.New("DATABASE_URL initialization failed; configuration value withheld")
	}

	ApplyPoolSettings(db, pool)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("PostgreSQL connection failed; dependency details withheld")
	}

	return &PostgresDB{DB: db}, nil
}
