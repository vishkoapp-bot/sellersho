package auth

import (
	"context"
	"testing"
	"time"
)

type testCapabilityReader struct {
	capabilities []string
}

func (r testCapabilityReader) ListActive(context.Context, string) ([]string, error) {
	return append([]string{}, r.capabilities...), nil
}

func TestUserClaimsHasCapabilityUsesExplicitMemberships(t *testing.T) {
	claims := &UserClaims{Role: "supplier", Capabilities: []string{"customer", "seller", "supplier"}}
	for _, capability := range []string{"customer", "seller", "supplier"} {
		if !claims.HasCapability(capability) {
			t.Fatalf("capability %q was not found in %+v", capability, claims.Capabilities)
		}
	}
	if claims.HasCapability("admin") {
		t.Fatal("ungranted capability was accepted")
	}
}

func TestUserClaimsLegacyRoleFallbackHasNoCapabilityWidening(t *testing.T) {
	legacy := &UserClaims{Role: "supplier"}
	if !legacy.HasCapability("supplier") {
		t.Fatal("legacy role should remain compatible")
	}
	if legacy.HasCapability("admin") {
		t.Fatal("legacy role widened into admin capability")
	}
}

func TestSuperAdminCapabilityProvidesExplicitAdminCompatibility(t *testing.T) {
	claims := &UserClaims{Capabilities: []string{"super_admin"}}
	if !claims.HasCapability("super_admin") || !claims.HasCapability("admin") {
		t.Fatal("super_admin should satisfy the explicit admin compatibility check")
	}
	if claims.HasDirectCapability("admin") {
		t.Fatal("super_admin must not become a direct admin ownership capability")
	}
	if claims.HasCapability("finance_specialist") {
		t.Fatal("super_admin must not imply finance capability")
	}
}

func TestGenerateTokenWithCapabilitiesRoundTrips(t *testing.T) {
	service := NewJWTService("jwt-capability-test-secret")
	token, err := service.GenerateTokenWithCapabilities("user-1", "09120000000", "Test User", "seller", []string{"customer", "seller"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.ValidateToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Role != "seller" || !claims.HasCapability("customer") || !claims.HasCapability("seller") || claims.HasCapability("admin") {
		t.Fatalf("unexpected capability claims: %+v", claims)
	}
}

func TestRefreshCapabilitiesReplacesStaleClaims(t *testing.T) {
	service := NewJWTService("jwt-refresh-test-secret")
	service.SetCapabilityReader(testCapabilityReader{capabilities: []string{"customer"}})
	claims := &UserClaims{UserID: "user-1", Role: "supplier", Capabilities: []string{"supplier"}}
	if err := service.RefreshCapabilities(context.Background(), claims); err != nil {
		t.Fatal(err)
	}
	if claims.HasCapability("supplier") || !claims.HasCapability("customer") {
		t.Fatalf("stale capabilities survived refresh: %+v", claims.Capabilities)
	}
}
