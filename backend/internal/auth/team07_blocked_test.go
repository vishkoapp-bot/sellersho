package auth

import (
	"context"
	"testing"
)

// Team 07 (D57): JWTService.IsUserBlocked delegates to the optional
// UserActiveReader. Readers without IsUserActive (e.g. the legacy
// testCapabilityReader) and a nil reader must not block, preserving
// the previous signature-only behaviour for those wirings.

type t07BlockedStatusReader struct {
	active bool
}

func (r t07BlockedStatusReader) ListActive(context.Context, string) ([]string, error) {
	return []string{"customer"}, nil
}

func (r t07BlockedStatusReader) IsUserActive(context.Context, string) (bool, error) {
	return r.active, nil
}

func TestT07IsUserBlockedUsesActiveStatus(t *testing.T) {
	ctx := context.Background()
	blockedSvc := NewJWTService("team07-blocked-test-secret")
	blockedSvc.SetCapabilityReader(t07BlockedStatusReader{active: false})
	blocked, err := blockedSvc.IsUserBlocked(ctx, "user-t07")
	if err != nil || !blocked {
		t.Fatalf("inactive user: blocked=%v err=%v, want blocked=true", blocked, err)
	}
	activeSvc := NewJWTService("team07-blocked-test-secret")
	activeSvc.SetCapabilityReader(t07BlockedStatusReader{active: true})
	blocked, err = activeSvc.IsUserBlocked(ctx, "user-t07")
	if err != nil || blocked {
		t.Fatalf("active user: blocked=%v err=%v, want blocked=false", blocked, err)
	}
}

func TestT07IsUserBlockedSkipsReadersWithoutStatus(t *testing.T) {
	ctx := context.Background()
	legacySvc := NewJWTService("team07-blocked-test-secret")
	legacySvc.SetCapabilityReader(testCapabilityReader{capabilities: []string{"customer"}})
	blocked, err := legacySvc.IsUserBlocked(ctx, "user-t07")
	if err != nil || blocked {
		t.Fatalf("legacy reader: blocked=%v err=%v, want blocked=false", blocked, err)
	}
	noReaderSvc := NewJWTService("team07-blocked-test-secret")
	blocked, err = noReaderSvc.IsUserBlocked(ctx, "user-t07")
	if err != nil || blocked {
		t.Fatalf("nil reader: blocked=%v err=%v, want blocked=false", blocked, err)
	}
	blocked, err = noReaderSvc.IsUserBlocked(ctx, "")
	if err != nil || blocked {
		t.Fatalf("empty user: blocked=%v err=%v, want blocked=false", blocked, err)
	}
}
