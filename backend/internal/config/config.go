package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/drup/platform/internal/database/seeder"
)

// Config represents runtime application configuration
type Config struct {
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
}

const developmentJWTSecret = "drup_jwt_secret_key_2026_dev_secure"
const developmentWebhookSecret = "drup_tracking_webhook_secret_dev_2026"

// LoadConfig loads configuration from environment variables with strict fail-closed production checks
func LoadConfig() (*Config, error) {
	env := getEnv("APP_ENV", "development")
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
		if jwtSecret == "" || jwtSecret == developmentJWTSecret {
			return nil, errors.New("JWT_SECRET environment variable is required in production (fail-closed)")
		}
		if len(jwtSecret) < 32 {
			return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters in production (got %d)", len(jwtSecret))
		}
		if dbURL == "" {
			return nil, errors.New("DATABASE_URL environment variable is required in production (fail-closed)")
		}
		if strings.Contains(dbURL, "drup_secure_password_2026") {
			return nil, errors.New("default development database password detected in production DATABASE_URL (fail-closed)")
		}
		if redisURL == "" {
			return nil, errors.New("REDIS_URL environment variable is required in production (fail-closed)")
		}
		if strings.Contains(redisURL, "drup_redis_pass_2026") {
			return nil, errors.New("default development redis password detected in production REDIS_URL (fail-closed)")
		}
		if webhookSecret == "" || webhookSecret == developmentWebhookSecret || len(webhookSecret) < 32 {
			return nil, errors.New("TRACKING_WEBHOOK_SECRET must be a non-development value of at least 32 characters in production (fail-closed)")
		}
		if gatewayMode != "real" {
			return nil, errors.New("PAYMENT_GATEWAY_MODE must be explicitly set to 'real' in production (fail-closed)")
		}
		if !ippanelEnabled || ippanelKey == "" || ippanelSender == "" || ippanelPattern == "" {
			return nil, errors.New("IPPANEL_ENABLED=true and IPPANEL_API_KEY, IPPANEL_SENDER, IPPANEL_PATTERN_CODE are required in production")
		}
		if len(allowedOrigins) == 0 {
			// In production, require explicit ALLOWED_ORIGINS to prevent open CORS
			allowedOrigins = []string{"https://sellershow.ir", "https://app.sellershow.ir"}
		}
	} else {
		// Development & Test Defaults
		if jwtSecret == "" {
			jwtSecret = developmentJWTSecret
		}
		if dbURL == "" {
			dbURL = "postgres://drup_admin:drup_secure_password_2026@localhost:5432/drup_db?sslmode=disable"
		}
		if redisURL == "" {
			redisURL = "redis://:drup_redis_pass_2026@localhost:6379/0"
		}
		if webhookSecret == "" {
			webhookSecret = developmentWebhookSecret
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
	}, nil
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
