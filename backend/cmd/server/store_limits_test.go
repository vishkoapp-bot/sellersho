package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestStoreLimitsAndOwnership(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewStoreRepository(db)
	for _, plan := range []string{"trial", "vip"} {
		t.Run(plan, func(t *testing.T) {
			owner := uuid.New().String()
			mobile := "09" + uuid.New().String()[:13]
			if _, err := db.ExecContext(ctx, `INSERT INTO users (id, mobile, full_name, role, is_active) VALUES ($1, $2, 'Store Limit Test', 'seller', true)`, owner, mobile); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := db.ExecContext(ctx, `DELETE FROM stores WHERE owner_user_id = $1`, owner); err != nil {
					t.Error(err)
				}
				if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, owner); err != nil {
					t.Error(err)
				}
			}()
			first := domain.Store{ID: uuid.New().String(), Name: "First", Slug: "s-" + uuid.New().String()[:20], OwnerType: "dropshipper", OwnerUserID: &owner, ActivePlanID: plan, ReferralCode: uuid.New().String()[:8]}
			if err := repo.CreateStore(ctx, first); err != nil {
				t.Fatal(err)
			}
			claims := &auth.UserClaims{UserID: owner, Role: "seller", FullName: "Store Limit Test"}
			requestCtx := context.WithValue(ctx, userClaimsKey, claims)
			limit := 1
			if plan == "vip" {
				limit = 10
			}
			for count := 2; count <= limit+1; count++ {
				payload, err := json.Marshal(map[string]string{"storeName": "Additional", "slug": "s-" + uuid.New().String()[:20]})
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, "/api/v1/stores/register", bytes.NewReader(payload)).WithContext(requestCtx)
				response := httptest.NewRecorder()
				handleRegisterStore(response, req, requestCtx, repo)
				want := http.StatusCreated
				if count > limit {
					want = http.StatusConflict
				}
				if response.Code != want {
					t.Fatalf("store %d: want %d, got %d: %s", count, want, response.Code, response.Body.String())
				}
			}
			response := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/mine", nil).WithContext(requestCtx)
			handleOwnedStoresList(response, req, requestCtx, repo)
			if response.Code != http.StatusOK {
				t.Fatalf("owned stores: %d: %s", response.Code, response.Body.String())
			}
			var responseStores []domain.Store
			if err := json.Unmarshal(response.Body.Bytes(), &responseStores); err != nil {
				t.Fatal(err)
			}
			if len(responseStores) != limit {
				t.Fatalf("want %d owned stores, got %d", limit, len(responseStores))
			}
			stores, err := repo.GetStoresByOwner(ctx, owner)
			if err != nil {
				t.Fatal(err)
			}
			if len(stores) != len(responseStores) {
				t.Fatalf("owned store response count differs from repository: %d vs %d", len(responseStores), len(stores))
			}
			for _, store := range stores {
				if store.OwnerUserID == nil || *store.OwnerUserID != owner {
					t.Fatalf("another owner's store returned: %s", store.ID)
				}
				if store.ActivePlanID != plan {
					t.Fatalf("want inherited plan %s, got %s", plan, store.ActivePlanID)
				}
			}
		})
	}
}
