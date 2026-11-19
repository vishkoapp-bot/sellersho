package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/drup/platform/internal/database/seeder"
	"github.com/drup/platform/internal/dependencybudget"
	"github.com/drup/platform/internal/ippanel"
	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/phone"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

var ErrOTPStoreUnavailable = errors.New("OTP reservation unavailable")
var ErrOTPDeliveryUnknown = errors.New("OTP SMS delivery outcome unknown")

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

// observeOTP records one OTP script as its own series (PERF-04 D-05: the
// hook can only see the "eval" command name, so OTP scripts were
// indistinguishable from hold scripts in metrics).
func observeOTP(ctx context.Context, op string, start time.Time, err error) {
	outcome := "success"
	if err != nil {
		outcome = "error"
		if ctx.Err() == context.DeadlineExceeded {
			outcome = "timeout"
		}
	}
	observability.Default.Observe("redis", op, outcome, time.Since(start))
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
	if s.redisClient == nil || s.redisClient.Client == nil {
		return "", ErrOTPStoreUnavailable
	}
	if !s.IsDemoMode() && s.sms == nil {
		return "", errors.New("OTP SMS provider is not configured")
	}
	value, err := rand.Int(rand.Reader, big.NewInt(9000))
	if err != nil {
		return "", errors.New("failed to generate OTP")
	}
	code := fmt.Sprintf("%04d", value.Int64()+1000)
	generation := uuid.NewString()
	keys := []string{"otp:" + mobile, "otp:cooldown:" + mobile, "otp:attempts:" + mobile}
	const reserve = `
if redis.call('EXISTS',KEYS[2])==1 then return 0 end
redis.call('SET',KEYS[2],ARGV[1],'EX',30)
redis.call('SET',KEYS[1],ARGV[2],'EX',120)
redis.call('DEL',KEYS[3])
return 1`
	reserveStart := time.Now()
	allowed, err := s.redisClient.Client.Eval(ctx, reserve, keys, generation, code).Int()
	observeOTP(ctx, "otp_reserve", reserveStart, err)
	if err != nil {
		return "", fmt.Errorf("%w: reservation could not be confirmed", ErrOTPStoreUnavailable)
	}
	if allowed != 1 {
		return "", errors.New("OTP resend cooldown is active")
	}
	if s.sms == nil {
		return "", nil
	}
	ids, sendErr := s.sms.SendPattern(ctx, mobile, s.patternCode, map[string]string{"code": code})
	if sendErr != nil {
		status := "failed"
		if dependencybudget.IsAmbiguous(sendErr) {
			status = "unknown"
		} else {
			// A failed old request cannot remove a newer reservation, even if the
			// four-digit code happens to collide. Never detach cancellation cleanup.
			cleanup, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
			const remove = `if redis.call('GET',KEYS[2])==ARGV[1] and redis.call('GET',KEYS[1])==ARGV[2] then return redis.call('DEL',KEYS[1],KEYS[2],KEYS[3]) end return 0`
			s.redisClient.Client.Eval(cleanup, remove, keys, generation, code).Err()
			cancel()
		}
		if s.logSMS != nil {
			s.logSMS(ctx, mobile, "otp", status, "", sendErr.Error())
		}
		if status == "unknown" {
			return "", fmt.Errorf("%w: %w", ErrOTPDeliveryUnknown, sendErr)
		}
		return "", fmt.Errorf("OTP SMS delivery failed: %w", sendErr)
	}
	if s.logSMS != nil {
		s.logSMS(ctx, mobile, "otp", "sent", ids[0], "")
	}
	_ = s.redisClient.Client.Set(ctx, "sms:bulk:"+ids[0], ids[0], 30*24*time.Hour).Err()
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
	verifyStart := time.Now()
	result, err := s.redisClient.Client.Eval(ctx, script, []string{key, attemptsKey, fmt.Sprintf("otp:cooldown:%s", mobile)}, inputCode).Int()
	observeOTP(ctx, "otp_verify", verifyStart, err)
	return err == nil && result == 1
}
