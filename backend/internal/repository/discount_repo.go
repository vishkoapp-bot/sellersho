package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/pricing"
	"strings"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Seller discount codes and active-campaign lookup (Phase 3, Team
// Discounts). Campaign CRUD itself is live-parity (Team 05, D48,
// seller_panel_repo.go) and is reused unchanged — this file only
// ADDS the price-application lookup on top of it plus the codes
// table, which live-parity does not have.
//
// Money rules: see internal/pricing/discount.go. In percent-pricing
// terms the campaign target margin is a percent of base cost B,
// where B = R (reference cost after the platform percent).
//
// Ownership: the seller-panel handler authorises the store via
// OwnedStore before calling these methods (the same pattern Team 05
// uses for campaigns), so the methods below are scoped by store_id
// and do not re-check owner_user_id. GetActiveDiscountCampaign is
// the one exception that reads the store row itself, for the lock
// check.

// DiscountCode is one row of seller_discount_codes (migration
// 202611030002, new in this branch; the table does not exist in
// live-parity).
type DiscountCode struct {
	Revision  int64     `json:"revision"`
	ID        string    `json:"id"`
	StoreID   string    `json:"storeId"`
	Code      string    `json:"code"`
	Mode      string    `json:"mode"` // 'percent' | 'fixed'
	Value     float64   `json:"value"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    time.Time `json:"endsAt"`
	IsActive  bool      `json:"isActive"`
	MaxUses   *int      `json:"maxUses,omitempty"`
	UsedCount int       `json:"usedCount"`
	CreatedAt time.Time `json:"createdAt"`
}

func validateDiscountWindow(start, end time.Time) error {
	if !end.After(start) {
		return fmt.Errorf("ends_at must be after starts_at")
	}
	return nil
}

func normalizeDiscountCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func (r *StoreRepository) CreateDiscountCode(ctx context.Context, storeID string, c DiscountCode) (*DiscountCode, error) {
	c.Code = normalizeDiscountCode(c.Code)
	if c.Code == "" || len(c.Code) > 32 {
		return nil, fmt.Errorf("invalid discount code")
	}
	if c.Mode != "percent" && c.Mode != "fixed" {
		return nil, fmt.Errorf("invalid mode")
	}
	if err := pricing.ValidateDiscount(c.Mode, c.Value); err != nil {
		return nil, fmt.Errorf("discount value must be non-negative")
	}
	if err := validateDiscountWindow(c.StartsAt, c.EndsAt); err != nil {
		return nil, err
	}
	c.ID = "dcode-" + uuid.NewString()
	_, err := r.db.ExecContext(ctx, `INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at,is_active,max_uses,used_count) VALUES($1,$2,$3,$4,$5,$6,$7,TRUE,$8,0)`, c.ID, storeID, c.Code, c.Mode, c.Value, c.StartsAt, c.EndsAt, c.MaxUses)
	if err != nil {
		return nil, err
	}
	c.StoreID = storeID
	c.IsActive = true
	c.Revision = 1
	return &c, nil
}

func (r *StoreRepository) ListDiscountCodes(ctx context.Context, storeID string) ([]DiscountCode, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,store_id,code,mode,value,starts_at,ends_at,is_active,max_uses,used_count,created_at,revision FROM seller_discount_codes WHERE store_id=$1 ORDER BY created_at DESC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscountCode{}
	for rows.Next() {
		var c DiscountCode
		if err := rows.Scan(&c.ID, &c.StoreID, &c.Code, &c.Mode, &c.Value, &c.StartsAt, &c.EndsAt, &c.IsActive, &c.MaxUses, &c.UsedCount, &c.CreatedAt, &c.Revision); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *StoreRepository) SetDiscountCodeActive(ctx context.Context, storeID, codeID string, active bool) error {
	res, err := r.db.ExecContext(ctx, `UPDATE seller_discount_codes SET is_active=$1, updated_at=NOW() WHERE id=$2 AND store_id=$3`, active, codeID, storeID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrStoreForbidden
	}
	return nil
}

// ErrDiscountCodeUsed is returned by DeleteDiscountCode when the code
// has been used (used_count > 0) or any order references its code
// string for the store. Deleting a used code would orphan the order
// audit trail (orders carry the code as free text), so the handler
// maps this to HTTP 409. Only a 0-use, unreferenced code can be
// deleted — deleting one cannot change any placed order's total.
var ErrDiscountCodeUsed = errors.New("discount code has been used and cannot be deleted")

// DeleteDiscountCode hard-deletes a seller discount code, 0-use-only.
// The guard is enforced here, server-side, never trusted from the
// client: the row is locked FOR UPDATE (which serialises against a
// checkout reserving the code), used_count must be 0, and no order
// for the store may reference the code string. A missing id — or a
// code belonging to another store — returns ErrStoreForbidden, the
// same answer as the toggle path, so ownership is never leaked.
func (r *StoreRepository) DeleteDiscountCode(ctx context.Context, storeID, codeID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var code string
	var used int
	err = tx.QueryRowContext(ctx, `SELECT code, used_count FROM seller_discount_codes WHERE id=$1 AND store_id=$2 FOR UPDATE`, codeID, storeID).Scan(&code, &used)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrStoreForbidden
		}
		return err
	}
	if used > 0 {
		return ErrDiscountCodeUsed
	}
	// Belt-and-braces audit check: orders.discount_code is free text,
	// so a 0 used_count with a live order reference must still refuse.
	var referenced int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE store_id=$1 AND UPPER(discount_code)=$2`, storeID, normalizeDiscountCode(code)).Scan(&referenced); err != nil {
		return err
	}
	if referenced > 0 {
		return ErrDiscountCodeUsed
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM seller_discount_codes WHERE id=$1 AND store_id=$2 AND used_count=0`, codeID, storeID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// used_count moved under us (the FOR UPDATE lock makes this
		// near-impossible, but never delete on a stale read).
		return ErrDiscountCodeUsed
	}
	return tx.Commit()
}

// ValidateDiscountCode returns the code if it is active, in window and
// not exhausted. This is advisory only; the order transaction reserves capacity.
func (r *StoreRepository) ValidateDiscountCode(ctx context.Context, storeID, code string) (*DiscountCode, error) {
	code = normalizeDiscountCode(code)
	var c DiscountCode
	err := r.db.QueryRowContext(ctx, `SELECT id,store_id,code,mode,value,starts_at,ends_at,is_active,max_uses,used_count,created_at,revision FROM seller_discount_codes WHERE store_id=$1 AND code=$2 AND is_active=TRUE AND starts_at <= NOW() AND ends_at > NOW()`, storeID, code).Scan(&c.ID, &c.StoreID, &c.Code, &c.Mode, &c.Value, &c.StartsAt, &c.EndsAt, &c.IsActive, &c.MaxUses, &c.UsedCount, &c.CreatedAt, &c.Revision)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("discount code is invalid or expired")
		}
		return nil, err
	}
	if c.MaxUses != nil && c.UsedCount >= *c.MaxUses {
		return nil, ErrCouponExhausted
	}
	return &c, nil
}

// IncrementDiscountCodeUse is retained for source compatibility. Capacity can
// only change with the order-owned reservation lifecycle.
func (r *StoreRepository) IncrementDiscountCodeUse(ctx context.Context, codeID string) error {
	return ErrCouponLifecycleRequired
}

// DiscountCampaignCreationBlocked reports whether a new discount
// campaign for the given scope/target must be rejected because an
// admin margin lock covers it: the store-wide boolean, or a
// product/niche lock row for the target. Campaigns created under a
// lock could never apply (GetActiveDiscountCampaign returns nil
// under a lock), so creation fails fast instead of silently creating
// a dead campaign. (Codes are NOT blocked here: code application does
// not consult the lock today, so blocking code creation would be
// inconsistent -- that application gap is flagged in Fix 14.)
func (r *StoreRepository) DiscountCampaignCreationBlocked(ctx context.Context, storeID, scope, targetID string) (bool, error) {
	var marginLocked bool
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(is_margin_locked_by_admin, FALSE) FROM stores WHERE id=$1`, storeID).Scan(&marginLocked); err != nil {
		return false, err
	}
	if marginLocked {
		return true, nil
	}
	if (scope == "product" || scope == "niche" || scope == "variant") && targetID != "" {
		var n int
		if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_margin_locks WHERE store_id=$1 AND scope=$2 AND scope_id=$3`, storeID, scope, targetID).Scan(&n); err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// GetActiveDiscountCampaign returns the winning campaign for one
// priced line: scope variant (only when variantID is set) > product >
// niche (in the product's niche order) > store, then most recently
// created. variantID == "" prices the product level, where
// variant-scoped campaigns never apply. Returns nil when no campaign
// applies or the store margin is admin-locked.
//
// Admin margin lock blocks application in BOTH live-parity lock
// forms: the legacy boolean stores.is_margin_locked_by_admin, and
// any row in store_margin_locks (D29, freeze or admin_set) scoped to
// this variant (when given), this product, or one of its niches. On
// a lock-query error the error is returned and the caller applies no
// campaign (fail-safe: no discount rather than a discount that
// bypasses a lock it could not read).
func (r *StoreRepository) GetActiveDiscountCampaign(ctx context.Context, storeID, productID string, nicheIDs []string, variantID string) (*domain.SellerDiscountCampaign, error) {
	var marginLocked bool
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(is_margin_locked_by_admin, FALSE) FROM stores WHERE id=$1`, storeID).Scan(&marginLocked); err != nil {
		return nil, err
	}
	if marginLocked {
		return nil, nil
	}
	lockQuery := `SELECT COUNT(*) FROM store_margin_locks WHERE store_id=$1 AND ((scope='product' AND scope_id=$2) OR (scope='niche' AND scope_id = ANY($3))`
	lockArgs := []interface{}{storeID, productID, pq.Array(nicheIDs)}
	if variantID != "" {
		lockQuery += ` OR (scope='variant' AND scope_id=$4)`
		lockArgs = append(lockArgs, variantID)
	}
	lockQuery += `)`
	var lockedCount int
	if err := r.db.QueryRowContext(ctx, lockQuery, lockArgs...).Scan(&lockedCount); err != nil {
		return nil, err
	}
	if lockedCount > 0 {
		return nil, nil
	}
	// One query resolves the whole specificity chain (the variant only
	// when a variant line is priced), then the product's niche order,
	// then the most recently created.
	query := `SELECT ` + team05CampaignColumns + ` FROM seller_discount_campaigns
		WHERE store_id=$1 AND is_active=TRUE AND starts_at <= NOW() AND ends_at > NOW()
		  AND (($4 <> '' AND scope='variant' AND target_id=$4)
		    OR (scope='product' AND target_id=$2)
		    OR (scope='niche' AND target_id = ANY($3))
		    OR (scope='store' AND target_id=''))
		ORDER BY CASE scope WHEN 'variant' THEN 0 WHEN 'product' THEN 1 WHEN 'niche' THEN 2 ELSE 3 END,
		         array_position($3, target_id), created_at DESC
		LIMIT 1`
	row := r.db.QueryRowContext(ctx, query, storeID, productID, pq.Array(nicheIDs), variantID)
	c, err := team05ScanCampaignRow(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// ListActiveDiscountCampaignsForStore returns every active, in-window
// campaign of one store for the display path (list/PDP/variant
// prices). The caller resolves scope specificity in memory, so one
// query serves a whole request instead of one query per product.
func (r *StoreRepository) ListActiveDiscountCampaignsForStore(ctx context.Context, storeID string) ([]domain.SellerDiscountCampaign, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+team05CampaignColumns+` FROM seller_discount_campaigns WHERE store_id=$1 AND is_active=TRUE AND starts_at <= NOW() AND ends_at > NOW()`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SellerDiscountCampaign{}
	for rows.Next() {
		c, err := team05ScanCampaignRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
