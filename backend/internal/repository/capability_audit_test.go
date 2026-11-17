package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	_ "github.com/lib/pq"
)

// This opt-in integration check is used by the capability audit command and
// is skipped during normal unit test runs unless CAPABILITY_AUDIT_DATABASE_URL
// is provided.
func TestCapabilityAuditBackfill(t *testing.T) {
	databaseURL := os.Getenv("CAPABILITY_AUDIT_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CAPABILITY_AUDIT_DATABASE_URL is not set")
	}
	db, err := testdb.Open(t, databaseURL)
	if err != nil {
		t.Skipf("capability audit database unavailable: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("capability audit database unavailable: %v", err)
	}
	repo := NewCapabilityRepository(db)
	if err := repo.BackfillOwnershipCapabilities(ctx); err != nil {
		t.Fatal(err)
	}
	var duplicates int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT user_id, capability FROM user_capabilities
			GROUP BY user_id, capability HAVING COUNT(*) > 1
		) duplicates
	`).Scan(&duplicates); err != nil {
		t.Fatal(err)
	}
	if duplicates != 0 {
		t.Fatalf("found %d duplicate capability keys", duplicates)
	}
}
