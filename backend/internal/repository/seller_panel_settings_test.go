package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

// TestSaveStoreSettingsAddonSectionTitle (Batch 4b, G2): the seller
// store-profile form always sends addonSectionTitle, so SaveStoreSettings
// must accept the exact form payload {tagline, shortDescription,
// addonSectionTitle}. Storage home (decision, recorded in the commit):
// the dedicated stores.addon_section_title VARCHAR(200) column — the
// same column the PDP reads (GetPDPAddons), so the saved title is the
// title the PDP renders; panel_config only ever holds a merge copy.
// Bound: 200 characters (rune count — PostgreSQL VARCHAR counts
// characters, and Persian titles are multi-byte).
func TestSaveStoreSettingsAddonSectionTitle(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SELLERSHO_TEST_DATABASE_URL is not set; skipping disposable-DB integration test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	repo := NewStoreRepository(db)
	owner := uuid.NewString()
	slug := "g2-" + uuid.NewString()[:20]
	storeID := "s-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role,is_active) VALUES($1,$2,'G2 test','seller',TRUE)`, owner, fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO stores(id,name,slug,owner_type,owner_name,owner_user_id,panel_config,tagline,description,status,active_categories) VALUES($1,'G2 Store',$2,'dropshipper','Owner',$3,'{}'::jsonb,'','', 'active','{}')`, storeID, slug, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM store_products WHERE store_id=$1`, storeID)
		db.Exec(`DELETE FROM stores WHERE id=$1`, storeID)
		db.Exec(`DELETE FROM users WHERE id=$1`, owner)
	})
	column := func() string {
		var v sql.NullString
		if err := db.QueryRow(`SELECT addon_section_title FROM stores WHERE id=$1`, storeID).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v.String
	}

	// (a) The exact form payload succeeds — today it fails ErrStoreInput.
	if err := repo.SaveStoreSettings(ctx, owner, slug, map[string]interface{}{
		"tagline": "مرجع تخصصی لوازم جانبی و دیجیتال", "shortDescription": "x", "addonSectionTitle": "پیشنهاد ما",
	}); err != nil {
		t.Fatalf("form payload: %v, want nil", err)
	}
	if got := column(); got != "پیشنهاد ما" {
		t.Fatalf("column = %q, want %q", got, "پیشنهاد ما")
	}
	loaded, err := repo.GetStoreBySlug(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AddonSectionTitle != "پیشنهاد ما" || loaded.Tagline != "مرجع تخصصی لوازم جانبی و دیجیتال" || loaded.ShortDescription != "x" {
		t.Fatalf("round-trip: addon=%q tagline=%q short=%q", loaded.AddonSectionTitle, loaded.Tagline, loaded.ShortDescription)
	}

	// (b) A 201-char title fails with ErrStoreInput and persists nothing.
	long := strings.Repeat("a", 201)
	if err := repo.SaveStoreSettings(ctx, owner, slug, map[string]interface{}{"addonSectionTitle": long}); !errors.Is(err, ErrStoreInput) {
		t.Fatalf("201-char title: %v, want ErrStoreInput", err)
	}
	if got := column(); got != "پیشنهاد ما" {
		t.Fatalf("failed save changed the column to %q", got)
	}

	// (c) A non-string title fails.
	if err := repo.SaveStoreSettings(ctx, owner, slug, map[string]interface{}{"addonSectionTitle": 42}); !errors.Is(err, ErrStoreInput) {
		t.Fatalf("non-string title: %v, want ErrStoreInput", err)
	}

	// (d) An unknown key still fails (allowlist intact).
	if err := repo.SaveStoreSettings(ctx, owner, slug, map[string]interface{}{"bogusKey": "x"}); !errors.Is(err, ErrStoreInput) {
		t.Fatalf("unknown key: %v, want ErrStoreInput", err)
	}

	// (e) Clearing the title (empty string) resets the column, so the
	// PDP default «محصولات مکمل» shows again.
	if err := repo.SaveStoreSettings(ctx, owner, slug, map[string]interface{}{"addonSectionTitle": ""}); err != nil {
		t.Fatalf("clear title: %v, want nil", err)
	}
	if got := column(); got != "" {
		t.Fatalf("cleared column = %q, want empty", got)
	}
}

func TestSaveStoreSettingsSelectedNicheIDs(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo := NewStoreRepository(db)
	owner := uuid.NewString()
	prefix := "drp31-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role) VALUES ($1,$2,'Niche settings test','seller')`, owner, "09"+owner[:13]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	if _, err := db.Exec(`INSERT INTO stores (id, name, slug, owner_name, owner_user_id, tagline, panel_config)
		VALUES ($1,'Niche settings test',$1,'Owner',$2,'Preserved tagline',
		'{"tagline":"Preserved tagline","builderConfig":{"selectedTheme":"home","accentColor":"#112233"},"shortDescription":"Preserved description","otherKey":{"enabled":true}}')`, prefix, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM stores WHERE id=$1`, prefix) })
	if _, err := db.Exec(`INSERT INTO niches (id,slug,title)
		SELECT $1 || '-' || i, $1 || '-' || i, 'Fixture niche' FROM generate_series(1,101) AS g(i)`, prefix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM niches WHERE left(id,length($1)+1)=$1 || '-'`, prefix) })
	var tooMany []any
	for i := 1; i <= 101; i++ {
		tooMany = append(tooMany, fmt.Sprintf("%s-%d", prefix, i))
	}
	for _, tc := range []struct {
		name string
		ids  []any
	}{
		{"unknown", []any{prefix + "-missing"}},
		{"too many", tooMany},
		{"duplicate", []any{tooMany[0], tooMany[0]}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := repo.SaveStoreSettings(ctx, owner, prefix, map[string]any{"selectedNicheIds": tc.ids})
			if !errors.Is(err, ErrStoreInput) {
				t.Fatalf("SaveStoreSettings = %v, want ErrStoreInput", err)
			}
		})
	}
	if err := repo.SaveStoreSettings(ctx, owner, prefix, map[string]any{"selectedNicheIds": tooMany[:2]}); err != nil {
		t.Fatal(err)
	}
	store, err := repo.GetStoreBySlug(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{prefix + "-1", prefix + "-2"}
	if !slices.Equal(store.SelectedNicheIDs, want) {
		t.Fatalf("selectedNicheIds = %v, want %v", store.SelectedNicheIDs, want)
	}
	var config struct {
		Tagline string `json:"tagline"`
		Other   struct {
			Enabled bool `json:"enabled"`
		} `json:"otherKey"`
	}
	var builder map[string]string
	if err := json.Unmarshal(store.PanelConfig, &config); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(store.BuilderConfig, &builder); err != nil {
		t.Fatal(err)
	}
	if store.Tagline != "Preserved tagline" || config.Tagline != store.Tagline ||
		store.ShortDescription != "Preserved description" || !config.Other.Enabled ||
		builder["selectedTheme"] != "home" || builder["accentColor"] != "#112233" {
		t.Fatalf("niche save altered existing settings: %s", store.PanelConfig)
	}
}
