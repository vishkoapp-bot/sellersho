package auth

import "testing"

func TestOTPServiceDoesNotExposeCodesOutsideTestEnvironment(t *testing.T) {
	for _, env := range []string{"production", "staging", "live", "", "unknown"} {
		if NewOTPService(nil, env).IsDemoMode() {
			t.Fatalf("%q must not expose test codes", env)
		}
	}
	for _, env := range []string{"development", "dev", "local", "test"} {
		if !NewOTPService(nil, env).IsDemoMode() {
			t.Fatalf("%q should support the test generator", env)
		}
	}
}
