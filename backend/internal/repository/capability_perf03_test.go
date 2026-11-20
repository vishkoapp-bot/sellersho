package repository

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/drup/platform/internal/testdb"
	"github.com/lib/pq"
)

// PERF-03 G1: LoadActiveState must return results byte-identical to
// the legacy ListActive + IsUserActive pair it replaces, including the
// unknown-user, blocked-user and revoked-capability cases. Runs ONLY
// when SELLERSHO_TEST_DATABASE_URL points at a disposable migrated
// database (testdb.Open skips otherwise).
func TestPERF03LoadActiveStateMatchesLegacyPair(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SELLERSHO_TEST_DATABASE_URL is not set; skipping disposable-DB integration test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	ctx := context.Background()
	repo := NewCapabilityRepository(db)

	users := []struct {
		id     string
		mobile string
		active bool
	}{
		{"11111111-1111-4111-8111-111111111111", "09120000001", true},
		{"22222222-2222-4222-8222-222222222222", "09120000002", false},
		{"44444444-4444-4444-8444-444444444444", "09120000004", true},
	}
	for _, u := range users {
		if _, err := db.ExecContext(ctx, `INSERT INTO users (id, mobile, full_name, role, is_active) VALUES ($1, $2, 'Perf03 Test', 'customer', $3) ON CONFLICT (id) DO UPDATE SET mobile = EXCLUDED.mobile, is_active = EXCLUDED.is_active`,
			u.id, u.mobile, u.active); err != nil {
			t.Fatalf("seed user %s: %v", u.id, err)
		}
	}
	// The super-admin mobile (09933070870) already belongs to the
	// migration seed user; reuse that row instead of inserting a
	// duplicate mobile. Its is_active is whatever the seed says —
	// the test compares against the legacy pair, so it holds either
	// way. The capabilities granted below are removed in cleanup.
	var superID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM users WHERE mobile = '09933070870'`).Scan(&superID); err != nil {
		t.Fatalf("find migration super-admin user: %v", err)
	}
	t.Cleanup(func() {
		for _, u := range users {
			_, _ = db.Exec(`DELETE FROM user_capabilities WHERE user_id = $1`, u.id)
			_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, u.id)
		}
		_, _ = db.Exec(`DELETE FROM user_capabilities WHERE user_id = $1 AND source_type = 'perf03-test'`, superID)
	})
	caps := []struct {
		user, cap, status string
	}{
		{"11111111-1111-4111-8111-111111111111", "customer", "active"},
		{"11111111-1111-4111-8111-111111111111", "seller", "active"},
		{"11111111-1111-4111-8111-111111111111", "supplier", "revoked"},
		{"11111111-1111-4111-8111-111111111111", "super_admin", "active"},
		{"22222222-2222-4222-8222-222222222222", "customer", "active"},
		{superID, "super_admin", "active"},
		{superID, "customer", "active"},
	}
	for _, c := range caps {
		if _, err := db.ExecContext(ctx, `INSERT INTO user_capabilities (user_id, capability, status, source_type) VALUES ($1, $2, $3, 'perf03-test') ON CONFLICT (user_id, capability) DO UPDATE SET status = EXCLUDED.status`,
			c.user, c.cap, c.status); err != nil {
			t.Fatalf("seed capability %v: %v", c, err)
		}
	}

	ids := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		superID,
		"44444444-4444-4444-8444-444444444444",
		"55555555-5555-4555-8555-555555555555", // unknown user: no row
	}
	for _, id := range ids {
		wantActive, err := repo.IsUserActive(ctx, id)
		if err != nil {
			t.Fatalf("IsUserActive(%s): %v", id, err)
		}
		wantCaps, err := repo.ListActive(ctx, id)
		if err != nil {
			t.Fatalf("ListActive(%s): %v", id, err)
		}
		found, active, gotCaps, err := repo.LoadActiveState(ctx, id)
		if err != nil {
			t.Fatalf("LoadActiveState(%s): %v", id, err)
		}
		wantFound := id != "55555555-5555-4555-8555-555555555555"
		if found != wantFound {
			t.Errorf("user %s: found=%v, want %v", id, found, wantFound)
		}
		if active != wantActive {
			t.Errorf("user %s: active=%v, want %v (legacy IsUserActive)", id, active, wantActive)
		}
		if !slices.Equal(gotCaps, wantCaps) {
			t.Errorf("user %s: capabilities=%v, want %v (legacy ListActive)", id, gotCaps, wantCaps)
		}
		// The blocked decision the middleware makes from the pair:
		// unknown user and inactive user are both blocked.
		if blocked := !found || !active; blocked != !wantActive {
			t.Errorf("user %s: blocked=%v, want %v", id, blocked, !wantActive)
		}
	}
}

// PERF-03 G1: a non-UUID user id cannot run the indexed join; the
// fallback must still answer "unknown user" (found=false, no error)
// instead of failing the request or, worse, allowing it.
func TestPERF03LoadActiveStateNonUUIDUserIsUnknown(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SELLERSHO_TEST_DATABASE_URL is not set; skipping disposable-DB integration test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	repo := NewCapabilityRepository(db)
	found, active, caps, err := repo.LoadActiveState(context.Background(), "not-a-uuid")
	if err != nil {
		t.Fatalf("LoadActiveState(non-UUID): %v", err)
	}
	if found || active || len(caps) != 0 {
		t.Fatalf("non-UUID user: found=%v active=%v caps=%v, want unknown/empty", found, active, caps)
	}
}

// PERF-03 G1: the type-mismatch fallback triggers only on type
// errors — a real failure (unique violation, serialization failure)
// must surface, never silently fall back to stale legacy reads.
func TestPERF03IsAuthTypeMismatchOnlyForTypeErrors(t *testing.T) {
	for _, code := range []string{"42883", "42804", "22P02"} {
		if !isAuthTypeMismatch(&pq.Error{Code: pq.ErrorCode(code)}) {
			t.Errorf("code %s: want type-mismatch=true", code)
		}
	}
	for _, code := range []string{"23505", "40001", "42P01", "57014"} {
		if isAuthTypeMismatch(&pq.Error{Code: pq.ErrorCode(code)}) {
			t.Errorf("code %s: want type-mismatch=false", code)
		}
	}
	if isAuthTypeMismatch(nil) {
		t.Error("nil error: want type-mismatch=false")
	}
}
