package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/customdomain"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

// TestEDGE01DomainAllowCache (EDGE-01 steps 3-4): the bounded allow
// cache answers repeated checks from memory (allow DB queries
// collapse to the refresh rate), write-path invalidation makes
// revocation visible on the NEXT request (not after the 60s TTL),
// and the full domain matrix still behaves correctly under cache
// churn: approved / revoked / re-approved / unknown / malformed /
// IDN / punycode / primary-site routes, plus tenant isolation
// (store A's approval never authorizes store B's domain, and
// revoking A leaves B serving).
func TestEDGE01DomainAllowCache(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewStoreRepository(db)
	adminRepo := repository.NewAdminRepository(db)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PLATFORM_DOMAINS", "primary.edge01.test")

	fixture := func(mobile string) (owner, slug, host, approvalID string) {
		t.Helper()
		owner = uuid.NewString()
		slug = "cache-" + owner[:8]
		host = slug + ".example.com"
		approvalID = uuid.NewString()
		exec(`INSERT INTO users(id,mobile,full_name) VALUES($1,$2,'EDGE-01 cache fixture')`, owner, mobile)
		t.Cleanup(func() { exec(`DELETE FROM users WHERE id=$1`, owner) })
		exec(`INSERT INTO stores(id,name,slug,owner_name,owner_user_id,referral_code) VALUES($1,$1,$1,'EDGE-01 cache fixture',$2,$1)`, slug, owner)
		t.Cleanup(func() { exec(`DELETE FROM stores WHERE id=$1`, slug) })
		exec(`INSERT INTO store_domains(domain,store_id,verification_token,status,verified_at) VALUES($2,$1,'fixture','active',NOW())`, slug, host)
		t.Cleanup(func() {
			exec(`DELETE FROM pending_approvals WHERE target_id=$1`, slug)
			exec(`DELETE FROM store_domains WHERE domain=$1`, host)
			// The test's own direct-SQL cleanup can leave a stale
			// cache entry behind for the next test in this package;
			// drop it like the write paths do.
			customdomain.DefaultAllowCache.Invalidate(host)
		})
		exec(`INSERT INTO pending_approvals(id,entity_type,target_id,title,requested_data,requested_by,status)
			VALUES($1,'custom_domain',$2,'EDGE-01 cache fixture',jsonb_build_object('value',$3::text),$4,'approved')`, approvalID, slug, host, owner)
		return owner, slug, host, approvalID
	}
	allow := func(domain string) int {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/internal/domains/allow?domain="+url.QueryEscape(domain), nil)
		w := httptest.NewRecorder()
		handleDomainPermission(w, r, repo)
		return w.Code
	}
	store := func(hostHeader string) (int, string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/domain-store", nil)
		r.Host = hostHeader
		w := httptest.NewRecorder()
		handleDomainStore(w, r, repo)
		slug := ""
		if w.Code == http.StatusOK {
			var got map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &got); err == nil {
				slug = got["slug"]
			}
		}
		return w.Code, slug
	}

	ownerA, slugA, hostA, approvalA := fixture("09000040321")
	ownerB, slugB, hostB, _ := fixture("09000040322")

	before := scrapeAllowSeries(t)

	// Approved domain: first call misses (1 DB query), the next two
	// are cache hits (0 queries) — including the domain-store call,
	// which reuses the cached slug instead of re-querying.
	if got := allow(hostA); got != http.StatusNoContent {
		t.Fatalf("approved allow = %d, want 204", got)
	}
	if got := allow(hostA); got != http.StatusNoContent {
		t.Fatalf("approved allow (repeat) = %d, want 204", got)
	}
	if code, slug := store(hostA); code != http.StatusOK || slug != slugA {
		t.Fatalf("domain-store(%q) = %d %q, want 200 %q", hostA, code, slug, slugA)
	}
	if got := delta(t, before, scrapeAllowSeries(t), "db_queries"); got != 1 {
		t.Fatalf("db_queries after 2x allow + domain-store = %d, want 1 (cache)", got)
	}
	if got := delta(t, before, scrapeAllowSeries(t), "cache_hits"); got != 2 {
		t.Fatalf("cache_hits = %d, want 2", got)
	}

	// Unknown domain: deny is cached too (short TTL) — the repeat
	// costs no DB query.
	unknown := "unknown-" + ownerA[:8] + ".example.com"
	if got := allow(unknown); got != http.StatusForbidden {
		t.Fatalf("unknown allow = %d, want 403", got)
	}
	if got := allow(unknown); got != http.StatusForbidden {
		t.Fatalf("unknown allow (repeat) = %d, want 403", got)
	}
	if got := delta(t, before, scrapeAllowSeries(t), "db_queries"); got != 2 {
		t.Fatalf("db_queries after unknown x2 = %d, want 2 (deny cached)", got)
	}

	// Revoke via the admin write path: the NEXT request is 403 —
	// invalidation, not the 60s TTL. The domain-store lookup 404s
	// on the same entry (the two callers share it).
	if err := adminRepo.ReviewPendingApproval(ctx, approvalA, "rejected", "EDGE-01 cache test"); err != nil {
		t.Fatalf("ReviewPendingApproval(rejected): %v", err)
	}
	if got := allow(hostA); got != http.StatusForbidden {
		t.Fatalf("revoked allow = %d, want 403 immediately after revoke", got)
	}
	if code, _ := store(hostA); code != http.StatusNotFound {
		t.Fatalf("revoked domain-store = %d, want 404", code)
	}
	// Tenant isolation: revoking A leaves B serving.
	if got := allow(hostB); got != http.StatusNoContent {
		t.Fatalf("store B allow after A revoked = %d, want 204", got)
	}
	if code, slug := store(hostB); code != http.StatusOK || slug != slugB {
		t.Fatalf("store B domain-store = %d %q, want 200 %q", code, slug, slugB)
	}

	// Re-approve via the same write path: 204 again, on the next
	// request. Then flap approve/revoke 3x: every flip is visible
	// on the next request (cache churn within the SLA).
	if err := adminRepo.ReviewPendingApproval(ctx, approvalA, "approved", ""); err != nil {
		t.Fatalf("ReviewPendingApproval(approved): %v", err)
	}
	if got := allow(hostA); got != http.StatusNoContent {
		t.Fatalf("re-approved allow = %d, want 204", got)
	}
	for i := 0; i < 3; i++ {
		if err := adminRepo.ReviewPendingApproval(ctx, approvalA, "rejected", "churn"); err != nil {
			t.Fatalf("churn reject %d: %v", i, err)
		}
		if got := allow(hostA); got != http.StatusForbidden {
			t.Fatalf("churn %d revoked allow = %d, want 403", i, got)
		}
		if err := adminRepo.ReviewPendingApproval(ctx, approvalA, "approved", ""); err != nil {
			t.Fatalf("churn approve %d: %v", i, err)
		}
		if got := allow(hostA); got != http.StatusNoContent {
			t.Fatalf("churn %d re-approved allow = %d, want 204", i, got)
		}
	}

	// Malformed / IDN matrix: all fail closed (403), none poisons
	// the cache, and the punycode form of an unknown IDN is cached
	// as a deny like any other unknown ASCII name.
	malformed := []string{
		"", "not a domain", "https://" + hostA, hostA + "/path",
		hostA + ".",           // trailing dot
		"127.0.0.1",           // IP
		"münchen.example.com", // unicode IDN: punycode form required
		"*.example.com", "a..com",
	}
	for _, d := range malformed {
		if got := allow(d); got != http.StatusForbidden {
			t.Fatalf("malformed allow(%q) = %d, want 403", d, got)
		}
	}
	punycode := "xn--mnchen-3ya.example.com"
	if got := allow(punycode); got != http.StatusForbidden {
		t.Fatalf("punycode unknown allow = %d, want 403", got)
	}
	// Uppercase: normalization hits the same entry (204). (A ":443"
	// suffix on the allow QUERY would 403 here: Normalize rejects
	// ports, and only Caddy's {host} strips the port before the
	// check — see the audit §2.2 direct-vs-edge matrix. The
	// domain-store path below strips it itself via SplitHostPort.)
	if got := allow(strings.ToUpper(hostA)); got != http.StatusNoContent {
		t.Fatalf("uppercase allow = %d, want 204", got)
	}
	if code, slug := store(strings.ToUpper(hostA) + ":443"); code != http.StatusOK || slug != slugA {
		t.Fatalf("uppercase domain-store = %d %q, want 200 %q", code, slug, slugA)
	}

	// Primary-site route: 204 with no DB query (env-list
	// short-circuit, before the repo — the cache is never consulted).
	dbBefore := scrapeAllowSeries(t)["db_queries"]
	if got := allow("primary.edge01.test"); got != http.StatusNoContent {
		t.Fatalf("primary allow = %d, want 204", got)
	}
	if got := scrapeAllowSeries(t)["db_queries"] - dbBefore; got != 0 {
		t.Fatalf("primary allow ran %d DB queries, want 0", got)
	}

	// --- Second fixture: the seller-side write paths. ---
	// RequestStoreChange inserts a pending custom_domain row, which
	// becomes the latest approval (pending != approved) — the allow
	// decision flips to denied on the next request.
	if err := repo.RequestStoreChange(ctx, ownerB, slugB, "custom_domain", hostB); err != nil {
		t.Fatalf("RequestStoreChange(custom_domain): %v", err)
	}
	if got := allow(hostB); got != http.StatusForbidden {
		t.Fatalf("allow after RequestStoreChange = %d, want 403", got)
	}
	// Approving that new pending row restores the domain.
	var newApprovalID string
	if err := db.QueryRow(`SELECT id FROM pending_approvals WHERE target_id=$1 AND entity_type='custom_domain' ORDER BY created_at DESC, id DESC LIMIT 1`, slugB).Scan(&newApprovalID); err != nil {
		t.Fatalf("new pending row id: %v", err)
	}
	if err := adminRepo.ReviewPendingApproval(ctx, newApprovalID, "approved", ""); err != nil {
		t.Fatalf("ReviewPendingApproval(new row, approved): %v", err)
	}
	if got := allow(hostB); got != http.StatusNoContent {
		t.Fatalf("allow after re-approval = %d, want 204", got)
	}
	// DisableOwnedDomain revokes via the store_domains path: the
	// next allow check is 403 and the domain-store lookup 404s.
	if err := repo.DisableOwnedDomain(ctx, ownerB, hostB); err != nil {
		t.Fatalf("DisableOwnedDomain: %v", err)
	}
	if got := allow(hostB); got != http.StatusForbidden {
		t.Fatalf("allow after DisableOwnedDomain = %d, want 403", got)
	}
	if code, _ := store(hostB); code != http.StatusNotFound {
		t.Fatalf("domain-store after DisableOwnedDomain = %d, want 404", code)
	}
}
