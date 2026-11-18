package config

import (
	"bytes"
	"log"
	"net/url"
	"os"
	"strings"
	"testing"
)

// SEC-01 gate tests (G-05 fail-closed matrix additions + G-01/G-03
// dev-default removal). All values below are synthetic test-only
// values. The retired-literal fixtures are the historical development
// defaults: they must never appear in non-test code, and this file
// proves production rejects them.

// retiredDevFixtures are the retired development default values, kept
// ONLY as test fixtures proving production rejects them. They are
// already public in Git history and treated as exposed; they must
// never be used as real configuration anywhere.
const (
	retiredDevJWT     = "drup_jwt_secret_key_2026_dev_secure"
	retiredDevWebhook = "drup_tracking_webhook_secret_dev_2026"
)

const (
	synthJWT     = "synthetic-test-jwt-secret-0123456789abcdef"
	synthDB      = "postgres://synthetic-user:synthetic-pass-0123456789@db:5432/synthetic_db"
	synthRedis   = "redis://:synthetic-redis-pass-0123456789@redis:6379/0"
	synthWebhook = "synthetic-test-webhook-secret-0123456789abcdef"
	synthPSPTerm = "synthetic-test-psp-terminal-0123456789"
	synthPSPKey  = "synthetic-test-psp-api-key-0123456789"
	synthTapin   = "synthetic-test-tapin-token-key-0123456789"
	synthOrigin  = "https://shop.test.invalid"
)

// setValidProdEnv sets every production-required variable to synthetic
// values, so a test can then remove or corrupt exactly one of them.
func setValidProdEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", synthJWT)
	t.Setenv("DATABASE_URL", synthDB)
	t.Setenv("REDIS_URL", synthRedis)
	t.Setenv("TRACKING_WEBHOOK_SECRET", synthWebhook)
	t.Setenv("PAYMENT_GATEWAY_MODE", "real")
	t.Setenv("IPPANEL_ENABLED", "true")
	t.Setenv("IPPANEL_API_KEY", "synthetic-test-ippanel-key")
	t.Setenv("IPPANEL_SENDER", "synthetic-sender")
	t.Setenv("IPPANEL_PATTERN_CODE", "synthetic-pattern")
	t.Setenv("PSP_TERMINAL_ID", synthPSPTerm)
	t.Setenv("PSP_API_KEY", synthPSPKey)
	t.Setenv("TAPIN_TOKEN_KEY", synthTapin)
	t.Setenv("ALLOWED_ORIGINS", synthOrigin)
}

func TestLoadConfig_Production_ValidWithSEC01Gates(t *testing.T) {
	setValidProdEnv(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("valid production config rejected: %v", err)
	}
	if cfg.PSPTerminalID != synthPSPTerm || cfg.PSPAPIKey != synthPSPKey {
		t.Error("PSP credentials not carried through config")
	}
	if cfg.TapinTokenKey != synthTapin {
		t.Error("TAPIN_TOKEN_KEY not carried through config")
	}
	if !cfg.TapinTokenKeyExplicit {
		t.Error("explicit TAPIN_TOKEN_KEY not recorded as explicit")
	}
	if len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != synthOrigin {
		t.Errorf("ALLOWED_ORIGINS not carried through config: %v", cfg.AllowedOrigins)
	}
}

// SEC-01 PSP re-scope (supervisor F-02 / critic F-01): a PARTIAL
// PSP pair (exactly one of the two set) fails boot; both absent
// boots (with a warning); both present boots.
func TestLoadConfig_Production_PartialPSPConfigFails(t *testing.T) {
	for _, tc := range []struct {
		name  string
		unset string
	}{
		{"missing PSP_TERMINAL_ID", "PSP_TERMINAL_ID"},
		{"missing PSP_API_KEY", "PSP_API_KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setValidProdEnv(t)
			t.Setenv(tc.unset, "")
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("production accepted real-mode config without %s", tc.unset)
			}
		})
	}
	// PSP credentials are not required when the gateway is mock — but
	// production requires mode=real, so mock mode fails on its own
	// gate, not on PSP. Prove the failure is the mode gate.
	t.Run("mock mode still fails on mode gate", func(t *testing.T) {
		setValidProdEnv(t)
		t.Setenv("PAYMENT_GATEWAY_MODE", "mock")
		t.Setenv("PSP_TERMINAL_ID", "")
		t.Setenv("PSP_API_KEY", "")
		_, err := LoadConfig()
		if err == nil || !strings.Contains(err.Error(), "PAYMENT_GATEWAY_MODE") {
			t.Fatalf("expected mode-gate error, got %v", err)
		}
	})
}

func TestLoadConfig_Production_BothAbsentPSPBootsWithWarning(t *testing.T) {
	// Both PSP vars absent (the live prod case until FIN-010 lands):
	// boot must succeed, with a prominent warning that real-mode
	// payments are unverifiable. VerifyTransaction still fails
	// closed at payment time (unchanged old behaviour).
	setValidProdEnv(t)
	t.Setenv("PSP_TERMINAL_ID", "")
	t.Setenv("PSP_API_KEY", "")
	var buf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldOut)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("both-absent PSP config rejected: %v", err)
	}
	if cfg.PSPTerminalID != "" || cfg.PSPAPIKey != "" {
		t.Error("both-absent PSP config carried non-empty credentials")
	}
	if !strings.Contains(buf.String(), "PSP_TERMINAL_ID") || !strings.Contains(buf.String(), "unverifiable") {
		t.Errorf("both-absent PSP boot logged no prominent warning, got %q", buf.String())
	}
}

func TestLoadConfig_Production_ShortTapinAndPSPKeysFail(t *testing.T) {
	// SEC-01 fix (critic F-06): TAPIN_TOKEN_KEY and both PSP keys
	// need >=32 chars of entropy in production, like JWT/webhook.
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{"short TAPIN_TOKEN_KEY", "TAPIN_TOKEN_KEY", "short-tapin-key-123"},
		{"short PSP_TERMINAL_ID", "PSP_TERMINAL_ID", "short-psp-terminal-123"},
		{"short PSP_API_KEY", "PSP_API_KEY", "short-psp-api-key-123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setValidProdEnv(t)
			t.Setenv(tc.key, tc.value)
			_, err := LoadConfig()
			if err == nil {
				t.Fatalf("production accepted short %s", tc.key)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("short-%s error does not name the key: %v", tc.key, err)
			}
		})
	}
}

func TestLoadConfig_Production_MissingTapinKeyFails(t *testing.T) {
	setValidProdEnv(t)
	t.Setenv("TAPIN_TOKEN_KEY", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("production accepted config without TAPIN_TOKEN_KEY")
	}
}

func TestLoadConfig_Production_MissingAllowedOriginsFails(t *testing.T) {
	setValidProdEnv(t)
	t.Setenv("ALLOWED_ORIGINS", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("production accepted config without ALLOWED_ORIGINS (silent fallback is removed)")
	}
}

func TestLoadConfig_Production_RetiredDevDefaultsRejected(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{"retired dev JWT secret", "JWT_SECRET", retiredDevJWT},
		{"retired dev webhook secret", "TRACKING_WEBHOOK_SECRET", retiredDevWebhook},
		{"retired dev DB password in URL", "DATABASE_URL", "postgres://prod_user:drup_secure_password_2026@prod-db:5432/drup_prod"},
		{"retired dev Redis password in URL", "REDIS_URL", "redis://:drup_redis_pass_2026@prod-redis:6379/0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setValidProdEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("production accepted retired dev material in %s", tc.key)
			}
		})
	}
}

// OBS-01 NF-02: a retired value must fail with error text DISTINCT from
// the empty-value error, so probe cases 02/09 are distinguishable from
// the missing-value cases by probe output alone.
func TestLoadConfig_Production_RetiredVsEmptyErrorTextDistinct(t *testing.T) {
	for _, tc := range []struct {
		key, retired string
	}{
		{"JWT_SECRET", retiredDevJWT},
		{"TRACKING_WEBHOOK_SECRET", retiredDevWebhook},
	} {
		t.Run(tc.key, func(t *testing.T) {
			setValidProdEnv(t)
			t.Setenv(tc.key, "")
			_, emptyErr := LoadConfig()
			if emptyErr == nil {
				t.Fatalf("production accepted empty %s", tc.key)
			}
			setValidProdEnv(t)
			t.Setenv(tc.key, tc.retired)
			_, retiredErr := LoadConfig()
			if retiredErr == nil {
				t.Fatalf("production accepted retired %s", tc.key)
			}
			if emptyErr.Error() == retiredErr.Error() {
				t.Fatalf("retired-value error is identical to empty-value error: %q", emptyErr.Error())
			}
			if !strings.Contains(strings.ToLower(retiredErr.Error()), "retired") {
				t.Fatalf("retired-value error does not say retired: %q", retiredErr.Error())
			}
		})
	}
}

func TestLoadConfig_AppEnvUnsetIsProduction(t *testing.T) {
	// Unset APP_ENV + missing secrets must fail (no silent dev
	// fallback). Unsetting uses os.Unsetenv with cleanup, mirroring
	// lifecycle_test.go.
	unsetAppEnv := func(t *testing.T) {
		t.Helper()
		old, ok := os.LookupEnv("APP_ENV")
		os.Unsetenv("APP_ENV")
		t.Cleanup(func() {
			if ok {
				os.Setenv("APP_ENV", old)
			} else {
				os.Unsetenv("APP_ENV")
			}
		})
	}
	t.Run("unset APP_ENV with missing secrets fails", func(t *testing.T) {
		unsetAppEnv(t)
		for _, key := range []string{"JWT_SECRET", "DATABASE_URL", "REDIS_URL", "TRACKING_WEBHOOK_SECRET", "PAYMENT_GATEWAY_MODE"} {
			t.Setenv(key, "")
		}
		if _, err := LoadConfig(); err == nil {
			t.Fatal("unset APP_ENV with missing secrets was accepted (silent dev fallback)")
		}
	})
	t.Run("unset APP_ENV with valid secrets behaves as production", func(t *testing.T) {
		unsetAppEnv(t)
		// setValidProdEnv sets APP_ENV=production via t.Setenv; undo
		// that by unsetting again AFTER it (t.Setenv cleanup runs
		// after this test, so the unset holds for the LoadConfig call).
		setValidProdEnv(t)
		os.Unsetenv("APP_ENV")
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unset APP_ENV with valid secrets rejected: %v", err)
		}
		if cfg.Environment != "production" {
			t.Errorf("unset APP_ENV resolved to environment %q, want %q", cfg.Environment, "production")
		}
		if cfg.PaymentGatewayMode != "real" {
			t.Errorf("unset APP_ENV resolved gateway mode %q, want %q", cfg.PaymentGatewayMode, "real")
		}
	})
}

func TestLoadConfig_DevDefaultsAreEphemeral(t *testing.T) {
	// Explicit dev only: missing secrets become fresh random values
	// per LoadConfig call — never a fixed literal, never equal across
	// calls, and never equal to a retired value.
	t.Setenv("APP_ENV", "development")
	for _, key := range []string{"JWT_SECRET", "TRACKING_WEBHOOK_SECRET", "DATABASE_URL", "REDIS_URL", "PAYMENT_GATEWAY_MODE", "TAPIN_TOKEN_KEY", "ALLOWED_ORIGINS"} {
		t.Setenv(key, "")
	}
	first, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if first.JWTSecret == "" || first.TrackingWebhookSecret == "" || first.TapinTokenKey == "" {
		t.Fatal("dev fallback left a secret empty")
	}
	if first.TapinTokenKeyExplicit {
		t.Fatal("ephemeral dev TAPIN_TOKEN_KEY recorded as explicit (startup migration must skip it)")
	}
	if len(first.JWTSecret) < 32 || len(first.TrackingWebhookSecret) < 32 || len(first.TapinTokenKey) < 32 {
		t.Fatal("dev fallback secret shorter than 32 characters")
	}
	if first.JWTSecret == retiredDevJWT || first.TrackingWebhookSecret == retiredDevWebhook {
		t.Fatal("dev fallback equals a retired dev literal")
	}
	if first.JWTSecret == first.TrackingWebhookSecret || first.JWTSecret == first.TapinTokenKey {
		t.Fatal("dev fallback secrets are not independent")
	}
	second, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if second.JWTSecret == first.JWTSecret || second.TrackingWebhookSecret == first.TrackingWebhookSecret {
		t.Fatal("dev fallback secret was reused across loads (not ephemeral)")
	}
	// Dev DB/Redis fallbacks carry no password at all.
	for _, raw := range []string{first.DatabaseURL, first.RedisURL} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("dev fallback URL unparsable: %v", err)
		}
		if u.User != nil {
			if _, hasPass := u.User.Password(); hasPass {
				t.Fatalf("dev fallback URL carries a password: %s", u.Host)
			}
		}
	}
	if first.PaymentGatewayMode != "mock" {
		t.Errorf("dev gateway mode %q, want mock", first.PaymentGatewayMode)
	}
}

func TestLoadConfig_DevTapinKeyExplicitIsKept(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("TAPIN_TOKEN_KEY", synthTapin)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TapinTokenKey != synthTapin {
		t.Error("explicit dev TAPIN_TOKEN_KEY was replaced by an ephemeral one")
	}
	if !cfg.TapinTokenKeyExplicit {
		t.Error("explicit dev TAPIN_TOKEN_KEY not recorded as explicit")
	}
}
