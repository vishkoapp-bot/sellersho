package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/golang-jwt/jwt/v5"
)

type sessionRevocationFixture struct {
	entries map[string]time.Time
	err     error
}

func TestSessionLogoutSupportsPreviouslyIssuedTokens(t *testing.T) {
	const secret = "legacy-session-logout-test"
	service := auth.NewJWTService(secret)
	service.SetSessionRevocations(&sessionRevocationFixture{entries: make(map[string]time.Time)})
	claims := auth.UserClaims{
		UserID: "legacy-user",
		Role:   "customer",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "drup-platform",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := service.ValidateSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ValidateSession(ctx, token); !errors.Is(err, auth.ErrSessionRevoked) {
		t.Fatalf("previously issued credential should end on logout: %v", err)
	}
}

func (s *sessionRevocationFixture) IsSessionRevoked(_ context.Context, digest string) (bool, error) {
	_, revoked := s.entries[digest]
	return revoked, s.err
}

func (s *sessionRevocationFixture) RevokeSession(_ context.Context, digest string, expiresAt time.Time) error {
	if s.err != nil {
		return s.err
	}
	s.entries[digest] = expiresAt
	return nil
}

func TestSessionLogoutEndsOnlyTheCurrentSession(t *testing.T) {
	store := &sessionRevocationFixture{entries: make(map[string]time.Time)}
	service := auth.NewJWTService("session-logout-test")
	service.SetSessionRevocations(store)
	protected := RequireCapabilities(service, "seller")(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	optional := OptionalAuthMiddleware(service, func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(userClaimsKey) != nil {
			w.WriteHeader(http.StatusNoContent)
		}
	})
	logout := handleSessionLogout(service)
	request := func(handler http.HandlerFunc, method, token string) int {
		t.Helper()
		req := httptest.NewRequest(method, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res := httptest.NewRecorder()
		handler(res, req)
		return res.Code
	}
	issue := func() string {
		t.Helper()
		token, err := service.GenerateToken("customer-1", "test", "Test Buyer", "seller", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	current := issue()
	otherDevice := issue()
	if current == otherDevice {
		t.Fatal("independent sign-ins issued the same session")
	}
	if got := request(protected, http.MethodGet, current); got != http.StatusNoContent {
		t.Fatalf("active session: got %d", got)
	}
	if got := request(logout, http.MethodGet, current); got != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected logout method: got %d", got)
	}
	for range 2 {
		if got := request(logout, http.MethodPost, current); got != http.StatusOK {
			t.Fatalf("logout: got %d", got)
		}
	}
	if len(store.entries) != 1 {
		t.Fatalf("expected one idempotent revocation, got %d", len(store.entries))
	}
	for digest, expiry := range store.entries {
		if len(digest) != 64 || strings.Contains(digest, current) || time.Until(expiry) > time.Hour || time.Until(expiry) < 59*time.Minute {
			t.Fatal("expected a digest with the session's original expiry")
		}
	}
	if got := request(protected, http.MethodGet, current); got != http.StatusUnauthorized {
		t.Fatalf("ended session: got %d", got)
	}
	if got := request(optional, http.MethodGet, current); got != http.StatusOK {
		t.Fatalf("public route should treat the ended session as anonymous: got %d", got)
	}
	if got := request(protected, http.MethodGet, otherDevice); got != http.StatusNoContent {
		t.Fatalf("other device's session: got %d", got)
	}
	store.err = errors.New("session store unavailable")
	if got := request(protected, http.MethodGet, otherDevice); got != http.StatusServiceUnavailable {
		t.Fatalf("protected route during store outage: got %d", got)
	}
	if got := request(logout, http.MethodPost, otherDevice); got != http.StatusServiceUnavailable {
		t.Fatalf("logout during store outage: got %d", got)
	}
}
