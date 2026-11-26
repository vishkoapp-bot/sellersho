package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/drup/platform/internal/pricing"
	"github.com/google/uuid"
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
	// early_withdrawal_fee_pct is nullable by schema (migration
	// 202611020002 adds it with no default; NULL is a legitimate
	// "not configured" state). Scanning it straight into a float64
	// fails the WHOLE settings load on a NULL row ("converting NULL
	// to float64 is unsupported") and every consumer silently falls
	// back to DefaultPlatformPricingSettings, discarding the admin's
	// real row. Scan it as NullFloat64 instead: NULL maps to 0, the
	// value DefaultPlatformPricingSettings itself carries.
	var earlyWithdrawalFeePct sql.NullFloat64
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
		&s.SettlementDay, &earlyWithdrawalFeePct, &s.WalletMinWithdrawal,
		&s.WalletSpendEnabled, &s.WalletWithdrawEnabled,
	)
	if err == nil {
		s.EarlyWithdrawalFeePct = earlyWithdrawalFeePct.Float64
	}
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
	if _, err := pricing.ParsePercent(pct); err != nil {
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
	if _, err := pricing.ParsePercent(pct); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO platform_pct_products (product_id, pct, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (product_id) DO UPDATE SET pct = EXCLUDED.pct, updated_at = NOW()
	`, productID, pct)
	return err
}

// SetCategoryPlatformPct upserts one category ladder row.
func (r *PricingRepository) SetCategoryPlatformPct(ctx context.Context, categorySlug string, pct float64) error {
	if _, err := pricing.ParsePercent(pct); err != nil {
		return err
	}
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
	if err := pricing.ValidateSettings(s); err != nil {
		return err
	}
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

// VariantAdminLocked reports whether any store carries an admin_set
// margin lock covering a variant — variant-scoped on the variant, or
// product-scoped on its product (P1(c) sale gate: a sale would undercut
// the locked margin, so the sale endpoint rejects with 409). Locks are
// per-store rows, so the check scans all stores.
func (r *PricingRepository) VariantAdminLocked(ctx context.Context, productID, variantID string) (bool, error) {
	var locked bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM store_margin_locks
		WHERE lock_mode = 'admin_set' AND ((scope = 'variant' AND scope_id = $1) OR (scope = 'product' AND scope_id = $2)))`, variantID, productID).Scan(&locked)
	return locked, err
}

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
	if lock.Value != nil {
		if _, err := pricing.ParsePercent(*lock.Value); err != nil {
			return err
		}
	}
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
	if _, err := pricing.ParsePercent(o.Value); err != nil {
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

// ---------------------------------------------------------------------------
// Price-increase governance (gap 11; migrations 202610310003 +
// 202610310006). The policy recorded there is enforced here: a supplier
// reference-price increase either auto-applies (within the effective cap
// during an unlocked window, or under a per-product exception grant — a
// grant is itself an approval, including while locked and including for
// implicit-agreement products) or is queued as a pending
// supplier_price_increases row for admin review. Decreases and unchanged
// prices never reach this code: the table CHECK forbids new <= old rows,
// and the direct PUT path applies them exactly as before.
// ---------------------------------------------------------------------------

var (
	// ErrPriceIncreaseNotFound: no supplier_price_increases row with the id.
	ErrPriceIncreaseNotFound = errors.New("price increase request not found")
	// ErrPriceIncreaseNotPending: the row exists but was already reviewed.
	ErrPriceIncreaseNotPending = errors.New("price increase request is not pending")
	// ErrPriceIncreaseStaleCost: on approve, the product's current cost no
	// longer equals the row's old_reference; the row STAYS pending.
	ErrPriceIncreaseStaleCost = errors.New("قیمت کالا از زمان ثبت درخواست تغییر کرده است")
	// ErrPriceIncreaseDuplicatePending: a pending row already exists for
	// the product; a second governed increase is refused with HTTP 409.
	ErrPriceIncreaseDuplicatePending = errors.New("a pending price increase already exists for this product")
	ErrVariantWorkspacePriceRejected = errors.New("variant_workspace_price_rejected")
	// ErrGovernedProductNotFound: the product does not belong to the supplier.
	ErrGovernedProductNotFound = errors.New("product not found for supplier")
	// ErrProductNotFound: no product with the id exists at all.
	ErrProductNotFound = errors.New("product not found")
	// ErrPriceExceptionNotFound: no grant row exists for the product.
	ErrPriceExceptionNotFound = errors.New("no price exception grant exists for this product")
)

// ProductPriceException mirrors product_price_exceptions: the per-product
// grant that replaces the global increase cap for one product only.
type ProductPriceException struct {
	ProductID       string
	ExceptionCapPct float64
	TicketID        *string
	GrantedBy       string
	CreatedAt       time.Time
}

// IsImplicitAgreementActive implements the migration 202610310006 ACTIVE
// definition: the flag is TRUE AND (implicit_agreement_until IS NULL OR
// implicit_agreement_until >= NOW()). A missing product is not under an
// active agreement.
func (r *PricingRepository) IsImplicitAgreementActive(ctx context.Context, productID string) (bool, error) {
	var active bool
	err := r.db.QueryRowContext(ctx, `
		SELECT implicit_agreement AND (implicit_agreement_until IS NULL OR implicit_agreement_until >= NOW())
		FROM products WHERE id = $1
	`, productID).Scan(&active)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return active, nil
}

// GetProductPriceException returns the product's grant, or (nil, nil)
// when the product holds no grant.
func (r *PricingRepository) GetProductPriceException(ctx context.Context, productID string) (*ProductPriceException, error) {
	var ex ProductPriceException
	var ticket sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT product_id, exception_cap_pct, ticket_id, granted_by, created_at
		FROM product_price_exceptions WHERE product_id = $1
	`, productID).Scan(&ex.ProductID, &ex.ExceptionCapPct, &ticket, &ex.GrantedBy, &ex.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ticket.Valid {
		ex.TicketID = &ticket.String
	}
	return &ex, nil
}

// PriceIncreaseRow is one supplier_price_increases insert.
type PriceIncreaseRow struct {
	ProductID       string
	VariantID       string // "" = product-level request; else the product_variants row the increase targets (migration 202611050009)
	SupplierID      string
	OldReference    int64
	NewReference    int64
	Pct             float64
	Status          string // 'pending' | 'approved' | 'rejected'
	IsException     bool
	ExceptionCapPct *float64
	AutoApproved    bool
	AgreementReview bool
	ClearOnApprove  bool // variant rows only: approval applies as a clear-to-inherit (migration 202611050010)
	Reviewed        bool // stamp reviewed_at = NOW() (auto-approved rows)
}

func insertPriceIncreaseTx(ctx context.Context, tx *sql.Tx, row PriceIncreaseRow) (string, error) {
	id := "PIN-" + uuid.NewString()
	var reviewedAt interface{}
	if row.Reviewed {
		reviewedAt = time.Now()
	}
	var variantArg interface{}
	if row.VariantID != "" {
		variantArg = row.VariantID
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO supplier_price_increases
			(id, product_id, variant_id, supplier_id, old_reference, new_reference, pct, status,
			 is_exception, exception_cap_pct, auto_approved, agreement_review, clear_on_approve, reviewed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, id, row.ProductID, variantArg, row.SupplierID, row.OldReference, row.NewReference, row.Pct,
		row.Status, row.IsException, row.ExceptionCapPct, row.AutoApproved, row.AgreementReview, row.ClearOnApprove, reviewedAt)
	if err != nil {
		return "", err
	}
	return id, nil
}

// CreatePriceIncreaseRow inserts one supplier_price_increases row in its
// own transaction and returns the new row id.
func (r *PricingRepository) CreatePriceIncreaseRow(ctx context.Context, row PriceIncreaseRow) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	id, err := insertPriceIncreaseTx(ctx, tx, row)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

// priceIncreaseDecision is the recorded decision matrix for new > old,
// factored pure so the matrix (and the fail-closed schema defaults) can be
// unit-tested without a database. grantCap is nil when the product holds
// no exception grant; the effective cap is the grant cap when a grant
// exists, else the global increase_cap_pct.
type priceIncreaseDecision struct {
	Apply       bool
	Reason      string // "" when Apply; else "locked" | "above_cap" | "agreement" | "exception_cap"
	CapPct      float64
	IsException bool
}

func decidePriceIncrease(settings *pricing.PlatformPricingSettings, grantCap *float64, agreementActive bool, pct float64) priceIncreaseDecision {
	if grantCap != nil {
		if pct <= *grantCap {
			return priceIncreaseDecision{Apply: true, CapPct: *grantCap, IsException: true}
		}
		return priceIncreaseDecision{CapPct: *grantCap, Reason: "exception_cap", IsException: true}
	}
	if agreementActive {
		return priceIncreaseDecision{CapPct: settings.IncreaseCapPct, Reason: "agreement"}
	}
	if settings.IncreaseLocked {
		return priceIncreaseDecision{CapPct: settings.IncreaseCapPct, Reason: "locked"}
	}
	if pct <= settings.IncreaseCapPct {
		return priceIncreaseDecision{Apply: true, CapPct: settings.IncreaseCapPct}
	}
	return priceIncreaseDecision{CapPct: settings.IncreaseCapPct, Reason: "above_cap"}
}

// GovernedIncreaseResult is the outcome of ApplyGovernedPriceIncrease.
type GovernedIncreaseResult struct {
	Applied      bool
	AutoApproved bool
	RequestID    string // supplier_price_increases row id; "" when applied directly with no row
	OldReference int64
	Pct          float64
	CapPct       float64
	Reason       string // pending reason when Applied is false
}

// ApplyGovernedPriceIncrease runs one governed increase (new > old) in a
// single transaction: the product row is locked and its cost re-read
// (stale-check against the caller's earlier read), the recorded matrix is
// evaluated, and the increase is either applied (products.supplier_base_cost
// updated + approved audit row + supplier_pricing_update audit entry) or
// queued as a pending row with the cost untouched. A pending row already
// existing for the product refuses the request with
// ErrPriceIncreaseDuplicatePending. Settings-load failure fails CLOSED to
// the schema defaults (locked, cap 10) via
// pricing.DefaultPlatformPricingSettings. If the locked re-read shows the
// cost already reached or passed the requested value (concurrent write),
// the request degrades to the direct-apply path with no row, exactly like
// a decrease.
func (r *PricingRepository) ApplyGovernedPriceIncrease(ctx context.Context, supplierID, productID string, newCost int64, reason string) (*GovernedIncreaseResult, error) {
	settings, serr := r.GetPlatformPricingSettings(ctx)
	if serr != nil || settings == nil {
		settings = pricing.DefaultPlatformPricingSettings()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var oldCost int64
	var implicitAgreement bool
	var agreementUntil sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT supplier_base_cost, COALESCE(implicit_agreement, FALSE), implicit_agreement_until
		FROM products WHERE id = $1 AND supplier_id = $2 FOR UPDATE
	`, productID, supplierID).Scan(&oldCost, &implicitAgreement, &agreementUntil)
	if err == sql.ErrNoRows {
		return nil, ErrGovernedProductNotFound
	}
	if err != nil {
		return nil, err
	}

	applyDirect := func() (*GovernedIncreaseResult, error) {
		if _, err := tx.ExecContext(ctx, `UPDATE products SET supplier_base_cost = $1, updated_at = NOW() WHERE id = $2`, newCost, productID); err != nil {
			return nil, err
		}
		if err := insertSupplierAuditTx(ctx, tx, supplierID, "supplier_pricing_update", map[string]interface{}{
			"product_id": productID, "new_supplier_base_cost": newCost, "reason": reason,
		}); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &GovernedIncreaseResult{Applied: true, OldReference: oldCost}, nil
	}

	if newCost <= oldCost {
		return applyDirect()
	}
	pct := float64(newCost-oldCost) / float64(oldCost) * 100

	var hasPending bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM supplier_price_increases WHERE product_id = $1 AND status = 'pending')
	`, productID).Scan(&hasPending); err != nil {
		return nil, err
	}
	if hasPending {
		return nil, ErrPriceIncreaseDuplicatePending
	}

	var grantCap *float64
	var grantCapVal sql.NullFloat64
	if err := tx.QueryRowContext(ctx, `
		SELECT exception_cap_pct FROM product_price_exceptions WHERE product_id = $1
	`, productID).Scan(&grantCapVal); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if grantCapVal.Valid {
		grantCap = &grantCapVal.Float64
	}

	agreementActive := implicitAgreement && (!agreementUntil.Valid || !agreementUntil.Time.Before(time.Now()))
	decision := decidePriceIncrease(settings, grantCap, agreementActive, pct)

	row := PriceIncreaseRow{
		ProductID:    productID,
		SupplierID:   supplierID,
		OldReference: oldCost,
		NewReference: newCost,
		Pct:          pct,
		IsException:  decision.IsException,
	}
	if decision.IsException {
		cap := decision.CapPct
		row.ExceptionCapPct = &cap
	}

	if !decision.Apply {
		row.Status = "pending"
		row.AgreementReview = decision.Reason == "agreement"
		id, err := insertPriceIncreaseTx(ctx, tx, row)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &GovernedIncreaseResult{
			Applied: false, RequestID: id, OldReference: oldCost,
			Pct: pct, CapPct: decision.CapPct, Reason: decision.Reason,
		}, nil
	}

	row.Status = "approved"
	row.AutoApproved = true
	row.Reviewed = true
	id, err := insertPriceIncreaseTx(ctx, tx, row)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE products SET supplier_base_cost = $1, updated_at = NOW() WHERE id = $2`, newCost, productID); err != nil {
		return nil, err
	}
	if err := insertSupplierAuditTx(ctx, tx, supplierID, "supplier_pricing_update", map[string]interface{}{
		"product_id": productID, "new_supplier_base_cost": newCost, "reason": reason,
		"request_id": id, "auto_approved": true,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &GovernedIncreaseResult{
		Applied: true, AutoApproved: true, RequestID: id, OldReference: oldCost,
		Pct: pct, CapPct: decision.CapPct,
	}, nil
}

// ApplyGovernedVariantPriceIncrease is ApplyGovernedPriceIncrease for
// one variant's supplier_base_cost (critic fix F1, migration
// 202611050009): gap 10's write path previously applied any variant
// cost change immediately, bypassing the recorded matrix entirely.
// The decision matrix is the product one (decidePriceIncrease); the
// grant and the implicit-agreement flag are product-level and are
// read from the locked product row, exactly as for a product
// increase. The "old" reference is the variant's EFFECTIVE cost —
// its own supplier_base_cost when set, else the product's current
// cost (the inheritance every pricing read applies). A new cost at
// or below the effective cost degrades to the direct-apply path with
// no row (mirroring a product decrease). A clear likewise applies
// directly unless the variant's own cost sits
// BELOW the product's, where clearing would RAISE the effective cost
// to the product's — that raise is governed like any other increase,
// with the clear intent recorded on the row (clear_on_approve,
// migration 202611050010) so approval applies as a clear instead of
// a set. The pending-duplicate guard is
// scoped to (product, variant): a product-level pending row does not
// block a variant request and vice versa. Settings-load failure
// fails CLOSED to the schema defaults, as in the product path.
func (r *PricingRepository) ApplyGovernedVariantPriceIncrease(ctx context.Context, supplierID, productID, variantID string, newCost int64, reason string) (*GovernedIncreaseResult, error) {
	return r.applyGovernedVariantIncrease(ctx, supplierID, productID, variantID, newCost, false, reason)
}

// ApplyGovernedVariantClear evaluates restoration of a variant's inherited
// cost. A clear raises the effective cost only when the variant's own cost
// sits below the product's.
// The increase the matrix evaluates is own cost -> product cost,
// derived from the locked rows inside the transaction (never from a
// caller-side read); an approval — auto or reviewed — applies AS A
// CLEAR (the variant's cost returns to NULL and thereafter tracks
// the product's), recorded via the row's clear_on_approve flag. A
// locked re-read showing no raise (nothing to clear, or the own cost
// already meets the product's) degrades to the direct clear with no
// row, mirroring a decrease.
func (r *PricingRepository) ApplyGovernedVariantClear(ctx context.Context, supplierID, productID, variantID string, reason string) (*GovernedIncreaseResult, error) {
	return r.applyGovernedVariantIncrease(ctx, supplierID, productID, variantID, 0, true, reason)
}

type VariantCostPrecondition struct {
	Cost        *int64 `json:"cost"`
	ProductCost int64  `json:"productCost"`
	Key         string `json:"-"`
	Actor       string `json:"-"`
}

type workspaceVariantPriceReceipt struct {
	PayloadHash string                 `json:"payloadHash"`
	Outcome     GovernedIncreaseResult `json:"outcome"`
}

func readWorkspaceVariantPriceReceipt(ctx context.Context, tx *sql.Tx, supplierID, receiptID, payloadHash string) (*GovernedIncreaseResult, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT details FROM audit_logs
		WHERE id=$1 AND operator=$2 AND action='supplier_workspace_variant_price'`, receiptID, "supplier:"+supplierID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipt workspaceVariantPriceReceipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		return nil, err
	}
	if receipt.PayloadHash != payloadHash {
		return nil, ErrVariantWorkspaceConflict
	}
	result := &receipt.Outcome
	if result.Applied || result.RequestID == "" {
		return result, nil
	}
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM supplier_price_increases WHERE id=$1 AND supplier_id=$2`, result.RequestID, supplierID).Scan(&status)
	if err == sql.ErrNoRows {
		return nil, ErrVariantWorkspaceConflict
	}
	if err != nil {
		return nil, err
	}
	switch status {
	case "pending":
		return result, nil
	case "approved":
		result.Applied = true
		result.Reason = ""
		return result, nil
	case "rejected":
		return nil, ErrVariantWorkspacePriceRejected
	default:
		return nil, ErrVariantWorkspaceConflict
	}
}

func (r *PricingRepository) ApplyWorkspaceVariantCost(ctx context.Context, supplierID, productID, variantID string, cost *int64, reason string, expected VariantCostPrecondition) (*GovernedIncreaseResult, error) {
	if cost == nil {
		return r.applyGovernedVariantIncrease(ctx, supplierID, productID, variantID, 0, true, reason, expected)
	}
	return r.applyGovernedVariantIncrease(ctx, supplierID, productID, variantID, *cost, false, reason, expected)
}

func (r *PricingRepository) applyGovernedVariantIncrease(ctx context.Context, supplierID, productID, variantID string, newCost int64, clearOnApprove bool, reason string, precondition ...VariantCostPrecondition) (*GovernedIncreaseResult, error) {
	settings, serr := r.GetPlatformPricingSettings(ctx)
	if serr != nil || settings == nil {
		settings = pricing.DefaultPlatformPricingSettings()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var receiptID, payloadHash string
	if len(precondition) > 0 && precondition[0].Key != "" {
		expected := precondition[0]
		keyDigest := sha256.Sum256([]byte(supplierID + "\x00" + expected.Actor + "\x00" + expected.Key))
		receiptID = "WS-PRICE-" + hex.EncodeToString(keyDigest[:20])
		payload, err := json.Marshal(struct {
			ProductID string
			VariantID string
			Cost      int64
			Clear     bool
			Reason    string
			Expected  VariantCostPrecondition
		}{productID, variantID, newCost, clearOnApprove, reason, expected})
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(payload)
		payloadHash = hex.EncodeToString(digest[:])
		// Serialize reuse of one operation key, including requests for different products.
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, receiptID); err != nil {
			return nil, err
		}
	}

	var productCost int64
	var implicitAgreement bool
	var agreementUntil sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT supplier_base_cost, COALESCE(implicit_agreement, FALSE), implicit_agreement_until
		FROM products WHERE id = $1 AND supplier_id = $2 FOR UPDATE
	`, productID, supplierID).Scan(&productCost, &implicitAgreement, &agreementUntil)
	if err == sql.ErrNoRows {
		return nil, ErrGovernedProductNotFound
	}
	if err != nil {
		return nil, err
	}
	var variantCost sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT supplier_base_cost FROM product_variants
		WHERE id = $1 AND product_id = $2 FOR UPDATE
	`, variantID, productID).Scan(&variantCost)
	if err == sql.ErrNoRows {
		return nil, ErrGovernedProductNotFound
	}
	if err != nil {
		return nil, err
	}
	if receiptID != "" {
		if replay, err := readWorkspaceVariantPriceReceipt(ctx, tx, supplierID, receiptID, payloadHash); replay != nil || err != nil {
			return replay, err
		}
	}
	finish := func(result GovernedIncreaseResult) (*GovernedIncreaseResult, error) {
		if receiptID != "" {
			raw, err := json.Marshal(workspaceVariantPriceReceipt{PayloadHash: payloadHash, Outcome: result})
			if err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,operator,action,category,details)
				VALUES($1,$2,'supplier_workspace_variant_price','supplier',$3)`, receiptID, "supplier:"+supplierID, string(raw)); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &result, nil
	}
	oldCost := productCost
	if variantCost.Valid {
		oldCost = variantCost.Int64
	}
	if len(precondition) > 0 {
		expected := precondition[0]
		alreadyApplied := (!variantCost.Valid && clearOnApprove) || (variantCost.Valid && !clearOnApprove && variantCost.Int64 == newCost)
		if alreadyApplied {
			return finish(GovernedIncreaseResult{Applied: true, OldReference: oldCost})
		}
		matching := (!variantCost.Valid && expected.Cost == nil) || (variantCost.Valid && expected.Cost != nil && variantCost.Int64 == *expected.Cost)
		if !matching || expected.ProductCost != productCost {
			return nil, ErrVariantWorkspaceConflict
		}
	}
	if clearOnApprove {
		// The governed "increase" is the raise the clear itself would
		// cause: from the variant's effective cost up to the product's
		// cost. When the locked re-read shows no such raise (nothing
		// to clear, or the own cost already meets/exceeds the
		// product's), newCost <= oldCost below degrades to the direct
		// clear, exactly like a decrease degrades to a direct set.
		newCost = productCost
	}

	applyDirect := func() (*GovernedIncreaseResult, error) {
		if clearOnApprove {
			if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET supplier_base_cost = NULL WHERE id = $1 AND product_id = $2`, variantID, productID); err != nil {
				return nil, err
			}
			if err := insertSupplierAuditTx(ctx, tx, supplierID, "supplier_variant_pricing_update", map[string]interface{}{
				"product_id": productID, "variant_id": variantID, "cleared": true, "reason": reason,
			}); err != nil {
				return nil, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET supplier_base_cost = $1 WHERE id = $2 AND product_id = $3`, newCost, variantID, productID); err != nil {
				return nil, err
			}
			if err := insertSupplierAuditTx(ctx, tx, supplierID, "supplier_variant_pricing_update", map[string]interface{}{
				"product_id": productID, "variant_id": variantID, "new_supplier_base_cost": newCost, "reason": reason,
			}); err != nil {
				return nil, err
			}
		}
		return finish(GovernedIncreaseResult{Applied: true, OldReference: oldCost})
	}

	if newCost <= oldCost {
		return applyDirect()
	}
	pct := float64(newCost-oldCost) / float64(oldCost) * 100

	var hasPending bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM supplier_price_increases WHERE product_id = $1 AND variant_id = $2 AND status = 'pending')
	`, productID, variantID).Scan(&hasPending); err != nil {
		return nil, err
	}
	if hasPending {
		if len(precondition) > 0 && receiptID == "" {
			var existing GovernedIncreaseResult
			err := tx.QueryRowContext(ctx, `SELECT id,pct FROM supplier_price_increases
				WHERE product_id=$1 AND variant_id=$2 AND status='pending' AND new_reference=$3 AND clear_on_approve=$4 LIMIT 1`,
				productID, variantID, newCost, clearOnApprove).Scan(&existing.RequestID, &existing.Pct)
			if err == nil {
				existing.OldReference = oldCost
				return &existing, nil
			}
			if err != sql.ErrNoRows {
				return nil, err
			}
		}
		return nil, ErrPriceIncreaseDuplicatePending
	}

	var grantCap *float64
	var grantCapVal sql.NullFloat64
	if err := tx.QueryRowContext(ctx, `
		SELECT exception_cap_pct FROM product_price_exceptions WHERE product_id = $1
	`, productID).Scan(&grantCapVal); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if grantCapVal.Valid {
		grantCap = &grantCapVal.Float64
	}

	agreementActive := implicitAgreement && (!agreementUntil.Valid || !agreementUntil.Time.Before(time.Now()))
	decision := decidePriceIncrease(settings, grantCap, agreementActive, pct)

	row := PriceIncreaseRow{
		ProductID:      productID,
		VariantID:      variantID,
		SupplierID:     supplierID,
		OldReference:   oldCost,
		NewReference:   newCost,
		Pct:            pct,
		IsException:    decision.IsException,
		ClearOnApprove: clearOnApprove,
	}
	if decision.IsException {
		cap := decision.CapPct
		row.ExceptionCapPct = &cap
	}

	if !decision.Apply {
		row.Status = "pending"
		row.AgreementReview = decision.Reason == "agreement"
		id, err := insertPriceIncreaseTx(ctx, tx, row)
		if err != nil {
			return nil, err
		}
		return finish(GovernedIncreaseResult{
			Applied: false, RequestID: id, OldReference: oldCost,
			Pct: pct, CapPct: decision.CapPct, Reason: decision.Reason,
		})
	}

	row.Status = "approved"
	row.AutoApproved = true
	row.Reviewed = true
	id, err := insertPriceIncreaseTx(ctx, tx, row)
	if err != nil {
		return nil, err
	}
	auditDetails := map[string]interface{}{
		"product_id": productID, "variant_id": variantID, "reason": reason,
		"request_id": id, "auto_approved": true,
	}
	if clearOnApprove {
		if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET supplier_base_cost = NULL WHERE id = $1 AND product_id = $2`, variantID, productID); err != nil {
			return nil, err
		}
		auditDetails["cleared"] = true
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET supplier_base_cost = $1 WHERE id = $2 AND product_id = $3`, newCost, variantID, productID); err != nil {
			return nil, err
		}
		auditDetails["new_supplier_base_cost"] = newCost
	}
	if err := insertSupplierAuditTx(ctx, tx, supplierID, "supplier_variant_pricing_update", auditDetails); err != nil {
		return nil, err
	}
	return finish(GovernedIncreaseResult{
		Applied: true, AutoApproved: true, RequestID: id, OldReference: oldCost,
		Pct: pct, CapPct: decision.CapPct,
	})
}

// insertPricingAuditTx appends one audit_logs row inside the caller's
// transaction (same column shape as insertSupplierAuditTx; the operator
// carries the actor prefix, e.g. "admin:<userID>"). The category is
// 'finance', NOT 'pricing': the audit_logs_category_check constraint
// (migration 202610210001) allows exactly 16 categories and 'pricing' is
// not one of them — a 'pricing' insert is rejected with SQLSTATE 23514
// (proven by the gap-11 DB-gated tests). 'finance' is the sanctioned
// category for money-adjacent admin decisions and matches the exception
// tickets' own section = 'finance' routing.
func insertPricingAuditTx(ctx context.Context, tx *sql.Tx, operator, action string, details map[string]interface{}) error {
	payload, _ := json.Marshal(details)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (id, timestamp, operator, action, category, details)
		VALUES ($1, NOW(), $2, $3, 'finance', $4)
	`, "LOG-"+uuid.NewString(), operator, action, string(payload))
	return err
}

// ReviewPriceIncrease applies the admin decision for one pending row in a
// single transaction. Approve re-checks the current cost against the
// row's old_reference (stale guard): a drifted cost returns
// ErrPriceIncreaseStaleCost and the row STAYS pending. A successful
// approve updates products.supplier_base_cost to new_reference — or,
// for a variant row (variant_id set, migration 202611050009),
// product_variants.supplier_base_cost, with the stale guard comparing
// the variant's current EFFECTIVE cost (its own cost, else the
// product's). A variant row flagged clear_on_approve (migration
// 202611050010) applies AS A CLEAR instead: approval sets the
// variant's cost back to NULL, so its effective cost lands on the
// product's current cost — the raise the row was filed for. Reject
// changes no cost. Both stamp reviewed_at and
// write an audit_logs row (operator "admin:<userID>", category
// 'finance' — see insertPricingAuditTx).
func (r *PricingRepository) ReviewPriceIncrease(ctx context.Context, requestID string, approve bool, adminUserID string) (*SupplierPriceIncrease, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var row SupplierPriceIncrease
	var supplierID string
	err = tx.QueryRowContext(ctx, `
		SELECT id, product_id, variant_id, supplier_id, old_reference, new_reference, pct, status, is_exception,
		       exception_cap_pct, COALESCE(auto_approved, FALSE), COALESCE(agreement_review, FALSE),
		       COALESCE(clear_on_approve, FALSE), created_at, reviewed_at
		FROM supplier_price_increases WHERE id = $1 FOR UPDATE
	`, requestID).Scan(&row.ID, &row.ProductID, &row.VariantID, &supplierID, &row.OldReference, &row.NewReference,
		&row.Pct, &row.Status, &row.IsException, &row.ExceptionCapPct, &row.AutoApproved,
		&row.AgreementReview, &row.ClearOnApprove, &row.CreatedAt, &row.ReviewedAt)
	if err == sql.ErrNoRows {
		return nil, ErrPriceIncreaseNotFound
	}
	if err != nil {
		return nil, err
	}
	if row.Status != "pending" {
		return nil, ErrPriceIncreaseNotPending
	}

	action := "price_increase_rejected"
	newStatus := "rejected"
	if approve {
		if row.VariantID != nil && *row.VariantID != "" {
			// Variant row: the stale guard compares the variant's
			// EFFECTIVE cost (own cost, else the product's) and the
			// approval lands on the variant, never the product.
			var productCost int64
			err = tx.QueryRowContext(ctx, `
				SELECT supplier_base_cost FROM products WHERE id = $1 FOR UPDATE
			`, row.ProductID).Scan(&productCost)
			if err == sql.ErrNoRows {
				return nil, ErrPriceIncreaseStaleCost
			}
			if err != nil {
				return nil, err
			}
			var variantCost sql.NullInt64
			err = tx.QueryRowContext(ctx, `
				SELECT supplier_base_cost FROM product_variants WHERE id = $1 AND product_id = $2 FOR UPDATE
			`, *row.VariantID, row.ProductID).Scan(&variantCost)
			if err == sql.ErrNoRows {
				return nil, ErrPriceIncreaseStaleCost
			}
			if err != nil {
				return nil, err
			}
			current := productCost
			if variantCost.Valid {
				current = variantCost.Int64
			}
			if current != row.OldReference || (row.ClearOnApprove && productCost != row.NewReference) {
				return nil, ErrPriceIncreaseStaleCost
			}
			if row.ClearOnApprove {
				// Governed clear (migration 202611050010): approval
				// restores inheritance instead of writing the row's
				// new_reference onto the variant.
				if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET supplier_base_cost = NULL WHERE id = $1 AND product_id = $2`, *row.VariantID, row.ProductID); err != nil {
					return nil, err
				}
			} else if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET supplier_base_cost = $1 WHERE id = $2 AND product_id = $3`, row.NewReference, *row.VariantID, row.ProductID); err != nil {
				return nil, err
			}
		} else {
			var current int64
			err = tx.QueryRowContext(ctx, `
				SELECT supplier_base_cost FROM products WHERE id = $1 FOR UPDATE
			`, row.ProductID).Scan(&current)
			if err == sql.ErrNoRows || (err == nil && current != row.OldReference) {
				return nil, ErrPriceIncreaseStaleCost
			}
			if err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE products SET supplier_base_cost = $1, updated_at = NOW() WHERE id = $2`, row.NewReference, row.ProductID); err != nil {
				return nil, err
			}
		}
		action = "price_increase_approved"
		newStatus = "approved"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_price_increases SET status = $1, reviewed_at = NOW() WHERE id = $2`, newStatus, requestID); err != nil {
		return nil, err
	}
	auditDetails := map[string]interface{}{
		"request_id": requestID, "product_id": row.ProductID, "supplier_id": supplierID,
		"old_reference": row.OldReference, "new_reference": row.NewReference, "pct": row.Pct,
	}
	if row.VariantID != nil && *row.VariantID != "" {
		auditDetails["variant_id"] = *row.VariantID
	}
	if row.ClearOnApprove {
		auditDetails["clear_on_approve"] = true
	}
	if err := insertPricingAuditTx(ctx, tx, "admin:"+adminUserID, action, auditDetails); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	now := time.Now()
	row.Status = newStatus
	row.ReviewedAt = &now
	return &row, nil
}

// AdminPriceIncrease is one review-queue row: the supplier's read model
// plus the admin display fields (product title, supplier id + name).
type AdminPriceIncrease struct {
	SupplierPriceIncrease
	SupplierID   string `json:"supplierId"`
	ProductTitle string `json:"productTitle"`
	SupplierName string `json:"supplierName"`
}

// ListAdminPriceIncreases returns the review queue joined with product
// titles and supplier names, newest first. status "pending" (the queue),
// "approved", "rejected" filter; "" and "all" return every row.
func (r *PricingRepository) ListAdminPriceIncreases(ctx context.Context, status string) ([]AdminPriceIncrease, error) {
	query := `
		SELECT spi.id, spi.product_id, spi.variant_id, spi.supplier_id, spi.old_reference, spi.new_reference,
		       spi.pct, spi.status, spi.is_exception, spi.exception_cap_pct,
		       COALESCE(spi.auto_approved, FALSE), COALESCE(spi.agreement_review, FALSE),
		       COALESCE(spi.clear_on_approve, FALSE), spi.created_at, spi.reviewed_at,
		       COALESCE(p.title, ''), COALESCE(s.name, ''), COALESCE(pv.variant_name, '')
		FROM supplier_price_increases spi
		LEFT JOIN products p ON p.id = spi.product_id
		LEFT JOIN suppliers s ON s.id = spi.supplier_id
		LEFT JOIN product_variants pv ON pv.id::text = spi.variant_id
	`
	args := []interface{}{}
	if status != "" && status != "all" {
		query += ` WHERE spi.status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY spi.created_at DESC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminPriceIncrease{}
	for rows.Next() {
		var a AdminPriceIncrease
		if err := rows.Scan(&a.ID, &a.ProductID, &a.VariantID, &a.SupplierID, &a.OldReference, &a.NewReference,
			&a.Pct, &a.Status, &a.IsException, &a.ExceptionCapPct, &a.AutoApproved,
			&a.AgreementReview, &a.ClearOnApprove, &a.CreatedAt, &a.ReviewedAt, &a.ProductTitle, &a.SupplierName, &a.VariantName); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertProductPriceException creates or replaces the product's exception
// grant (a later grant replaces the earlier one) and records the grant in
// audit_logs. The product must exist.
func (r *PricingRepository) UpsertProductPriceException(ctx context.Context, productID string, capPct float64, ticketID *string, adminUserID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)`, productID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrProductNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO product_price_exceptions (product_id, exception_cap_pct, ticket_id, granted_by, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (product_id) DO UPDATE SET
			exception_cap_pct = EXCLUDED.exception_cap_pct,
			ticket_id = EXCLUDED.ticket_id,
			granted_by = EXCLUDED.granted_by,
			created_at = NOW()
	`, productID, capPct, ticketID, adminUserID); err != nil {
		return err
	}
	if err := insertPricingAuditTx(ctx, tx, "admin:"+adminUserID, "price_exception_granted", map[string]interface{}{
		"product_id": productID, "exception_cap_pct": capPct, "ticket_id": ticketID,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteProductPriceException revokes the product's grant and records the
// revocation in audit_logs. No grant → ErrPriceExceptionNotFound.
func (r *PricingRepository) DeleteProductPriceException(ctx context.Context, productID, adminUserID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM product_price_exceptions WHERE product_id = $1`, productID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrPriceExceptionNotFound
	}
	if err := insertPricingAuditTx(ctx, tx, "admin:"+adminUserID, "price_exception_revoked", map[string]interface{}{
		"product_id": productID,
	}); err != nil {
		return err
	}
	return tx.Commit()
}
