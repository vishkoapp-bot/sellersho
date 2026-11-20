package config

import (
	"testing"
	"time"
)

// PERF-03 G4: DB_POOL_* env parsing and validation. Defaults preserve
// the historical 25/10 pool; bad values fail boot instead of silently
// over- or under-provisioning the pool.
func TestPERF03PoolDefaultsPreserve25And10(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DB_POOL_MAX_OPEN", "")
	t.Setenv("DB_POOL_MAX_IDLE", "")
	t.Setenv("DB_POOL_MAX_LIFETIME_SECONDS", "")
	t.Setenv("DB_POOL_MAX_IDLE_SECONDS", "")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBPoolMaxOpen != 25 || cfg.DBPoolMaxIdle != 10 {
		t.Fatalf("defaults: maxOpen=%d maxIdle=%d, want 25/10", cfg.DBPoolMaxOpen, cfg.DBPoolMaxIdle)
	}
	if cfg.DBPoolMaxLifetime != 0 || cfg.DBPoolMaxIdleTime != 0 {
		t.Fatalf("defaults: lifetime=%v idleTime=%v, want 0/0 (unlimited reuse)", cfg.DBPoolMaxLifetime, cfg.DBPoolMaxIdleTime)
	}
}

func TestPERF03PoolValidCustomValuesAccepted(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DB_POOL_MAX_OPEN", "50")
	t.Setenv("DB_POOL_MAX_IDLE", "5")
	t.Setenv("DB_POOL_MAX_LIFETIME_SECONDS", "3600")
	t.Setenv("DB_POOL_MAX_IDLE_SECONDS", "300")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBPoolMaxOpen != 50 || cfg.DBPoolMaxIdle != 5 {
		t.Fatalf("maxOpen=%d maxIdle=%d, want 50/5", cfg.DBPoolMaxOpen, cfg.DBPoolMaxIdle)
	}
	if cfg.DBPoolMaxLifetime != time.Hour || cfg.DBPoolMaxIdleTime != 5*time.Minute {
		t.Fatalf("lifetime=%v idleTime=%v, want 1h/5m", cfg.DBPoolMaxLifetime, cfg.DBPoolMaxIdleTime)
	}
}

func TestPERF03PoolInvalidValuesRejected(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{"max open zero", "DB_POOL_MAX_OPEN", "0"},
		{"max open negative", "DB_POOL_MAX_OPEN", "-1"},
		{"max open above budget", "DB_POOL_MAX_OPEN", "101"},
		{"max open not a number", "DB_POOL_MAX_OPEN", "many"},
		{"max idle negative", "DB_POOL_MAX_IDLE", "-1"},
		{"lifetime negative", "DB_POOL_MAX_LIFETIME_SECONDS", "-5"},
		{"lifetime above one day", "DB_POOL_MAX_LIFETIME_SECONDS", "86401"},
		{"idle time not a number", "DB_POOL_MAX_IDLE_SECONDS", "soon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", "development")
			t.Setenv("DB_POOL_MAX_OPEN", "25")
			t.Setenv("DB_POOL_MAX_IDLE", "10")
			t.Setenv("DB_POOL_MAX_LIFETIME_SECONDS", "")
			t.Setenv("DB_POOL_MAX_IDLE_SECONDS", "")
			t.Setenv(tc.key, tc.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("%s=%q was accepted, want a boot error", tc.key, tc.value)
			}
		})
	}
}

func TestPERF03PoolIdleAboveOpenRejected(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DB_POOL_MAX_OPEN", "5")
	t.Setenv("DB_POOL_MAX_IDLE", "6")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("max idle 6 with max open 5 was accepted, want a boot error")
	}
}
