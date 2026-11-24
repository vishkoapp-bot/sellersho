package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
)

// CatalogScopeInputs is every price input for one store scope, loaded
// inside ONE snapshot transaction. The refresh must price from the same
// snapshot its fingerprints are computed in: loading settings, locks or
// overrides outside the tx would let a concurrent edit slip between the
// load and the refresh, storing a price computed from old inputs under a
// fingerprint of the new ones (a row that reads fresh but holds a stale
// price). Both the paged response and the index refresh use these
// snapshot inputs.
type CatalogScopeInputs struct {
	Store     domain.Store // price-relevant fields only (see below)
	Settings  *pricing.PlatformPricingSettings
	Locks     []pricing.MarginLock
	Overrides map[string]pricing.StoreVariantOverride
	AsOf      time.Time
	Campaigns []domain.SellerDiscountCampaign // ALL campaigns for the store
}

// LoadCatalogScopeInputsTx loads the scope inputs in the caller's tx.
// The store row contributes only price-relevant fields (id, slug, owner
// type, widget config, margin lock, custom override, product overrides);
// display fields (name, logo, ...) stay on the handler's store object.
func (r *CatalogRepository) LoadCatalogScopeInputsTx(ctx context.Context, tx *sql.Tx, storeID string) (*CatalogScopeInputs, error) {
	inputs := &CatalogScopeInputs{
		Overrides: map[string]pricing.StoreVariantOverride{},
	}
	store := domain.Store{ID: storeID}
	var widgetRaw sql.NullString
	var customOverride sql.NullFloat64
	if err := tx.QueryRowContext(ctx, `SELECT slug, owner_type, seller_deals_widget_config,
		is_margin_locked_by_admin, custom_store_margin_override, CURRENT_TIMESTAMP
		FROM stores WHERE id = $1`, storeID).Scan(
		&store.Slug, &store.OwnerType, &widgetRaw,
		&store.IsMarginLockedByAdmin, &customOverride, &inputs.AsOf); err != nil {
		return nil, err
	}
	if widgetRaw.Valid {
		store.SellerDealsWidgetConfig = json.RawMessage(widgetRaw.String)
	}
	if customOverride.Valid {
		v := customOverride.Float64
		store.CustomStoreMarginOverride = &v
	}
	store.ProductPriceOverrides = map[string]float64{}
	store.ProductPriceOverrideModes = map[string]string{}
	prows, err := tx.QueryContext(ctx, `SELECT product_id, override_value, mode FROM store_product_overrides WHERE store_id = $1`, storeID)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var pid, mode string
		var val float64
		if err := prows.Scan(&pid, &val, &mode); err != nil {
			prows.Close()
			return nil, err
		}
		store.ProductPriceOverrides[pid] = val
		store.ProductPriceOverrideModes[pid] = mode
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return nil, err
	}
	inputs.Store = store

	settings, err := catalogPricingSettingsTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	inputs.Settings = settings

	locks, err := catalogStoreMarginLocksTx(ctx, tx, storeID)
	if err != nil {
		return nil, err
	}
	inputs.Locks = locks

	orows, err := tx.QueryContext(ctx, `SELECT store_id, product_id, variant_id, override_value, mode
		FROM store_variant_overrides WHERE store_id = $1 ORDER BY product_id, variant_id`, storeID)
	if err != nil {
		return nil, err
	}
	for orows.Next() {
		var o pricing.StoreVariantOverride
		if err := orows.Scan(&o.StoreID, &o.ProductID, &o.VariantID, &o.Value, &o.Mode); err != nil {
			orows.Close()
			return nil, err
		}
		inputs.Overrides[o.ProductID+"/"+o.VariantID] = o
	}
	orows.Close()
	if err := orows.Err(); err != nil {
		return nil, err
	}

	campaigns, err := r.listStoreCampaignsTx(ctx, tx, storeID)
	if err != nil {
		return nil, err
	}
	inputs.Campaigns = campaigns
	return inputs, nil
}

// catalogPricingSettingsTx mirrors PricingRepository.GetPlatformPricingSettings
// (settings row + pct ladder + margin ceilings) inside the caller's tx.
func catalogPricingSettingsTx(ctx context.Context, tx *sql.Tx) (*pricing.PlatformPricingSettings, error) {
	s := &pricing.PlatformPricingSettings{
		ProductPlatformPcts:  map[string]float64{},
		CategoryPlatformPcts: map[string]float64{},
	}
	var earlyWithdrawalFeePct sql.NullFloat64
	err := tx.QueryRowContext(ctx, `
		SELECT platform_pct, central_profit_pct, increase_cap_pct, increase_locked,
		       max_profit_margin, min_profit_margin, default_margin, central_store_markup_percent,
		       referral_is_enabled, referral_commission_percent,
		       settlement_day, early_withdrawal_fee_pct, wallet_min_withdrawal,
		       wallet_spend_enabled, wallet_withdraw_enabled
		FROM platform_settings WHERE id = 1
	`).Scan(
		&s.PlatformPct, &s.CentralProfitPct, &s.IncreaseCapPct, &s.IncreaseLocked,
		&s.MaxProfitMargin, &s.MinProfitMargin, &s.DefaultMargin, &s.CentralStoreMarkupPercent,
		&s.ReferralIsEnabled, &s.ReferralCommissionPercent,
		&s.SettlementDay, &earlyWithdrawalFeePct, &s.WalletMinWithdrawal,
		&s.WalletSpendEnabled, &s.WalletWithdrawEnabled,
	)
	if err != nil {
		return nil, err
	}
	s.EarlyWithdrawalFeePct = earlyWithdrawalFeePct.Float64

	prows, err := tx.QueryContext(ctx, `SELECT product_id, pct FROM platform_pct_products`)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var id string
		var pct float64
		if err := prows.Scan(&id, &pct); err != nil {
			prows.Close()
			return nil, err
		}
		s.ProductPlatformPcts[id] = pct
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return nil, err
	}
	crows, err := tx.QueryContext(ctx, `SELECT category_slug, pct FROM platform_pct_categories`)
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var slug string
		var pct float64
		if err := crows.Scan(&slug, &pct); err != nil {
			crows.Close()
			return nil, err
		}
		s.CategoryPlatformPcts[slug] = pct
	}
	crows.Close()
	if err := crows.Err(); err != nil {
		return nil, err
	}
	s.ProductMarginCeilings = map[string]float64{}
	mrows, err := tx.QueryContext(ctx, `SELECT product_id, max_margin_pct FROM product_margin_ceilings`)
	if err != nil {
		return nil, err
	}
	for mrows.Next() {
		var id string
		var v float64
		if err := mrows.Scan(&id, &v); err != nil {
			mrows.Close()
			return nil, err
		}
		s.ProductMarginCeilings[id] = v
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// catalogStoreMarginLocksTx mirrors PricingRepository.ListStoreMarginLocks
// inside the caller's tx.
func catalogStoreMarginLocksTx(ctx context.Context, tx *sql.Tx, storeID string) ([]pricing.MarginLock, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT scope, scope_id, lock_mode, lock_value
		FROM store_margin_locks WHERE store_id = $1 ORDER BY scope, scope_id
	`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	locks := []pricing.MarginLock{}
	for rows.Next() {
		var l pricing.MarginLock
		var val sql.NullFloat64
		if err := rows.Scan(&l.Scope, &l.ScopeID, &l.Mode, &val); err != nil {
			return nil, err
		}
		if val.Valid {
			v := val.Float64
			l.Value = &v
		}
		locks = append(locks, l)
	}
	return locks, rows.Err()
}
