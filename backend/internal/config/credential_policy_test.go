package config

import (
	"strings"
	"testing"
)

func TestProductionRejectsDevelopmentSigningAndWebhookMaterial(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	for _, key := range []string{"JWT_SECRET", "TRACKING_WEBHOOK_SECRET", "DATABASE_URL", "REDIS_URL", "PAYMENT_GATEWAY_MODE"} {
		t.Setenv(key, "")
	}
	dev, e := LoadConfig()
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ name, key, value string }{{"development signing key", "JWT_SECRET", dev.JWTSecret}, {"development webhook key", "TRACKING_WEBHOOK_SECRET", dev.TrackingWebhookSecret}, {"short webhook key", "TRACKING_WEBHOOK_SECRET", "short"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", "production")
			t.Setenv("JWT_SECRET", strings.Repeat("s", 40))
			t.Setenv("TRACKING_WEBHOOK_SECRET", strings.Repeat("w", 40))
			t.Setenv("DATABASE_URL", "postgres://fixture:fixture-secret@db:5432/fixture")
			t.Setenv("REDIS_URL", "redis://:fixture-secret@redis:6379/0")
			t.Setenv("PAYMENT_GATEWAY_MODE", "real")
			t.Setenv("IPPANEL_ENABLED", "true")
			t.Setenv("IPPANEL_API_KEY", "synthetic-adapter-key")
			t.Setenv("IPPANEL_SENDER", "synthetic")
			t.Setenv("IPPANEL_PATTERN_CODE", "synthetic")
			t.Setenv(tc.key, tc.value)
			if _, e := LoadConfig(); e == nil {
				t.Fatal("production accepted unsafe credential material")
			}
		})
	}
}
