package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestSupplierTeamPermissions(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	migration, err := os.ReadFile("../../migrations/000014_supplier_team_roles.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewSupplierRepository(db)
	suffix := uuid.New().String()[:8]
	owner, supplier, err := repo.RegisterSupplier(context.Background(), "Team Test", "Owner", "team-"+suffix, "TestTeamPassword!", "09"+suffix, "", "Tehran")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM users WHERE id IN (SELECT user_id FROM supplier_members WHERE supplier_id = $1)`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM suppliers WHERE id = $1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, owner.ID)
	})
	jwtService := auth.NewJWTService("team-test-secret")
	for _, role := range []string{"finance", "products", "orders", "operations", "marketing", "manager"} {
		t.Run(role, func(t *testing.T) {
			member, err := repo.CreateMember(context.Background(), supplier.ID, owner.ID, role, suffix+role, "t"+suffix+role[:3], "TestTeamPassword!", role)
			if err != nil {
				t.Fatal(err)
			}
			token, err := jwtService.GenerateToken(member.UserID, member.Mobile, role, "supplier", time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			request := func(path string) int {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				RequireSupplierAccess(jwtService, repo)(func(w http.ResponseWriter, r *http.Request) {
					if got := r.Context().Value(supplierIDKey); got != supplier.ID {
						t.Errorf("wrong supplier scope: %v", got)
					}
					w.WriteHeader(http.StatusOK)
				})(w, req)
				return w.Code
			}
			for _, path := range []string{"/api/v1/supplier/products", "/api/v1/supplier/settlement/history", "/api/v1/supplier/team", "/api/v1/supplier/orders", "/api/v1/supplier/inbounds", "/api/v1/supplier/campaigns/create"} {
				want := http.StatusForbidden
				for _, permission := range member.Permissions {
					if permission == "supplier.manage" || permission == supplierPermissionForPath(path) {
						want = http.StatusOK
					}
				}
				if path == "/api/v1/supplier/products" && (role == "operations" || role == "marketing") {
					want = http.StatusOK
				}
				if got := request(path); got != want {
					t.Errorf("%s: want %d, got %d", path, want, got)
				}
			}
			if got := request("/api/v1/supplier/profile"); got != http.StatusOK {
				t.Errorf("profile access: %d", got)
			}
			if got := request("/api/v1/supplier/products/foreign-product/stock"); got != http.StatusForbidden {
				t.Errorf("foreign resource access: %d", got)
			}
			if err := repo.SetMemberActive(context.Background(), supplier.ID, member.ID, false); err != nil {
				t.Fatal(err)
			}
			if got := request("/api/v1/supplier/profile"); got != http.StatusForbidden {
				t.Errorf("revoked token access: %d", got)
			}
		})
	}
}
