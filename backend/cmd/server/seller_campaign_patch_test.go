package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestSellerCampaignPatchAtomic(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires a guarded disposable PostgreSQL database")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo := repository.NewStoreRepository(db)
	owner := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role,is_active) VALUES($1,$2,'Campaign patch test','seller',true)`, owner, "09"+owner[:13]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	store := domain.Store{ID: uuid.NewString(), Name: "Campaign patch test", Slug: "campaign-patch-" + uuid.NewString()[:8], OwnerType: "dropshipper", OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: "CMP-" + uuid.NewString()[:8]}
	if err := repo.CreateStore(ctx, store); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM stores WHERE id=$1`, store.ID) })
	var product string
	if err := db.QueryRow(`SELECT id FROM products WHERE supplier_base_cost>0 ORDER BY id LIMIT 1`).Scan(&product); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO store_products(store_id,product_id,position) VALUES($1,$2,0)`, store.ID, product); err != nil {
		t.Fatal(err)
	}
	campaign, err := repo.CreateSellerCampaign(ctx, owner, store.Slug, "Original campaign", "banner", []string{product}, nil, "draft")
	if err != nil {
		t.Fatal(err)
	}

	type state struct {
		name, status, products, updatedAt string
		history                           int
	}
	readState := func(t *testing.T) state {
		t.Helper()
		var s state
		if err := db.QueryRow(`SELECT name,status,product_ids::text,updated_at::text,
			(SELECT count(*) FROM seller_campaign_history WHERE campaign_id=c.id)
			FROM seller_campaigns c WHERE id=$1`, campaign.ID).Scan(&s.name, &s.status, &s.products, &s.updatedAt, &s.history); err != nil {
			t.Fatal(err)
		}
		return s
	}
	initial := readState(t)
	for _, tc := range []struct {
		label, name, status, wantName, wantStatus string
		metadata                                  bool
		wantCode, historyDelta                    int
	}{
		{"invalid status rolls back metadata and history", "Rejected update", "invalid", "Original campaign", "draft", true, http.StatusBadRequest, 0},
		{"metadata only", "Edited campaign", "", "Edited campaign", "draft", true, http.StatusNoContent, 1},
		{"status only", "", "active", "Edited campaign", "active", false, http.StatusNoContent, 1},
		{"combined", "Combined campaign", "paused", "Combined campaign", "paused", true, http.StatusNoContent, 2},
	} {
		t.Run(tc.label, func(t *testing.T) {
			before := readState(t)
			payload := map[string]interface{}{"status": tc.status}
			if tc.metadata {
				payload["name"] = tc.name
				payload["productIds"] = []string{product}
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/stores/" + store.Slug + "/campaigns/" + campaign.ID
			r := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(string(body)))
			r = r.WithContext(sellerCampaignRequest(http.MethodPatch, path, owner).Context())
			w := httptest.NewRecorder()
			handleSellerCampaignAction(w, r, repo, store.Slug, "edit")
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.wantCode, w.Body.String())
			}
			after := readState(t)
			if tc.historyDelta == 0 && after != before {
				t.Fatalf("rejected PATCH changed campaign or history: before %+v, after %+v", before, after)
			}
			if after.name != tc.wantName || after.status != tc.wantStatus || after.products != initial.products || after.history != before.history+tc.historyDelta {
				t.Fatalf("unexpected persisted state: %+v (history before: %d)", after, before.history)
			}
			if tc.historyDelta == 2 {
				var metadataEntries, statusEntries int
				if err := db.QueryRow(`SELECT
					count(*) FILTER (WHERE snapshot->>'status'=$3 AND actor_user_id=$4),
					count(*) FILTER (WHERE snapshot->>'status'=$5 AND actor_user_id IS NULL)
					FROM seller_campaign_history WHERE campaign_id=$1 AND action='updated' AND snapshot->>'name'=$2`,
					campaign.ID, tc.wantName, before.status, owner, tc.wantStatus).Scan(&metadataEntries, &statusEntries); err != nil {
					t.Fatal(err)
				}
				if metadataEntries != 1 || statusEntries != 1 {
					t.Fatalf("combined PATCH history: metadata=%d status=%d, want one of each", metadataEntries, statusEntries)
				}
			}
		})
	}
}
