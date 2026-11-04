package config

import (
	"testing"
)

func TestLoadConfig_DevEnvironment(t *testing.T) {
	// Dev environment allows safe defaults
	t.Setenv("APP_ENV", "development")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("TRACKING_WEBHOOK_SECRET", "")
	t.Setenv("PAYMENT_GATEWAY_MODE", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("expected nil error in dev, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config in dev")
	}
	if cfg.JWTSecret == "" {
		t.Error("expected non-empty dev fallback for JWT_SECRET")
	}
	if cfg.TrackingWebhookSecret == "" {
		t.Error("expected non-empty dev fallback for TrackingWebhookSecret")
	}
	if cfg.PaymentGatewayMode != "mock" {
		t.Errorf("expected PaymentGatewayMode 'mock' in dev, got %q", cfg.PaymentGatewayMode)
	}
}

func TestLoadConfig_FeatureFlagsDefaultDisabled(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("ENABLE_NETWORK_VOLUME_INCENTIVE", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("expected nil error in dev, got %v", err)
	}
	if cfg.NetworkVolumeIncentiveEnabled {
		t.Error("expected network volume incentive to default to disabled")
	}
}

func TestLoadConfig_FeatureFlagsCanBeEnabled(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("ENABLE_NETWORK_VOLUME_INCENTIVE", "1")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("expected nil error in dev, got %v", err)
	}
	if !cfg.NetworkVolumeIncentiveEnabled {
		t.Fatal("expected network volume incentive to be enabled")
	}
}

func TestLoadConfig_Production_FailClosed(t *testing.T) {
	cases := []struct {
		name          string
		jwtSecret     string
		dbURL         string
		redisURL      string
		webhookSecret string
		gatewayMode   string
		expectErr     bool
	}{
		{
			name:          "missing JWT_SECRET",
			jwtSecret:     "",
			dbURL:         "postgres://prod_user:strong_prod_pass@prod-db:5432/drup_prod",
			redisURL:      "redis://:strong_redis_pass@prod-redis:6379/0",
			webhookSecret: "super_secure_tracking_webhook_secret_key_prod",
			gatewayMode:   "real",
			expectErr:     true,
		},
		{
			name:          "short JWT_SECRET (<32 chars)",
			jwtSecret:     "short_secret_key_123",
			dbURL:         "postgres://prod_user:strong_prod_pass@prod-db:5432/drup_prod",
			redisURL:      "redis://:strong_redis_pass@prod-redis:6379/0",
			webhookSecret: "super_secure_tracking_webhook_secret_key_prod",
			gatewayMode:   "real",
			expectErr:     true,
		},
		{
			name:          "dev DB password in production",
			jwtSecret:     "very_long_production_jwt_secret_with_more_than_32_characters!",
			dbURL:         "postgres://prod_user:drup_secure_password_2026@prod-db:5432/drup_prod",
			redisURL:      "redis://:strong_redis_pass@prod-redis:6379/0",
			webhookSecret: "super_secure_tracking_webhook_secret_key_prod",
			gatewayMode:   "real",
			expectErr:     true,
		},
		{
			name:          "dev Redis password in production",
			jwtSecret:     "very_long_production_jwt_secret_with_more_than_32_characters!",
			dbURL:         "postgres://prod_user:strong_prod_pass@prod-db:5432/drup_prod",
			redisURL:      "redis://:drup_redis_pass_2026@prod-redis:6379/0",
			webhookSecret: "super_secure_tracking_webhook_secret_key_prod",
			gatewayMode:   "real",
			expectErr:     true,
		},
		{
			name:          "missing TRACKING_WEBHOOK_SECRET in production",
			jwtSecret:     "very_long_production_jwt_secret_with_more_than_32_characters!",
			dbURL:         "postgres://prod_user:strong_prod_pass@prod-db:5432/drup_prod",
			redisURL:      "redis://:strong_redis_pass@prod-redis:6379/0",
			webhookSecret: "",
			gatewayMode:   "real",
			expectErr:     true,
		},
		{
			name:          "mock PAYMENT_GATEWAY_MODE in production (fail-closed)",
			jwtSecret:     "very_long_production_jwt_secret_with_more_than_32_characters!",
			dbURL:         "postgres://prod_user:strong_prod_pass@prod-db:5432/drup_prod",
			redisURL:      "redis://:strong_redis_pass@prod-redis:6379/0",
			webhookSecret: "super_secure_tracking_webhook_secret_key_prod",
			gatewayMode:   "mock",
			expectErr:     true,
		},
		{
			name:          "empty PAYMENT_GATEWAY_MODE in production (fail-closed)",
			jwtSecret:     "very_long_production_jwt_secret_with_more_than_32_characters!",
			dbURL:         "postgres://prod_user:strong_prod_pass@prod-db:5432/drup_prod",
			redisURL:      "redis://:strong_redis_pass@prod-redis:6379/0",
			webhookSecret: "super_secure_tracking_webhook_secret_key_prod",
			gatewayMode:   "",
			expectErr:     true,
		},
		{
			name:          "valid production configuration with PAYMENT_GATEWAY_MODE=real",
			jwtSecret:     "very_long_production_jwt_secret_with_more_than_32_characters!",
			dbURL:         "postgres://prod_user:strong_prod_pass@prod-db:5432/drup_prod",
			redisURL:      "redis://:strong_redis_pass@prod-redis:6379/0",
			webhookSecret: "super_secure_tracking_webhook_secret_key_prod",
			gatewayMode:   "real",
			expectErr:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", "production")
			t.Setenv("JWT_SECRET", tc.jwtSecret)
			t.Setenv("DATABASE_URL", tc.dbURL)
			t.Setenv("REDIS_URL", tc.redisURL)
			t.Setenv("TRACKING_WEBHOOK_SECRET", tc.webhookSecret)
			t.Setenv("PAYMENT_GATEWAY_MODE", tc.gatewayMode)
			t.Setenv("IPPANEL_ENABLED", "true")
			t.Setenv("IPPANEL_API_KEY", "test-ippanel-key")
			t.Setenv("IPPANEL_SENDER", "+983000505")
			t.Setenv("IPPANEL_PATTERN_CODE", "test-pattern")

			cfg, err := LoadConfig()
			if tc.expectErr && err == nil {
				t.Errorf("expected error for case %q, but got nil", tc.name)
			}
			if !tc.expectErr && err != nil {
				t.Errorf("unexpected error for case %q: %v", tc.name, err)
			}
			if !tc.expectErr && cfg == nil {
				t.Errorf("expected valid config for case %q", tc.name)
			}
		})
	}
}
