package seeder

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type Seeder struct {
	db  *sql.DB
	env string
}

func NewSeeder(db *sql.DB, env string) *Seeder {
	return &Seeder{
		db:  db,
		env: strings.ToLower(strings.TrimSpace(env)),
	}
}

// SeedBootstrap seeds essential system-level data (settings, base categories, official niches).
// This is idempotent and safe across all environments.
func (s *Seeder) SeedBootstrap(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Platform Settings
	settings := DefaultBootstrapSettings()
	querySettings := `
		INSERT INTO platform_settings (
			id, base_fee_f, min_profit_margin, max_profit_margin, default_margin,
			attribution_model, cookie_days, delivery_hold_period_days, delivery_followup_trigger_days,
			referral_commission_percent, referral_is_enabled, referral_subject_to_delivery_lock,
			seller_contract_version, seller_contract_text, supplier_contract_version, supplier_contract_text
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
		)
		ON CONFLICT (id) DO UPDATE SET
			base_fee_f = EXCLUDED.base_fee_f,
			min_profit_margin = EXCLUDED.min_profit_margin,
			max_profit_margin = EXCLUDED.max_profit_margin,
			default_margin = EXCLUDED.default_margin,
			attribution_model = EXCLUDED.attribution_model,
			cookie_days = EXCLUDED.cookie_days,
			delivery_hold_period_days = EXCLUDED.delivery_hold_period_days,
			delivery_followup_trigger_days = EXCLUDED.delivery_followup_trigger_days,
			referral_commission_percent = EXCLUDED.referral_commission_percent,
			referral_is_enabled = EXCLUDED.referral_is_enabled,
			referral_subject_to_delivery_lock = EXCLUDED.referral_subject_to_delivery_lock,
			seller_contract_version = EXCLUDED.seller_contract_version,
			seller_contract_text = EXCLUDED.seller_contract_text,
			supplier_contract_version = EXCLUDED.supplier_contract_version,
			supplier_contract_text = EXCLUDED.supplier_contract_text,
			updated_at = NOW()
	`
	if _, err := tx.ExecContext(ctx, querySettings,
		settings.ID, settings.BaseFeeF, settings.MinProfitMargin, settings.MaxProfitMargin, settings.DefaultMargin,
		settings.AttributionModel, settings.CookieDays, settings.DeliveryHoldPeriodDays, settings.DeliveryFollowupTriggerDays,
		settings.ReferralCommissionPercent, settings.ReferralIsEnabled, settings.ReferralSubjectToDeliveryLock,
		settings.SellerContractVersion, settings.SellerContractText, settings.SupplierContractVersion, settings.SupplierContractText,
	); err != nil {
		return fmt.Errorf("failed to bootstrap platform_settings: %w", err)
	}

	// 2. Categories
	queryCategory := `
		INSERT INTO categories (id, title, slug, icon_name, gradient, image_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (slug) DO UPDATE SET
			title = EXCLUDED.title,
			icon_name = EXCLUDED.icon_name,
			gradient = EXCLUDED.gradient,
			image_url = EXCLUDED.image_url
	`
	for _, cat := range DefaultBootstrapCategories() {
		if _, err := tx.ExecContext(ctx, queryCategory, cat.ID, cat.Title, cat.Slug, cat.IconName, cat.Gradient, cat.ImageURL); err != nil {
			return fmt.Errorf("failed to bootstrap category %s: %w", cat.ID, err)
		}
	}

	// 3. Niches
	queryNiche := `
		INSERT INTO niches (id, title, slug, category_slug, badge, description)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (slug) DO UPDATE SET
			title = EXCLUDED.title,
			category_slug = EXCLUDED.category_slug,
			badge = EXCLUDED.badge,
			description = EXCLUDED.description
	`
	for _, n := range DefaultBootstrapNiches() {
		if _, err := tx.ExecContext(ctx, queryNiche, n.ID, n.Title, n.Slug, n.CategorySlug, n.Badge, n.Description); err != nil {
			return fmt.Errorf("failed to bootstrap niche %s: %w", n.ID, err)
		}
	}

	return tx.Commit()
}
