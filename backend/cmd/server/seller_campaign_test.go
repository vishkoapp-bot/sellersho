package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func sellerCampaignRequest(method, path, owner string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	return r.WithContext(context.WithValue(r.Context(), userClaimsKey, &auth.UserClaims{UserID: owner}))
}

func TestSellerCampaignUnsupportedMethods(t *testing.T) {
	const slug = "test-store"
	base := "/api/v1/stores/" + slug + "/campaigns"
	item := base + "/" + uuid.NewString()
	for _, tc := range []struct{ name, method, path, action, allow string }{
		{"history-post", http.MethodPost, item + "/history", "history", http.MethodGet},
		{"history-head", http.MethodHead, item + "/history", "history", http.MethodGet},
		{"analytics-post", http.MethodPost, item + "/analytics", "analytics", ""},
		{"item-put", http.MethodPut, item, "edit", ""},
		{"collection-put", http.MethodPut, base, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := sellerCampaignRequest(tc.method, tc.path, uuid.NewString())
			w := httptest.NewRecorder()
			// Rejected methods must not need a repository.
			if tc.action == "" {
				handleSellerCampaigns(w, r, nil, slug)
			} else {
				handleSellerCampaignAction(w, r, nil, slug, tc.action)
			}
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405", w.Code)
			}
			if tc.allow != "" && w.Header().Get("Allow") != tc.allow {
				t.Fatalf("Allow = %q, want %q", w.Header().Get("Allow"), tc.allow)
			}
		})
	}
}

func TestSellerCampaignLifecycleAndIsolation(t *testing.T) {
	db := setupTestPostgresDB(t)
	ctx := context.Background()
	repo := repository.NewStoreRepository(db)
	migration := `CREATE TABLE IF NOT EXISTS seller_campaigns (id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,name VARCHAR(200) NOT NULL,campaign_type VARCHAR(20) NOT NULL,status VARCHAR(20) NOT NULL DEFAULT 'draft',product_ids JSONB NOT NULL DEFAULT '[]'::jsonb,configuration JSONB NOT NULL DEFAULT '{}'::jsonb,starts_at TIMESTAMPTZ,ends_at TIMESTAMPTZ,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()); CREATE TABLE IF NOT EXISTS seller_campaign_events (id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,campaign_id UUID NOT NULL REFERENCES seller_campaigns(id) ON DELETE CASCADE,store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,event_type VARCHAR(20) NOT NULL,ad_token VARCHAR(200),product_id VARCHAR(50),occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW());`
	if _, err := db.Exec(migration); err != nil {
		t.Fatal(err)
	}
	owner, other := uuid.NewString(), uuid.NewString()
	for _, id := range []string{owner, other} {
		if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role,is_active) VALUES($1,$2,'Campaign test','seller',true)`, id, "09"+id[:13]); err != nil {
			t.Fatal(err)
		}
	}
	var product string
	if err := db.QueryRow(`SELECT id FROM products WHERE supplier_base_cost>0 LIMIT 1`).Scan(&product); err != nil {
		t.Fatal(err)
	}
	stores := []domain.Store{{ID: uuid.NewString(), Name: "Campaign owner", Slug: "campaign-owner-" + uuid.NewString()[:8], OwnerType: "dropshipper", OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: "CMP-" + uuid.NewString()[:8]}, {ID: uuid.NewString(), Name: "Other", Slug: "campaign-other-" + uuid.NewString()[:8], OwnerType: "dropshipper", OwnerUserID: &other, ActivePlanID: "trial", ReferralCode: "CMP-" + uuid.NewString()[:8]}}
	for _, s := range stores {
		if err := repo.CreateStore(ctx, s); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO store_products(store_id,product_id,position) VALUES($1,$2,0) ON CONFLICT DO NOTHING`, s.ID, product); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, s := range stores {
			db.Exec(`DELETE FROM stores WHERE id=$1`, s.ID)
		}
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, owner, other)
	})
	c, err := repo.CreateSellerCampaign(ctx, owner, stores[0].Slug, "Banner campaign", "banner", []string{product}, map[string]interface{}{}, "draft")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("owner can read history", func(t *testing.T) {
		slug := stores[0].Slug
		r := sellerCampaignRequest(http.MethodGet, "/api/v1/stores/"+slug+"/campaigns/"+c.ID+"/history", owner)
		w := httptest.NewRecorder()
		handleSellerCampaignAction(w, r, repo, slug, "history")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
		var items []repository.SellerCampaignHistory
		if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Action != "created" || items[0].CreatedAt.IsZero() {
			t.Fatalf("expected a timestamped creation entry, got %+v", items)
		}
	})
	for _, action := range []string{"history", "analytics"} {
		t.Run("non-owner cannot read "+action, func(t *testing.T) {
			slug := stores[0].Slug
			r := sellerCampaignRequest(http.MethodGet, "/api/v1/stores/"+slug+"/campaigns/"+c.ID+"/"+action, other)
			w := httptest.NewRecorder()
			handleSellerCampaignAction(w, r, repo, slug, action)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", w.Code)
			}
		})
	}
	if _, err := repo.GetSellerCampaigns(ctx, other, stores[0].Slug); !errors.Is(err, repository.ErrStoreForbidden) {
		t.Fatalf("cross-owner list: %v", err)
	}
	if err := repo.SetSellerCampaignStatus(ctx, other, stores[0].Slug, c.ID, "active"); !errors.Is(err, repository.ErrStoreForbidden) {
		t.Fatalf("cross-owner update: %v", err)
	}
	if err := repo.SetSellerCampaignStatus(ctx, owner, stores[0].Slug, c.ID, "active"); err != nil {
		t.Fatal(err)
	}
	token := uuid.NewString()
	accepted, err := repo.RecordSellerCampaignEvent(ctx, c.ID, "impression", token, product)
	if err != nil || !accepted {
		t.Fatalf("event: %v %v", accepted, err)
	}
	duplicate, err := repo.RecordSellerCampaignEvent(ctx, c.ID, "impression", token, product)
	if err != nil || duplicate {
		t.Fatalf("duplicate event: %v %v", duplicate, err)
	}
	a, err := repo.SellerCampaignAnalytics(ctx, owner, stores[0].Slug, c.ID)
	if err != nil || a.Impressions != 1 || a.Clicks != 0 || a.CTR != 0 {
		t.Fatalf("analytics: %+v %v", a, err)
	}
	public, err := repo.PublicSellerCampaigns(ctx, stores[0].Slug)
	if err != nil || len(public) != 1 {
		t.Fatalf("public active campaigns: %+v %v", public, err)
	}
	if err := repo.SetSellerCampaignStatus(ctx, owner, stores[0].Slug, c.ID, "paused"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordSellerCampaignEvent(ctx, c.ID, "click", uuid.NewString(), product); !errors.Is(err, repository.ErrCampaignInactive) {
		t.Fatalf("paused event: %v", err)
	}
	raw, _ := json.Marshal(public)
	if len(raw) == 0 {
		t.Fatal("empty campaign JSON")
	}
}
