package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestSellerPanelPersistenceOwnershipAndWallet(t *testing.T) {
	db := setupTestPostgresDB(t)
	// TEST-02 round 4: store_variant_visibility/defaults have
	// REFERENCES products(id) ON DELETE CASCADE, so any products-DROP
	// fixture (gap13, discount, supplier_gaps, ...) drops them, and
	// SaveStoreProducts DELETEs from both tables — a missing table
	// 500s the empty-catalog PUT. Recreate them verbatim from
	// migrations 202611130003/202611130005 (IF NOT EXISTS: harmless
	// when no fixture dropped them).
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS store_variant_visibility (store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE, product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE, variant_id UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE, is_visible BOOLEAN NOT NULL DEFAULT TRUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (store_id, product_id, variant_id))`,
		`CREATE INDEX IF NOT EXISTS idx_store_variant_visibility_store_product ON store_variant_visibility (store_id, product_id)`,
		`CREATE TABLE IF NOT EXISTS store_variant_defaults (store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE, product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE, variant_id UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (store_id, product_id))`,
		`CREATE INDEX IF NOT EXISTS idx_store_variant_defaults_variant ON store_variant_defaults (variant_id)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	repo := repository.NewStoreRepository(db)
	fin := repository.NewFinanceRepository(db)
	owner := uuid.NewString()
	stranger := uuid.NewString()
	for _, id := range []string{owner, stranger} {
		if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role,is_active) VALUES($1,$2,'Seller panel test','seller',true)`, id, "09"+id[:13]); err != nil {
			t.Fatal(err)
		}
	}
	store := domain.Store{ID: "s-" + uuid.NewString(), Name: "Panel test", Slug: "s-" + uuid.NewString()[:20], OwnerType: "dropshipper", OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8]}
	referred := domain.Store{ID: "s-" + uuid.NewString(), Name: "Referred test", Slug: "s-" + uuid.NewString()[:20], OwnerType: "dropshipper", OwnerUserID: &stranger, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8]}
	for _, s := range []domain.Store{store, referred} {
		if err := repo.CreateStore(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM seller_service_purchases WHERE store_id=$1`, `DELETE FROM payout_requests WHERE store_id=$1`, `DELETE FROM referral_bonuses WHERE referrer_store_id=$1`, `DELETE FROM stores WHERE id=$1`} {
			if _, err := db.Exec(q, store.ID); err != nil {
				t.Error(err)
			}
		}
		db.Exec(`DELETE FROM stores WHERE id=$1`, referred.ID)
		db.Exec(`DELETE FROM support_tickets WHERE requester_user_id=$1`, owner)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, owner, stranger)
	})
	request := func(user, action, method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/stores/"+store.Slug+"/"+action, bytes.NewBufferString(body))
		r = r.WithContext(context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: user, Role: "seller"}))
		w := httptest.NewRecorder()
		handleSellerStore(w, r, repo, fin, store.Slug, action, "")
		return w
	}
	for _, action := range []string{"settings", "products", "report", "balance", "statement", "payouts", "services", "change-requests"} {
		if w := request(stranger, action, http.MethodGet, ""); w.Code != 403 {
			t.Fatalf("%s foreign store: %d %s", action, w.Code, w.Body.String())
		}
	}
	if w := request(owner, "settings", http.MethodPut, `{"tagline":"persisted","shortDescription":"saved"}`); w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body.String())
	}
	loaded, err := repo.GetStoreBySlug(ctx, store.Slug)
	if err != nil || loaded.Tagline != "persisted" || loaded.ShortDescription != "saved" {
		t.Fatalf("settings not persisted: %+v %v", loaded, err)
	}
	if w := request(owner, "settings", http.MethodPut, `{"ownerUserId":"other"}`); w.Code != 400 {
		t.Fatalf("invalid settings: %d", w.Code)
	}
	if w := request(owner, "products", http.MethodPut, `{"productIds":["missing-product"]}`); w.Code != 400 {
		t.Fatalf("invalid product: %d %s", w.Code, w.Body.String())
	}
	if w := request(owner, "products", http.MethodPut, `{"productIds":[]}`); w.Code != 200 {
		t.Fatalf("empty catalog: %d %s", w.Code, w.Body.String())
	}
	if w := request(owner, "report", http.MethodGet, ""); w.Code != 200 {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	if w := request(owner, "statement", http.MethodGet, ""); w.Code != http.StatusOK {
		t.Fatalf("statement: %d %s", w.Code, w.Body.String())
	}
	if err := repo.CreateSellerTicket(ctx, owner, "Panel issue", "Persistent message"); err != nil {
		t.Fatal(err)
	}
	tickets, err := repo.SellerTickets(ctx, owner)
	if err != nil || len(tickets) != 1 || tickets[0].Messages[0] != "Persistent message" {
		t.Fatalf("ticket persistence: %+v %v", tickets, err)
	}
	foreignTickets, err := repo.SellerTickets(ctx, stranger)
	if err != nil || len(foreignTickets) != 0 {
		t.Fatalf("ticket isolation: %+v %v", foreignTickets, err)
	}
	if _, err := db.Exec(`INSERT INTO referral_bonuses(id,referrer_store_id,referred_store_id,amount,status) VALUES($1,$2,$3,1000000,'released')`, "b-"+uuid.NewString(), store.ID, referred.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := fin.RequestOwnedStorePayout(ctx, owner, store.ID, "IR", "test", 700000)
			outcomes <- err
		}()
	}
	wg.Wait()
	close(outcomes)
	accepted := 0
	for err := range outcomes {
		if err == nil {
			accepted++
		} else if !errors.Is(err, repository.ErrStoreInput) {
			t.Fatal(err)
		}
	}
	if accepted != 1 {
		t.Fatalf("concurrent payouts accepted %d, want 1", accepted)
	}
	balance, err := fin.GetStoreBalance(ctx, store.ID)
	if err != nil || balance.AvailableAmount != 300000 {
		t.Fatalf("wallet after payouts: %+v %v", balance, err)
	}
	if _, err := db.Exec(`UPDATE payout_requests SET status='approved' WHERE store_id=$1`, store.ID); err != nil {
		t.Fatal(err)
	}
	balance, err = fin.GetStoreBalance(ctx, store.ID)
	if err != nil || balance.AvailableAmount != 300000 {
		t.Fatalf("approved payout spent twice: %+v %v", balance, err)
	}
	// SMS packs are not for sale until a sending path exists: both
	// packs must be rejected before any wallet debit, with no purchase
	// row and no smsCredits granted.
	if err := fin.PurchaseSellerService(ctx, owner, store.ID, "sms_500"); !errors.Is(err, repository.ErrSMSServiceUnavailable) {
		t.Fatalf("sms_500 purchase: %v, want ErrSMSServiceUnavailable", err)
	}
	if err := fin.PurchaseSellerService(ctx, owner, store.ID, "sms_2000"); !errors.Is(err, repository.ErrSMSServiceUnavailable) {
		t.Fatalf("sms_2000 purchase: %v, want ErrSMSServiceUnavailable", err)
	}
	balance, err = fin.GetStoreBalance(ctx, store.ID)
	if err != nil || balance.AvailableAmount != 300000 {
		t.Fatalf("rejected sms purchase debited wallet: %+v %v", balance, err)
	}
	var purchaseCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM seller_service_purchases WHERE store_id=$1`, store.ID).Scan(&purchaseCount); err != nil || purchaseCount != 0 {
		t.Fatalf("rejected sms purchase wrote history: count %d: %v", purchaseCount, err)
	}
	if err := fin.PurchaseSellerService(ctx, stranger, store.ID, "vip"); !errors.Is(err, repository.ErrStoreForbidden) {
		t.Fatalf("foreign subscription: %v", err)
	}
	if err := fin.PurchaseSellerService(ctx, owner, store.ID, "vip"); !errors.Is(err, repository.ErrStoreInsufficientBalance) {
		t.Fatalf("unfunded subscription: %v", err)
	}
	var credits int
	if err := db.QueryRow(`SELECT COALESCE((panel_config->>'smsCredits')::int,0) FROM stores WHERE id=$1`, store.ID).Scan(&credits); err != nil || credits != 0 {
		t.Fatalf("rejected sms purchase granted credits %d: %v", credits, err)
	}
	payouts, err := fin.GetStorePayouts(ctx, owner, store.ID)
	if err != nil || len(payouts) != 1 {
		t.Fatalf("nullable payout fields: %+v %v", payouts, err)
	}
	raw, _ := json.Marshal(payouts)
	if len(raw) == 0 {
		t.Fatal("empty payout response")
	}
}

func TestSellerSettingsSelectedNichesRoundTrip(t *testing.T) {
	db := setupTestPostgresDB(t)
	ctx := context.Background()
	repo := repository.NewStoreRepository(db)
	owner := uuid.NewString()
	mobile := "09" + owner[:13]
	if _, err := db.Exec(`INSERT INTO users (id,mobile,full_name,role) VALUES ($1,$2,'Seller niche test','seller')`, owner, mobile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	storeID := "drp31-" + uuid.NewString()
	if err := repo.CreateStore(ctx, domain.Store{ID: storeID, Name: "Niche test", Slug: storeID,
		OwnerType: "dropshipper", OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8]}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM stores WHERE id=$1`, storeID) })
	nicheID := "niche-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO niches (id, slug, title) VALUES ($1,$1,'Fixture niche')`, nicheID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM niches WHERE id=$1`, nicheID) })

	jwt := auth.NewJWTService("seller-niche-settings-test-secret")
	fixture := supplierNotificationsFixture{jwtService: jwt}
	token := fixture.token(t, owner, mobile, "seller")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/stores/", handleStoresBlock(jwt, repo, nil, nil, nil, nil))
	mux.HandleFunc("/api/v1/stores/mine", RequireRoles(jwt, "customer", "seller", "supplier", "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleOwnedStoresList(w, r, r.Context(), repo)
	}))
	request := func(method, path, body string, status int) string {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: status %d, want %d, body %s", method, path, w.Code, status, w.Body.String())
		}
		return w.Body.String()
	}
	path := "/api/v1/stores/" + storeID + "/settings"
	body := request(http.MethodPut, path, `{"selectedNicheIds":["`+nicheID+`"]}`, http.StatusOK)
	var saved domain.Store
	if err := json.Unmarshal([]byte(body), &saved); err != nil || saved.ID != storeID || !slices.Equal(saved.SelectedNicheIDs, []string{nicheID}) {
		t.Fatalf("settings PUT response = %s, decode error %v", body, err)
	}
	request(http.MethodPut, path, `{"selectedNicheIds":["missing-`+nicheID+`"]}`, http.StatusBadRequest)
	body = request(http.MethodGet, "/api/v1/stores/mine", "", http.StatusOK)
	var owned []domain.Store
	if err := json.Unmarshal([]byte(body), &owned); err != nil || len(owned) != 1 ||
		owned[0].ID != storeID || !slices.Equal(owned[0].SelectedNicheIDs, []string{nicheID}) {
		t.Fatalf("GET /stores/mine response = %s, decode error %v", body, err)
	}
}
