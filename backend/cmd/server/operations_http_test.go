package main

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/drup/platform/internal/adguard"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func opsHTTPDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("OPERATIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("OPERATIONS_TEST_DATABASE_URL required")
	}
	db, e := testdb.Open(t, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func TestOperationsAdHTTPAndProxySpoof(t *testing.T) {
	db := opsHTTPDB(t)
	ctx := context.Background()
	repo := repository.NewStoreRepository(db)
	var store string
	if e := db.QueryRow(`SELECT id FROM stores WHERE status='active' LIMIT 1`).Scan(&store); e != nil {
		t.Fatal(e)
	}
	campaign := uuid.NewString()
	if _, e := db.Exec(`INSERT INTO seller_campaigns(id,store_id,name,campaign_type,status) VALUES($1,$2,'http ops','link','active')`, campaign, store); e != nil {
		t.Fatal(e)
	}
	guard := adguard.New(strings.Repeat("x", 32))
	now := time.Now()
	guard.Now = func() time.Time { return now }
	request := func(body string, challenge bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/ads/events", strings.NewReader(body))
		r.RemoteAddr = "203.0.113.100:1234"
		r.Header.Set("User-Agent", "Mozilla/5.0")
		r.Header.Set("X-Real-IP", "spoof")
		w := httptest.NewRecorder()
		if challenge {
			handleAdChallenge(w, r, guard, repo)
		} else {
			handleSellerAdEvent(w, r, repo, guard)
		}
		return w
	}
	body := `{"campaignId":"` + campaign + `","fingerprint":"` + strings.Repeat("a", 64) + `"}`
	w := request(body, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var proof map[string]string
	if e := json.Unmarshal(w.Body.Bytes(), &proof); e != nil {
		t.Fatal(e)
	}
	p := map[string]interface{}{"campaignId": campaign, "type": "click", "token": "token1", "fingerprint": strings.Repeat("a", 64), "challenge": proof["challenge"]}
	b, _ := json.Marshal(p)
	w = request(string(b), false)
	if !strings.Contains(w.Body.String(), "too_fast") {
		t.Fatal(w.Body.String())
	}
	now = now.Add(time.Second)
	w = request(string(b), false)
	if !strings.Contains(w.Body.String(), `"accepted":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	p["token"] = "different-token"
	b, _ = json.Marshal(p)
	w = request(string(b), false)
	if !strings.Contains(w.Body.String(), `"accepted":false`) {
		t.Fatal(w.Body.String())
	}
	p["webdriver"] = true
	b, _ = json.Marshal(p)
	w = request(string(b), true)
	if w.Code != 403 {
		t.Fatal("webdriver not rejected", w.Body.String())
	}
	for i := 0; i < 60; i++ {
		_, _ = repo.AllowAdChallenge(ctx, "rate-test-"+campaign)
	}
	if ok, e := repo.AllowAdChallenge(ctx, "rate-test-"+campaign); e != nil || ok {
		t.Fatal("rate limit not enforced", e)
	}
	t.Setenv("TRUSTED_PROXY_CIDRS", "172.18.0.0/16")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.100:123"
	r.Header.Set("X-Real-IP", "198.51.100.5")
	if adClientIP(r) != "203.0.113.100" {
		t.Fatal("trusted internet forwarded header")
	}
	r.RemoteAddr = "172.18.0.3:123"
	if adClientIP(r) != "198.51.100.5" {
		t.Fatal("lost trusted proxy IP")
	}
}
func TestOperationsAuthorizationAndDomainPermission(t *testing.T) {
	service := auth.NewJWTService(strings.Repeat("a", 32))
	handler := RequireRoles(service, "admin", "logistics_specialist")(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, test := range []struct {
		role string
		want int
	}{{"", 401}, {"supplier", 403}, {"warehouse_staff", 403}, {"logistics_specialist", 204}, {"admin", 204}} {
		r := httptest.NewRequest("PUT", "/warehouse/bins/a/position", nil)
		if test.role != "" {
			token, e := service.GenerateToken("test", "09123456789", "test", test.role, time.Hour)
			if e != nil {
				t.Fatal(e)
			}
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != test.want {
			t.Fatalf("role=%s status=%d", test.role, w.Code)
		}
	}
	t.Setenv("PLATFORM_DOMAINS", "ops.example.com")
	w := httptest.NewRecorder()
	handleDomainPermission(w, httptest.NewRequest("GET", "/internal/domains/allow?domain=ops.example.com", nil), nil)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	handleDomainPermission(w, httptest.NewRequest("GET", "/internal/domains/allow?domain=not-approved.example.com", nil), nil)
	if w.Code >= 200 && w.Code < 300 {
		t.Fatal("unknown hostname authorized")
	}
}
