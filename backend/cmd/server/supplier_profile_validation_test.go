package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/accesspolicy"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestSupplierScopeRequiresDirectAdminOrMembership(t *testing.T) {
	repo := repository.NewSupplierRepository(testdb.NoConnections(t))
	claims := &auth.UserClaims{UserID: "nonmember", Capabilities: []string{accesspolicy.SuperAdminCapability}}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/supplier/profile", nil)
	r = r.WithContext(context.WithValue(r.Context(), userClaimsKey, claims))
	if _, ok := supplierAccess(r, repo, "profile.view"); ok {
		t.Fatal("a compatibility alias granted supplier access without membership")
	}
}

func TestSupplierProfileWriteRequiresManagement(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	suffix := uuid.NewString()[:8]
	owner, supplier, err := repo.RegisterSupplier(ctx, "Profile fixture", "Owner", "profile-"+suffix, "FixturePassword!", fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), "", "Tehran")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repo.CreateMember(ctx, supplier.ID, owner.ID, "Inventory member", "inventory-"+suffix, fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), "FixturePassword!", "operations")
	if err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTService("supplier-profile-test")
	h := NewSupplierHandlers(repo, jwt, false)
	request := func(method, userID string, body string) *httptest.ResponseRecorder {
		t.Helper()
		token, err := jwt.GenerateToken(userID, "", "Profile test", "supplier", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/api/v1/supplier/profile", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler := h.HandleGetProfile
		if method == http.MethodPut {
			handler = h.HandleUpdateSupplierProfile
		}
		RequireSupplierAccess(jwt, repo)(handler)(w, r)
		return w
	}
	if w := request(http.MethodGet, member.UserID, ""); w.Code != http.StatusOK {
		t.Fatalf("read by team member: %d %s", w.Code, w.Body)
	}
	if w := request(http.MethodPut, member.UserID, `{"description":"Team edit"}`); w.Code != http.StatusForbidden {
		t.Fatalf("write by team member: %d %s", w.Code, w.Body)
	}
	product := "profile-variants-" + suffix
	if _, err := db.ExecContext(ctx, `INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin) VALUES ($1,'Variants fixture',$1,$2,2000,1000,0)`, product, supplier.ID); err != nil {
		t.Fatal(err)
	}
	memberToken, err := jwt.GenerateToken(member.UserID, member.Mobile, member.FullName, "supplier", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		want   int
	}{
		{http.MethodGet, http.StatusNoContent},
		{http.MethodPost, http.StatusForbidden},
	} {
		r := httptest.NewRequest(tc.method, "/api/v1/supplier/products/"+product+"/variants", nil)
		r.Header.Set("Authorization", "Bearer "+memberToken)
		w := httptest.NewRecorder()
		RequireSupplierAccess(jwt, repo)(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(supplierIDKey) != supplier.ID {
				t.Error("request lost the owning supplier scope")
			}
			w.WriteHeader(http.StatusNoContent)
		})(w, r)
		if w.Code != tc.want {
			t.Errorf("%s own product variants: got %d, want %d", tc.method, w.Code, tc.want)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE suppliers SET warehouse_city_confirmed=true WHERE id=$1`, supplier.ID); err != nil {
		t.Fatal(err)
	}
	const newCity = "شیراز"
	longDescription := strings.Repeat("م", 2000)
	write := request(http.MethodPut, owner.ID, fmt.Sprintf(`{"description":%q,"warehouse_city":"  %s  "}`, longDescription, newCity))
	if write.Code != http.StatusOK {
		t.Fatalf("write with Persian description: %d %s", write.Code, write.Body)
	}
	var profile struct {
		Description   string `json:"description"`
		City          string `json:"warehouse_city"`
		CityConfirmed bool   `json:"warehouse_city_confirmed"`
	}
	if err := json.NewDecoder(write.Body).Decode(&profile); err != nil {
		t.Fatal(err)
	}
	if profile.Description != longDescription || profile.City != newCity || profile.CityConfirmed {
		t.Fatal("expected Persian description, normalized city and confirmation reset after city change")
	}
	if _, err := db.ExecContext(ctx, `UPDATE suppliers SET warehouse_city_confirmed=true WHERE id=$1`, supplier.ID); err != nil {
		t.Fatal(err)
	}
	if w := request(http.MethodPut, owner.ID, fmt.Sprintf(`{"description":"","logo_url":"","warehouse_city":%q}`, newCity)); w.Code != http.StatusOK {
		t.Fatalf("partial update and explicit clears: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{
		`null`, `{"description":null}`, `{"logo_url":null}`, `{"warehouse_city":"  "}`,
		fmt.Sprintf(`{"warehouse_city":%q}`, strings.Repeat("ش", 51)),
		fmt.Sprintf(`{"description":%q}`, strings.Repeat("م", 2001)),
	} {
		if w := request(http.MethodPut, owner.ID, body); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid profile input: %d %s", w.Code, w.Body)
		}
	}
	if err := db.QueryRowContext(ctx, `SELECT description, warehouse_city, warehouse_city_confirmed FROM suppliers WHERE id=$1`, supplier.ID).Scan(&profile.Description, &profile.City, &profile.CityConfirmed); err != nil {
		t.Fatal(err)
	}
	if profile.Description != "" || profile.City != newCity || !profile.CityConfirmed {
		t.Fatal("unchanged city should preserve confirmation and invalid inputs should have no effect")
	}
}

func TestSupplierSettlementRejectsAccountHolderOverSchemaLimit(t *testing.T) {
	h := NewSupplierHandlers(repository.NewSupplierRepository(testdb.NoConnections(t)), nil, false)
	const iban = "IR060170000000000000000000"
	body := fmt.Sprintf(`{"amount":1,"shaba":%q,"account_holder":%q}`, iban, strings.Repeat("م", 101))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/settlement/request", strings.NewReader(body))
	r.Header.Set("Idempotency-Key", "holder-validation-fixture")
	r = r.WithContext(context.WithValue(r.Context(), supplierIDKey, "supplier-fixture"))
	w := httptest.NewRecorder()
	h.HandleRequestSettlement(w, r)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "۱۰۰") {
		t.Fatalf("expected account-holder validation before DB call: %d %s", w.Code, w.Body)
	}
}
