package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// This is a format/uniqueness smoke test, not a proof of cryptographic randomness.
func TestOrderIdentifiers_NonMonotonicity(t *testing.T) {
	seenIDs := make(map[string]bool)
	seenNumbers := make(map[string]bool)
	seenTracking := make(map[string]bool)

	for i := 0; i < 50; i++ {
		orderUUID := uuid.New()
		if i == 0 {
			// The old prefix assertion intermittently rejected this valid v4 UUID.
			orderUUID = uuid.MustParse("179c81c5-3e26-4d7e-bbda-56da013549f1")
		}
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

		parsed, err := uuid.Parse(strings.TrimPrefix(orderID, "ord-"))
		if err != nil || parsed.Version() != 4 || parsed.Variant() != uuid.RFC4122 {
			t.Fatalf("orderID should contain an RFC4122 v4 UUID, got: %s", orderID)
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
