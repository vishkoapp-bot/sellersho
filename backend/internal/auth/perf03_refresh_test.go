package auth

import (
	"context"
	"slices"
	"testing"
)

// PERF-03 G1: RefreshAuthState must make exactly ONE state read when
// the reader implements LoadActiveState, and must fall back to the
// legacy two-call path for readers that do not.

type perf03CountingStateReader struct {
	found, active bool
	capabilities  []string
	loadCalls     int
	listCalls     int
	activeCalls   int
}

func (r *perf03CountingStateReader) LoadActiveState(context.Context, string) (bool, bool, []string, error) {
	r.loadCalls++
	return r.found, r.active, append([]string{}, r.capabilities...), nil
}

func (r *perf03CountingStateReader) ListActive(context.Context, string) ([]string, error) {
	r.listCalls++
	return append([]string{}, r.capabilities...), nil
}

func (r *perf03CountingStateReader) IsUserActive(context.Context, string) (bool, error) {
	r.activeCalls++
	return r.active, nil
}

func TestPERF03RefreshAuthStateSingleRead(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		found      bool
		active     bool
		caps       []string
		wantBlock  bool
	}{
		{"active user with capabilities", true, true, []string{"customer", "seller"}, false},
		{"blocked user keeps refreshed capabilities", true, false, []string{"customer"}, true},
		{"unknown user is blocked with no capabilities", false, false, nil, true},
		{"active user with no capabilities", true, true, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &perf03CountingStateReader{found: tc.found, active: tc.active, capabilities: tc.caps}
			svc := NewJWTService("perf03-refresh-test-secret")
			svc.SetCapabilityReader(reader)
			claims := &UserClaims{UserID: "user-perf03", Role: "customer", Capabilities: []string{"stale"}}
			blocked, err := svc.RefreshAuthState(ctx, claims)
			if err != nil {
				t.Fatal(err)
			}
			if blocked != tc.wantBlock {
				t.Fatalf("blocked=%v, want %v", blocked, tc.wantBlock)
			}
			if reader.loadCalls != 1 || reader.listCalls != 0 || reader.activeCalls != 0 {
				t.Fatalf("calls: LoadActiveState=%d ListActive=%d IsUserActive=%d, want 1/0/0",
					reader.loadCalls, reader.listCalls, reader.activeCalls)
			}
			if !slices.Equal(claims.Capabilities, tc.caps) && !(len(claims.Capabilities) == 0 && len(tc.caps) == 0) {
				t.Fatalf("capabilities=%v, want %v", claims.Capabilities, tc.caps)
			}
		})
	}
}

func TestPERF03RefreshAuthStateFallsBackForLegacyReader(t *testing.T) {
	ctx := context.Background()
	// Legacy reader: ListActive + IsUserActive only (no LoadActiveState).
	svc := NewJWTService("perf03-refresh-test-secret")
	svc.SetCapabilityReader(t07BlockedStatusReader{active: false})
	claims := &UserClaims{UserID: "user-perf03", Role: "customer"}
	blocked, err := svc.RefreshAuthState(ctx, claims)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Fatal("inactive legacy-reader user must be blocked")
	}
	if !claims.HasCapability("customer") {
		t.Fatalf("legacy fallback did not refresh capabilities: %+v", claims.Capabilities)
	}
}

func TestPERF03RefreshAuthStateNoReaderMeansNotBlocked(t *testing.T) {
	ctx := context.Background()
	svc := NewJWTService("perf03-refresh-test-secret")
	claims := &UserClaims{UserID: "user-perf03", Capabilities: []string{"customer"}}
	blocked, err := svc.RefreshAuthState(ctx, claims)
	if err != nil || blocked {
		t.Fatalf("nil reader: blocked=%v err=%v, want not blocked", blocked, err)
	}
	blocked, err = svc.RefreshAuthState(ctx, &UserClaims{UserID: ""})
	if err != nil || blocked {
		t.Fatalf("empty user: blocked=%v err=%v, want not blocked", blocked, err)
	}
}
