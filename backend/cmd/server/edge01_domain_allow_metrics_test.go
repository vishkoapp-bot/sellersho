package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

// scrapeAllowSeries reads the EDGE-01 allow series out of the global
// registry's exposition. Keys are "allowed", "denied", "error" (outcome
// counts), "db_queries", "duration_count", and (EDGE-01 step 3)
// "cache_hits", "cache_misses", "cache_evictions".
func scrapeAllowSeries(t *testing.T) map[string]int64 {
	t.Helper()
	w := httptest.NewRecorder()
	observability.Default.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	out := map[string]int64{}
	for _, line := range strings.Split(w.Body.String(), "\n") {
		for _, outcome := range []string{"allowed", "denied", "error"} {
			if prefix := `sellersho_domain_allow_total{outcome="` + outcome + `"} `; strings.HasPrefix(line, prefix) {
				n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, prefix)), 10, 64)
				if err != nil {
					t.Fatalf("unparsable allow series %q: %v", line, err)
				}
				out[outcome] = n
			}
		}
		if prefix := `sellersho_domain_allow_db_queries_total `; strings.HasPrefix(line, prefix) {
			n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, prefix)), 10, 64)
			if err != nil {
				t.Fatalf("unparsable allow db series %q: %v", line, err)
			}
			out["db_queries"] = n
		}
		if prefix := `sellersho_domain_allow_cache_hits_total `; strings.HasPrefix(line, prefix) {
			n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, prefix)), 10, 64)
			if err != nil {
				t.Fatalf("unparsable allow cache-hits series %q: %v", line, err)
			}
			out["cache_hits"] = n
		}
		if prefix := `sellersho_domain_allow_cache_misses_total `; strings.HasPrefix(line, prefix) {
			n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, prefix)), 10, 64)
			if err != nil {
				t.Fatalf("unparsable allow cache-misses series %q: %v", line, err)
			}
			out["cache_misses"] = n
		}
		if prefix := `sellersho_domain_allow_cache_evictions_total `; strings.HasPrefix(line, prefix) {
			n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, prefix)), 10, 64)
			if err != nil {
				t.Fatalf("unparsable allow cache-evictions series %q: %v", line, err)
			}
			out["cache_evictions"] = n
		}
		if prefix := `sellersho_domain_allow_duration_seconds_count `; strings.HasPrefix(line, prefix) {
			n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, prefix)), 10, 64)
			if err != nil {
				t.Fatalf("unparsable allow duration series %q: %v", line, err)
			}
			out["duration_count"] = n
		}
	}
	return out
}

func delta(t *testing.T, before, after map[string]int64, key string) int64 {
	t.Helper()
	return after[key] - before[key]
}

// TestEDGE01DomainAllowHandlerMetrics (EDGE-01 step 1): the allow
// endpoint's decisions move the outcome counter, the allow DB-query
// counter and the handler-latency histogram exactly as the handler
// decides — including the primary-domain short-circuit, which must
// record "allowed" WITHOUT a DB query, and the nil-repo 503, which
// records "error".
func TestEDGE01DomainAllowHandlerMetrics(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewStoreRepository(db)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner := uuid.NewString()
	slug := "edge01-" + owner[:8]
	host := slug + ".example.com"
	exec(`INSERT INTO users(id,mobile,full_name) VALUES($1,$2,'EDGE-01 allow fixture')`, owner, "09000040311")
	t.Cleanup(func() { exec(`DELETE FROM users WHERE id=$1`, owner) })
	exec(`INSERT INTO stores(id,name,slug,owner_name,owner_user_id,referral_code) VALUES($1,$1,$1,'EDGE-01 allow fixture',$2,$1)`, slug, owner)
	t.Cleanup(func() { exec(`DELETE FROM stores WHERE id=$1`, slug) })
	exec(`INSERT INTO store_domains(domain,store_id,verification_token,status,verified_at) VALUES($2,$1,'fixture','active',NOW())`, slug, host)
	t.Cleanup(func() {
		exec(`DELETE FROM pending_approvals WHERE target_id=$1`, slug)
		exec(`DELETE FROM store_domains WHERE domain=$1`, host)
	})
	exec(`INSERT INTO pending_approvals(id,entity_type,target_id,title,requested_data,requested_by,status)
		VALUES($1,'custom_domain',$2,'EDGE-01 allow fixture',jsonb_build_object('value',$3::text),$4,'approved')`, uuid.NewString(), slug, host, owner)

	t.Setenv("PLATFORM_DOMAINS", "primary.edge01.test")

	allow := func(domain string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/internal/domains/allow?domain="+domain, nil)
		w := httptest.NewRecorder()
		handleDomainPermission(w, r, repo)
		return w
	}

	before := scrapeAllowSeries(t)
	if w := allow(host); w.Code != http.StatusNoContent {
		t.Fatalf("approved domain: status %d, want 204: %s", w.Code, w.Body)
	}
	if w := allow("unknown.example.com"); w.Code != http.StatusForbidden {
		t.Fatalf("unknown domain: status %d, want 403: %s", w.Code, w.Body)
	}
	if w := allow("primary.edge01.test"); w.Code != http.StatusNoContent {
		t.Fatalf("primary domain: status %d, want 204: %s", w.Code, w.Body)
	}
	// Nil repo: 503 "error", no DB query.
	r := httptest.NewRequest(http.MethodGet, "/internal/domains/allow?domain="+host, nil)
	w := httptest.NewRecorder()
	handleDomainPermission(w, r, nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil repo: status %d, want 503: %s", w.Code, w.Body)
	}
	// Non-GET: 405, no outcome recorded (but the handler duration is).
	r = httptest.NewRequest(http.MethodPost, "/internal/domains/allow?domain="+host, nil)
	w = httptest.NewRecorder()
	handleDomainPermission(w, r, repo)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status %d, want 405: %s", w.Code, w.Body)
	}
	// Direct repository call (the handleDomainStore call site): with
	// the step-3 cache this is a HIT on the entry the approved allow
	// call just stored — one more cache hit, no new DB query, and the
	// same slug (the two callers can no longer disagree).
	if got, err := repo.AllowedDomain(context.Background(), host); err != nil || got != slug {
		t.Fatalf("AllowedDomain(%q) = %q, %v; want %q, nil", host, got, err, slug)
	}
	after := scrapeAllowSeries(t)
	// Outcomes: approved + primary = 2 allowed; unknown = 1 denied;
	// nil repo = 1 error; POST records no outcome.
	if got := delta(t, before, after, "allowed"); got != 2 {
		t.Fatalf("allowed delta %d, want 2", got)
	}
	if got := delta(t, before, after, "denied"); got != 1 {
		t.Fatalf("denied delta %d, want 1", got)
	}
	if got := delta(t, before, after, "error"); got != 1 {
		t.Fatalf("error delta %d, want 1", got)
	}
	// DB queries: approved (miss, 1) + unknown (miss, 1) = 2. The
	// direct AllowedDomain call is a cache hit (0 queries). Primary
	// short-circuits before the repo; nil-repo and POST never reach it.
	if got := delta(t, before, after, "db_queries"); got != 2 {
		t.Fatalf("db_queries delta %d, want 2", got)
	}
	// Cache: 2 misses (approved + unknown loads) and 1 hit (the
	// direct AllowedDomain call). hits + misses = 3 = the repo calls
	// that reached the cache.
	if got := delta(t, before, after, "cache_misses"); got != 2 {
		t.Fatalf("cache_misses delta %d, want 2", got)
	}
	if got := delta(t, before, after, "cache_hits"); got != 1 {
		t.Fatalf("cache_hits delta %d, want 1", got)
	}
	// Handler latency: one sample per handleDomainPermission call (5).
	if got := delta(t, before, after, "duration_count"); got != 5 {
		t.Fatalf("duration_count delta %d, want 5", got)
	}
}
