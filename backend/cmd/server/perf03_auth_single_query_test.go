package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
)

// PERF-03 G1: AuthMiddleware must refresh capabilities AND the blocked
// check in a single state read, and a blocked or unknown user must
// still be denied immediately (403) — the immediate-blocked-behavior
// constraint, tested explicitly at the middleware layer.

type perf03MiddlewareStateReader struct {
	found, active bool
	capabilities  []string
	loadCalls     int
	listCalls     int
	activeCalls   int
}

func (r *perf03MiddlewareStateReader) LoadActiveState(context.Context, string) (bool, bool, []string, error) {
	r.loadCalls++
	return r.found, r.active, append([]string{}, r.capabilities...), nil
}

func (r *perf03MiddlewareStateReader) ListActive(context.Context, string) ([]string, error) {
	r.listCalls++
	return append([]string{}, r.capabilities...), nil
}

func (r *perf03MiddlewareStateReader) IsUserActive(context.Context, string) (bool, error) {
	r.activeCalls++
	return r.active, nil
}

func perf03MiddlewareService(t *testing.T, reader *perf03MiddlewareStateReader) (*auth.JWTService, string) {
	t.Helper()
	svc := auth.NewJWTService("perf03-middleware-test-secret")
	svc.SetCapabilityReader(reader)
	token, err := svc.GenerateTokenWithCapabilities("user-perf03", "09120000000", "Perf03 User", "customer", []string{"stale"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return svc, token
}

func TestPERF03AuthMiddlewareSingleStateRead(t *testing.T) {
	reader := &perf03MiddlewareStateReader{found: true, active: true, capabilities: []string{"customer"}}
	svc, token := perf03MiddlewareService(t, reader)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	entered := false
	AuthMiddleware(svc, func(w http.ResponseWriter, r *http.Request) {
		entered = true
		w.WriteHeader(http.StatusOK)
	})(res, req)
	if res.Code != http.StatusOK || !entered {
		t.Fatalf("active user: status=%d entered=%v, want 200/true", res.Code, entered)
	}
	if reader.loadCalls != 1 || reader.listCalls != 0 || reader.activeCalls != 0 {
		t.Fatalf("state reads: LoadActiveState=%d ListActive=%d IsUserActive=%d, want 1/0/0",
			reader.loadCalls, reader.listCalls, reader.activeCalls)
	}
}

func TestPERF03AuthMiddlewareBlocksImmediately(t *testing.T) {
	for _, tc := range []struct {
		name   string
		found  bool
		active bool
	}{
		{"blocked user is denied at once", true, false},
		{"unknown user is denied at once", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &perf03MiddlewareStateReader{found: tc.found, active: tc.active, capabilities: []string{"customer"}}
			svc, token := perf03MiddlewareService(t, reader)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			res := httptest.NewRecorder()
			entered := false
			AuthMiddleware(svc, func(w http.ResponseWriter, r *http.Request) {
				entered = true
				w.WriteHeader(http.StatusOK)
			})(res, req)
			if res.Code != http.StatusForbidden || entered {
				t.Fatalf("status=%d entered=%v, want 403/false", res.Code, entered)
			}
			if reader.loadCalls != 1 {
				t.Fatalf("LoadActiveState calls=%d, want 1", reader.loadCalls)
			}
		})
	}
}

func TestPERF03OptionalAuthMiddlewareSingleStateRead(t *testing.T) {
	reader := &perf03MiddlewareStateReader{found: true, active: false, capabilities: []string{"customer"}}
	svc, token := perf03MiddlewareService(t, reader)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	entered := false
	OptionalAuthMiddleware(svc, func(w http.ResponseWriter, r *http.Request) {
		entered = true
		if r.Context().Value(userClaimsKey) != nil {
			t.Error("blocked user must degrade to anonymous, but claims were set")
		}
		w.WriteHeader(http.StatusOK)
	})(res, req)
	if res.Code != http.StatusOK || !entered {
		t.Fatalf("blocked optional-auth user: status=%d entered=%v, want 200/true (anonymous)", res.Code, entered)
	}
	if reader.loadCalls != 1 || reader.listCalls != 0 || reader.activeCalls != 0 {
		t.Fatalf("state reads: LoadActiveState=%d ListActive=%d IsUserActive=%d, want 1/0/0",
			reader.loadCalls, reader.listCalls, reader.activeCalls)
	}
}
