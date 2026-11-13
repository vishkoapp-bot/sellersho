package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/payment"
	"github.com/drup/platform/internal/repository"
)

// FakeOrderRepository implements PaymentOrderRepository for isolated testing of callback verification
type FakeOrderRepository struct {
	tx            *repository.PaymentTransaction
	order         *domain.Order
	markPaidCalls int
}

func (f *FakeOrderRepository) GetPaymentTransaction(ctx context.Context, gatewayToken string) (*repository.PaymentTransaction, error) {
	if f.tx != nil && f.tx.GatewayToken == gatewayToken {
		return f.tx, nil
	}
	return nil, sql.ErrNoRows
}

func (f *FakeOrderRepository) GetOrderByID(ctx context.Context, id string) (*domain.Order, error) {
	if f.order != nil && f.order.ID == id {
		return f.order, nil
	}
	return nil, sql.ErrNoRows
}

func (f *FakeOrderRepository) RecordPaymentFailure(ctx context.Context, gatewayToken string) error {
	if f.tx != nil && f.tx.GatewayToken == gatewayToken {
		f.tx.Status = "failed"
	}
	return nil
}

func (f *FakeOrderRepository) MarkPaymentPaid(ctx context.Context, gatewayToken, rrn, cardMask string) (*repository.PaymentTransaction, error) {
	f.markPaidCalls++
	f.tx.Status = "paid"
	f.tx.RRN = sql.NullString{String: rrn, Valid: true}
	f.tx.CardMask = sql.NullString{String: cardMask, Valid: true}
	return f.tx, nil
}

// Test 1: Production environment gating logic via HandleShaparakCallbackHTTP
func TestPaymentCallback_ProductionDisabled(t *testing.T) {
	cases := []struct {
		env            string
		expectedStatus int
	}{
		{env: "production", expectedStatus: http.StatusForbidden},
		{env: "staging", expectedStatus: http.StatusForbidden},
		{env: "live", expectedStatus: http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run("Env_"+tc.env, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/payments/shaparak/callback", bytes.NewBuffer([]byte(`{"token":"tok-test"}`)))
			w := httptest.NewRecorder()

			HandleShaparakCallbackHTTP(w, req, tc.env, nil, nil, nil)

			if w.Code != tc.expectedStatus {
				t.Fatalf("expected HTTP %d in env %s, got %d", tc.expectedStatus, tc.env, w.Code)
			}

			var body map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if body["error"] == nil {
				t.Fatalf("expected error message in JSON response for env %s", tc.env)
			}
		})
	}
}

// Test 2: Gateway verification failure returns HTTP 402 with gateway_verification_failed
func TestPaymentCallback_GatewayVerificationFailure(t *testing.T) {
	fakeRepo := &FakeOrderRepository{
		tx: &repository.PaymentTransaction{
			ID:           "pay-1",
			OrderID:      "ord-1",
			OrderNumber:  "ORD-998877",
			GatewayToken: "valid-token-123",
			Amount:       500000,
			Status:       "pending",
			CreatedAt:    time.Now(),
		},
	}
	failingGateway := payment.NewMockPaymentGatewayClient(false) // Verification will fail

	payload := []byte(`{"token":"valid-token-123","orderNumber":"ORD-998877","amount":500000}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments/shaparak/callback", bytes.NewBuffer(payload))
	w := httptest.NewRecorder()

	HandleShaparakCallbackHTTP(w, req, "development", fakeRepo, nil, failingGateway)

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("expected HTTP 402 StatusPaymentRequired, got %d. Body: %s", w.Code, w.Body.String())
	}

	var res map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res["code"] != "gateway_verification_failed" {
		t.Fatalf("expected code 'gateway_verification_failed', got %v", res["code"])
	}
	if fakeRepo.markPaidCalls != 0 {
		t.Fatalf("MarkPaymentPaid must not be called when gateway verification fails")
	}
}

// Test 3: Amount mismatch between callback request and database transaction returns HTTP 400 with amount_mismatch
func TestPaymentCallback_AmountMismatch(t *testing.T) {
	fakeRepo := &FakeOrderRepository{
		tx: &repository.PaymentTransaction{
			ID:           "pay-2",
			OrderID:      "ord-2",
			OrderNumber:  "ORD-112233",
			GatewayToken: "token-mismatch-test",
			Amount:       750000, // DB has 750,000
			Status:       "pending",
			CreatedAt:    time.Now(),
		},
	}
	workingGateway := payment.NewMockPaymentGatewayClient(true)

	// Callback attempts to pass 500,000 instead of 750,000
	payload := []byte(`{"token":"token-mismatch-test","orderNumber":"ORD-112233","amount":500000}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments/shaparak/callback", bytes.NewBuffer(payload))
	w := httptest.NewRecorder()

	HandleShaparakCallbackHTTP(w, req, "development", fakeRepo, nil, workingGateway)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 BadRequest on amount mismatch, got %d. Body: %s", w.Code, w.Body.String())
	}

	var res map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["code"] != "amount_mismatch" {
		t.Fatalf("expected code 'amount_mismatch', got %v", res["code"])
	}
	if fakeRepo.markPaidCalls != 0 {
		t.Fatalf("MarkPaymentPaid must not be called on amount mismatch")
	}
}
