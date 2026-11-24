package repository

import (
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
)

// PERF-03 G4: pool bounds validate at boot and apply to a *sql.DB.
// ApplyPoolSettings is exercised on an unconnected *sql.DB: Stats
// reflects the configured limits without any live connection.
func TestPERF03PoolSettingsValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		s       PoolSettings
		wantErr bool
	}{
		{"defaults", DefaultPoolSettings(), false},
		{"custom valid", PoolSettings{MaxOpenConns: 50, MaxIdleConns: 5, ConnMaxLifetime: time.Hour, ConnMaxIdleTime: 5 * time.Minute}, false},
		{"idle equals open", PoolSettings{MaxOpenConns: 1, MaxIdleConns: 1}, false},
		{"max open zero", PoolSettings{MaxOpenConns: 0, MaxIdleConns: 0}, true},
		{"max open above budget", PoolSettings{MaxOpenConns: 101, MaxIdleConns: 10}, true},
		{"idle above open", PoolSettings{MaxOpenConns: 5, MaxIdleConns: 6}, true},
		{"negative idle", PoolSettings{MaxOpenConns: 25, MaxIdleConns: -1}, true},
		{"negative lifetime", PoolSettings{MaxOpenConns: 25, MaxIdleConns: 10, ConnMaxLifetime: -time.Second}, true},
		{"lifetime above one day", PoolSettings{MaxOpenConns: 25, MaxIdleConns: 10, ConnMaxLifetime: 25 * time.Hour}, true},
		{"idle time above one day", PoolSettings{MaxOpenConns: 25, MaxIdleConns: 10, ConnMaxIdleTime: 25 * time.Hour}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.s.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("settings %+v: want validation error", tc.s)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("settings %+v: unexpected error: %v", tc.s, err)
			}
		})
	}
}

func TestPERF03ApplyPoolSettings(t *testing.T) {
	db := testdb.NoConnections(t)
	ApplyPoolSettings(db, PoolSettings{MaxOpenConns: 7, MaxIdleConns: 3, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: 30 * time.Second})
	stats := db.Stats()
	if stats.MaxOpenConnections != 7 {
		t.Fatalf("MaxOpenConnections=%d, want 7", stats.MaxOpenConnections)
	}
	// Defaults preserve the historical 25/10 pool exactly.
	ApplyPoolSettings(db, DefaultPoolSettings())
	stats = db.Stats()
	if stats.MaxOpenConnections != 25 {
		t.Fatalf("default MaxOpenConnections=%d, want 25", stats.MaxOpenConnections)
	}
}
