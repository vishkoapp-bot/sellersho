package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

type middlewareCapabilityReader struct {
	capabilities []string
}

func (r *middlewareCapabilityReader) ListActive(context.Context, string) ([]string, error) {
	return append([]string{}, r.capabilities...), nil
}

func TestRequireCapabilitiesRejectsRoleFallbackWhenMembershipsExist(t *testing.T) {
	service := auth.NewJWTService("capability-middleware-test-secret")
	reader := &middlewareCapabilityReader{capabilities: []string{"customer"}}
	service.SetCapabilityReader(reader)
	token, err := service.GenerateTokenWithCapabilities(
		"user-1", "09120000000", "Capability Test", "supplier", []string{"customer"}, time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		role string
		want int
	}{
		{name: "active capability", role: "customer", want: http.StatusOK},
		{name: "legacy role cannot widen populated claims", role: "supplier", want: http.StatusForbidden},
		{name: "ungranted capability", role: "admin", want: http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			res := httptest.NewRecorder()
			RequireCapabilities(service, tc.role)(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})(res, req)
			if res.Code != tc.want {
				t.Fatalf("got status %d, want %d", res.Code, tc.want)
			}
		})
	}
	reader.capabilities = []string{"supplier"}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	RequireCapabilities(service, "supplier")(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("refreshed capability was not accepted: got %d", res.Code)
	}
}

// Exercises the shared gate, not downstream finance tenant/object checks.
func TestQual01FinanceCapabilityGate(t *testing.T) {
	service := auth.NewJWTService("qual01-fixture-signing-key")
	reader := &middlewareCapabilityReader{}
	service.SetCapabilityReader(reader)
	for _, tc := range []struct {
		name   string
		active []string
		ttl    time.Duration
		want   int
	}{
		{"finance specialist retains access", []string{"finance_specialist"}, time.Hour, http.StatusNoContent},
		{"legacy finance capability retains access", []string{"finance"}, time.Hour, http.StatusNoContent},
		{"support cannot enter finance gate", []string{"support_specialist"}, time.Hour, http.StatusForbidden},
		{"all capabilities revoked", []string{}, time.Hour, http.StatusForbidden},
		{"expired token with active capability", []string{"finance_specialist"}, -time.Hour, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader.capabilities = tc.active
			token, err := service.GenerateTokenWithCapabilities("fixture-user", "", "", "finance_specialist", []string{"finance_specialist"}, tc.ttl)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			res := httptest.NewRecorder()
			entered := false
			RequireRoles(service, "finance_specialist")(func(w http.ResponseWriter, r *http.Request) {
				entered = true
				w.WriteHeader(http.StatusNoContent)
			})(res, req)
			if res.Code != tc.want || entered != (tc.want == http.StatusNoContent) {
				t.Fatalf("status=%d handler entered=%v, want status=%d", res.Code, entered, tc.want)
			}
		})
	}
}

func TestPrivateStoreReadRequiresCapabilityAndOwnership(t *testing.T) {
	_, stores := storeScopedRepos(t)
	service := auth.NewJWTService("private-store-read-test-secret")
	reader := &middlewareCapabilityReader{}
	service.SetCapabilityReader(reader)
	for _, tc := range []struct {
		name   string
		userID string
		active []string
		want   int
	}{
		{"owner with seller membership", "owner-shopx", []string{"seller"}, http.StatusOK},
		{"owner with customer membership", "owner-shopx", []string{"customer"}, http.StatusForbidden},
		{"seller of a different store", "another-owner", []string{"seller"}, http.StatusForbidden},
		{"direct administrator", "fixture-admin", []string{"admin"}, http.StatusOK},
		{"super administrator without commercial membership", "fixture-super-admin", []string{"super_admin"}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader.capabilities = tc.active
			token, err := service.GenerateToken(tc.userID, "", "", "seller", time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/private-store-read", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			res := httptest.NewRecorder()
			allowed := authorizeStoreRead(res, req, service, stores, "shopx")
			if res.Code != tc.want || allowed != (tc.want == http.StatusOK) {
				t.Fatalf("status=%d allowed=%v, want status=%d", res.Code, allowed, tc.want)
			}
		})
	}
}

func TestReturnRefundAcceptsBothFinanceCapabilities(t *testing.T) {
	orders := repository.NewOrderRepository(supplierFlowDB(t))
	service := auth.NewJWTService("return-refund-role-test-secret")
	for _, tc := range []struct {
		role string
		want int
	}{
		{"finance", http.StatusNotFound},
		{"finance_specialist", http.StatusNotFound},
		{"support_specialist", http.StatusForbidden},
	} {
		t.Run(tc.role, func(t *testing.T) {
			token, err := service.GenerateToken("fixture-user", "", "", tc.role, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/return-refund", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			res := httptest.NewRecorder()
			RequireRoles(service, "finance_specialist", "support_specialist")(func(w http.ResponseWriter, r *http.Request) {
				handleCustomerReturnAction(w, r, r.Context(), "67a11c82-5599-4a76-a2a6-796a1cefe15d", "refund", orders)
			})(res, req)
			if res.Code != tc.want {
				t.Fatalf("status=%d, want %d", res.Code, tc.want)
			}
		})
	}
}
