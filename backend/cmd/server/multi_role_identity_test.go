package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// TestMultiRoleIdentityCurrentArchitecture verifies one normalized identity
// can hold several explicit capabilities without widening unrelated access.
func TestMultiRoleIdentityCurrentArchitecture(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()

	ctx := context.Background()
	var capabilityTable *string
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.user_capabilities')`).Scan(&capabilityTable); err != nil {
		t.Fatal(err)
	}
	if capabilityTable == nil {
		t.Skip("user_capabilities migration 000027 is not applied to the integration database")
	}
	userRepo := repository.NewUserRepository(db)
	supplierRepo := repository.NewSupplierRepository(db)
	storeRepo := repository.NewStoreRepository(db)
	capabilityRepo := repository.NewCapabilityRepository(db)
	jwtService := auth.NewJWTService("multi-role-identity-test-secret")

	userID := uuid.NewString()
	mobile := fmt.Sprintf("09%09d", time.Now().UnixNano()%1_000_000_000)
	supplierID := "sup-multi-" + uuid.NewString()[:8]
	storeID := "store-multi-" + uuid.NewString()
	storeSlug := "multi-role-" + uuid.NewString()[:8]

	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (id, mobile, full_name, role, is_active)
		VALUES ($1, $2, 'Multi-role identity test', 'supplier', TRUE)
	`, userID, mobile); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO suppliers (id, owner_user_id, name, logo_url, warehouse_city, description, badge)
		VALUES ($1, $2, 'Multi-role supplier test', '', 'Tehran', '', '')
	`, supplierID, userID); err != nil {
		t.Fatal(err)
	}
	if err := capabilityRepo.Grant(ctx, userID, "customer", "test", userID, nil); err != nil {
		t.Fatal(err)
	}
	if err := capabilityRepo.Grant(ctx, userID, "supplier", "test", supplierID, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM stores WHERE id = $1`, storeID)
		_, _ = db.ExecContext(ctx, `DELETE FROM suppliers WHERE id = $1`, supplierID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})

	t.Run("normalized_mobile_resolves_one_identity", func(t *testing.T) {
		international := "+98" + mobile[1:]
		resolved, err := userRepo.FindByMobile(ctx, international)
		if err != nil {
			t.Fatal(err)
		}
		if resolved == nil || resolved.ID != userID {
			t.Fatalf("normalized mobile resolved %+v, want user %s", resolved, userID)
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE mobile = $1`, mobile).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("mobile %s has %d user rows, want exactly one", mobile, count)
		}
	})

	t.Run("supplier_identity_and_store_owner_can_share_uuid", func(t *testing.T) {
		supplier, err := supplierRepo.GetSupplierByUserID(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		if supplier == nil || supplier.OwnerUserID == nil || *supplier.OwnerUserID != userID {
			t.Fatalf("supplier owner mismatch: %+v", supplier)
		}

		owner := userID
		if err := storeRepo.CreateStore(ctx, domain.Store{
			ID:                  storeID,
			Name:                "Multi-role seller store",
			Slug:                storeSlug,
			OwnerType:           "dropshipper",
			OwnerName:           "Multi-role identity test",
			OwnerUserID:         &owner,
			ProfitMarginPercent: 15,
			ActivePlanID:        "trial",
			ReferralCode:        "MR-" + uuid.NewString()[:8],
		}); err != nil {
			t.Fatal(err)
		}
		owned, err := storeRepo.GetStoresByOwner(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, store := range owned {
			if store.ID == storeID && store.OwnerUserID != nil && *store.OwnerUserID == userID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("store %s was not owned by identity %s", storeID, userID)
		}
		activeSeller, err := capabilityRepo.HasActive(ctx, userID, "seller")
		if err != nil {
			t.Fatal(err)
		}
		if activeSeller {
			t.Fatal("seller capability was active before seller onboarding completed")
		}
		if err := capabilityRepo.Grant(ctx, userID, "seller", "store_ownership", storeID, nil); err != nil {
			t.Fatal(err)
		}
	})

	capabilities, err := capabilityRepo.ListActive(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	claims := &auth.UserClaims{UserID: userID, Mobile: mobile, FullName: "Multi-role identity test", Role: "supplier", Capabilities: capabilities}
	token, err := jwtService.GenerateTokenWithCapabilities(claims.UserID, claims.Mobile, claims.FullName, claims.Role, claims.Capabilities, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("supplier_access_is_preserved_and_scoped", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/supplier/profile", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Context().Value(supplierIDKey); got != supplierID {
				t.Errorf("supplier scope = %v, want %s", got, supplierID)
			}
			w.WriteHeader(http.StatusOK)
		})(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("supplier owner access: got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("seller_onboarding_uses_explicit_seller_capability", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/stores/register", bytes.NewBufferString(`{"storeName":"blocked","slug":"blocked-multi-role"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		RequireRoles(jwtService, "seller", "admin")(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("multi-capability token on seller onboarding: got %d, want 200", w.Code)
		}
		var storeCount int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stores WHERE slug = 'blocked-multi-role'`).Scan(&storeCount); err != nil {
			t.Fatal(err)
		}
		if storeCount != 0 {
			t.Fatalf("test handler unexpectedly created %d stores", storeCount)
		}
	})

	t.Run("ungranted_privileged_capabilities_are_denied", func(t *testing.T) {
		cases := []struct {
			name string
			role string
		}{
			{name: "admin approvals", role: "admin"},
			{name: "warehouse", role: "warehouse_staff"},
			{name: "finance", role: "finance_specialist"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				RequireRoles(jwtService, tc.role)(func(w http.ResponseWriter, r *http.Request) {
					t.Fatal("identity crossed an ungranted capability boundary")
				})(w, req)
				if w.Code != http.StatusForbidden {
					t.Fatalf("supplier token for %s: got %d, want 403", tc.name, w.Code)
				}
			})
		}
	})

	t.Run("same_identity_can_use_customer_orders", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		RequireRoles(jwtService, "customer", "seller", "admin")(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("customer capability on multi-role identity: got %d, want 200", w.Code)
		}
	})

	t.Run("jwt_contains_active_capability_memberships", func(t *testing.T) {
		validated, err := jwtService.ValidateToken(token)
		if err != nil {
			t.Fatal(err)
		}
		if validated.UserID != userID || validated.Mobile != mobile || validated.Role != "supplier" || len(validated.Capabilities) != 3 {
			t.Fatalf("unexpected claims: %+v", validated)
		}
	})
}
