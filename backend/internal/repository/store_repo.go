package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/pricing"
	"strings"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"time"
)

// StoreRepository handles queries for dropshipper stores and settings
type StoreRepository struct {
	db *sql.DB
}

func NewStoreRepository(db *sql.DB) *StoreRepository {
	return &StoreRepository{db: db}
}

func (r *StoreRepository) SaveWidgetConfig(ctx context.Context, storeID string, config []byte) error {
	_, err := r.db.ExecContext(ctx, `UPDATE stores SET seller_deals_widget_config = $1, updated_at = NOW() WHERE id = $2`, string(config), storeID)
	return err
}

func (r *StoreRepository) WidgetProductsExist(ctx context.Context, ids []string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM products WHERE id = ANY($1)`, pq.Array(ids)).Scan(&count)
	return count == len(ids), err
}

// GetStoreBySlug returns store by URL slug
func (r *StoreRepository) GetStoreBySlug(ctx context.Context, slug string) (*domain.Store, error) {
	query := `
		SELECT id, name, slug, owner_type, owner_name, owner_user_id, profit_margin_percent,
		       custom_store_margin_override, is_margin_locked_by_admin, active_plan_id,
		       COALESCE(referral_code, ''), referrer_seller_id, signed_contract_version,
		       COALESCE(custom_domain, ''), total_sales, COALESCE(seller_deals_widget_config, 'null'::jsonb),
		       COALESCE(template_id, ''), COALESCE(template_config, '{}'::jsonb), COALESCE(logo, '')
		FROM stores
		WHERE LOWER(slug) = LOWER($1)
	`
	row := r.db.QueryRowContext(ctx, query, slug)

	var s domain.Store
	if err := row.Scan(
		&s.ID, &s.Name, &s.Slug, &s.OwnerType, &s.OwnerName, &s.OwnerUserID, &s.ProfitMarginPercent,
		&s.CustomStoreMarginOverride, &s.IsMarginLockedByAdmin, &s.ActivePlanID,
		&s.ReferralCode, &s.ReferrerSellerID, &s.SignedContractVersion,
		&s.CustomDomain, &s.TotalSales, &s.SellerDealsWidgetConfig,
		&s.TemplateID, &s.TemplateConfig, &s.Logo,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query store: %w", err)
	}

	// Load product overrides for this store
	pRows, err := r.db.QueryContext(ctx, `SELECT product_id, override_value, mode FROM store_product_overrides WHERE store_id = $1`, s.ID)
	if err == nil {
		s.ProductPriceOverrides = make(map[string]float64)
		s.ProductPriceOverrideModes = make(map[string]string)
		for pRows.Next() {
			var pid, mode string
			var val float64
			if err := pRows.Scan(&pid, &val, &mode); err == nil {
				s.ProductPriceOverrides[pid] = val
				s.ProductPriceOverrideModes[pid] = mode
			}
		}
		pRows.Close()
	}

	// Load niche overrides for this store
	nRows, err := r.db.QueryContext(ctx, `SELECT niche_id, override_value, mode FROM store_niche_overrides WHERE store_id = $1`, s.ID)
	if err == nil {
		s.NichePriceOverrides = make(map[string]float64)
		s.NichePriceOverrideModes = make(map[string]string)
		for nRows.Next() {
			var nid, mode string
			var val float64
			if err := nRows.Scan(&nid, &val, &mode); err == nil {
				s.NichePriceOverrides[nid] = val
				s.NichePriceOverrideModes[nid] = mode
			}
		}
		nRows.Close()
	}

	if err := r.loadPanelConfig(ctx, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// GetPlatformSettings returns runtime platform governance settings
func (r *StoreRepository) GetPlatformSettings(ctx context.Context) (*domain.PlatformSettings, error) {
	query := `
		SELECT base_fee_f, min_profit_margin, max_profit_margin, default_margin,
		       attribution_model, cookie_days, delivery_hold_period_days, delivery_followup_trigger_days,
		       referral_commission_percent, referral_is_enabled, referral_subject_to_delivery_lock,
		       seller_contract_version, supplier_contract_version, supplier_return_postal_charge
		FROM platform_settings
		WHERE id = 1
	`
	row := r.db.QueryRowContext(ctx, query)

	var p domain.PlatformSettings
	if err := row.Scan(
		&p.BaseFeeF, &p.MinProfitMargin, &p.MaxProfitMargin, &p.DefaultMargin,
		&p.AttributionModel, &p.CookieDays, &p.DeliveryHoldPeriodDays, &p.DeliveryFollowupTriggerDays,
		&p.ReferralCommissionPercent, &p.ReferralIsEnabled, &p.ReferralSubjectToDeliveryLock,
		&p.SellerContractVersion, &p.SupplierContractVersion, &p.SupplierReturnPostalCharge,
	); err != nil {
		return nil, fmt.Errorf("failed to query platform settings: %w", err)
	}

	return &p, nil
}

// GetAllStores returns all active stores
func (r *StoreRepository) GetAllStores(ctx context.Context) ([]domain.Store, error) {
	query := `
		SELECT id, name, slug, owner_type, owner_name, owner_user_id, profit_margin_percent,
		       custom_store_margin_override, is_margin_locked_by_admin, active_plan_id,
		       COALESCE(referral_code, ''), referrer_seller_id, signed_contract_version,
		       COALESCE(custom_domain, ''), total_sales, COALESCE(seller_deals_widget_config, 'null'::jsonb),
		       COALESCE(template_id, ''), COALESCE(template_config, '{}'::jsonb), COALESCE(logo, '')
		FROM stores
		ORDER BY total_sales DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all stores: %w", err)
	}
	defer rows.Close()

	var stores []domain.Store
	for rows.Next() {
		var s domain.Store
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Slug, &s.OwnerType, &s.OwnerName, &s.OwnerUserID, &s.ProfitMarginPercent,
			&s.CustomStoreMarginOverride, &s.IsMarginLockedByAdmin, &s.ActivePlanID,
			&s.ReferralCode, &s.ReferrerSellerID, &s.SignedContractVersion,
			&s.CustomDomain, &s.TotalSales, &s.SellerDealsWidgetConfig,
			&s.TemplateID, &s.TemplateConfig, &s.Logo,
		); err != nil {
			return nil, err
		}
		stores = append(stores, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range stores {
		if err := r.loadPanelConfig(ctx, &stores[i]); err != nil {
			return nil, err
		}
	}
	if stores == nil {
		stores = []domain.Store{}
	}
	return stores, nil
}

// GetStoresByOwner returns only storefronts owned by one seller account.
func (r *StoreRepository) GetStoresByOwner(ctx context.Context, ownerUserID string) ([]domain.Store, error) {
	query := `
		SELECT id, name, slug, owner_type, owner_name, owner_user_id, profit_margin_percent,
		       custom_store_margin_override, is_margin_locked_by_admin, active_plan_id,
		       COALESCE(referral_code, ''), referrer_seller_id, signed_contract_version,
		       COALESCE(custom_domain, ''), total_sales, COALESCE(seller_deals_widget_config, 'null'::jsonb)
		FROM stores
		WHERE owner_user_id = $1
		ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to query owned stores: %w", err)
	}
	defer rows.Close()

	var stores []domain.Store
	for rows.Next() {
		var s domain.Store
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Slug, &s.OwnerType, &s.OwnerName, &s.OwnerUserID, &s.ProfitMarginPercent,
			&s.CustomStoreMarginOverride, &s.IsMarginLockedByAdmin, &s.ActivePlanID,
			&s.ReferralCode, &s.ReferrerSellerID, &s.SignedContractVersion,
			&s.CustomDomain, &s.TotalSales, &s.SellerDealsWidgetConfig,
		); err != nil {
			return nil, err
		}
		stores = append(stores, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range stores {
		loaded, err := r.GetStoreBySlug(ctx, stores[i].Slug)
		if err != nil {
			return nil, err
		}
		stores[i] = *loaded
	}
	if stores == nil {
		stores = []domain.Store{}
	}
	return stores, rows.Err()
}

// CreateStore inserts a new dropshipper store
func (r *StoreRepository) CreateStore(ctx context.Context, s domain.Store) error {
	query := `
		INSERT INTO stores (
			id, name, slug, owner_type, owner_name, owner_user_id, profit_margin_percent,
			active_plan_id, referral_code, referrer_seller_id, signed_contract_version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := r.db.ExecContext(ctx, query,
		s.ID, s.Name, s.Slug, s.OwnerType, s.OwnerName, s.OwnerUserID, s.ProfitMarginPercent,
		s.ActivePlanID, s.ReferralCode, s.ReferrerSellerID, s.SignedContractVersion,
	)
	return err
}

// SetStoreProductOverride sets or updates a specific product override
func (r *StoreRepository) SetStoreProductOverride(ctx context.Context, storeID, productID string, value float64, mode string) error {
	if mode != "percent" {
		return pricing.ErrInvalidMoney
	}
	if _, err := pricing.ParsePercent(value); err != nil {
		return err
	}
	query := `
		INSERT INTO store_product_overrides (store_id, product_id, override_value, mode)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (store_id, product_id)
		DO UPDATE SET override_value = EXCLUDED.override_value, mode = EXCLUDED.mode
	`
	_, err := r.db.ExecContext(ctx, query, storeID, productID, value, mode)
	return err
}

// SetStoreNicheOverride sets or updates a specific niche override
func (r *StoreRepository) SetStoreNicheOverride(ctx context.Context, storeID, nicheID string, value float64, mode string) error {
	if mode != "percent" {
		return pricing.ErrInvalidMoney
	}
	if _, err := pricing.ParsePercent(value); err != nil {
		return err
	}
	query := `
		INSERT INTO store_niche_overrides (store_id, niche_id, override_value, mode)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (store_id, niche_id)
		DO UPDATE SET override_value = EXCLUDED.override_value, mode = EXCLUDED.mode
	`
	_, err := r.db.ExecContext(ctx, query, storeID, nicheID, value, mode)
	return err
}

// SetStoreMargin updates the store's default margin
func (r *StoreRepository) SetStoreMargin(ctx context.Context, storeID string, margin float64) error {
	if _, err := pricing.ParsePercent(margin); err != nil {
		return err
	}
	query := `UPDATE stores SET custom_store_margin_override = $1, profit_margin_percent = $1 WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, margin, storeID)
	return err
}

// ReferralBonusAmount represents the fixed onboarding referral reward (100,000 Toman)
const ReferralBonusAmount int64 = 100000

// ReferredStoreView represents a direct referral with bonus & activation lifecycle status
type ReferredStoreView struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	OwnerName        string `json:"ownerName"`
	TotalSales       int64  `json:"totalSales"`
	IsActivated      bool   `json:"isActivated"`
	BonusStatus      string `json:"bonusStatus"` // "locked", "released", "none"
	BonusAmount      int64  `json:"bonusAmount"`
	EarnedCommission int64  `json:"earnedCommission"` // Real released 2% referral commission from orders
	TotalPlatformFee int64  `json:"totalPlatformFee"` // Real platform fee (F) generated by this subordinate
}

// GetStoreByReferralCode finds a store by its unique referral code
func (r *StoreRepository) GetStoreByReferralCode(ctx context.Context, code string) (*domain.Store, error) {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return nil, nil
	}
	query := `
		SELECT id, name, slug, owner_type, owner_name, owner_user_id, profit_margin_percent,
		       custom_store_margin_override, is_margin_locked_by_admin, active_plan_id,
		       COALESCE(referral_code, ''), referrer_seller_id, signed_contract_version,
		       COALESCE(custom_domain, ''), total_sales, COALESCE(seller_deals_widget_config, 'null'::jsonb)
		FROM stores
		WHERE UPPER(referral_code) = UPPER($1)
		LIMIT 1
	`
	var s domain.Store
	err := r.db.QueryRowContext(ctx, query, trimmed).Scan(
		&s.ID, &s.Name, &s.Slug, &s.OwnerType, &s.OwnerName, &s.OwnerUserID, &s.ProfitMarginPercent,
		&s.CustomStoreMarginOverride, &s.IsMarginLockedByAdmin, &s.ActivePlanID,
		&s.ReferralCode, &s.ReferrerSellerID, &s.SignedContractVersion,
		&s.CustomDomain, &s.TotalSales, &s.SellerDealsWidgetConfig,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ErrStorePlanLimit indicates that the owner cannot add another store.
var ErrStorePlanLimit = errors.New("Store limit reached for your plan")

// RegisterStore enforces the existing registration quota and inserts
// the new store and any locked referral bonus in a single transaction.
func (r *StoreRepository) RegisterStore(ctx context.Context, s *domain.Store, referrerStoreID string) error {
	return r.createStoreWithBonus(ctx, s, referrerStoreID, true)
}

func (r *StoreRepository) CreateStoreWithBonus(ctx context.Context, s domain.Store, referrerStoreID string) error {
	return r.createStoreWithBonus(ctx, &s, referrerStoreID, false)
}

func (r *StoreRepository) createStoreWithBonus(ctx context.Context, s *domain.Store, referrerStoreID string, register bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	planID := s.ActivePlanID
	var expiry sql.NullTime
	if register {
		planID, expiry, err = registrationPlan(ctx, tx, s.OwnerUserID)
		if err != nil {
			return err
		}
	}

	if referrerStoreID != "" {
		var set TransactionLockSet
		if err := LockAccountsTx(ctx, tx, &set, FinancialAccount{Type: "seller", ID: referrerStoreID}); err != nil {
			return err
		}
	}
	if err := insertStoreWithBonus(ctx, tx, s, referrerStoreID, planID, expiry); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.ActivePlanID = planID
	return nil
}

func registrationPlan(ctx context.Context, tx *sql.Tx, ownerID *string) (string, sql.NullTime, error) {
	var expiry sql.NullTime
	if ownerID == nil {
		return "", expiry, ErrStoreForbidden
	}
	// Serialize admissions for the same seller so they cannot consume the same slot.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('store-registration:' || ($1::uuid)::text, 0))`, *ownerID); err != nil {
		return "", expiry, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM stores WHERE owner_user_id=$1`, *ownerID).Scan(&count); err != nil {
		return "", expiry, err
	}
	planID, maxStores := "trial", 1
	if count > 0 {
		// Keep the existing VIP-only expansion. Inherit its deadline so a
		// newly created store cannot extend a dated VIP grant indefinitely.
		err := tx.QueryRowContext(ctx, `SELECT s.plan_expires_at
			FROM stores s JOIN subscription_plans p ON p.id=s.active_plan_id AND p.is_active
			WHERE s.owner_user_id=$1 AND s.active_plan_id='vip'
			AND (s.plan_expires_at IS NULL OR s.plan_expires_at > NOW())
			ORDER BY s.plan_expires_at DESC NULLS LAST, s.id LIMIT 1`, *ownerID).Scan(&expiry)
		if err == nil {
			planID, maxStores = "vip", 10
		} else if err != sql.ErrNoRows {
			return "", expiry, err
		}
	}
	if count >= maxStores {
		return "", expiry, fmt.Errorf("%w (%d stores)", ErrStorePlanLimit, maxStores)
	}
	return planID, expiry, nil
}

func insertStoreWithBonus(ctx context.Context, tx *sql.Tx, s *domain.Store, referrerStoreID, planID string, expiry sql.NullTime) error {
	queryStore := `
		INSERT INTO stores (
			id, name, slug, owner_type, owner_name, owner_user_id, profit_margin_percent,
			active_plan_id, referral_code, referrer_seller_id, signed_contract_version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	if _, err := tx.ExecContext(ctx, queryStore,
		s.ID, s.Name, s.Slug, s.OwnerType, s.OwnerName, s.OwnerUserID, s.ProfitMarginPercent,
		planID, s.ReferralCode, s.ReferrerSellerID, s.SignedContractVersion,
	); err != nil {
		return err
	}
	if expiry.Valid {
		if _, err := tx.ExecContext(ctx, `UPDATE stores SET plan_expires_at=$2 WHERE id=$1`, s.ID, expiry); err != nil {
			return err
		}
	}

	if referrerStoreID != "" {
		bonusID := fmt.Sprintf("bon-%s", uuid.New().String())
		queryBonus := `
			INSERT INTO referral_bonuses (id, referrer_store_id, referred_store_id, amount, status, created_at)
			VALUES ($1, $2, $3, $4, 'locked', NOW())
			ON CONFLICT (referred_store_id) DO NOTHING
		`
		if _, err := tx.ExecContext(ctx, queryBonus, bonusID, referrerStoreID, s.ID, ReferralBonusAmount); err != nil {
			return err
		}
	}

	return nil
}

// ReferralBonusNotification represents an in-app notice when a subordinate bonus unlocks
type ReferralBonusNotification struct {
	ID                string     `json:"id"`
	ReferredStoreID   string     `json:"referredStoreId"`
	ReferredStoreName string     `json:"referredStoreName"`
	Amount            int64      `json:"amount"`
	ReleasedAt        *time.Time `json:"releasedAt"`
	Message           string     `json:"message"`
}

// GetReferralBonusNotifications returns all released bonuses for referrerStoreID
func (r *StoreRepository) GetReferralBonusNotifications(ctx context.Context, referrerStoreID string) ([]ReferralBonusNotification, error) {
	query := `
		SELECT b.id, b.referred_store_id, s.name, b.amount, b.released_at
		FROM referral_bonuses b
		JOIN stores s ON b.referred_store_id = s.id
		WHERE b.referrer_store_id = $1
		  AND b.status = 'released'
		ORDER BY b.released_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, referrerStoreID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ReferralBonusNotification
	for rows.Next() {
		var n ReferralBonusNotification
		if err := rows.Scan(&n.ID, &n.ReferredStoreID, &n.ReferredStoreName, &n.Amount, &n.ReleasedAt); err != nil {
			return nil, err
		}
		n.Message = fmt.Sprintf("هدیه %d تومانی برای فروشگاه «%s» قابل برداشت شد", n.Amount, n.ReferredStoreName)
		list = append(list, n)
	}
	if list == nil {
		list = []ReferralBonusNotification{}
	}
	return list, nil
}
func (r *StoreRepository) GetReferredStoresWithStatus(ctx context.Context, storeID string) ([]ReferredStoreView, error) {
	query := `
		SELECT s.id, s.name, s.slug, s.owner_name, s.total_sales,
		       COALESCE(b.status, 'none') as bonus_status,
		       COALESCE(b.amount, 0) as bonus_amount,
		       EXISTS(
		           SELECT 1 FROM orders o
		           WHERE o.store_id = s.id AND o.status = 'delivered_confirmed'
		       ) as is_activated,
		       COALESCE((
		           SELECT SUM(referral_commission)
		           FROM orders o
		           WHERE o.store_id = s.id
		             AND o.referrer_seller_id = $1
		             AND o.referral_commission_released = TRUE
		             AND o.status != 'cancelled'
		       ), 0)
	       - COALESCE((
	           SELECT SUM(r.amount)
	           FROM referral_commission_reversals r
	           WHERE r.referrer_store_id = $1
	             AND EXISTS(SELECT 1 FROM orders o WHERE o.id = r.order_id AND o.referral_commission_released AND o.status != 'cancelled')
	       ), 0) as earned_commission,
		       COALESCE((
		           SELECT SUM(platform_profit)
		           FROM orders o
		           WHERE o.store_id = s.id
		             AND o.status != 'cancelled'
		       ), 0) as total_platform_fee
		FROM stores s
		LEFT JOIN referral_bonuses b ON b.referred_store_id = s.id AND b.referrer_store_id = $1
		WHERE s.referrer_seller_id = $1
		ORDER BY s.total_sales DESC, s.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ReferredStoreView
	for rows.Next() {
		var v ReferredStoreView
		if err := rows.Scan(
			&v.ID, &v.Name, &v.Slug, &v.OwnerName, &v.TotalSales,
			&v.BonusStatus, &v.BonusAmount, &v.IsActivated,
			&v.EarnedCommission, &v.TotalPlatformFee,
		); err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	if list == nil {
		list = []ReferredStoreView{}
	}
	return list, nil
}

// RecordCustomerStoreLink attributes a buyer to a store at signup /
// first sighting (Fix83). Idempotent: re-recording the same
// (user, store) pair is a no-op and never overwrites first_seen_at.
func (r *StoreRepository) RecordCustomerStoreLink(ctx context.Context, userID, storeID string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO customer_store_links (user_id, store_id) VALUES ($1, $2) ON CONFLICT (user_id, store_id) DO NOTHING`, userID, storeID)
	return err
}
