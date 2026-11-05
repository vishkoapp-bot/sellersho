package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestSellerPanelPersistenceOwnershipAndWallet(t *testing.T) {
	db := setupTestPostgresDB(t)
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
		handleSellerStore(w, r, repo, fin, store.Slug, action)
		return w
	}
	for _, action := range []string{"settings", "products", "report", "balance", "payouts", "services", "change-requests"} {
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
	if err := fin.PurchaseSellerService(ctx, owner, store.ID, "sms_500"); err != nil {
		t.Fatal(err)
	}
	balance, err = fin.GetStoreBalance(ctx, store.ID)
	if err != nil || balance.AvailableAmount != 150000 {
		t.Fatalf("service debit: %+v %v", balance, err)
	}
	if err := fin.PurchaseSellerService(ctx, stranger, store.ID, "vip"); !errors.Is(err, repository.ErrStoreForbidden) {
		t.Fatalf("foreign subscription: %v", err)
	}
	if err := fin.PurchaseSellerService(ctx, owner, store.ID, "vip"); !errors.Is(err, repository.ErrStoreInsufficientBalance) {
		t.Fatalf("unfunded subscription: %v", err)
	}
	var credits int
	if err := db.QueryRow(`SELECT (panel_config->>'smsCredits')::int FROM stores WHERE id=$1`, store.ID).Scan(&credits); err != nil || credits != 500 {
		t.Fatalf("credits %d: %v", credits, err)
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
