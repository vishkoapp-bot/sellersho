package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
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
