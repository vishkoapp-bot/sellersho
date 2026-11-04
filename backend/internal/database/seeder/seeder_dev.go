package seeder

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

// SeedDev seeds development and demo fixture data.
// Strict Fail-Safe: Will abort immediately if environment is not in the allowed dev list.
func (s *Seeder) SeedDev(ctx context.Context) error {
	if !IsDevEnvironment(s.env) {
		return fmt.Errorf("security violation: SeedDev is strictly restricted to development/test environments (rejected environment: %q)", s.env)
	}

	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Dev Users (Explicit test numbers 0900XXXXXXX, marked test fixtures)
	queryUser := `
		INSERT INTO users (id, mobile, full_name, role, username, password_hash, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), TRUE, NOW(), NOW())
		ON CONFLICT (mobile) DO UPDATE SET
			full_name = EXCLUDED.full_name,
			role = EXCLUDED.role,
			username = COALESCE(EXCLUDED.username, users.username),
			password_hash = COALESCE(EXCLUDED.password_hash, users.password_hash),
			updated_at = NOW()
	`
	for _, u := range GetDevUsers() {
		passwordHash := ""
		if u.PasswordEnv != "" {
			password := os.Getenv(u.PasswordEnv)
			if password == "" {
				return fmt.Errorf("missing required development credential environment variable %s", u.PasswordEnv)
			}
			hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("failed to hash password for %s: %w", u.Username, err)
			}
			passwordHash = string(hashed)
		}
		if _, err := tx.ExecContext(ctx, queryUser, u.ID, u.Mobile, u.FullName, u.Role, u.Username, passwordHash); err != nil {
			return fmt.Errorf("failed to seed user %s: %w", u.Mobile, err)
		}
	}

	// 2. Dev Suppliers
	querySupplier := `
		INSERT INTO suppliers (id, owner_user_id, name, logo_url, rating, total_products, delivery_score, warehouse_city, description, badge)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			owner_user_id = EXCLUDED.owner_user_id,
			name = EXCLUDED.name,
			rating = EXCLUDED.rating,
			total_products = EXCLUDED.total_products,
			delivery_score = EXCLUDED.delivery_score
	`
	for _, sup := range GetDevSuppliers() {
		if _, err := tx.ExecContext(ctx, querySupplier,
			sup.ID, sup.OwnerUserID, sup.Name, sup.LogoURL, sup.Rating, sup.TotalProducts, sup.DeliveryScore,
			sup.WarehouseCity, sup.Description, sup.Badge,
		); err != nil {
			return fmt.Errorf("failed to seed supplier %s: %w", sup.ID, err)
		}
	}

	// 3. Dev Stores (Upsert on unique business key slug, preserving deterministic hierarchy)
	queryStore := `
		INSERT INTO stores (
			id, name, slug, owner_type, owner_name, owner_user_id, profit_margin_percent,
			active_plan_id, referral_code, referrer_seller_id, signed_contract_version, total_sales
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7, $8, $9, NULLIF($10, ''), 2, $11)
		ON CONFLICT (slug) DO UPDATE SET
			name = EXCLUDED.name,
			owner_type = EXCLUDED.owner_type,
			owner_name = EXCLUDED.owner_name,
			owner_user_id = EXCLUDED.owner_user_id,
			profit_margin_percent = EXCLUDED.profit_margin_percent,
			active_plan_id = EXCLUDED.active_plan_id,
			referral_code = EXCLUDED.referral_code,
			referrer_seller_id = EXCLUDED.referrer_seller_id,
			total_sales = EXCLUDED.total_sales,
			updated_at = NOW()
	`
	for _, st := range GetDevStores() {
		if _, err := tx.ExecContext(ctx, queryStore,
			st.ID, st.Name, st.Slug, st.OwnerType, st.OwnerName, st.OwnerUserID, st.ProfitMarginPercent,
			st.ActivePlanID, st.ReferralCode, st.ReferrerSellerID, st.TotalSales,
		); err != nil {
			return fmt.Errorf("failed to seed store %s: %w", st.ID, err)
		}
	}

	return s.seedDevOperations(ctx, tx)
}
