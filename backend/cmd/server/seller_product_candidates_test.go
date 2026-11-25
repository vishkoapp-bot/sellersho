package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func seedCandidateFixtureRow(t *testing.T, db *sql.DB, insert, delete string, args ...any) {
	t.Helper()
	if _, err := db.Exec(insert, args...); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(delete, args[0]); err != nil {
			t.Error(err)
		}
	})
}

func seedCandidateSeller(t *testing.T, db *sql.DB) (id, mobile string) {
	t.Helper()
	id = uuid.NewString()
	mobile = "09" + id[:13]
	seedCandidateFixtureRow(t, db, `INSERT INTO users(id,mobile,full_name,role) VALUES ($1,$2,'Candidate test','seller')`,
		`DELETE FROM users WHERE id=$1`, id, mobile)
	return id, mobile
}

func seedCandidateStore(t *testing.T, db *sql.DB, repo *repository.StoreRepository, owner string) domain.Store {
	t.Helper()
	id := "drp32-" + uuid.NewString()
	store := domain.Store{ID: id, Name: "Candidate test", Slug: id, OwnerType: "dropshipper",
		OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8]}
	if err := repo.CreateStore(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM stores WHERE id=$1`, store.ID); err != nil {
			t.Error(err)
		}
	})
	return store
}

func seedCandidateProduct(t *testing.T, db *sql.DB, id, title string, supplier *string, cost int, active bool) {
	t.Helper()
	seedCandidateFixtureRow(t, db, `INSERT INTO products (id,slug,title,supplier_id,original_price,supplier_base_cost,platform_margin,image_url,is_active)
		VALUES ($1,$1,$2,$3,1000,$4,100,'/fixture.jpg',$5)`, `DELETE FROM products WHERE id=$1`,
		id, title, supplier, cost, active)
}

func TestSellerProductCandidates(t *testing.T) {
	db := setupTestPostgresDB(t)
	repo := repository.NewStoreRepository(db)
	owner, mobile := seedCandidateSeller(t, db)
	stranger, strangerMobile := seedCandidateSeller(t, db)
	store := seedCandidateStore(t, db, repo, owner)
	prefix := "drp32-" + uuid.NewString()
	suspended := prefix + "-sup"
	seedCandidateFixtureRow(t, db, `INSERT INTO suppliers(id,name,approval_status) VALUES ($1,'Candidate supplier','suspended')`,
		`DELETE FROM suppliers WHERE id=$1`, suspended)
	active, inactive, showcase, free, blocked := prefix+"-a", prefix+"-b", prefix+"-c", prefix+"-d", prefix+"-e"
	seedCandidateProduct(t, db, active, prefix+" Speaker", nil, 500, true)
	seedCandidateProduct(t, db, inactive, prefix+` Headset 50%_\`, nil, 500, false)
	seedCandidateProduct(t, db, showcase, prefix+" Showcase", nil, 500, true)
	seedCandidateProduct(t, db, free, prefix+" Free", nil, 0, true)
	seedCandidateProduct(t, db, blocked, prefix+" Blocked", &suspended, 500, true)
	seedCandidateFixtureRow(t, db, `INSERT INTO products (id,slug,title,original_price,supplier_base_cost,platform_margin)
		VALUES ($1,$2,$3,1000,500,100)`, `DELETE FROM products WHERE id=$1`, "", prefix+"-blank-id", prefix+" Missing ID")
	if _, err := db.Exec(`INSERT INTO store_products(store_id,product_id,position) VALUES ($1,$2,0)`, store.ID, showcase); err != nil {
		t.Fatal(err)
	}

	fixture := supplierNotificationsFixture{jwtService: auth.NewJWTService("seller-candidates-test-secret")}
	token := fixture.token(t, owner, mobile, "seller")
	handler := func(w http.ResponseWriter, r *http.Request) { handleSellerProductCandidates(w, r, repo) }
	request := func(token string, params url.Values) (int, string) {
		t.Helper()
		params.Set("storeSlug", store.Slug)
		return fixture.callRole(t, http.MethodGet, "/api/v1/seller/product-candidates?"+params.Encode(), token, "", []string{"seller"}, handler)
	}
	strangerToken := fixture.token(t, stranger, strangerMobile, "seller")
	if code, body := request(strangerToken, url.Values{}); code != http.StatusForbidden {
		t.Fatalf("foreign store: status %d, body %s", code, body)
	}
	if code, body := request("", url.Values{}); code != http.StatusUnauthorized {
		t.Fatalf("no token: status %d, body %s", code, body)
	}
	reject := func(params url.Values) {
		t.Helper()
		if code, body := request(token, params); code != http.StatusBadRequest {
			t.Fatalf("invalid candidate parameters: status %d, body %s", code, body)
		}
	}
	reject(url.Values{"cursor": {"!"}})
	reject(url.Values{"limit": {"0"}})
	reject(url.Values{"ids": {strings.Repeat("x,", 100) + "x"}})

	type page struct {
		Success    bool                         `json:"success"`
		Products   []map[string]json.RawMessage `json:"products"`
		NextCursor string                       `json:"nextCursor"`
		HasMore    bool                         `json:"hasMore"`
	}
	get := func(params url.Values, want ...string) page {
		t.Helper()
		code, body := request(token, params)
		var result page
		if err := json.Unmarshal([]byte(body), &result); err != nil || code != http.StatusOK || !result.Success || result.Products == nil {
			t.Fatalf("candidate list: status %d, body %s, decode error %v", code, body, err)
		}
		var ids []string
		for _, product := range result.Products {
			var id, title, image string
			var activeFlag bool
			if len(product) != 4 || json.Unmarshal(product["id"], &id) != nil ||
				json.Unmarshal(product["title"], &title) != nil || title == "" ||
				json.Unmarshal(product["imageUrl"], &image) != nil || image != "/fixture.jpg" ||
				json.Unmarshal(product["isActive"], &activeFlag) != nil || activeFlag != (id != inactive) {
				t.Fatalf("unexpected candidate fields: %s", body)
			}
			ids = append(ids, id)
		}
		if !slices.Equal(ids, want) {
			t.Fatalf("candidate ids = %v, want %v", ids, want)
		}
		return result
	}
	get(url.Values{"q": {prefix}}, active, inactive)
	get(url.Values{"q": {prefix + ` headset 50%_\`}}, inactive)
	get(url.Values{"q": {prefix + " Unmatched"}})
	get(url.Values{"q": {prefix + "%"}})
	get(url.Values{"ids": {strings.Join([]string{inactive, free, showcase, blocked}, ",")}}, inactive)
	first := get(url.Values{"q": {prefix}, "limit": {"1"}}, active)
	if !first.HasMore || first.NextCursor == "" || first.NextCursor == active {
		t.Fatalf("first-page cursor = %q, hasMore = %v", first.NextCursor, first.HasMore)
	}
	last := get(url.Values{"q": {prefix}, "limit": {"1"}, "cursor": {first.NextCursor}}, inactive)
	if last.HasMore || last.NextCursor != "" {
		t.Fatalf("last-page cursor = %q, hasMore = %v", last.NextCursor, last.HasMore)
	}

	put := handleStoresBlock(fixture.jwtService, repo, nil, nil, nil, nil)
	for _, tc := range []struct {
		id   string
		want int
	}{{inactive, http.StatusOK}, {free, http.StatusBadRequest}} {
		payload, err := json.Marshal(map[string][]string{"productIds": {showcase, tc.id}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPut, "/api/v1/stores/"+store.Slug+"/products", strings.NewReader(string(payload)))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		put(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("products PUT with %s: status %d, want %d, body %s", tc.id, rec.Code, tc.want, rec.Body.String())
		}
	}
	get(url.Values{"ids": {inactive + "," + free}})
}

func TestSellerProductReAddResetsVariantPreferences(t *testing.T) {
	db := setupTestPostgresDB(t)
	ctx := context.Background()
	repo := repository.NewStoreRepository(db)
	catalog := repository.NewCatalogRepository(db)
	owner, _ := seedCandidateSeller(t, db)
	store := seedCandidateStore(t, db, repo, owner)
	product := "drp32-" + uuid.NewString()
	retained := "drp32-" + uuid.NewString()
	for _, id := range []string{product, retained} {
		seedCandidateProduct(t, db, id, "Variant lifecycle", nil, 500, true)
	}

	save := func(ids ...string) {
		t.Helper()
		if err := repo.SaveStoreProducts(ctx, owner, store.Slug, ids); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	save(product, retained)
	addVariant := func(id string, isDefault bool) string {
		t.Helper()
		variantID := uuid.NewString()
		exec(`INSERT INTO product_variants(id,product_id,spnc,is_default) VALUES ($1,$2,$3,$4)`,
			variantID, id, strings.ReplaceAll(variantID, "-", "")[:12], isDefault)
		return variantID
	}
	productDefault, storeDefault := addVariant(product, true), addVariant(product, false)
	retainedHidden, retainedDefault := addVariant(retained, true), addVariant(retained, false)
	setPreferences := func(id, hidden, preferred string) {
		t.Helper()
		exec(`INSERT INTO store_variant_visibility(store_id,product_id,variant_id,is_visible) VALUES ($1,$2,$3,FALSE)`, store.ID, id, hidden)
		exec(`INSERT INTO store_variant_defaults(store_id,product_id,variant_id) VALUES ($1,$2,$3)`, store.ID, id, preferred)
	}
	setPreferences(product, productDefault, storeDefault)
	setPreferences(retained, retainedHidden, retainedDefault)
	exec(`INSERT INTO store_variant_overrides(store_id,product_id,variant_id,override_value) VALUES ($1,$2,$3,15)`,
		store.ID, product, storeDefault)

	assertPreferences := func(id, defaultID string, visibleIDs ...string) {
		t.Helper()
		variants, err := catalog.GetProductVariantsForStore(ctx, id, store.ID)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, variant := range variants {
			got = append(got, variant.ID)
			if variant.ID == productDefault && !variant.IsDefault {
				t.Fatal("product default flag was lost after re-add")
			}
		}
		if !slices.Equal(got, visibleIDs) {
			t.Fatalf("visible variants = %v, want %v", got, visibleIDs)
		}
		defaults, err := catalog.GetStoreVariantDefaults(ctx, store.ID, []string{id})
		if err != nil || defaults[id] != defaultID {
			t.Fatalf("store variant defaults = %v, want %q: %v", defaults, defaultID, err)
		}
	}
	assertPreferences(product, storeDefault, storeDefault)
	save(retained)
	assertPreferences(product, "", productDefault, storeDefault)
	assertPreferences(retained, retainedDefault, retainedDefault)
	save(retained, product)
	assertPreferences(product, "", productDefault, storeDefault)
	assertPreferences(retained, retainedDefault, retainedDefault)
	var override float64
	if err := db.QueryRow(`SELECT override_value FROM store_variant_overrides WHERE store_id=$1 AND product_id=$2 AND variant_id=$3`,
		store.ID, product, storeDefault).Scan(&override); err != nil || override != 15 {
		t.Fatalf("variant override after re-add = %v, want 15: %v", override, err)
	}
}
