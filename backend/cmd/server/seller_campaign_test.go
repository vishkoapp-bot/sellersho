package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
	"testing"
)

func TestSellerCampaignLifecycleAndIsolation(t *testing.T) {
	db := setupTestPostgresDB(t)
	ctx := context.Background()
	repo := repository.NewStoreRepository(db)
	migration := `CREATE TABLE IF NOT EXISTS seller_campaigns (id UUID PRIMARY KEY,store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,name VARCHAR(200) NOT NULL,campaign_type VARCHAR(20) NOT NULL,status VARCHAR(20) NOT NULL DEFAULT 'draft',product_ids JSONB NOT NULL DEFAULT '[]'::jsonb,configuration JSONB NOT NULL DEFAULT '{}'::jsonb,starts_at TIMESTAMPTZ,ends_at TIMESTAMPTZ,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()); CREATE TABLE IF NOT EXISTS seller_campaign_events (id UUID PRIMARY KEY,campaign_id UUID NOT NULL REFERENCES seller_campaigns(id) ON DELETE CASCADE,store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,event_type VARCHAR(20) NOT NULL,ad_token VARCHAR(200),product_id VARCHAR(50),occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW());`
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
