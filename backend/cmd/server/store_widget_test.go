package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestStoreWidgetPersistenceAndOwnership(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	migration, err := os.ReadFile("../../migrations/000015_store_ad_widgets.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	campaignMigration, err := os.ReadFile("../../migrations/000019_seller_campaigns.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(campaignMigration)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo := repository.NewStoreRepository(db)
	owner := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO users (id, mobile, full_name, role) VALUES ($1,$2,'Widget Test','seller')`, owner, owner[:15]); err != nil {
		t.Fatal(err)
	}
	store := domain.Store{ID: uuid.New().String(), Slug: "widget-" + uuid.New().String()[:8], Name: "Widget Test", OwnerType: "dropshipper", OwnerName: "Owner", OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: "WGT-" + uuid.New().String()[:8]}
	if err := repo.CreateStore(ctx, store); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM users WHERE id=$1`, owner)
	defer db.Exec(`DELETE FROM stores WHERE id=$1`, store.ID)
	var productID string
	if err := db.QueryRow(`SELECT id FROM products LIMIT 1`).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO store_products(store_id, product_id, position) VALUES ($1,$2,0)`, store.ID, productID); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]interface{}{"title": "Widget title", "productIds": []string{productID}, "countdownHours": 24})
	for _, test := range []struct {
		userID string
		want   int
	}{{uuid.New().String(), http.StatusForbidden}, {owner, http.StatusOK}} {
		req := httptest.NewRequest(http.MethodPut, "/stores/"+store.Slug+"/ad-widget", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: test.userID, Role: "seller"}))
		w := httptest.NewRecorder()
		handleSaveStoreWidget(w, req, repo, store.Slug)
		if w.Code != test.want {
			t.Fatalf("want %d got %d: %s", test.want, w.Code, w.Body.String())
		}
	}
	loaded, err := repo.GetStoreBySlug(ctx, store.Slug)
	if err != nil || loaded == nil {
		t.Fatalf("read persisted widget: %v", err)
	}
	if !bytes.Contains(loaded.SellerDealsWidgetConfig, []byte(productID)) {
		t.Fatal("saved product missing on reload")
	}
	owned, err := repo.GetStoresByOwner(ctx, owner)
	if err != nil || len(owned) != 1 || !bytes.Equal(owned[0].SellerDealsWidgetConfig, loaded.SellerDealsWidgetConfig) {
		t.Fatalf("owned store widget mismatch: %v", err)
	}
}
