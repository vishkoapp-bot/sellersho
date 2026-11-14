package main

import (
	"context"
	"fmt"
	"github.com/drup/platform/internal/config"
	"github.com/lib/pq"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFinanceCORSAllowsIdempotencyHeader(t *testing.T) {
	handler := enableCORS(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("preflight reached business handler") }), &config.Config{Environment: "production", AllowedOrigins: []string{"https://seller.example.test"}})
	req := httptest.NewRequest("OPTIONS", "/api/v1/stores/shop/payouts", nil)
	req.Header.Set("Origin", "https://seller.example.test")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type,idempotency-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != 200 || response.Header().Get("Access-Control-Allow-Origin") != "https://seller.example.test" || !strings.Contains(strings.ToLower(response.Header().Get("Access-Control-Allow-Headers")), "idempotency-key") {
		t.Fatalf("preflight status=%d headers=%v", response.Code, response.Header())
	}
}

func TestFinanceBusyRequestReturnsRetryableStatus(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, context.Canceled, fmt.Errorf("wrapped: %w", &pq.Error{Code: "55P03"}), &pq.Error{Code: "40001"}, &pq.Error{Code: "40P01"}} {
		w := httptest.NewRecorder()
		sellerError(w, err)
		if w.Code != 503 || w.Header().Get("Retry-After") == "" {
			t.Fatalf("retry status=%d", w.Code)
		}
	}
}
