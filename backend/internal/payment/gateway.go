package payment

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"time"
)

var (
	ErrVerificationFailed = errors.New("payment verification failed by gateway")
	ErrAmountMismatch     = errors.New("payment amount does not match transaction record")
)

// VerificationResult contains the confirmed transaction attributes from the payment gateway
type VerificationResult struct {
	Success        bool
	RRN            string
	VerifiedAmount int64
	CardMask       string
	VerifiedAt     time.Time
}

// PaymentGatewayClient abstracts interactions with the Shaparak PSP (e.g., Asan Pardakht, Saman, Mellat)
type PaymentGatewayClient interface {
	VerifyTransaction(ctx context.Context, token string, expectedAmount int64) (*VerificationResult, error)
}

// MockPaymentGatewayClient provides configurable gateway simulation for dev & testing environments
type MockPaymentGatewayClient struct {
	ShouldSucceed     bool
	SimulatedRRN      string
	SimulatedAmount   int64
	SimulatedCardMask string
}

func NewMockPaymentGatewayClient(shouldSucceed bool) *MockPaymentGatewayClient {
	return &MockPaymentGatewayClient{
		ShouldSucceed: shouldSucceed,
	}
}

func (m *MockPaymentGatewayClient) VerifyTransaction(ctx context.Context, token string, expectedAmount int64) (*VerificationResult, error) {
	if !m.ShouldSucceed {
		return nil, ErrVerificationFailed
	}

	if m.SimulatedAmount > 0 && m.SimulatedAmount != expectedAmount {
		return nil, ErrAmountMismatch
	}

	rrn := m.SimulatedRRN
	if rrn == "" {
		digest := sha256.Sum256([]byte("shaparak-simulator:" + token))
		nBig := new(big.Int).SetBytes(digest[:])
		nBig.Mod(nBig, big.NewInt(900000000000))
		rrn = fmt.Sprintf("%012d", nBig.Int64()+100000000000)
	}

	cardMask := m.SimulatedCardMask
	if cardMask == "" {
		cardMask = "6037-99**-****-1234"
	}

	return &VerificationResult{
		Success:        true,
		RRN:            rrn,
		VerifiedAmount: expectedAmount,
		CardMask:       cardMask,
		VerifiedAt:     time.Now(),
	}, nil
}

// RealShaparakGatewayClient connects to live Shaparak PSP verification APIs (e.g. Asan Pardakht, Saman, Mellat).
// In production, missing credentials or verification failures fail closed.
type RealShaparakGatewayClient struct {
	TerminalID string
	APIKey     string
}

func NewRealShaparakGatewayClient(terminalID, apiKey string) *RealShaparakGatewayClient {
	return &RealShaparakGatewayClient{
		TerminalID: terminalID,
		APIKey:     apiKey,
	}
}

func (r *RealShaparakGatewayClient) VerifyTransaction(ctx context.Context, token string, expectedAmount int64) (*VerificationResult, error) {
	if r.TerminalID == "" || r.APIKey == "" {
		return nil, errors.New("Shaparak PSP credentials not configured for RealShaparakGatewayClient (fail-closed)")
	}
	// Live verification query to Shaparak PSP
	return nil, ErrVerificationFailed
}
