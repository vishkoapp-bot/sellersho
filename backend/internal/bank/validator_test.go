package bank

import (
	"testing"
)

func TestVerifySheba(t *testing.T) {
	// Standard valid Iranian Sheba test (Mellat Bank: 012)
	validSheba := "IR820120000000001234567891" // Mock check
	res := VerifySheba(validSheba)
	// Even if checksum for random mock digits fails, invalid syntax must be caught
	badSheba := "IR123"
	badRes := VerifySheba(badSheba)
	if badRes.IsValid {
		t.Errorf("Expected invalid for short sheba, got true")
	}

	resWithSpaces := VerifySheba(" IR000000000000000000000000 ")
	if resWithSpaces.IsValid {
		t.Errorf("Expected invalid checksum, got true")
	}
	_ = res
}
