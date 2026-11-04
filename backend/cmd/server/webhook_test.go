package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drup/platform/internal/repository"
)

func TestWebhookTrackingUpdate_AuthenticationGuard(t *testing.T) {
	expectedSecret := "secret_tracking_webhook_guard_2026"

	cases := []struct {
		name           string
		headerSecret   string
		expectedStatus int
	}{
		{
			name:           "missing X-Webhook-Secret header",
			headerSecret:   "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid X-Webhook-Secret header",
			headerSecret:   "wrong-secret-token",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte(`{"trackingNumber":"TRK-123456","status":"delivered"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/tracking/update", bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			if tc.headerSecret != "" {
				req.Header.Set("X-Webhook-Secret", tc.headerSecret)
			}

			w := httptest.NewRecorder()
			handleWebhookTrackingUpdate(w, req, context.Background(), nil, nil, expectedSecret)

			if w.Code != tc.expectedStatus {
				t.Fatalf("expected HTTP status %d, got %d. Body: %s", tc.expectedStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestConfirmOrderDelivery_EmptyCustomerUserIDFails(t *testing.T) {
	// ConfirmOrderDelivery with empty customerUserID must fail immediately (fail-closed)
	repo := &repository.OrderRepository{}
	err := repo.ConfirmOrderDelivery(context.Background(), "ord-123", "")
	if err == nil {
		t.Fatal("expected error when confirming order with empty customerUserID, got nil")
	}
}
