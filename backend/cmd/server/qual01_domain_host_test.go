package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestQual01DomainStoreHostBinding(t *testing.T) {
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
	fixture := func(mobile string) (owner, slug, host string) {
		t.Helper()
		owner = uuid.NewString()
		slug = "host-" + owner[:8]
		host = slug + ".example.com"
		exec(`INSERT INTO users(id,mobile,full_name) VALUES($1,$2,'Host fixture')`, owner, mobile)
		t.Cleanup(func() { exec(`DELETE FROM users WHERE id=$1`, owner) })
		exec(`INSERT INTO stores(id,name,slug,owner_name,owner_user_id,referral_code) VALUES($1,$1,$1,'Host fixture',$2,$1)`, slug, owner)
		t.Cleanup(func() { exec(`DELETE FROM stores WHERE id=$1`, slug) })
		exec(`INSERT INTO store_domains(domain,store_id,verification_token,status,verified_at) VALUES($2,$1,'fixture','active',NOW())`, slug, host)
		t.Cleanup(func() { exec(`DELETE FROM pending_approvals WHERE target_id=$1`, slug) })
		return owner, slug, host
	}
	approve := func(owner, slug, host, state string) {
		t.Helper()
		exec(`INSERT INTO pending_approvals(id,entity_type,target_id,title,requested_data,requested_by,status)
			VALUES($1,'custom_domain',$2,'Host fixture',jsonb_build_object('value',$3::text),$4,$5)`, uuid.NewString(), slug, host, owner, state)
	}
	ownerA, slugA, hostA := fixture("09000040301")
	ownerB, slugB, hostB := fixture("09000040302")
	assertLookup := func(host, otherHost, otherSlug string, code int, slug string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/domain-store?domain="+otherHost+"&slug="+otherSlug, nil)
		r.Host = host
		r.Header.Set("X-Forwarded-Host", otherHost)
		w := httptest.NewRecorder()
		handleDomainStore(w, r, repo)
		if w.Code != code {
			t.Fatalf("lookup(%q): status %d, want %d: %s", host, w.Code, code, w.Body)
		}
		if code == http.StatusOK {
			var got map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got) != 1 || got["slug"] != slug {
				t.Fatalf("lookup(%q): want only slug %q, got %s (%v)", host, slug, w.Body, err)
			}
		}
	}

	approve(ownerA, slugA, hostA, "approved")
	approve(ownerA, slugA, hostB, "approved") // Another store's approval is insufficient.
	assertLookup(hostB, hostA, slugA, http.StatusNotFound, "")
	approve(ownerB, slugB, hostB, "approved")
	assertLookup(strings.ToUpper(hostA)+":443", hostB, slugB, http.StatusOK, slugA)
	assertLookup(hostB, hostA, slugA, http.StatusOK, slugB)
	assertLookup("unknown.example.com", hostB, slugB, http.StatusNotFound, "")
	approve(ownerB, slugB, hostB, "rejected")
	assertLookup(hostB, hostA, slugA, http.StatusNotFound, "")
	exec(`UPDATE store_domains SET status='disabled' WHERE domain=$1`, hostA)
	assertLookup(hostA, hostB, slugB, http.StatusNotFound, "")
}
