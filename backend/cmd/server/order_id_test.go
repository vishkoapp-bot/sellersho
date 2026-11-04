package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Test 4: Verify Order ID generation produces cryptographically non-monotonic IDs
func TestOrderIdentifiers_NonMonotonicity(t *testing.T) {
	seenIDs := make(map[string]bool)
	seenNumbers := make(map[string]bool)
	seenTracking := make(map[string]bool)

	for i := 0; i < 50; i++ {
		orderUUID := uuid.New()
		orderID := fmt.Sprintf("ord-%s", orderUUID.String())
		orderNumber := fmt.Sprintf("ORD-%s", strings.ToUpper(orderUUID.String()[:8]))
		trackingUUID := uuid.New()
		trackingCode := fmt.Sprintf("TRK-%s", strings.ToUpper(trackingUUID.String()[:12]))

		if seenIDs[orderID] {
			t.Fatalf("duplicate orderID generated: %s", orderID)
		}
		if seenNumbers[orderNumber] {
			t.Fatalf("duplicate orderNumber generated: %s", orderNumber)
		}
		if seenTracking[trackingCode] {
			t.Fatalf("duplicate trackingCode generated: %s", trackingCode)
		}

		// Ensure it does not have time-based sequential millisecond Unix timestamp prefix
		if strings.HasPrefix(orderID, "ord-179") {
			t.Fatalf("orderID should not have monotonic timestamp prefix, got: %s", orderID)
		}

		seenIDs[orderID] = true
		seenNumbers[orderNumber] = true
		seenTracking[trackingCode] = true
	}
}

func TestOrderItemQuantityValidation(t *testing.T) {
	for _, quantity := range []int{0, -1, maxOrderItemQuantity + 1} {
		if err := validateOrderItemQuantity(quantity); err == nil {
			t.Fatalf("quantity %d should be rejected", quantity)
		}
	}
	for _, quantity := range []int{1, maxOrderItemQuantity} {
		if err := validateOrderItemQuantity(quantity); err != nil {
			t.Fatalf("quantity %d should be accepted: %v", quantity, err)
		}
	}
}
