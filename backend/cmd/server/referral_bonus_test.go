package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func setupTestPostgresDB(t *testing.T) *sql.DB {
	dbURL := os.Getenv("CAPABILITY_AUDIT_DATABASE_URL")
	db, err := testdb.Open(t, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("database offline, skipping: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE stores ADD COLUMN IF NOT EXISTS seller_deals_widget_config JSONB`); err != nil {
		t.Fatalf("failed to prepare store widget schema: %v", err)
	}
	return db
}

func TestReferralBonusLifecycle(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	// TEST-02 round 4: CreateStoreWithBonus does INSERT INTO
	// referral_bonuses ... ON CONFLICT (referred_store_id), but the
	// referral_bonuses rebuilds in this package carry no UNIQUE on
	// referred_store_id, so the insert fails with 42P10. Restore the
	// migrated unique index first.
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS referral_bonuses_referred_store_id_key ON referral_bonuses(referred_store_id)`); err != nil {
		t.Fatalf("restore referral_bonuses unique index: %v", err)
	}

	ctx := context.Background()
	storeRepo := repository.NewStoreRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	finRepo := repository.NewFinanceRepository(db)

	uniqueSuffix := fmt.Sprintf("%07d", time.Now().UnixNano()%10000000)

	// 1. Create Referrer Store
	referrerUser := uuid.New().String()
	referrerStoreID := fmt.Sprintf("store-ref-%s", uniqueSuffix)
	refCode := fmt.Sprintf("DRUP-%s", uniqueSuffix)

	_, err := db.ExecContext(ctx, `
		INSERT INTO users (id, mobile, full_name, role, is_active)
		VALUES ($1, $2, 'Referrer Test', 'seller', true)
	`, referrerUser, fmt.Sprintf("0987%s", uniqueSuffix))
	if err != nil {
		t.Fatalf("failed to insert referrer user: %v", err)
	}

	referrerStore := domain.Store{
		ID:                  referrerStoreID,
		Name:                "فروشگاه معرف",
		Slug:                fmt.Sprintf("ref-store-%s", uniqueSuffix),
		OwnerType:           "dropshipper",
		OwnerName:           "Referrer Test",
		OwnerUserID:         &referrerUser,
		ProfitMarginPercent: 15,
		ActivePlanID:        "vip",
		ReferralCode:        refCode,
	}
	if err := storeRepo.CreateStore(ctx, referrerStore); err != nil {
		t.Fatalf("failed to create referrer store: %v", err)
	}

	// 2. Test handleRegisterStore with valid referral code -> Creates locked bonus
	referredUser := uuid.New().String()
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, mobile, full_name, role, is_active)
		VALUES ($1, $2, 'Referred Test', 'seller', true)
	`, referredUser, fmt.Sprintf("0988%s", uniqueSuffix))
	if err != nil {
		t.Fatalf("failed to insert referred user: %v", err)
	}

	referredSlug := fmt.Sprintf("sub-store-%s", uniqueSuffix)
	body := map[string]interface{}{
		"storeName":           "فروشگاه زیرمجموعه",
		"slug":                referredSlug,
		"profitMarginPercent": 14.0,
		"referralCode":        refCode,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores", bytes.NewBuffer(b))
	claims := &auth.UserClaims{
		UserID:   referredUser,
		Mobile:   "09880000000",
		FullName: "Referred Test",
		Role:     "seller",
	}
	req = req.WithContext(context.WithValue(req.Context(), userClaimsKey, claims))
	w := httptest.NewRecorder()

	handleRegisterStore(w, req, req.Context(), storeRepo)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for referred store, got %d. Body: %s", w.Code, w.Body.String())
	}

	referredStore, err := storeRepo.GetStoreBySlug(ctx, referredSlug)
	if err != nil || referredStore == nil {
		t.Fatalf("failed to find created referred store: %v", err)
	}
	if referredStore.ReferrerSellerID == nil || *referredStore.ReferrerSellerID != referrerStoreID {
		t.Errorf("expected ReferrerSellerID %s, got %v", referrerStoreID, referredStore.ReferrerSellerID)
	}

	// Check locked bonus in referral_bonuses
	var bonusStatus string
	var bonusAmount int64
	err = db.QueryRowContext(ctx, `
		SELECT status, amount FROM referral_bonuses
		WHERE referrer_store_id = $1 AND referred_store_id = $2
	`, referrerStoreID, referredStore.ID).Scan(&bonusStatus, &bonusAmount)
	if err != nil {
		t.Fatalf("expected locked referral bonus in db: %v", err)
	}
	if bonusStatus != "locked" {
		t.Errorf("expected status 'locked', got %s", bonusStatus)
	}
	if bonusAmount != 100000 {
		t.Errorf("expected bonus amount 100000, got %d", bonusAmount)
	}

	// 3. Test GetStoreBalance: Locked bonus is NOT in AvailableAmount
	bal, err := finRepo.GetStoreBalance(ctx, referrerStoreID)
	if err != nil {
		t.Fatalf("GetStoreBalance failed: %v", err)
	}
	if bal.BonusLocked != 100000 {
		t.Errorf("expected BonusLocked = 100000, got %d", bal.BonusLocked)
	}
	if bal.BonusAvailable != 0 {
		t.Errorf("expected BonusAvailable = 0, got %d", bal.BonusAvailable)
	}

	// 4. Test Self-referral rejection: should not create a bonus for same user
	selfSlug := fmt.Sprintf("self-store-%s", uniqueSuffix)
	selfBody := map[string]interface{}{
		"storeName":           "فروشگاه سلف ریفرال",
		"slug":                selfSlug,
		"profitMarginPercent": 12.0,
		"referralCode":        refCode, // Self code
	}
	sb, _ := json.Marshal(selfBody)
	selfReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores", bytes.NewBuffer(sb))
	selfReq = selfReq.WithContext(context.WithValue(selfReq.Context(), userClaimsKey, &auth.UserClaims{
		UserID: referrerUser, // Same as owner
		Role:   "seller",
	}))
	selfW := httptest.NewRecorder()
	handleRegisterStore(selfW, selfReq, selfReq.Context(), storeRepo)
	if selfW.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for self-referral store, got %d", selfW.Code)
	}
	createdSelf, _ := storeRepo.GetStoreBySlug(ctx, selfSlug)
	if createdSelf.ReferrerSellerID != nil {
		t.Errorf("self-referral must not assign ReferrerSellerID, got %v", *createdSelf.ReferrerSellerID)
	}

	// 5. Test First ConfirmOrderDelivery -> bonus must STAY LOCKED.
	// FIN-011 (Q-02 retained-value): confirmation no longer releases
	// the bonus; it releases only at window close, when the referred
	// store has an order that closed its window with retained value > 0.
	customerUser := uuid.New().String()
	_, _ = db.ExecContext(ctx, `
		INSERT INTO users (id, mobile, full_name, role, is_active)
		VALUES ($1, $2, 'Customer Test', 'customer', true)
	`, customerUser, fmt.Sprintf("0989%s", uniqueSuffix))

	orderID := fmt.Sprintf("ord-sub-%s", uniqueSuffix)
	orderNum := fmt.Sprintf("ORD-%s", uniqueSuffix)
	trkCode := fmt.Sprintf("TRK-%s", uniqueSuffix)
	_, err = db.ExecContext(ctx, `
		INSERT INTO orders (
			id, order_number, store_id, store_name, total_amount, payable_amount,
			customer_full_name, customer_phone, customer_province, customer_city,
			customer_address, customer_postal_code, status, tracking_code, customer_user_id
		) VALUES (
			$1, $2, $3, 'فروشگاه زیرمجموعه', 500000, 500000,
			'Customer Test', '09120000000', 'Tehran', 'Tehran', 'Street 1', '1234567890',
			'shipped', $4, $5::uuid
		)
	`, orderID, orderNum, referredStore.ID, trkCode, customerUser)
	if err != nil {
		t.Fatalf("failed to insert order for referred store: %v", err)
	}

	// Confirm order delivery
	err = orderRepo.ConfirmOrderDelivery(ctx, orderID, customerUser)
	if err != nil {
		t.Fatalf("ConfirmOrderDelivery failed: %v", err)
	}

	// Verify bonus is still LOCKED after confirmation (FIN-011/Q-02).
	err = db.QueryRowContext(ctx, `
		SELECT status FROM referral_bonuses
		WHERE referrer_store_id = $1 AND referred_store_id = $2
	`, referrerStoreID, referredStore.ID).Scan(&bonusStatus)
	if err != nil {
		t.Fatalf("failed to query bonus status: %v", err)
	}
	if bonusStatus != "locked" {
		t.Errorf("expected bonus to stay 'locked' after confirmation, got %s", bonusStatus)
	}

	// Close the return window and run the window-close release: the
	// order retained its full 500,000 value, so the bonus unlocks now.
	if _, err := db.ExecContext(ctx, `UPDATE orders SET confirmed_at=NOW()-INTERVAL '8 days', return_window_ends_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, orderID); err != nil {
		t.Fatalf("failed to close return window: %v", err)
	}
	if _, err := orderRepo.ReleaseClosedReturnWindows(ctx); err != nil {
		t.Fatalf("ReleaseClosedReturnWindows failed: %v", err)
	}

	// Verify bonus is now RELEASED
	err = db.QueryRowContext(ctx, `
		SELECT status FROM referral_bonuses
		WHERE referrer_store_id = $1 AND referred_store_id = $2
	`, referrerStoreID, referredStore.ID).Scan(&bonusStatus)
	if err != nil {
		t.Fatalf("failed to query bonus status: %v", err)
	}
	if bonusStatus != "released" {
		t.Errorf("expected bonus to be 'released' after window close, got %s", bonusStatus)
	}

	// Verify GetStoreBalance reflects released bonus
	balAfter, _ := finRepo.GetStoreBalance(ctx, referrerStoreID)
	if balAfter.BonusAvailable != 100000 {
		t.Errorf("expected BonusAvailable = 100000 after delivery, got %d", balAfter.BonusAvailable)
	}
	if balAfter.BonusLocked != 0 {
		t.Errorf("expected BonusLocked = 0 after delivery, got %d", balAfter.BonusLocked)
	}

	// 6. Test Idempotency: Second delivery of another order does NOT release any further bonus
	orderID2 := fmt.Sprintf("ord-sub-2-%s", uniqueSuffix)
	orderNum2 := fmt.Sprintf("ORD2-%s", uniqueSuffix)
	_, _ = db.ExecContext(ctx, `
		INSERT INTO orders (
			id, order_number, store_id, store_name, total_amount, payable_amount,
			customer_full_name, customer_phone, customer_province, customer_city,
			customer_address, customer_postal_code, status, tracking_code, customer_user_id
		) VALUES (
			$1, $2, $3, 'فروشگاه زیرمجموعه', 600000, 600000,
			'Customer Test', '09120000000', 'Tehran', 'Tehran', 'Street 2', '1234567890',
			'shipped', $4, $5::uuid
		)
	`, orderID2, orderNum2, referredStore.ID, fmt.Sprintf("TRK2-%s", uniqueSuffix), customerUser)

	_ = orderRepo.ConfirmOrderDelivery(ctx, orderID2, customerUser)
	balAfterSecond, _ := finRepo.GetStoreBalance(ctx, referrerStoreID)
	if balAfterSecond.BonusAvailable != 100000 {
		t.Errorf("expected BonusAvailable to remain 100000 (idempotent), got %d", balAfterSecond.BonusAvailable)
	}
}

func TestReferredStoresWithStatus(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	// TEST-02 round 4: CreateStoreWithBonus does INSERT INTO
	// referral_bonuses ... ON CONFLICT (referred_store_id), but the
	// referral_bonuses rebuilds in this package carry no UNIQUE on
	// referred_store_id, so the insert fails with 42P10. Restore the
	// migrated unique index first.
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS referral_bonuses_referred_store_id_key ON referral_bonuses(referred_store_id)`); err != nil {
		t.Fatalf("restore referral_bonuses unique index: %v", err)
	}

	ctx := context.Background()
	storeRepo := repository.NewStoreRepository(db)

	uniqueSuffix := fmt.Sprintf("%07d", time.Now().UnixNano()%10000000)
	refUser := uuid.New().String()
	refStoreID := fmt.Sprintf("store-ref-%s", uniqueSuffix)

	_, _ = db.ExecContext(ctx, `INSERT INTO users (id, mobile, full_name, role, is_active) VALUES ($1, $2, 'Ref', 'seller', true)`, refUser, fmt.Sprintf("0981%s", uniqueSuffix))
	parentErr := storeRepo.CreateStore(ctx, domain.Store{
		ID: refStoreID, Name: "Parent Store", Slug: fmt.Sprintf("parent-%s", uniqueSuffix), OwnerType: "dropshipper", OwnerName: "Ref", OwnerUserID: &refUser, ReferralCode: fmt.Sprintf("REF-%s", uniqueSuffix), ActivePlanID: "trial",
	})
	if parentErr != nil {
		t.Fatalf("failed to create parent store: %v", parentErr)
	}

	// Add sub store
	subUser := uuid.New().String()
	subStoreID := fmt.Sprintf("store-sub-%s", uniqueSuffix)
	_, userErr := db.ExecContext(ctx, `INSERT INTO users (id, mobile, full_name, role, is_active) VALUES ($1, $2, 'Sub', 'seller', true)`, subUser, fmt.Sprintf("0982%s", uniqueSuffix))
	if userErr != nil {
		t.Fatalf("failed to insert sub user: %v", userErr)
	}

	createErr := storeRepo.CreateStoreWithBonus(ctx, domain.Store{
		ID: subStoreID, Name: "Sub Store", Slug: fmt.Sprintf("sub-%s", uniqueSuffix), OwnerType: "dropshipper", OwnerName: "Sub", OwnerUserID: &subUser, ReferrerSellerID: &refStoreID, ReferralCode: fmt.Sprintf("REFSUB-%s", uniqueSuffix), ActivePlanID: "trial",
	}, refStoreID)
	if createErr != nil {
		t.Fatalf("CreateStoreWithBonus failed: %v", createErr)
	}

	refs, err := storeRepo.GetReferredStoresWithStatus(ctx, refStoreID)
	if err != nil {
		t.Fatalf("GetReferredStoresWithStatus failed: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 referred store, got %d", len(refs))
	}
	if refs[0].BonusStatus != "locked" {
		t.Errorf("expected BonusStatus 'locked', got %s", refs[0].BonusStatus)
	}
	if refs[0].BonusAmount != 100000 {
		t.Errorf("expected BonusAmount 100000, got %d", refs[0].BonusAmount)
	}
	if refs[0].IsActivated {
		t.Errorf("expected IsActivated to be false initially")
	}

	// Add an order with released referral commission to subStore
	orderID := fmt.Sprintf("ord-com-%s", uniqueSuffix)
	custUser := uuid.New().String()
	_, _ = db.ExecContext(ctx, `INSERT INTO users (id, mobile, full_name, role, is_active) VALUES ($1, $2, 'Cust', 'customer', true)`, custUser, fmt.Sprintf("0983%s", uniqueSuffix))
	_, err = db.ExecContext(ctx, `
		INSERT INTO orders (
			id, order_number, store_id, store_name, total_amount, payable_amount,
			customer_full_name, customer_phone, customer_province, customer_city,
			customer_address, customer_postal_code, status, tracking_code, customer_user_id,
			referral_commission, referral_commission_released, referrer_seller_id, platform_profit
		) VALUES (
			$1, $2, $3, 'Sub Store', 1000000, 1000000,
			'Cust', '09120000000', 'Tehran', 'Tehran', 'Street 3', '1234567890',
			'delivered_confirmed', $4, $5::uuid,
			12000, TRUE, $6, 100000
		)
	`, orderID, fmt.Sprintf("ORD-C-%s", uniqueSuffix), subStoreID, fmt.Sprintf("TRK-C-%s", uniqueSuffix), custUser, refStoreID)
	if err != nil {
		t.Fatalf("failed to insert order with commission: %v", err)
	}

	// Verify GetReferredStoresWithStatus returns actual earned commission and platform fee
	refsAfter, err := storeRepo.GetReferredStoresWithStatus(ctx, refStoreID)
	if err != nil {
		t.Fatalf("GetReferredStoresWithStatus failed: %v", err)
	}
	if len(refsAfter) != 1 {
		t.Fatalf("expected 1 referred store, got %d", len(refsAfter))
	}
	if refsAfter[0].EarnedCommission != 12000 {
		t.Errorf("expected EarnedCommission = 12000, got %d", refsAfter[0].EarnedCommission)
	}
	if refsAfter[0].TotalPlatformFee != 100000 {
		t.Errorf("expected TotalPlatformFee = 100000, got %d", refsAfter[0].TotalPlatformFee)
	}

	// Verify GetStoreBalance isolates BonusAvailable and ReferralCommissionEarned
	balRef, err := repository.NewFinanceRepository(db).GetStoreBalance(ctx, refStoreID)
	if err != nil {
		t.Fatalf("GetStoreBalance failed: %v", err)
	}
	if balRef.ReferralCommissionEarned != 12000 {
		t.Errorf("expected ReferralCommissionEarned = 12000, got %d", balRef.ReferralCommissionEarned)
	}
	// Available should include referralCommissionEarned
	if balRef.AvailableAmount < 12000 {
		t.Errorf("expected AvailableAmount >= 12000, got %d", balRef.AvailableAmount)
	}
}

func TestRequestPayout_RejectsLockedBonusOnly(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	// TEST-02 round 4: CreateStoreWithBonus does INSERT INTO
	// referral_bonuses ... ON CONFLICT (referred_store_id), but the
	// referral_bonuses rebuilds in this package carry no UNIQUE on
	// referred_store_id, so the insert fails with 42P10. Restore the
	// migrated unique index first.
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS referral_bonuses_referred_store_id_key ON referral_bonuses(referred_store_id)`); err != nil {
		t.Fatalf("restore referral_bonuses unique index: %v", err)
	}

	ctx := context.Background()
	storeRepo := repository.NewStoreRepository(db)
	finRepo := repository.NewFinanceRepository(db)

	uniqueSuffix := fmt.Sprintf("%07d", time.Now().UnixNano()%10000000)
	refUser := uuid.New().String()
	refStoreID := fmt.Sprintf("store-payout-ref-%s", uniqueSuffix)

	_, _ = db.ExecContext(ctx, `INSERT INTO users (id, mobile, full_name, role, is_active) VALUES ($1, $2, 'Payout User', 'seller', true)`, refUser, fmt.Sprintf("0984%s", uniqueSuffix))
	_ = storeRepo.CreateStore(ctx, domain.Store{
		ID: refStoreID, Name: "Payout Ref Store", Slug: fmt.Sprintf("payout-ref-%s", uniqueSuffix), OwnerType: "dropshipper", OwnerName: "Payout User", OwnerUserID: &refUser, ReferralCode: fmt.Sprintf("PO-%s", uniqueSuffix), ActivePlanID: "trial",
	})

	// Add sub store with locked bonus
	subUser := uuid.New().String()
	subStoreID := fmt.Sprintf("store-payout-sub-%s", uniqueSuffix)
	_, _ = db.ExecContext(ctx, `INSERT INTO users (id, mobile, full_name, role, is_active) VALUES ($1, $2, 'Sub Payout', 'seller', true)`, subUser, fmt.Sprintf("0985%s", uniqueSuffix))
	_ = storeRepo.CreateStoreWithBonus(ctx, domain.Store{
		ID: subStoreID, Name: "Sub Payout Store", Slug: fmt.Sprintf("payout-sub-%s", uniqueSuffix), OwnerType: "dropshipper", OwnerName: "Sub Payout", OwnerUserID: &subUser, ReferrerSellerID: &refStoreID, ReferralCode: fmt.Sprintf("POSUB-%s", uniqueSuffix), ActivePlanID: "trial",
	}, refStoreID)

	// Since this store has 0 sales and only a locked bonus, in production availableAmount is 0
	bal, err := finRepo.GetStoreBalance(ctx, refStoreID)
	if err != nil {
		t.Fatalf("GetStoreBalance failed: %v", err)
	}
	if bal.BonusLocked != 100000 {
		t.Errorf("expected BonusLocked = 100000, got %d", bal.BonusLocked)
	}
	if bal.BonusAvailable != 0 {
		t.Errorf("expected BonusAvailable = 0, got %d", bal.BonusAvailable)
	}

	// Attempting to withdraw any amount higher than availableAmount (e.g. attempting to withdraw the 100k locked bonus) must be rejected
	attemptAmount := bal.AvailableAmount + 50000
	_, err = finRepo.RequestStorePayout(ctx, refStoreID, "IR120170000000123456789012", "بانک ملی", attemptAmount)
	if err == nil {
		t.Fatalf("expected payout request to fail when exceeding availableAmount, but succeeded")
	}
}
