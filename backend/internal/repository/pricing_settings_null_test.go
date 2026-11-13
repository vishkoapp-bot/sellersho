package repository

import (
	"context"
	"os"
	"testing"

	"github.com/drup/platform/internal/testdb"

	_ "github.com/lib/pq"
)

// TestGetPlatformPricingSettingsNullEarlyWithdrawalFee pins the
// NULL-tolerance of the settings loader: migration 202611020002 adds
// early_withdrawal_fee_pct with no default, so a platform_settings row
// may legitimately carry NULL there. Before the NullFloat64 scan fix,
// that NULL failed the whole load ("converting NULL to float64 is
// unsupported") and every pricing consumer silently fell back to
// DefaultPlatformPricingSettings, discarding the admin's real row.
func TestGetPlatformPricingSettingsNullEarlyWithdrawalFee(t *testing.T) {
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB integration test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	ddl := []string{
		`DROP TABLE IF EXISTS platform_settings CASCADE`,
		`CREATE TABLE platform_settings (
			id INT PRIMARY KEY,
			platform_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
			central_profit_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
			increase_cap_pct DOUBLE PRECISION NOT NULL DEFAULT 10,
			increase_locked BOOLEAN NOT NULL DEFAULT TRUE,
			max_profit_margin DOUBLE PRECISION NOT NULL DEFAULT 35,
			min_profit_margin DOUBLE PRECISION NOT NULL DEFAULT 5,
			default_margin DOUBLE PRECISION NOT NULL DEFAULT 20,
			central_store_markup_percent DOUBLE PRECISION NOT NULL DEFAULT 0,
			referral_is_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			referral_commission_percent DOUBLE PRECISION NOT NULL DEFAULT 0,
			settlement_day INT,
			early_withdrawal_fee_pct NUMERIC(5,2),
			wallet_min_withdrawal BIGINT NOT NULL DEFAULT 0,
			wallet_spend_enabled BOOLEAN NOT NULL DEFAULT TRUE,
			wallet_withdraw_enabled BOOLEAN NOT NULL DEFAULT TRUE
		)`,
		// Distinctive non-default values prove the loaded row — not the
		// fallback — is what comes back.
		`INSERT INTO platform_settings (id, platform_pct, increase_cap_pct, early_withdrawal_fee_pct)
		 VALUES (1, 7.5, 12, NULL)`,
	}
	for _, q := range ddl {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("ddl %q: %v", q, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS platform_settings CASCADE`)
	})

	repo := NewPricingRepository(db)
	s, err := repo.GetPlatformPricingSettings(ctx)
	if err != nil {
		t.Fatalf("settings with NULL early_withdrawal_fee_pct must load, got error: %v", err)
	}
	if s.EarlyWithdrawalFeePct != 0 {
		t.Errorf("EarlyWithdrawalFeePct = %v, want 0 for a NULL column (the DefaultPlatformPricingSettings value)", s.EarlyWithdrawalFeePct)
	}
	if s.PlatformPct != 7.5 || s.IncreaseCapPct != 12 {
		t.Errorf("loaded row values lost (fell back to defaults?): platform_pct=%v increase_cap_pct=%v, want 7.5/12", s.PlatformPct, s.IncreaseCapPct)
	}

	if _, err := db.ExecContext(ctx, `UPDATE platform_settings SET early_withdrawal_fee_pct = 5.00 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	s, err = repo.GetPlatformPricingSettings(ctx)
	if err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	if s.EarlyWithdrawalFeePct != 5 {
		t.Errorf("EarlyWithdrawalFeePct = %v, want 5 for a configured column", s.EarlyWithdrawalFeePct)
	}
}
