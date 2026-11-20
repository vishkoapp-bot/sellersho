package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/database/seeder"
)

// Config represents runtime application configuration
type Config struct {
	StartupTimeout                time.Duration
	ShutdownGrace                 time.Duration
	Port                          string
	DatabaseURL                   string
	RedisURL                      string
	Environment                   string
	JWTSecret                     string
	BaseFeeF                      int64
	TrackingWebhookSecret         string
	PaymentGatewayMode            string // "mock" or "real"
	NetworkVolumeIncentiveEnabled bool
	UploadDir                     string
	AllowedOrigins                []string
	IPPanelEnabled                bool
	IPPanelBaseURL                string
	IPPanelAPIKey                 string
	IPPanelSender                 string
	IPPanelPatternCode            string
	// PSPTerminalID / PSPAPIKey are the Shaparak gateway credentials.
	// They are read here (not via os.Getenv at the call site) so the
	// production startup gate can require them; see LoadConfig.
	PSPTerminalID string
	PSPAPIKey     string
	// TapinTokenKey is the AES-256-GCM key material for supplier (and
	// platform) Tapin tokens at rest; see internal/tapin.TokenCipher.
	// TapinPreviousTokenKey is the previous key material, accepted only
	// as a decryption fallback during a key rotation window.
	TapinTokenKey         string
	TapinPreviousTokenKey string
	// TapinTokenKeyExplicit records whether TAPIN_TOKEN_KEY was set
	// explicitly via env (SEC-01 fix, critic F-11): in an
	// explicitly-dev environment an unset key becomes an ephemeral
	// per-process value, and the startup Tapin re-encryption
	// migration must be skipped then (it would orphan dev tokens
	// on the next restart, when the key is gone).
	TapinTokenKeyExplicit bool
	// DB pool bounds (PERF-03 G4), from DB_POOL_* env. Defaults
	// preserve the historical 25/10 pool with unlimited connection
	// reuse; see docs/operations/perf03-connection-budget.md for the
	// whole-deployment budget (100 PostgreSQL connections, 3 reserved
	// for superuser, <80% headroom rule).
	DBPoolMaxOpen     int
	DBPoolMaxIdle     int
	DBPoolMaxLifetime time.Duration
	DBPoolMaxIdleTime time.Duration
}

// Retired development defaults (SEC-01, gates G-01/G-03).
//
// The VALUES of the four retired dev defaults must never re-enter this
// file, or the production binary: a freshly built binary must contain
// none of them. Only their SHA-256 fingerprints are kept here, so
// production still rejects the exact retired values without embedding
// them. A fingerprint is not a secret and cannot be used to recover
// the value; the values themselves are already public in Git history
// (commit 2df713ba era) and are treated as exposed.
//
// Dev fallbacks are now ephemeral: in an explicitly-dev environment a
// missing secret is replaced by a fresh random value generated per
// process (ephemeralSecret), never by a fixed literal.
const (
	retiredDevJWTSecretSHA256     = "c11368dc06d318f5363f6e5164c96f2873d753698639606e808599c005e34eaf"
	retiredDevWebhookSecretSHA256 = "056efcab3aa2b75d1b661410c476e3855b6ee510bafb778b32cd27b5b66766a0"
	retiredDevDBPasswordSHA256    = "d78082fe19a8c385d0e145e14ca3be14d3b031048e5bccebd2c19a395d18f419"
	retiredDevRedisPasswordSHA256 = "ae5fa6a7879bf81bdc378cec5486cd9a004df3991b79163fcc2fcfc489ef1488"
)

// LoadConfig loads configuration from environment variables with strict fail-closed production checks
func LoadConfig() (*Config, error) {
	startup, err := durationSeconds("STARTUP_TIMEOUT_SECONDS", 30, 600)
	if err != nil {
		return nil, err
	}
	shutdown, err := durationSeconds("SHUTDOWN_GRACE_SECONDS", 20, 25)
	if err != nil {
		return nil, err
	}
	// PERF-03 G4: validated pool bounds. Defaults preserve the
	// historical 25/10 behaviour; any explicit value is validated
	// here so a typo fails boot instead of silently over- or
	// under-provisioning the pool.
	poolMaxOpen, err := poolInt("DB_POOL_MAX_OPEN", 25)
	if err != nil {
		return nil, err
	}
	poolMaxIdle, err := poolInt("DB_POOL_MAX_IDLE", 10)
	if err != nil {
		return nil, err
	}
	poolMaxLifetime, err := poolSeconds("DB_POOL_MAX_LIFETIME_SECONDS", 0)
	if err != nil {
		return nil, err
	}
	poolMaxIdleTime, err := poolSeconds("DB_POOL_MAX_IDLE_SECONDS", 0)
	if err != nil {
		return nil, err
	}
	if poolMaxOpen < 1 || poolMaxOpen > 100 {
		return nil, fmt.Errorf("DB_POOL_MAX_OPEN must be in 1..100 (got %d); the whole-deployment budget is 100 PostgreSQL connections with 3 reserved for superuser", poolMaxOpen)
	}
	if poolMaxIdle < 0 || poolMaxIdle > poolMaxOpen {
		return nil, fmt.Errorf("DB_POOL_MAX_IDLE must be in 0..DB_POOL_MAX_OPEN (got %d, max open %d)", poolMaxIdle, poolMaxOpen)
	}
	// SEC-01: APP_ENV has NO default. An unset (or blank) APP_ENV is
	// production behaviour — fail-closed. Development defaults apply
	// ONLY when APP_ENV is explicitly one of development/dev/test/local
	// (seeder.IsDevEnvironment allowlist). A missing APP_ENV must never
	// silently select dev defaults again (probe case 19).
	env := strings.TrimSpace(os.Getenv("APP_ENV"))
	if env == "" {
		env = "production"
	}
	isDev := seeder.IsDevEnvironment(env)

	port := getEnv("PORT", "8080")
	baseFeeStr := getEnv("BASE_FEE_F", "100000")
	baseFee, err := strconv.ParseInt(baseFeeStr, 10, 64)
	if err != nil {
		baseFee = 100000
	}

	jwtSecret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	webhookSecret := strings.TrimSpace(os.Getenv("TRACKING_WEBHOOK_SECRET"))
	gatewayMode := strings.ToLower(strings.TrimSpace(os.Getenv("PAYMENT_GATEWAY_MODE")))
	networkVolumeIncentiveEnabled := getBoolEnv("ENABLE_NETWORK_VOLUME_INCENTIVE", false)
	ippanelEnabled := getBoolEnv("IPPANEL_ENABLED", false)
	ippanelBaseURL := getEnv("IPPANEL_BASE_URL", "https://edge.ippanel.com/v1")
	ippanelKey := strings.TrimSpace(os.Getenv("IPPANEL_API_KEY"))
	ippanelSender := strings.TrimSpace(os.Getenv("IPPANEL_SENDER"))
	ippanelPattern := strings.TrimSpace(os.Getenv("IPPANEL_PATTERN_CODE"))
	uploadDir := getEnv("UPLOAD_DIR", "./static/uploads/products")
	pspTerminalID := strings.TrimSpace(os.Getenv("PSP_TERMINAL_ID"))
	pspAPIKey := strings.TrimSpace(os.Getenv("PSP_API_KEY"))
	tapinTokenKey := strings.TrimSpace(os.Getenv("TAPIN_TOKEN_KEY"))
	tapinTokenKeyExplicit := tapinTokenKey != ""
	tapinPreviousTokenKey := strings.TrimSpace(os.Getenv("TAPIN_TOKEN_PREVIOUS_KEY"))

	var allowedOrigins []string
	if originsEnv := strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS")); originsEnv != "" {
		for _, o := range strings.Split(originsEnv, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	}

	if !isDev {
		// Strict Fail-Closed Security Validation for Non-Dev / Production
		// NF-02: the retired-value error is distinct from the
		// empty-value error, so probe cases 02/09 (retired literal)
		// are distinguishable from the missing-value cases by
		// probe output alone.
		if jwtSecret == "" {
			return nil, errors.New("JWT_SECRET environment variable is required in production (fail-closed)")
		}
		if sha256Hex(jwtSecret) == retiredDevJWTSecretSHA256 {
			return nil, errors.New("JWT_SECRET matches a retired development default and is rejected in production (fail-closed)")
		}
		if len(jwtSecret) < 32 {
			return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters in production (got %d)", len(jwtSecret))
		}
		if dbURL == "" {
			return nil, errors.New("DATABASE_URL environment variable is required in production (fail-closed)")
		}
		if urlPasswordMatchesRetired(dbURL, retiredDevDBPasswordSHA256) {
			return nil, errors.New("default development database password detected in production DATABASE_URL (fail-closed)")
		}
		if redisURL == "" {
			return nil, errors.New("REDIS_URL environment variable is required in production (fail-closed)")
		}
		if urlPasswordMatchesRetired(redisURL, retiredDevRedisPasswordSHA256) {
			return nil, errors.New("default development redis password detected in production REDIS_URL (fail-closed)")
		}
		if webhookSecret == "" {
			return nil, errors.New("TRACKING_WEBHOOK_SECRET environment variable is required in production (fail-closed)")
		}
		if sha256Hex(webhookSecret) == retiredDevWebhookSecretSHA256 {
			return nil, errors.New("TRACKING_WEBHOOK_SECRET matches a retired development default and is rejected in production (fail-closed)")
		}
		if len(webhookSecret) < 32 {
			return nil, fmt.Errorf("TRACKING_WEBHOOK_SECRET must be at least 32 characters in production (got %d)", len(webhookSecret))
		}
		if gatewayMode != "real" {
			return nil, errors.New("PAYMENT_GATEWAY_MODE must be explicitly set to 'real' in production (fail-closed)")
		}
		// SEC-01 PSP re-scope (supervisor F-02 / critic F-01): the
		// PSP pair is required IFF either var is non-empty. A
		// partial pair (exactly one set) is always a
		// misconfiguration (typo / half-injection) and fails boot.
		// Both absent is NOT a boot failure: no PSP is contracted
		// yet (FIN-010 not landed), VerifyTransaction already fails
		// closed at payment time, and real-mode payments are simply
		// unverifiable — so boot continues with a prominent warning.
		// No re-tightening is needed at FIN-010: once credentials
		// exist (both present) the partial rule subsumes the strict
		// rule; FIN-010 only injects the pair.
		// DEPLOY NOTE: production currently runs mode=real with no
		// PSP credentials injected, so the both-absent case is the
		// live prod case — it must boot (with the warning below).
		if (pspTerminalID == "") != (pspAPIKey == "") {
			return nil, errors.New("PSP_TERMINAL_ID and PSP_API_KEY must be set together in production when PAYMENT_GATEWAY_MODE is 'real' (partial PSP configuration fails closed)")
		}
		if pspTerminalID == "" {
			log.Println("[WARNING] PSP_TERMINAL_ID and PSP_API_KEY are both unset while PAYMENT_GATEWAY_MODE is 'real': real-mode payments are unverifiable (VerifyTransaction fails closed at payment time). Inject the PSP pair when FIN-010 lands; no gate change is needed then.")
		} else {
			// SEC-01 fix (critic F-06): PSP keys, like the Tapin
			// key, must carry real entropy in production — same
			// >=32-char minimum as JWT/webhook (see above).
			if len(pspTerminalID) < 32 {
				return nil, fmt.Errorf("PSP_TERMINAL_ID must be at least 32 characters in production (got %d)", len(pspTerminalID))
			}
			if len(pspAPIKey) < 32 {
				return nil, fmt.Errorf("PSP_API_KEY must be at least 32 characters in production (got %d)", len(pspAPIKey))
			}
		}
		if !ippanelEnabled || ippanelKey == "" || ippanelSender == "" || ippanelPattern == "" {
			return nil, errors.New("IPPANEL_ENABLED=true and IPPANEL_API_KEY, IPPANEL_SENDER, IPPANEL_PATTERN_CODE are required in production")
		}
		// SEC-01 (G-05): TAPIN_TOKEN_KEY is required in production.
		// It keys the Tapin-token encryption (internal/tapin); a
		// missing key must fail startup, never silently mean "store
		// tokens in plaintext". The previous key is optional: it is
		// only a decryption fallback during a rotation window.
		if tapinTokenKey == "" {
			return nil, errors.New("TAPIN_TOKEN_KEY environment variable is required in production (fail-closed)")
		}
		// SEC-01 fix (critic F-06): the Tapin key is SHA-256'd into
		// the AES-256-GCM key, so its strength is exactly the
		// material's entropy — enforce the same >=32-char minimum
		// as JWT/webhook. (Dev ephemeral defaults are 43 chars and
		// this branch never runs for them.)
		if len(tapinTokenKey) < 32 {
			return nil, fmt.Errorf("TAPIN_TOKEN_KEY must be at least 32 characters in production (got %d)", len(tapinTokenKey))
		}
		// SEC-01: ALLOWED_ORIGINS has no silent production fallback.
		// A missing value fails startup; the operator must set it
		// explicitly. (The previous silent default to sellershow.ir
		// is removed.)
		if len(allowedOrigins) == 0 {
			return nil, errors.New("ALLOWED_ORIGINS environment variable is required in production (fail-closed)")
		}
	} else {
		// Development & Test Defaults — ephemeral only, and only in an
		// explicitly-dev environment (see the APP_ENV note above).
		// Secrets are fresh random values per process: they are never
		// valid across restarts and never equal any retired literal.
		if jwtSecret == "" {
			jwtSecret, err = ephemeralSecret()
			if err != nil {
				return nil, fmt.Errorf("generate ephemeral development JWT secret: %w", err)
			}
		}
		if dbURL == "" {
			// Passwordless localhost: the retired dev password must
			// not appear here. A local stack that requires auth must
			// set DATABASE_URL explicitly.
			dbURL = "postgres://drup_admin@localhost:5432/drup_db?sslmode=disable"
		}
		if redisURL == "" {
			redisURL = "redis://localhost:6379/0"
		}
		if webhookSecret == "" {
			webhookSecret, err = ephemeralSecret()
			if err != nil {
				return nil, fmt.Errorf("generate ephemeral development webhook secret: %w", err)
			}
		}
		if tapinTokenKey == "" {
			// Ephemeral per-process key: dev Tapin tokens encrypted
			// under it are unreadable after a restart and must be
			// re-entered. Set TAPIN_TOKEN_KEY explicitly for a
			// persistent dev key.
			tapinTokenKey, err = ephemeralSecret()
			if err != nil {
				return nil, fmt.Errorf("generate ephemeral development Tapin key: %w", err)
			}
		}
		if gatewayMode == "" {
			gatewayMode = "mock"
		}
		if len(allowedOrigins) == 0 {
			allowedOrigins = []string{
				"http://localhost:3000",
				"http://localhost:5173",
				"http://127.0.0.1:3000",
				"http://127.0.0.1:5173",
			}
		}
	}

	return &Config{
		StartupTimeout:                startup,
		ShutdownGrace:                 shutdown,
		Port:                          port,
		DatabaseURL:                   dbURL,
		RedisURL:                      redisURL,
		Environment:                   env,
		JWTSecret:                     jwtSecret,
		BaseFeeF:                      baseFee,
		TrackingWebhookSecret:         webhookSecret,
		PaymentGatewayMode:            gatewayMode,
		NetworkVolumeIncentiveEnabled: networkVolumeIncentiveEnabled,
		UploadDir:                     uploadDir,
		AllowedOrigins:                allowedOrigins,
		IPPanelEnabled:                ippanelEnabled, IPPanelBaseURL: ippanelBaseURL, IPPanelAPIKey: ippanelKey, IPPanelSender: ippanelSender, IPPanelPatternCode: ippanelPattern,
		PSPTerminalID:         pspTerminalID,
		PSPAPIKey:             pspAPIKey,
		TapinTokenKey:         tapinTokenKey,
		TapinPreviousTokenKey: tapinPreviousTokenKey,
		TapinTokenKeyExplicit: tapinTokenKeyExplicit,
		DBPoolMaxOpen:         poolMaxOpen,
		DBPoolMaxIdle:         poolMaxIdle,
		DBPoolMaxLifetime:     poolMaxLifetime,
		DBPoolMaxIdleTime:     poolMaxIdleTime,
	}, nil
}

// sha256Hex returns the lowercase hex SHA-256 of s. Used only to compare
// a supplied value against a retired-default fingerprint; the digest is
// never logged and the input is never stored.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// urlPasswordMatchesRetired reports whether the password embedded in a
// database/Redis URL hashes to a retired-default fingerprint. It parses
// the URL and hashes only the password component, so the retired value
// itself never appears in this file. An unparsable URL (or one with no
// password) does not match; the driver rejects it later at connect.
func urlPasswordMatchesRetired(rawURL, retiredFingerprint string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return false
	}
	pw, ok := u.User.Password()
	if !ok || pw == "" {
		return false
	}
	return sha256Hex(pw) == retiredFingerprint
}

// ephemeralSecret generates a fresh 32-byte random secret for
// explicitly-dev environments only. The value is base64url (43 chars,
// no padding) and is never persisted.
func ephemeralSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getBoolEnv(key string, defaultVal bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return defaultVal
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return defaultVal
	}
	return parsed
}

// poolInt reads an optional integer env var; unset or blank means def.
// A present but unparsable value is an error, never a silent default.
func poolInt(key string, def int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return def, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return v, nil
}

// poolSeconds reads an optional non-negative seconds env var as a
// duration; unset or blank means def seconds. 0 means "no limit".
// The 0..86400 range (one day) bounds the value to something a pool
// lifetime or idle timeout can sanely be.
func poolSeconds(key string, def int) (time.Duration, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return time.Duration(def) * time.Second, nil
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || seconds < 0 || seconds > 86400 {
		return 0, fmt.Errorf("%s must be an integer in 0..86400", key)
	}
	return time.Duration(seconds) * time.Second, nil
}

func durationSeconds(key string, fallback, maximum int) (time.Duration, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return time.Duration(fallback) * time.Second, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 1 || seconds > maximum {
		return 0, fmt.Errorf("%s must be an integer in 1..%d", key, maximum)
	}
	return time.Duration(seconds) * time.Second, nil
}
