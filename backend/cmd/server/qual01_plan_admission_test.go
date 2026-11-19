package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestQual01PlanAdmissionPreservesStoresAndDomains(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewStoreRepository(db)
	for i, tc := range []struct {
		name, plan, expiry string
		inactive, entitled bool
	}{
		{"active", "vip", "NOW()+INTERVAL '1 day'", false, true},
		{"expired", "vip", "NOW()-INTERVAL '1 day'", false, false},
		{"inactive", "vip", "NOW()+INTERVAL '1 day'", true, false},
		{"legacy-undated", "vip", "NULL", false, true},
		{"missing-plan", "vip", "NULL", false, false},
		{"baseline-trial", "trial", "NULL", false, false},
		{"first-store", "", "NULL", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := db.Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			owner := uuid.NewString()
			slug := "plan-" + owner[:8]
			exec(`INSERT INTO users(id,mobile,full_name) VALUES($1,$2,'Plan fixture')`, owner, fmt.Sprintf("090000402%02d", i))
			t.Cleanup(func() { exec(`DELETE FROM users WHERE id=$1`, owner) })
			if tc.inactive || tc.name == "missing-plan" {
				// Only mutate the seeded catalog in the guarded disposable database.
				t.Cleanup(func() {
					exec(`UPDATE subscription_plans SET id='vip',is_active=TRUE WHERE id IN ('vip','qual01-hidden-vip')`)
				})
				if tc.inactive {
					exec(`UPDATE subscription_plans SET is_active=FALSE WHERE id='vip'`)
				} else {
					exec(`UPDATE subscription_plans SET id='qual01-hidden-vip' WHERE id='vip'`)
				}
			}
			oldDomain := slug + ".example.com"
			disabled := "disabled-" + oldDomain
			t.Cleanup(func() {
				exec(`DELETE FROM pending_approvals WHERE target_id=$1`, slug)
				exec(`DELETE FROM stores WHERE owner_user_id=$1`, owner)
			})
			if tc.plan != "" {
				exec(`INSERT INTO stores(id,name,slug,owner_name,owner_user_id,active_plan_id,referral_code,plan_expires_at) VALUES($1,$1,$1,'Plan fixture',$2,$3,$1,`+tc.expiry+`)`, slug, owner, tc.plan)
				exec(`INSERT INTO store_domains(domain,store_id,verification_token,status,verified_at) VALUES($2,$1,'fixture','active',NOW()),($3,$1,'fixture','disabled',NULL)`, slug, oldDomain, disabled)
				exec(`INSERT INTO pending_approvals(id,entity_type,target_id,title,requested_data,requested_by,status) VALUES($1,'custom_domain',$1,'Plan fixture',jsonb_build_object('value',$2::text),$3,'approved')`, slug, oldDomain, owner)
			}
			request := func(handler func(http.ResponseWriter, *http.Request), method, path, body string) *httptest.ResponseRecorder {
				t.Helper()
				r := httptest.NewRequest(method, path, strings.NewReader(body))
				r = r.WithContext(context.WithValue(r.Context(), userClaimsKey, &auth.UserClaims{UserID: owner, FullName: "Plan fixture"}))
				w := httptest.NewRecorder()
				handler(w, r)
				return w
			}
			newSlug := slug + "-new"
			w := request(func(w http.ResponseWriter, r *http.Request) { handleRegisterStore(w, r, r.Context(), repo) }, http.MethodPost, "/api/v1/stores/register", fmt.Sprintf(`{"storeName":"New store","slug":%q}`, newSlug))
			want := http.StatusConflict
			if tc.entitled || tc.plan == "" {
				want = http.StatusCreated
			}
			if w.Code != want {
				t.Fatalf("store registration = %d, want %d: %s", w.Code, want, w.Body)
			}
			var plan string
			var expiry sql.NullTime
			err := db.QueryRow(`SELECT active_plan_id,plan_expires_at FROM stores WHERE slug=$1`, newSlug).Scan(&plan, &expiry)
			if want == http.StatusCreated {
				expected := tc.plan
				if expected == "" {
					expected = "trial"
				}
				if err != nil || plan != expected || expiry.Valid != (tc.name == "active") {
					t.Fatalf("stored assignment = %q, %v, %v", plan, expiry, err)
				}
				if expiry.Valid {
					var original sql.NullTime
					if err := db.QueryRow(`SELECT plan_expires_at FROM stores WHERE id=$1`, slug).Scan(&original); err != nil || !original.Time.Equal(expiry.Time) {
						t.Fatalf("did not inherit exact expiry: %v, %v", original, err)
					}
					exec(`UPDATE stores SET plan_expires_at=NOW()-INTERVAL '1 day' WHERE owner_user_id=$1`, owner)
					w = request(func(w http.ResponseWriter, r *http.Request) { handleRegisterStore(w, r, r.Context(), repo) }, http.MethodPost, "/api/v1/stores/register", fmt.Sprintf(`{"storeName":"Third store","slug":%q}`, slug+"-third"))
					if w.Code != http.StatusConflict {
						t.Fatalf("inherited VIP outlived expiry: %d %s", w.Code, w.Body)
					}
					exec(`UPDATE stores SET plan_expires_at=$2 WHERE owner_user_id=$1`, owner, original)
				}
			} else if err != sql.ErrNoRows {
				t.Fatalf("denied registration left a store: %v", err)
			}
			if tc.plan == "" {
				return
			}
			for _, newDomain := range []string{"new-" + oldDomain, disabled} {
				w = request(func(w http.ResponseWriter, r *http.Request) { handleOwnedDomains(w, r, repo) }, http.MethodPost, "/api/v1/seller/domains", fmt.Sprintf(`{"slug":%q,"domain":%q}`, slug, newDomain))
				want = http.StatusBadRequest
				if tc.entitled {
					want = http.StatusCreated
				}
				if w.Code != want {
					t.Fatalf("domain admission = %d, want %d: %s", w.Code, want, w.Body)
				}
				var status string
				err := db.QueryRow(`SELECT status FROM store_domains WHERE domain=$1`, newDomain).Scan(&status)
				if tc.entitled && (err != nil || status != "pending_dns") || !tc.entitled && newDomain == disabled && (err != nil || status != "disabled") || !tc.entitled && newDomain != disabled && err != sql.ErrNoRows {
					t.Fatalf("unexpected domain state: %s %v", status, err)
				}
			}
			w = request(func(w http.ResponseWriter, r *http.Request) { handleDomainStore(w, r, repo) }, http.MethodGet, "http://"+oldDomain+"/", "")
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), slug) {
				t.Fatalf("existing domain stopped serving: %d %s", w.Code, w.Body)
			}
			store, err := repo.GetStoreBySlug(context.Background(), slug)
			if err != nil || store == nil {
				t.Fatalf("existing store no longer loads: %v %v", store, err)
			}
			var status string
			if err := db.QueryRow(`SELECT status FROM stores WHERE id=$1`, slug).Scan(&status); err != nil || status != "active" {
				t.Fatalf("existing store status = %q, %v", status, err)
			}
		})
	}
}
