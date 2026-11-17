package payment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func capEnv(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}
func TestCapacityDoubleRefusesNonTestOrMissingMarker(t *testing.T) {
	for _, env := range []map[string]string{{"APP_ENV": "production", "CAPACITY_PAYMENT_MODE": "success", "SELLERSHO_CAPACITY_RUN": "0123456789abcdef01234567"}, {"APP_ENV": "test", "CAPACITY_PAYMENT_MODE": "success"}, {"APP_ENV": "test", "CAPACITY_PAYMENT_MODE": "success", "SELLERSHO_CAPACITY_RUN": "reuse"}} {
		if _, _, err := CapacityGatewayFromEnvironment(capEnv(env)); err == nil {
			t.Fatal("unsafe capacity gateway accepted")
		}
	}
}
func TestCapacityDoubleAbsentLeavesDefaultsToCaller(t *testing.T) {
	c, enabled, err := CapacityGatewayFromEnvironment(capEnv(map[string]string{}))
	if c != nil || enabled || err != nil {
		t.Fatal("ordinary default selection changed")
	}
}
func TestCapacityDoubleStableResultAndCancellation(t *testing.T) {
	env := map[string]string{"APP_ENV": "test", "SELLERSHO_CAPACITY_RUN": "0123456789abcdef01234567", "CAPACITY_PAYMENT_MODE": "success", "CAPACITY_PAYMENT_LATENCIES_MS": "1,2,3"}
	c, enabled, err := CapacityGatewayFromEnvironment(capEnv(env))
	if err != nil || !enabled {
		t.Fatalf("double missing: %v", err)
	}
	a, err := c.VerifyTransaction(context.Background(), "synthetic-token", 123)
	if err != nil || a == nil || a.VerifiedAmount != 123 {
		t.Fatal("wrong verification result")
	}
	b, err := c.VerifyTransaction(context.Background(), "synthetic-token", 123)
	if err != nil || b.RRN != a.RRN {
		t.Fatal("duplicate verification identity changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := c.VerifyTransaction(ctx, "synthetic-token", 123); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled provider call continued")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("cancellation was not bounded")
	}
}
func TestCapacityDoubleBoundedFaults(t *testing.T) {
	for mode, want := range map[string]error{"reject": ErrVerificationFailed, "wrong_amount": ErrAmountMismatch, "timeout": context.DeadlineExceeded} {
		c, _, err := CapacityGatewayFromEnvironment(capEnv(map[string]string{"APP_ENV": "test", "SELLERSHO_CAPACITY_RUN": "0123456789abcdef01234567", "CAPACITY_PAYMENT_MODE": mode, "CAPACITY_PAYMENT_LATENCIES_MS": "1"}))
		if err != nil {
			t.Fatal(err)
		}
		if result, err := c.VerifyTransaction(context.Background(), "synthetic", 123); result != nil || !errors.Is(err, want) {
			t.Fatalf("fault %s was hidden: %v", mode, err)
		}
	}
	for _, latency := range []string{"-1", "5001", "1,bad", ""} {
		_, _, err := CapacityGatewayFromEnvironment(capEnv(map[string]string{"APP_ENV": "test", "SELLERSHO_CAPACITY_RUN": "0123456789abcdef01234567", "CAPACITY_PAYMENT_MODE": "success", "CAPACITY_PAYMENT_LATENCIES_MS": latency}))
		if err == nil {
			t.Fatalf("unbounded/invalid distribution accepted: %q", latency)
		}
	}
}
