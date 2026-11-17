package payment

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CapacityGatewayFromEnvironment is an opt-in selector for the isolated harness.
// The caller must fail startup on error and retain its ordinary real/mock choice
// when enabled is false. The marker is a runner capability, not a production flag.
// This client never opens a network connection. It simulates verification only;
// it cannot certify provider inquiry, refund or crash-reconciliation behavior.
func CapacityGatewayFromEnvironment(getenv func(string) string) (client PaymentGatewayClient, enabled bool, err error) {
	mode := getenv("CAPACITY_PAYMENT_MODE")
	if mode == "" {
		return nil, false, nil
	}
	if getenv("APP_ENV") != "test" || !regexp.MustCompile(`^[a-f0-9]{24}$`).MatchString(getenv("SELLERSHO_CAPACITY_RUN")) {
		return nil, false, errors.New("capacity provider requires test environment and private run marker")
	}
	switch mode {
	case "success", "reject", "wrong_amount", "timeout":
	default:
		return nil, false, errors.New("unsupported capacity verification mode")
	}
	parts := strings.Split(getenv("CAPACITY_PAYMENT_LATENCIES_MS"), ",")
	if len(parts) > 100 {
		return nil, false, errors.New("capacity latency distribution exceeds 100 samples")
	}
	delays := make([]time.Duration, 0, len(parts))
	for _, part := range parts {
		ms, e := strconv.Atoi(strings.TrimSpace(part))
		if e != nil || ms < 0 || ms > 5000 {
			return nil, false, errors.New("capacity provider latency must be explicit milliseconds in [0,5000]")
		}
		delays = append(delays, time.Duration(ms)*time.Millisecond)
	}
	return &capacityGateway{mode: mode, delays: delays}, true, nil
}

type capacityGateway struct {
	mode   string
	delays []time.Duration
}

func (c *capacityGateway) VerifyTransaction(ctx context.Context, token string, expectedAmount int64) (*VerificationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if token == "" || expectedAmount <= 0 {
		return nil, ErrVerificationFailed
	}
	digest := sha256.Sum256([]byte(token))
	number := binary.BigEndian.Uint64(digest[:8])
	timer := time.NewTimer(c.delays[number%uint64(len(c.delays))])
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	switch c.mode {
	case "reject":
		return nil, ErrVerificationFailed
	case "wrong_amount":
		return nil, ErrAmountMismatch
	case "timeout":
		return nil, context.DeadlineExceeded
	}
	return &VerificationResult{Success: true, RRN: fmt.Sprintf("%012d", number%1000000000000), VerifiedAmount: expectedAmount, CardMask: "0000-00**-****-0000", VerifiedAt: time.Now()}, nil
}
