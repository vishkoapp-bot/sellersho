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
			id INT  DEFAULT 1 PRIMARY KEY,
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
		,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			supplier_contract_version INTEGER NOT NULL DEFAULT 2,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attribution_model character varying(20) NOT NULL DEFAULT 'last_touch'::character varying,
			base_fee_f bigint NOT NULL DEFAULT 100000,
			central_campaign_budget bigint NOT NULL DEFAULT 0,
			cookie_days integer NOT NULL DEFAULT 30,
			default_item_weight_grams integer NOT NULL DEFAULT 500,
			default_markup_type character varying(10) NOT NULL DEFAULT 'percent'::character varying,
			default_markup_value numeric(10,2) NOT NULL DEFAULT 30,
			delivery_followup_trigger_days integer NOT NULL DEFAULT 10,
			delivery_hold_period_days integer NOT NULL DEFAULT 20,
			ippanel_api_key text NOT NULL DEFAULT ''::text,
			ippanel_base_url text NOT NULL DEFAULT 'https://edge.ippanel.com/v1'::text,
			ippanel_enabled boolean NOT NULL DEFAULT false,
			ippanel_pattern_code text NOT NULL DEFAULT ''::text,
			ippanel_sender text NOT NULL DEFAULT '+983000505'::text,
			max_dropshipper_margin_percent numeric(5,2) NOT NULL DEFAULT 100,
			packaging_tariffs jsonb,
			product_fee_overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
			referral_subject_to_delivery_lock boolean NOT NULL DEFAULT true,
			return_window_days integer NOT NULL DEFAULT 7,
			seller_contract_text text NOT NULL DEFAULT ''::text,
			seller_contract_version integer NOT NULL DEFAULT 2,
			shipping_base_rate bigint NOT NULL DEFAULT 0,
			shipping_per_kg bigint NOT NULL DEFAULT 0,
			storage_fee_enabled boolean NOT NULL DEFAULT false,
			storage_fee_free_days integer NOT NULL DEFAULT 30,
			storage_fee_per_kg_day bigint NOT NULL DEFAULT 2000,
			storage_fee_per_m3_day bigint NOT NULL DEFAULT 8000,
			stuck_order_expiry_enabled boolean NOT NULL DEFAULT false,
			supplier_contract_text text NOT NULL DEFAULT ''::text,
			supplier_fee_overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
			supplier_return_postal_charge bigint,
			tapin_default_box_id text NOT NULL DEFAULT '4'::text,
			tapin_platform_shop_id text,
			tapin_platform_token text,
			tapin_tariff_url text,
			trial_catalog_quota integer NOT NULL DEFAULT 25,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			volumetric_divisor integer NOT NULL DEFAULT 5000,
			warehouse_tariffs jsonb NOT NULL DEFAULT '{}'::jsonb)`,
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
