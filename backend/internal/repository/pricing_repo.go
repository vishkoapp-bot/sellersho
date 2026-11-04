package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	"github.com/drup/platform/internal/pricing"
)

// PricingRepository is the storage backing for the live percent engine
// (D14 pricing part, D24, D28, D29). Method names match the live binary's
// PricingRepository symbols (GetPlatformPct, SetProductPlatformPct, ...)
// where the audit recovered them. This file does NOT touch migrations,
// supplier data, workers, or domain models.
//
// Absolute fee overrides are read here ONLY as historical data (D30):
// GetFeeOverridesHistory returns the stored base_fee_f /
// supplier_fee_overrides / product_fee_overrides values so an admin read
// path can display them. Nothing in the price path consumes them.
type PricingRepository struct {
	db *sql.DB
}

func NewPricingRepository(db *sql.DB) *PricingRepository {
	return &PricingRepository{db: db}
}

// GetPlatformPricingSettings loads platform_settings id = 1 plus both
// platform_pct ladder tables into pricing.PlatformPricingSettings. A nil
// *int settlement_day stays nil (the live column is nullable); a missing
// settings row is an error, and callers fall back to
// pricing.DefaultPlatformPricingSettings only when the row cannot be
// loaded at all.
func (r *PricingRepository) GetPlatformPricingSettings(ctx context.Context) (*pricing.PlatformPricingSettings, error) {
	s := &pricing.PlatformPricingSettings{
		ProductPlatformPcts:  map[string]float64{},
		CategoryPlatformPcts: map[string]float64{},
	}
	err := r.db.QueryRowContext(ctx, `
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
		&s.SettlementDay, &s.EarlyWithdrawalFeePct, &s.WalletMinWithdrawal,
		&s.WalletSpendEnabled, &s.WalletWithdrawEnabled,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query platform pricing settings: %w", err)
	}
	// Absolute overrides are intentionally NOT loaded here: they are
	// historical data only (D30) and are read solely by
	// GetFeeOverridesHistory. Keeping them out of this struct's load
	// path guarantees no price computation can ever consume them.

	if pcts, err := r.ListPlatformPcts(ctx); err == nil {
		s.ProductPlatformPcts = pcts.Products
		s.CategoryPlatformPcts = pcts.Categories
	} else {
		// The ladder tables may be absent on a database that has not run
		// migration 202610310005; the engine then degrades to the global
		// platform_pct instead of failing (live binary behaviour per the
		// migration comment). Ladder errors are therefore non-fatal.
		s.ProductPlatformPcts = map[string]float64{}
		s.CategoryPlatformPcts = map[string]float64{}
	}
	if ceilings, err := r.ListProductMarginCeilings(ctx); err == nil {
		s.ProductMarginCeilings = ceilings
	} else {
		// The ceilings table may be absent on a database that has not
		// run migration 202611040020; the engine then degrades to the
		// global MaxProfitMargin instead of failing.
		s.ProductMarginCeilings = map[string]float64{}
	}
	return s, nil
}

// PlatformPctLists is the pair of ladder tables.
type PlatformPctLists struct {
	Products   map[string]float64 `json:"products"`
	Categories map[string]float64 `json:"categories"`
}

// ListPlatformPcts returns both ladder tables (empty maps when empty).
func (r *PricingRepository) ListPlatformPcts(ctx context.Context) (*PlatformPctLists, error) {
	out := &PlatformPctLists{Products: map[string]float64{}, Categories: map[string]float64{}}
	rows, err := r.db.QueryContext(ctx, `SELECT product_id, pct FROM platform_pct_products`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var pct float64
		if err := rows.Scan(&id, &pct); err != nil {
			rows.Close()
			return nil, err
		}
		out.Products[id] = pct
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	crows, err := r.db.QueryContext(ctx, `SELECT category_slug, pct FROM platform_pct_categories`)
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
		out.Categories[slug] = pct
	}
	crows.Close()
	return out, crows.Err()
}

// GetPlatformPct resolves one product's ladder value: product, then
// category, then global. It mirrors the live binary's GetPlatformPct
// query shape (SELECT pct FROM platform_pct_products WHERE product_id=$1
// first, then the category table, then platform_settings.platform_pct).
// ListProductMarginCeilings returns the per-product margin-ceiling map
// (ISO-C margin model). Empty map when the table is absent or empty.
func (r *PricingRepository) ListProductMarginCeilings(ctx context.Context) (map[string]float64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT product_id, max_margin_pct FROM product_margin_ceilings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var id string
		var v float64
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

// GetProductMarginCeiling returns (value, true) when a per-product
// ceiling row exists, (0, false) otherwise.
func (r *PricingRepository) GetProductMarginCeiling(ctx context.Context, productID string) (float64, bool, error) {
	var v float64
	err := r.db.QueryRowContext(ctx, `SELECT max_margin_pct FROM product_margin_ceilings WHERE product_id = $1`, productID).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return v, true, nil
}

// SetProductMarginCeiling upserts one per-product ceiling (admin only;
// enforced by the caller). Negative or absurd values are rejected.
func (r *PricingRepository) SetProductMarginCeiling(ctx context.Context, productID string, pct float64) error {
	if math.IsNaN(pct) || math.IsInf(pct, 0) || pct < 0 || pct > 1000 {
		return fmt.Errorf("invalid product margin ceiling %v", pct)
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO product_margin_ceilings (product_id, max_margin_pct, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (product_id) DO UPDATE SET max_margin_pct = EXCLUDED.max_margin_pct, updated_at = NOW()
	`, productID, pct)
	return err
}

// DeleteProductMarginCeiling removes a per-product ceiling row; the
// global MaxProfitMargin binds again afterwards.
func (r *PricingRepository) DeleteProductMarginCeiling(ctx context.Context, productID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM product_margin_ceilings WHERE product_id = $1`, productID)
	return err
}

func (r *PricingRepository) GetPlatformPct(ctx context.Context, productID, categorySlug string) (float64, string, error) {
	var pct float64
	err := r.db.QueryRowContext(ctx, `SELECT pct FROM platform_pct_products WHERE product_id = $1`, productID).Scan(&pct)
	if err == nil {
		return pct, "product", nil
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, "", err
	}
	if categorySlug != "" {
		err = r.db.QueryRowContext(ctx, `SELECT pct FROM platform_pct_categories WHERE category_slug = $1`, categorySlug).Scan(&pct)
		if err == nil {
			return pct, "category", nil
		}
		if err != nil && err != sql.ErrNoRows {
			return 0, "", err
		}
	}
	err = r.db.QueryRowContext(ctx, `SELECT platform_pct FROM platform_settings WHERE id = 1`).Scan(&pct)
	if err != nil {
		return 0, "", err
	}
	return pct, "global", nil
}

// GetPlatformPctsBatch resolves the ladder for many products at once
// (live binary symbol GetPlatformPctsBatch). Category/global fallbacks
// are resolved per product; products with no ladder row use the global.
func (r *PricingRepository) GetPlatformPctsBatch(ctx context.Context, productIDs []string, categoryByProduct map[string]string) (map[string]float64, error) {
	out := map[string]float64{}
	for _, id := range productIDs {
		pct, _, err := r.GetPlatformPct(ctx, id, categoryByProduct[id])
		if err != nil {
			return nil, err
		}
		out[id] = pct
	}
	return out, nil
}

// SetProductPlatformPct upserts one product ladder row.
func (r *PricingRepository) SetProductPlatformPct(ctx context.Context, productID string, pct float64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO platform_pct_products (product_id, pct, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (product_id) DO UPDATE SET pct = EXCLUDED.pct, updated_at = NOW()
	`, productID, pct)
	return err
}

// SetCategoryPlatformPct upserts one category ladder row.
func (r *PricingRepository) SetCategoryPlatformPct(ctx context.Context, categorySlug string, pct float64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO platform_pct_categories (category_slug, pct, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (category_slug) DO UPDATE SET pct = EXCLUDED.pct, updated_at = NOW()
	`, categorySlug, pct)
	return err
}

// DeleteProductPlatformPct removes one product ladder row.
func (r *PricingRepository) DeleteProductPlatformPct(ctx context.Context, productID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM platform_pct_products WHERE product_id = $1`, productID)
	return err
}

// DeleteCategoryPlatformPct removes one category ladder row.
func (r *PricingRepository) DeleteCategoryPlatformPct(ctx context.Context, categorySlug string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM platform_pct_categories WHERE category_slug = $1`, categorySlug)
	return err
}

// UpdatePlatformPricingSettings writes exactly the live binary's settings
// update (D14): platform_pct, central_profit_pct, increase_cap_pct,
// increase_locked, max_profit_margin. Other platform_settings columns are
// deliberately not written here; they belong to other settings families.
func (r *PricingRepository) UpdatePlatformPricingSettings(ctx context.Context, s *pricing.PlatformPricingSettings) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE platform_settings
		SET platform_pct = $1, central_profit_pct = $2, increase_cap_pct = $3,
		    increase_locked = $4, max_profit_margin = $5
		WHERE id = 1
	`, s.PlatformPct, s.CentralProfitPct, s.IncreaseCapPct, s.IncreaseLocked, s.MaxProfitMargin)
	return err
}

// FeeOverridesHistory is the historical absolute-override read path (D30).
// PriceInput is always false: these values must never feed a price.
type FeeOverridesHistory struct {
	BaseFeeF             int64            `json:"baseFeeF"`
	SupplierFeeOverrides map[string]int64 `json:"supplierFeeOverrides"`
	ProductFeeOverrides  map[string]int64 `json:"productFeeOverrides"`
	PriceInput           bool             `json:"priceInput"` // always false
}

// GetFeeOverridesHistory reads the stored absolute overrides for display
// only. The live row stores them as JSONB; this decodes them in SQL to
// text and parses them in Go. If the columns are absent or unparsable,
// empty maps are returned rather than an error, because history display
// must not break pricing.
func (r *PricingRepository) GetFeeOverridesHistory(ctx context.Context) (*FeeOverridesHistory, error) {
	out := &FeeOverridesHistory{
		SupplierFeeOverrides: map[string]int64{},
		ProductFeeOverrides:  map[string]int64{},
		PriceInput:           false,
	}
	var sup, prod sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT base_fee_f,
		       COALESCE(supplier_fee_overrides::text, ''),
		       COALESCE(product_fee_overrides::text, '')
		FROM platform_settings WHERE id = 1
	`).Scan(&out.BaseFeeF, &sup, &prod)
	if err != nil {
		return out, err
	}
	// Values are JSON objects mapping id -> int64. Parsing is best
	// effort: a malformed historical value yields an empty map, never a
	// price input.
	if sup.Valid && sup.String != "" {
		_ = decodeFeeOverrideJSON(sup.String, out.SupplierFeeOverrides)
	}
	if prod.Valid && prod.String != "" {
		_ = decodeFeeOverrideJSON(prod.String, out.ProductFeeOverrides)
	}
	return out, nil
}

// decodeFeeOverrideJSON is implemented in pricing_repo_json.go (kept
// separate so this file stays free of encoding/json plumbing).
func decodeFeeOverrideJSON(text string, into map[string]int64) error {
	return decodeFeeOverrides(text, into)
}

// --- Margin locks (D29) ---

// ListStoreMarginLocks returns all locks for a store (0 rows in live).
func (r *PricingRepository) ListStoreMarginLocks(ctx context.Context, storeID string) ([]pricing.MarginLock, error) {
	rows, err := r.db.QueryContext(ctx, `
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

// SetStoreMarginLock creates or replaces one lock. For admin_set,
// lock_value is required (enforced by the table CHECK); for freeze it
// may be nil. Snapshot semantics (migration 202610310015): creating a
// lock snapshots the seller's current override for that scope, so that
// deleting the lock can restore it. Snapshot rows are written
// best-effort from the store override tables; a lock without a snapshot
// simply keeps the current margin on unlock (the migration's documented
// fallback). No lock semantics beyond freeze/admin_set are invented.
func (r *PricingRepository) SetStoreMarginLock(ctx context.Context, storeID string, lock pricing.MarginLock) error {
	if lock.Mode == "admin_set" && lock.Value == nil {
		return fmt.Errorf("admin_set lock requires lock_value")
	}
	if lock.Mode != "freeze" && lock.Mode != "admin_set" {
		return fmt.Errorf("unknown lock_mode %q (only freeze and admin_set are live)", lock.Mode)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Snapshot the seller override for this scope, if any.
	if err := r.snapshotLockTargetTx(ctx, tx, storeID, lock); err != nil {
		return err
	}
	var val interface{}
	if lock.Value != nil {
		val = *lock.Value
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO store_margin_locks (store_id, scope, scope_id, lock_mode, lock_value, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (store_id, scope, scope_id)
		DO UPDATE SET lock_mode = EXCLUDED.lock_mode, lock_value = EXCLUDED.lock_value, updated_at = NOW()
	`, storeID, lock.Scope, lock.ScopeID, lock.Mode, val); err != nil {
		return err
	}
	return tx.Commit()
}

// snapshotLockTargetTx records the pre-lock seller override for one scope
// into store_margin_lock_snapshots (one row per target). Variant locks
// snapshot the variant override; product locks snapshot the product
// override; niche locks snapshot the niche override.
func (r *PricingRepository) snapshotLockTargetTx(ctx context.Context, tx *sql.Tx, storeID string, lock pricing.MarginLock) error {
	var table, idCol, targetKind string
	switch lock.Scope {
	case "product":
		table, idCol, targetKind = "store_product_overrides", "product_id", "product"
	case "niche":
		table, idCol, targetKind = "store_niche_overrides", "niche_id", "niche"
	case "variant":
		table, idCol, targetKind = "store_variant_overrides", "variant_id", "variant"
	default:
		return fmt.Errorf("unknown lock scope %q", lock.Scope)
	}
	var val sql.NullFloat64
	var mode sql.NullString
	q := fmt.Sprintf(`SELECT override_value, mode FROM %s WHERE store_id = $1 AND %s = $2`, table, idCol)
	err := tx.QueryRowContext(ctx, q, storeID, lock.ScopeID).Scan(&val, &mode)
	had := err == nil
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	var v interface{}
	if val.Valid {
		v = val.Float64
	}
	var m interface{}
	if mode.Valid {
		m = mode.String
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO store_margin_lock_snapshots
		  (store_id, scope, scope_id, target_kind, target_id, had_value, override_value, mode, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (store_id, scope, scope_id, target_kind, target_id)
		DO UPDATE SET had_value = EXCLUDED.had_value, override_value = EXCLUDED.override_value, mode = EXCLUDED.mode
	`, storeID, lock.Scope, lock.ScopeID, targetKind, lock.ScopeID, had, v, m)
	return err
}

// DeleteStoreMarginLock removes a lock and restores the snapshotted
// seller override for that scope (or deletes the override when the
// snapshot recorded had_value = false, per migration 202610310015).
func (r *PricingRepository) DeleteStoreMarginLock(ctx context.Context, storeID, scope, scopeID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT target_kind, target_id, had_value, override_value, mode
		FROM store_margin_lock_snapshots
		WHERE store_id = $1 AND scope = $2 AND scope_id = $3
	`, storeID, scope, scopeID)
	if err != nil {
		return err
	}
	type snap struct {
		kind string
		id   string
		had  bool
		val  sql.NullFloat64
		mode sql.NullString
	}
	var snaps []snap
	for rows.Next() {
		var s snap
		if err := rows.Scan(&s.kind, &s.id, &s.had, &s.val, &s.mode); err != nil {
			rows.Close()
			return err
		}
		snaps = append(snaps, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range snaps {
		var table, idCol string
		switch s.kind {
		case "product":
			table, idCol = "store_product_overrides", "product_id"
		case "niche":
			table, idCol = "store_niche_overrides", "niche_id"
		case "variant":
			table, idCol = "store_variant_overrides", "variant_id"
		default:
			continue
		}
		if !s.had {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE store_id = $1 AND %s = $2`, table, idCol), storeID, s.id); err != nil {
				return err
			}
			continue
		}
		mode := "percent"
		if s.mode.Valid && s.mode.String != "" {
			mode = s.mode.String
		}
		var v float64
		if s.val.Valid {
			v = s.val.Float64
		}
		if s.kind == "variant" {
			// Variant overrides also carry product_id; restoring one
			// without it is impossible from the snapshot alone, so a
			// variant snapshot without a matching existing row is
			// skipped rather than guessed. (Snapshot rows store the
			// variant target; the product link lives on the override
			// row itself when it exists.)
			if _, err := tx.ExecContext(ctx, `
				UPDATE store_variant_overrides SET override_value = $3, mode = $4, updated_at = NOW()
				WHERE store_id = $1 AND variant_id = $2
			`, storeID, s.id, v, mode); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s (store_id, %s, override_value, mode) VALUES ($1, $2, $3, $4)
			ON CONFLICT (store_id, %s) DO UPDATE SET override_value = EXCLUDED.override_value, mode = EXCLUDED.mode
		`, table, idCol, idCol), storeID, s.id, v, mode); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM store_margin_lock_snapshots WHERE store_id = $1 AND scope = $2 AND scope_id = $3`, storeID, scope, scopeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM store_margin_locks WHERE store_id = $1 AND scope = $2 AND scope_id = $3`, storeID, scope, scopeID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListStoreMarginLockSnapshots returns snapshot rows for a store/scope.
func (r *PricingRepository) ListStoreMarginLockSnapshots(ctx context.Context, storeID, scope, scopeID string) ([]map[string]interface{}, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT target_kind, target_id, had_value, override_value, mode, created_at
		FROM store_margin_lock_snapshots
		WHERE store_id = $1 AND scope = $2 AND scope_id = $3
		ORDER BY target_kind, target_id
	`, storeID, scope, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var kind, id, mode string
		var had bool
		var val sql.NullFloat64
		var createdAt interface{}
		// mode may be NULL in the table; scan into NullString first.
		var modeNull sql.NullString
		if err := rows.Scan(&kind, &id, &had, &val, &modeNull, &createdAt); err != nil {
			return nil, err
		}
		if modeNull.Valid {
			mode = modeNull.String
		}
		row := map[string]interface{}{"targetKind": kind, "targetId": id, "hadValue": had, "mode": mode, "createdAt": createdAt}
		if val.Valid {
			row["overrideValue"] = val.Float64
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// --- Variant overrides (D28) ---

// SetStoreVariantOverride upserts one store_variant_overrides row
// (live binary symbol SetStoreVariantOverride). The table is 0 rows in
// live; this implements the mechanism, not fake data.
func (r *PricingRepository) SetStoreVariantOverride(ctx context.Context, o pricing.StoreVariantOverride) error {
	// Percent-only: an empty mode means percent; any other mode
	// (including "fixed") is rejected, never silently ignored.
	if o.Mode == "" {
		o.Mode = "percent"
	}
	if o.Mode != "percent" {
		return fmt.Errorf("fixed variant override mode is not supported; margins are percent-only (%q)", o.Mode)
	}
	// ISO-C: variant overrides previously bypassed every margin bound (no
	// range check at all) while product overrides are validated against
	// [floor, ceiling]. Reject non-finite and negative values; percent
	// values must also sit inside the binding range for that product
	// (per-product ceiling replaces the global MaxProfitMargin when set).
	// Settings load failure fails closed.
	if math.IsNaN(o.Value) || math.IsInf(o.Value, 0) || o.Value < 0 {
		return fmt.Errorf("invalid variant override value %v", o.Value)
	}
	if o.Mode == "percent" {
		settings, err := r.GetPlatformPricingSettings(ctx)
		if err != nil {
			return err
		}
		floor := math.Max(0, settings.MinProfitMargin)
		ceiling := settings.MaxProfitMargin
		if v, ok := settings.ProductMarginCeilings[o.ProductID]; ok {
			ceiling = v
		}
		if o.Value < floor || o.Value > ceiling {
			return fmt.Errorf("variant override %v outside allowed range [%v, %v]", o.Value, floor, ceiling)
		}
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO store_variant_overrides (store_id, product_id, variant_id, override_value, mode, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (store_id, product_id, variant_id)
		DO UPDATE SET override_value = EXCLUDED.override_value, mode = EXCLUDED.mode, updated_at = NOW()
	`, o.StoreID, o.ProductID, o.VariantID, o.Value, o.Mode)
	return err
}

// DeleteStoreVariantOverride deletes one row (live binary symbol
// DeleteStoreVariantOverride, queried by variant_id).
func (r *PricingRepository) DeleteStoreVariantOverride(ctx context.Context, storeID, productID, variantID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM store_variant_overrides
		WHERE store_id = $1 AND product_id = $2 AND variant_id = $3
	`, storeID, productID, variantID)
	return err
}

// ListStoreVariantOverrides returns all variant overrides for a store.
func (r *PricingRepository) ListStoreVariantOverrides(ctx context.Context, storeID string) ([]pricing.StoreVariantOverride, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT store_id, product_id, variant_id, override_value, mode
		FROM store_variant_overrides WHERE store_id = $1 ORDER BY product_id, variant_id
	`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pricing.StoreVariantOverride{}
	for rows.Next() {
		var o pricing.StoreVariantOverride
		if err := rows.Scan(&o.StoreID, &o.ProductID, &o.VariantID, &o.Value, &o.Mode); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// GetStoreVariantOverride returns one variant override, or nil.
func (r *PricingRepository) GetStoreVariantOverride(ctx context.Context, storeID, productID, variantID string) (*pricing.StoreVariantOverride, error) {
	var o pricing.StoreVariantOverride
	err := r.db.QueryRowContext(ctx, `
		SELECT store_id, product_id, variant_id, override_value, mode
		FROM store_variant_overrides WHERE store_id = $1 AND product_id = $2 AND variant_id = $3
	`, storeID, productID, variantID).Scan(&o.StoreID, &o.ProductID, &o.VariantID, &o.Value, &o.Mode)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}
