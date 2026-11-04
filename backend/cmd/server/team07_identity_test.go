package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
)

// Team 07 (D57/D58) unit tests. These run without a database: the
// middleware test uses a fake status reader, and the grant/revoke
// tests stop at the super_admin guard / nil-database guard before any
// SQL runs.

type t07StatusReader struct {
	active       bool
	capabilities []string
}

func (r *t07StatusReader) ListActive(context.Context, string) ([]string, error) {
	return append([]string{}, r.capabilities...), nil
}

func (r *t07StatusReader) IsUserActive(context.Context, string) (bool, error) {
	return r.active, nil
}

func t07TokenForMiddlewareTest(t *testing.T, service *auth.JWTService) string {
	t.Helper()
	token, err := service.GenerateTokenWithCapabilities(
		"user-t07", "09120000000", "Team 07 Test", "customer",
		[]string{"customer"}, time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestT07AuthMiddlewareDeniesInactiveUser(t *testing.T) {
	service := auth.NewJWTService("team07-middleware-test-secret")
	service.SetCapabilityReader(&t07StatusReader{active: false, capabilities: []string{"customer"}})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+t07TokenForMiddlewareTest(t, service))
	res := httptest.NewRecorder()
	AuthMiddleware(service, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("inactive user: got status %d, want %d: %s", res.Code, http.StatusForbidden, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "Account is disabled") {
		t.Fatalf("inactive user: want existing disabled-account error, got %s", res.Body.String())
	}
}

func TestT07AuthMiddlewareAllowsActiveUser(t *testing.T) {
	service := auth.NewJWTService("team07-middleware-test-secret")
	service.SetCapabilityReader(&t07StatusReader{active: true, capabilities: []string{"customer"}})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+t07TokenForMiddlewareTest(t, service))
	res := httptest.NewRecorder()
	AuthMiddleware(service, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("active user: got status %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
}

func TestT07AdminCapabilityGrantRefusesSuperAdmin(t *testing.T) {
	// Grant and revoke share the super_admin guard; exercise both with
	// nil db/repo, which is only reached after the guard passes.
	cases := []struct {
		name string
		fn   func(http.ResponseWriter, *http.Request)
	}{
		{"grant", func(w http.ResponseWriter, r *http.Request) {
			handleT07AdminCapabilityGrant(w, r, nil, nil)
		}},
		{"revoke", func(w http.ResponseWriter, r *http.Request) {
			handleT07AdminCapabilityRevoke(w, r, nil, nil)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/capabilities/"+tc.name,
				strings.NewReader(`{"userId":"user-t07","capability":"super_admin"}`))
			res := httptest.NewRecorder()
			tc.fn(res, req)
			if res.Code != http.StatusForbidden {
				t.Fatalf("super_admin %s: got status %d, want %d: %s", tc.name, res.Code, http.StatusForbidden, res.Body.String())
			}
		})
	}
}

func TestT07AdminCapabilityGrantRejectsEmptyPayload(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/capabilities/grant",
		strings.NewReader(`{"userId":"","capability":""}`))
	res := httptest.NewRecorder()
	handleT07AdminCapabilityGrant(res, req, nil, nil)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("empty payload: got status %d, want %d", res.Code, http.StatusBadRequest)
	}
}

func TestT07AdminStaffIsGetOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/staff", nil)
	res := httptest.NewRecorder()
	handleT07AdminStaff(res, req, nil)
	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST staff: got status %d, want %d", res.Code, http.StatusMethodNotAllowed)
	}
}
