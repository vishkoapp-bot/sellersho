package main

import (
	"context"
	"github.com/drup/platform/internal/payment"
	"strings"
	"testing"
)

func TestCapacityAdaptersDefaultsPreserved(t *testing.T) {
	original := payment.NewMockPaymentGatewayClient(true)
	client, carrier, err := selectCapacityAdapters(func(string) string { return "" }, original)
	if err != nil || client != original || carrier != "" {
		t.Fatalf("ordinary selection changed: %v", err)
	}
}
func TestCapacityAdaptersGuardedSelection(t *testing.T) {
	env := map[string]string{"APP_ENV": "test", "SELLERSHO_CAPACITY_RUN": "1234567890abcdef12345678", "CAPACITY_PAYMENT_MODE": "success", "CAPACITY_PAYMENT_LATENCIES_MS": "0", "CAPACITY_CARRIER_BASE_URL": "http://carrier:8082"}
	original := payment.NewMockPaymentGatewayClient(false)
	client, carrier, err := selectCapacityAdapters(func(k string) string { return env[k] }, original)
	if err != nil || client == original || carrier != "http://carrier:8082" {
		t.Fatalf("guarded selection: %v", err)
	}
	got, err := client.VerifyTransaction(context.Background(), "synthetic-token", 123200)
	if err != nil || got.VerifiedAmount != 123200 {
		t.Fatalf("bounded in-process verification: %v", err)
	}
	for _, key := range []string{"APP_ENV", "SELLERSHO_CAPACITY_RUN"} {
		saved := env[key]
		env[key] = "production"
		if _, _, err := selectCapacityAdapters(func(k string) string { return env[k] }, original); err == nil {
			t.Fatalf("missing guard %s accepted", key)
		}
		env[key] = saved
	}
}
func TestCapacityAdaptersRejectExternalAndCredentialEndpoints(t *testing.T) {
	original := payment.NewMockPaymentGatewayClient(true)
	for _, endpoint := range []string{"https://api.tapin.ir", "http://127.0.0.1:8082", "http://localhost:8082", "http://private:secret@carrier:8082", "http://carrier:8082/path", "http://carrier:8082?secret=value", "http://carrier:8082#fragment", "http://carrier:8082/", "http://carrier:8083", "http://carrier:8082@evil.example"} {
		env := map[string]string{"APP_ENV": "test", "SELLERSHO_CAPACITY_RUN": "1234567890abcdef12345678", "CAPACITY_PAYMENT_MODE": "success", "CAPACITY_PAYMENT_LATENCIES_MS": "0", "CAPACITY_CARRIER_BASE_URL": endpoint}
		client, carrier, err := selectCapacityAdapters(func(k string) string { return env[k] }, original)
		if err == nil || client != nil || carrier != "" {
			t.Fatal("unsafe carrier selection was accepted or partially applied")
		}
		if strings.Contains(err.Error(), endpoint) || strings.Contains(err.Error(), "secret") {
			t.Fatal("rejection echoed endpoint credentials")
		}
	}
}
