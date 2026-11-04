package repository

import (
	"context"
	"database/sql"
	"fmt"
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
	if c.Value < 0 {
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
	return &c, nil
}

func (r *StoreRepository) ListDiscountCodes(ctx context.Context, storeID string) ([]DiscountCode, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,store_id,code,mode,value,starts_at,ends_at,is_active,max_uses,used_count,created_at FROM seller_discount_codes WHERE store_id=$1 ORDER BY created_at DESC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscountCode{}
	for rows.Next() {
		var c DiscountCode
		if err := rows.Scan(&c.ID, &c.StoreID, &c.Code, &c.Mode, &c.Value, &c.StartsAt, &c.EndsAt, &c.IsActive, &c.MaxUses, &c.UsedCount, &c.CreatedAt); err != nil {
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

// ValidateDiscountCode returns the code if it is active, in window and
// not exhausted. It does not consume a use; consumption happens after
// a successful order (IncrementDiscountCodeUse).
func (r *StoreRepository) ValidateDiscountCode(ctx context.Context, storeID, code string) (*DiscountCode, error) {
	code = normalizeDiscountCode(code)
	var c DiscountCode
	err := r.db.QueryRowContext(ctx, `SELECT id,store_id,code,mode,value,starts_at,ends_at,is_active,max_uses,used_count,created_at FROM seller_discount_codes WHERE store_id=$1 AND code=$2 AND is_active=TRUE AND starts_at <= NOW() AND ends_at > NOW() AND (max_uses IS NULL OR used_count < max_uses)`, storeID, code).Scan(&c.ID, &c.StoreID, &c.Code, &c.Mode, &c.Value, &c.StartsAt, &c.EndsAt, &c.IsActive, &c.MaxUses, &c.UsedCount, &c.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("discount code is invalid or expired")
		}
		return nil, err
	}
	return &c, nil
}

// IncrementDiscountCodeUse atomically consumes one use. It returns an
// error when the code is exhausted; callers log that case instead of
// failing an already-created order.
func (r *StoreRepository) IncrementDiscountCodeUse(ctx context.Context, codeID string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE seller_discount_codes SET used_count = used_count + 1, updated_at=NOW() WHERE id=$1 AND (max_uses IS NULL OR used_count < max_uses)`, codeID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("discount code exhausted")
	}
	return nil
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
	if (scope == "product" || scope == "niche") && targetID != "" {
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

// GetActiveDiscountCampaign returns the winning campaign for a
// product: scope product > niche > store, then most recently
// created. Returns nil when no campaign applies or the store margin
// is admin-locked.
//
// Admin margin lock blocks application in BOTH live-parity lock
// forms: the legacy boolean stores.is_margin_locked_by_admin, and
// any row in store_margin_locks (D29, freeze or admin_set) scoped to
// this product or one of its niches. A store-wide lock row does not
// exist in the D29 model (scopes are product/niche/variant), so
// store scope is not consulted here; the legacy boolean covers the
// store-wide case. On a lock-query error the error is returned and
// the caller applies no campaign (fail-safe: no discount rather
// than a discount that bypasses a lock it could not read).
func (r *StoreRepository) GetActiveDiscountCampaign(ctx context.Context, storeID, productID string, nicheIDs []string) (*domain.SellerDiscountCampaign, error) {
	var marginLocked bool
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(is_margin_locked_by_admin, FALSE) FROM stores WHERE id=$1`, storeID).Scan(&marginLocked); err != nil {
		return nil, err
	}
	if marginLocked {
		return nil, nil
	}
	var lockedCount int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_margin_locks WHERE store_id=$1 AND ((scope='product' AND scope_id=$2) OR (scope='niche' AND scope_id = ANY($3)))`, storeID, productID, pq.Array(nicheIDs)).Scan(&lockedCount); err != nil {
		return nil, err
	}
	if lockedCount > 0 {
		return nil, nil
	}
	// Try product, then each niche, then store, in priority order.
	candidates := []struct{ scope, target string }{{"product", productID}}
	for _, n := range nicheIDs {
		candidates = append(candidates, struct{ scope, target string }{"niche", n})
	}
	candidates = append(candidates, struct{ scope, target string }{"store", ""})
	for _, cand := range candidates {
		row := r.db.QueryRowContext(ctx, `SELECT `+team05CampaignColumns+` FROM seller_discount_campaigns WHERE store_id=$1 AND scope=$2 AND target_id=$3 AND is_active=TRUE AND starts_at <= NOW() AND ends_at > NOW() ORDER BY created_at DESC LIMIT 1`, storeID, cand.scope, cand.target)
		c, err := team05ScanCampaignRow(row.Scan)
		if err == nil {
			return &c, nil
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
	}
	return nil, nil
}
