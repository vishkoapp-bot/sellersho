package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// TestTeam05PublicStoreViewKeys pins the public shape (D52 as amended
// by FIX3): the 12 live keys plus templateId, templateConfig and
// logo; no profit/margin, no owner data, no phone/sales/referral/
// domain/builder keys; empty lists emitted as [] rather than null.
func TestTeam05PublicStoreViewKeys(t *testing.T) {
	want := map[string]bool{
		"activeCategories": true, "activeProductIds": true, "description": true,
		"id": true, "logo": true, "name": true, "ownerType": true, "selectedNicheIds": true,
		"sellerDealsWidgetConfig": true, "shortDescription": true, "slug": true,
		"status": true, "tagline": true, "templateConfig": true, "templateId": true,
	}
	store := &domain.Store{
		ID: "s-1", Name: "Test Store", Slug: "test-store", OwnerType: "dropshipper",
		OwnerName: "Secret Owner", ProfitMarginPercent: 15, ReferralCode: "DRUP-XXXX",
		CustomDomain: "secret.example", TotalSales: 99,
		Tagline: "tag", Description: "desc", ShortDescription: "short",
		Status: "suspended", BuilderConfig: json.RawMessage(`{"theme":"x"}`),
		TemplateID: "tpl-fashion-1", Logo: "https://example.test/logo.png",
		TemplateConfig:          json.RawMessage(`{"accentColor":"#FF3B30","custom":{"heroSlides":[{"image":"https://example.test/h.jpg","title":"Hi"}],"secretNote":"drop me"},"sections":[{"key":"deals","enabled":true}]}`),
		SellerDealsWidgetConfig: json.RawMessage(`{"widget":true}`),
		ActiveProductIDs:        []string{"p-1"},
	}
	raw, err := json.Marshal(team05PublicStoreView(store))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 15 {
		t.Fatalf("public store has %d keys, want exactly 15: %s", len(got), raw)
	}
	for k := range got {
		if !want[k] {
			t.Fatalf("unexpected public key %q in %s", k, raw)
		}
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Fatalf("missing public key %q in %s", k, raw)
		}
	}
	if string(got["templateId"]) != `"tpl-fashion-1"` {
		t.Fatalf("public templateId not emitted: %s", raw)
	}
	if string(got["logo"]) != `"https://example.test/logo.png"` {
		t.Fatalf("public logo not emitted: %s", raw)
	}
	var pubCfg map[string]json.RawMessage
	if err := json.Unmarshal(got["templateConfig"], &pubCfg); err != nil {
		t.Fatalf("public templateConfig is not JSON: %v", err)
	}
	if string(pubCfg["accentColor"]) != `"#FF3B30"` {
		t.Fatalf("public templateConfig lost accentColor: %s", got["templateConfig"])
	}
	var pubCustom map[string]json.RawMessage
	if err := json.Unmarshal(pubCfg["custom"], &pubCustom); err != nil {
		t.Fatalf("public templateConfig.custom is not JSON: %v", err)
	}
	if _, ok := pubCustom["heroSlides"]; !ok {
		t.Fatalf("public templateConfig dropped allowlisted heroSlides: %s", got["templateConfig"])
	}
	if _, ok := pubCustom["secretNote"]; ok {
		t.Fatalf("public templateConfig leaked non-allowlisted key: %s", got["templateConfig"])
	}
	for _, private := range []string{"ownerName", "profitMarginPercent", "referralCode", "customDomain", "totalSales", "builderConfig", "ownerPhone"} {
		if _, ok := got[private]; ok {
			t.Fatalf("private key %q leaked into public store: %s", private, raw)
		}
	}
	if string(got["status"]) != `"suspended"` {
		t.Fatalf("suspended status not served: %s", raw)
	}
	if string(got["activeCategories"]) != "[]" || string(got["selectedNicheIds"]) != "[]" {
		t.Fatalf("nil lists must marshal as []: %s", raw)
	}
	if string(got["sellerDealsWidgetConfig"]) != `{"widget":true}` {
		t.Fatalf("widget config not passed through: %s", raw)
	}
	// A nil store still yields the 15 keys (empty values), never null.
	raw, err = json.Marshal(team05PublicStoreView(nil))
	if err != nil {
		t.Fatal(err)
	}
	var nilGot map[string]json.RawMessage
	if err := json.Unmarshal(raw, &nilGot); err != nil {
		t.Fatal(err)
	}
	if len(nilGot) != 15 {
		t.Fatalf("nil public store has %d keys, want 15: %s", len(nilGot), raw)
	}
	// A nil list marshals as [], never null.
	raw, err = json.Marshal(team05PublicStoreList(nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" {
		t.Fatalf("nil public list = %s, want []", raw)
	}
}

// team05TestDB opens the disposable test database named by
// TEAM05_TEST_DATABASE_URL and creates the minimal schema these
// tests need. It skips (never fails) when the variable is unset, so
// `go test ./...` without a database stays green. NEVER point this
// at a live database: the tests insert and delete rows.
func team05TestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEAM05_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEAM05_TEST_DATABASE_URL unset, skipping database test")
	}
	db, err := testdb.Open(t, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS stores (id TEXT PRIMARY KEY, name TEXT NOT NULL, slug TEXT UNIQUE NOT NULL, owner_type TEXT NOT NULL, owner_name TEXT NOT NULL DEFAULT '', owner_user_id TEXT, profit_margin_percent DOUBLE PRECISION NOT NULL DEFAULT 0, custom_store_margin_override DOUBLE PRECISION, is_margin_locked_by_admin BOOLEAN NOT NULL DEFAULT FALSE, active_plan_id TEXT NOT NULL DEFAULT '', referral_code TEXT, referrer_seller_id TEXT, signed_contract_version INT NOT NULL DEFAULT 0, custom_domain TEXT NOT NULL DEFAULT '', total_sales BIGINT NOT NULL DEFAULT 0, seller_deals_widget_config JSONB, panel_config JSONB, tagline TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', active_categories TEXT[] NOT NULL DEFAULT '{}', template_id TEXT, template_config JSONB NOT NULL DEFAULT '{}'::jsonb, logo TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS store_product_overrides (store_id TEXT NOT NULL, product_id TEXT NOT NULL, override_value DOUBLE PRECISION NOT NULL, mode TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS store_niche_overrides (store_id TEXT NOT NULL, niche_id TEXT NOT NULL, override_value DOUBLE PRECISION NOT NULL, mode TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS store_products (store_id TEXT NOT NULL, product_id TEXT NOT NULL, position INT NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS seller_discount_campaigns (id TEXT PRIMARY KEY, store_id TEXT NOT NULL, name TEXT NOT NULL, scope TEXT NOT NULL, target_id TEXT NOT NULL DEFAULT '', mode TEXT NOT NULL, value DOUBLE PRECISION NOT NULL, starts_at TIMESTAMPTZ NOT NULL, ends_at TIMESTAMPTZ NOT NULL, is_active BOOLEAN NOT NULL DEFAULT TRUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE IF NOT EXISTS newsletter_subscribers (id VARCHAR(50) PRIMARY KEY, store_id VARCHAR(50) NOT NULL, mobile VARCHAR(15) NOT NULL, email VARCHAR(254), status VARCHAR(12) NOT NULL DEFAULT 'active', source VARCHAR(20) NOT NULL DEFAULT 'footer', unsubscribe_token VARCHAR(64) NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), unsubscribed_at TIMESTAMPTZ)`,
	}
	for _, q := range ddl {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func team05TestStore(t *testing.T, db *sql.DB, owner, slug, status string) string {
	t.Helper()
	id := "s-" + uuid.NewString()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO stores (id, name, slug, owner_type, owner_name, owner_user_id, panel_config, tagline, description, status, active_categories) VALUES ($1, $2, $3, 'dropshipper', 'Owner', $4, $5, 'tag', 'desc', $6, $7)`,
		id, "Team05 Store", slug, owner, `{"shortDescription":"short","selectedNicheIds":["n-1"]}`, status, "{c-1,c-2}")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM seller_discount_campaigns WHERE store_id = $1`, id)
		db.Exec(`DELETE FROM newsletter_subscribers WHERE store_id = $1`, id)
		db.Exec(`DELETE FROM store_products WHERE store_id = $1`, id)
		db.Exec(`DELETE FROM stores WHERE id = $1`, id)
	})
	return id
}

func team05Call(t *testing.T, handler func(http.ResponseWriter, *http.Request), method, path, body, user string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Buffer
	if body != "" {
		rdr = bytes.NewBufferString(body)
	} else {
		rdr = bytes.NewBuffer(nil)
	}
	r := httptest.NewRequest(method, path, rdr)
	r = r.WithContext(context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: user, Role: "seller"}))
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func TestTeam05DiscountCampaignsCRUD(t *testing.T) {
	db := team05TestDB(t)
	repo := repository.NewStoreRepository(db)
	owner := uuid.NewString()
	stranger := uuid.NewString()
	slug := "t05-" + uuid.NewString()[:20]
	storeID := team05TestStore(t, db, owner, slug, "active")
	call := func(method, path, body, user, id string) *httptest.ResponseRecorder {
		return team05Call(t, func(w http.ResponseWriter, r *http.Request) {
			handleTeam05StoreDiscountCampaigns(w, r, repo, slug, id)
		}, method, path, body, user)
	}
	// Invalid scope is rejected, never stored.
	if w := call(http.MethodPost, "/x", `{"name":"bad","scope":"galaxy","mode":"percent","value":5,"startsAt":"2030-01-01T00:00:00Z","endsAt":"2030-02-01T00:00:00Z","isActive":true}`, owner, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid scope: %d %s", w.Code, w.Body.String())
	}
	// Invalid window is rejected.
	if w := call(http.MethodPost, "/x", `{"name":"bad","scope":"store","mode":"percent","value":5,"startsAt":"2030-02-01T00:00:00Z","endsAt":"2030-01-01T00:00:00Z","isActive":true}`, owner, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid window: %d %s", w.Code, w.Body.String())
	}
	// A stranger cannot create for someone else's store.
	if w := call(http.MethodPost, "/x", `{"name":"x","scope":"store","mode":"percent","value":5,"startsAt":"2030-01-01T00:00:00Z","endsAt":"2030-02-01T00:00:00Z","isActive":true}`, stranger, ""); w.Code != http.StatusForbidden {
		t.Fatalf("stranger create: %d %s", w.Code, w.Body.String())
	}
	create := func(name string) string {
		body, _ := json.Marshal(map[string]interface{}{
			"name": name, "scope": "store", "targetId": "", "mode": "percent", "value": 10,
			"startsAt": "2030-01-01T00:00:00Z", "endsAt": "2030-02-01T00:00:00Z", "isActive": true,
		})
		w := call(http.MethodPost, "/x", string(body), owner, "")
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, w.Code, w.Body.String())
		}
		var c domain.SellerDiscountCampaign
		if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		if c.StoreID != storeID || c.Scope != "store" || c.Value != 10 {
			t.Fatalf("created campaign mismatch: %+v", c)
		}
		return c.ID
	}
	first := create("first")
	second := create("second")
	// Force a deterministic created_at order: first is older.
	if _, err := db.Exec(`UPDATE seller_discount_campaigns SET created_at = NOW() - INTERVAL '1 day' WHERE id = $1`, first); err != nil {
		t.Fatal(err)
	}
	w := call(http.MethodGet, "/x", "", owner, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var list []domain.SellerDiscountCampaign
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != second || list[1].ID != first {
		t.Fatalf("list order not created_at DESC: %+v", list)
	}
	// PATCH updates the value; PATCH of a foreign id is 404, not a leak.
	w = call(http.MethodPatch, "/x", `{"value":25}`, owner, first)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	var patched domain.SellerDiscountCampaign
	if err := json.Unmarshal(w.Body.Bytes(), &patched); err != nil {
		t.Fatal(err)
	}
	if patched.Value != 25 || patched.Name != "first" {
		t.Fatalf("patched campaign mismatch: %+v", patched)
	}
	if w := call(http.MethodPatch, "/x", `{"value":1}`, owner, "dcamp-missing"); w.Code != http.StatusNotFound {
		t.Fatalf("patch missing: %d %s", w.Code, w.Body.String())
	}
	// DELETE is scoped by id AND store_id: a stranger gets 403.
	if w := call(http.MethodDelete, "/x", "", stranger, first); w.Code != http.StatusForbidden {
		t.Fatalf("stranger delete: %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodDelete, "/x", "", owner, first); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodDelete, "/x", "", owner, first); w.Code != http.StatusNotFound {
		t.Fatalf("re-delete: %d %s", w.Code, w.Body.String())
	}
	w = call(http.MethodGet, "/x", "", owner, "")
	var remaining []domain.SellerDiscountCampaign
	if err := json.Unmarshal(w.Body.Bytes(), &remaining); err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != second {
		t.Fatalf("remaining after delete: %+v", remaining)
	}
}

func TestTeam05SellerSubscribers(t *testing.T) {
	db := team05TestDB(t)
	repo := repository.NewStoreRepository(db)
	owner := uuid.NewString()
	stranger := uuid.NewString()
	slug := "t05-" + uuid.NewString()[:20]
	storeID := team05TestStore(t, db, owner, slug, "active")
	otherSlug := "t05-" + uuid.NewString()[:20]
	otherID := team05TestStore(t, db, stranger, otherSlug, "active")
	ctx := context.Background()
	sub := func(id, store, mobile, status string) {
		if _, err := db.ExecContext(ctx, `INSERT INTO newsletter_subscribers (id, store_id, mobile, email, status, source, unsubscribe_token) VALUES ($1, $2, $3, $4, $5, 'footer', $6)`, id, store, mobile, mobile+"@example.test", status, "tok-"+id); err != nil {
			t.Fatal(err)
		}
	}
	sub("sub-own-active", storeID, "09120000001", "active")
	sub("sub-own-unsub", storeID, "09120000002", "unsubscribed")
	sub("sub-other", otherID, "09120000003", "active")
	call := func(method, path, user string) *httptest.ResponseRecorder {
		return team05Call(t, func(w http.ResponseWriter, r *http.Request) {
			handleTeam05StoreSubscribers(w, r, repo, slug)
		}, method, path, "", user)
	}
	if w := call(http.MethodPost, "/x", owner); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST subscribers: %d", w.Code)
	}
	if w := call(http.MethodGet, "/x", stranger); w.Code != http.StatusForbidden {
		t.Fatalf("stranger subscribers: %d %s", w.Code, w.Body.String())
	}
	w := call(http.MethodGet, "/x", owner)
	if w.Code != http.StatusOK {
		t.Fatalf("list subscribers: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "unsubscribe_token") || strings.Contains(body, "tok-sub-") {
		t.Fatalf("subscriber list leaks unsubscribe tokens: %s", body)
	}
	var out struct {
		Subscribers []domain.StoreSubscriber `json:"subscribers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Subscribers) != 2 {
		t.Fatalf("subscribers = %d, want 2 (own store only): %+v", len(out.Subscribers), out.Subscribers)
	}
	w = call(http.MethodGet, "/x?status=active", owner)
	var filtered struct {
		Subscribers []domain.StoreSubscriber `json:"subscribers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &filtered); err != nil {
		t.Fatal(err)
	}
	if len(filtered.Subscribers) != 1 || filtered.Subscribers[0].Mobile != "09120000001" {
		t.Fatalf("status filter mismatch: %+v", filtered.Subscribers)
	}
}

func TestTeam05PublicStoreListServesSuspended(t *testing.T) {
	db := team05TestDB(t)
	repo := repository.NewStoreRepository(db)
	owner := uuid.NewString()
	slug := "t05-" + uuid.NewString()[:20]
	team05TestStore(t, db, owner, slug, "suspended")
	r := httptest.NewRequest(http.MethodGet, "/api/v1/stores", nil)
	w := httptest.NewRecorder()
	handleStoresList(w, r, context.Background(), repo)
	if w.Code != http.StatusOK {
		t.Fatalf("public list: %d %s", w.Code, w.Body.String())
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	var found map[string]json.RawMessage
	for _, item := range list {
		if string(item["slug"]) == `"`+slug+`"` {
			found = item
		}
	}
	if found == nil {
		t.Fatalf("suspended store %s missing from public list: %s", slug, w.Body.String())
	}
	if len(found) != 12 {
		t.Fatalf("public list item has %d keys, want 12: %s", len(found), w.Body.String())
	}
	if string(found["status"]) != `"suspended"` {
		t.Fatalf("suspended status not served in list: %s", w.Body.String())
	}
	if string(found["activeCategories"]) != `["c-1","c-2"]` {
		t.Fatalf("activeCategories not served: %s", w.Body.String())
	}
	if _, ok := found["profitMarginPercent"]; ok {
		t.Fatalf("public list leaks profit margin: %s", w.Body.String())
	}
}

// TestTeam05PublicTemplateConfigSanitizer pins the FIX3 allowlist:
// invalid input yields {}, and unknown top-level/custom keys drop.
func TestTeam05PublicTemplateConfigSanitizer(t *testing.T) {
	if got := string(team05PublicTemplateConfig(nil)); got != `{}` {
		t.Fatalf("nil config: got %s, want {}", got)
	}
	if got := string(team05PublicTemplateConfig(json.RawMessage(`not json`))); got != `{}` {
		t.Fatalf("invalid config: got %s, want {}", got)
	}
	got := team05PublicTemplateConfig(json.RawMessage(`{"accentColor":"#123456","evil":1,"custom":{"banners":[],"ownerNote":"x"}}`))
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("sanitized config is not JSON: %v", err)
	}
	if _, ok := doc["evil"]; ok {
		t.Fatalf("top-level key leaked: %s", got)
	}
	var custom map[string]json.RawMessage
	if err := json.Unmarshal(doc["custom"], &custom); err != nil {
		t.Fatalf("sanitized custom is not JSON: %v", err)
	}
	if _, ok := custom["banners"]; !ok {
		t.Fatalf("allowlisted banners dropped: %s", got)
	}
	if _, ok := custom["ownerNote"]; ok {
		t.Fatalf("non-allowlisted custom key leaked: %s", got)
	}
}
