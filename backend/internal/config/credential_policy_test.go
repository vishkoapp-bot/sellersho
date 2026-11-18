package config

import (
	"testing"
)

// SEC-01: production must reject the retired development defaults.
// The retired values below are TEST FIXTURES ONLY — they are already
// public in Git history, treated as exposed, and must never appear in
// non-test code. (Previously this test derived them from the dev
// fallback; dev fallbacks are now ephemeral random values, so the
// fixtures are stated explicitly.)
const (
	retiredDevJWTForPolicyTest     = "drup_jwt_secret_key_2026_dev_secure"
	retiredDevWebhookForPolicyTest = "drup_tracking_webhook_secret_dev_2026"
)

func TestProductionRejectsDevelopmentSigningAndWebhookMaterial(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"retired development signing key", "JWT_SECRET", retiredDevJWTForPolicyTest},
		{"retired development webhook key", "TRACKING_WEBHOOK_SECRET", retiredDevWebhookForPolicyTest},
		{"short webhook key", "TRACKING_WEBHOOK_SECRET", "short"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", "production")
			t.Setenv("JWT_SECRET", "synthetic-test-jwt-secret-0123456789abcdef")
			t.Setenv("TRACKING_WEBHOOK_SECRET", "synthetic-test-webhook-secret-0123456789abcdef")
			t.Setenv("DATABASE_URL", "postgres://fixture:fixture-secret@db:5432/fixture")
			t.Setenv("REDIS_URL", "redis://:fixture-secret@redis:6379/0")
			t.Setenv("PAYMENT_GATEWAY_MODE", "real")
			t.Setenv("IPPANEL_ENABLED", "true")
			t.Setenv("IPPANEL_API_KEY", "synthetic-adapter-key")
			t.Setenv("IPPANEL_SENDER", "synthetic")
			t.Setenv("IPPANEL_PATTERN_CODE", "synthetic")
			t.Setenv("PSP_TERMINAL_ID", "synthetic-test-psp-terminal-0123456789")
			t.Setenv("PSP_API_KEY", "synthetic-test-psp-api-key-0123456789")
			t.Setenv("TAPIN_TOKEN_KEY", "synthetic-test-tapin-token-key-0123456789")
			t.Setenv("ALLOWED_ORIGINS", "https://shop.test.invalid")
			t.Setenv(tc.key, tc.value)
			if _, e := LoadConfig(); e == nil {
				t.Fatal("production accepted unsafe credential material")
			}
		})
	}
}
