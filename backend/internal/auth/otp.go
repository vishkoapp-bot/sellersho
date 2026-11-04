package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/drup/platform/internal/database/seeder"
	"github.com/drup/platform/internal/ippanel"
	"github.com/drup/platform/internal/phone"
	"github.com/drup/platform/internal/repository"
)

type OTPService struct {
	redisClient *repository.RedisClient
	env         string
	sms         *ippanel.Client
	patternCode string
	logSMS      func(context.Context, string, string, string, string, string)
}

func NewOTPService(redisClient *repository.RedisClient, env string) *OTPService {
	return &OTPService{redisClient: redisClient, env: env}
}

func (s *OTPService) ConfigureSMS(client *ippanel.Client, patternCode string, logger func(context.Context, string, string, string, string, string)) {
	s.sms = client
	s.patternCode = patternCode
	s.logSMS = logger
}

func (s *OTPService) IsDemoMode() bool {
	return seeder.IsDevEnvironment(s.env)
}

// GenerateAndStoreOTP creates a 4-digit code and stores it in Redis with a 2-minute TTL
func (s *OTPService) GenerateAndStoreOTP(ctx context.Context, mobile string) (string, error) {
	mobile = phone.Normalize(mobile)
	if !phone.IsValid(mobile) {
		return "", errors.New("invalid mobile number")
	}
	value, err := rand.Int(rand.Reader, big.NewInt(9000))
	if err != nil {
		return "", fmt.Errorf("failed to generate OTP: %w", err)
	}
	code := fmt.Sprintf("%04d", value.Int64()+1000)

	if !s.IsDemoMode() && s.sms == nil {
		return "", errors.New("OTP SMS provider is not configured")
	}
	key := fmt.Sprintf("otp:%s", mobile)
	if s.redisClient != nil {
		cooldownKey := fmt.Sprintf("otp:cooldown:%s", mobile)
		allowed, err := s.redisClient.Client.SetNX(ctx, cooldownKey, 1, 30*time.Second).Result()
		if err != nil {
			return "", fmt.Errorf("failed to apply OTP cooldown: %w", err)
		}
		if !allowed {
			return "", errors.New("OTP resend cooldown is active")
		}
		if !s.IsDemoMode() {
			ids, sendErr := s.sms.SendPattern(ctx, mobile, s.patternCode, map[string]string{"code": code})
			if sendErr != nil {
				s.redisClient.Client.Del(ctx, cooldownKey)
				if s.logSMS != nil {
					s.logSMS(ctx, mobile, "otp", "failed", "", sendErr.Error())
				}
				return "", fmt.Errorf("OTP SMS delivery failed: %w", sendErr)
			}
			if s.logSMS != nil {
				id := ""
				if len(ids) > 0 {
					id = ids[0]
				}
				s.logSMS(ctx, mobile, "otp", "sent", id, "")
			}
			if len(ids) > 0 {
				_ = s.redisClient.Client.Set(ctx, fmt.Sprintf("sms:bulk:%s", ids[0]), ids[0], 30*24*time.Hour).Err()
			}
			if err := s.redisClient.Client.Set(ctx, key, code, 120*time.Second).Err(); err != nil {
				return "", fmt.Errorf("failed to save OTP in redis: %w", err)
			}
			_ = ids
		} else if err := s.redisClient.Client.Set(ctx, key, code, 120*time.Second).Err(); err != nil {
			return "", fmt.Errorf("failed to save OTP in redis: %w", err)
		}
	}

	if s.IsDemoMode() {
		// Dev/test convenience: code is still generated and stored in Redis,
		// but it MUST also be delivered via real SMS (no auto-returned code).
		// The response never exposes the code; read it from the SMS.
		if s.sms != nil {
			ids, sendErr := s.sms.SendPattern(ctx, mobile, s.patternCode, map[string]string{"code": code})
			if sendErr != nil {
				s.redisClient.Client.Del(ctx, fmt.Sprintf("otp:cooldown:%s", mobile))
				if s.logSMS != nil {
					s.logSMS(ctx, mobile, "otp", "failed", "", sendErr.Error())
				}
				return "", fmt.Errorf("OTP SMS delivery failed: %w", sendErr)
			}
			if s.logSMS != nil {
				id := ""
				if len(ids) > 0 {
					id = ids[0]
				}
				s.logSMS(ctx, mobile, "otp", "sent", id, "")
			}
		}
		if err := s.redisClient.Client.Set(ctx, key, code, 120*time.Second).Err(); err != nil {
			return "", fmt.Errorf("failed to save OTP in redis: %w", err)
		}
		return "", nil
	}
	return "", nil
}

// VerifyOTP checks if the provided code matches the Redis stored code
func (s *OTPService) VerifyOTP(ctx context.Context, mobile, inputCode string) bool {
	mobile = phone.Normalize(mobile)
	if s.redisClient == nil {
		// Mock bypass only in dev/test environment if Redis is offline
		return false
	}

	key := fmt.Sprintf("otp:%s", mobile)
	attemptsKey := fmt.Sprintf("otp:attempts:%s", mobile)
	const script = `
local code = redis.call('GET', KEYS[1])
if not code then return 0 end
if tonumber(redis.call('GET', KEYS[2]) or '0') >= 5 then return 0 end
if code == ARGV[1] then
  redis.call('DEL', KEYS[1], KEYS[2], KEYS[3])
  return 1
end
local attempts = redis.call('INCR', KEYS[2])
redis.call('EXPIRE', KEYS[2], 120)
if attempts >= 5 then redis.call('DEL', KEYS[1], KEYS[3]) end
return 0
`
	result, err := s.redisClient.Client.Eval(ctx, script, []string{key, attemptsKey, fmt.Sprintf("otp:cooldown:%s", mobile)}, inputCode).Int()
	return err == nil && result == 1
}
