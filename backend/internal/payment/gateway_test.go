package payment

import (
	"context"
	"testing"
)

func TestMockPaymentGatewayClient_Success(t *testing.T) {
	client := NewMockPaymentGatewayClient(true)
	res, err := client.VerifyTransaction(context.Background(), "token-valid-123", 500000)
	if err != nil {
		t.Fatalf("expected successful verification, got error: %v", err)
	}
	if res == nil || !res.Success {
		t.Fatalf("expected res.Success to be true")
	}
	if res.RRN == "" {
		t.Errorf("expected generated RRN")
	}
	if res.VerifiedAmount != 500000 {
		t.Errorf("expected amount 500000, got %d", res.VerifiedAmount)
	}
}

func TestMockPaymentGatewayClient_Failure(t *testing.T) {
	client := NewMockPaymentGatewayClient(false)
	res, err := client.VerifyTransaction(context.Background(), "token-failed-123", 500000)
	if err == nil {
		t.Fatalf("expected error from failed gateway client, got nil")
	}
	if res != nil {
		t.Fatalf("expected nil result on failure")
	}
}

func TestMockPaymentGatewayClient_AmountMismatch(t *testing.T) {
	client := &MockPaymentGatewayClient{
		ShouldSucceed:   true,
		SimulatedAmount: 600000, // Gateway saw 600000
	}
	res, err := client.VerifyTransaction(context.Background(), "token-123", 500000) // Expected 500000
	if err != ErrAmountMismatch {
		t.Fatalf("expected ErrAmountMismatch, got: %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil result on mismatch")
	}
}

func TestFIN003SimulatorVerificationInquiryHasStableReference(t *testing.T) {
	g := NewMockPaymentGatewayClient(true)
	first, e := g.VerifyTransaction(context.Background(), "same-payment-token", 10000000)
	if e != nil {
		t.Fatal(e)
	}
	second, e := g.VerifyTransaction(context.Background(), "same-payment-token", 10000000)
	if e != nil {
		t.Fatal(e)
	}
	if first.RRN != second.RRN {
		t.Fatalf("inquiry invented a second payment reference %s != %s", first.RRN, second.RRN)
	}
}
