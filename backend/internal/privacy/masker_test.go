package privacy

import (
	"testing"
)

func TestMaskName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"علی رضایی", "ع. ر***"},
		{"سارا محمدی نژاد", "س. ن***"},
		{"رضا", "ر. ض***"},
		{"", "کاربر محترم"},
	}

	for _, c := range cases {
		got := MaskName(c.input)
		if got != c.expected {
			t.Errorf("MaskName(%q) = %q; expected %q", c.input, got, c.expected)
		}
	}
}

func TestMaskPhone(t *testing.T) {
	got := MaskPhone("09121234567")
	expected := "0912***567"
	if got != expected {
		t.Errorf("MaskPhone = %q; expected %q", got, expected)
	}
}

func TestMaskCustomerForRole(t *testing.T) {
	raw := CustomerInfo{
		FullName:   "مریم حسینی",
		Phone:      "09351234567",
		Province:   "تهران",
		City:       "تهران",
		Address:    "خیابان آزادی پلاک ۱۲ واحد ۴",
		PostalCode: "1458923456",
	}

	// 1. Role: seller -> masked
	masked := MaskCustomerForRole(raw, "seller")
	if !masked.IsMasked {
		t.Errorf("Expected IsMasked to be true for seller")
	}
	if masked.FullName != "م. ح***" {
		t.Errorf("Expected masked name 'م. ح***', got %q", masked.FullName)
	}
	if masked.Phone != "0935***567" {
		t.Errorf("Expected masked phone '0935***567', got %q", masked.Phone)
	}

	// 2. Role: warehouse -> unmasked
	unmasked := MaskCustomerForRole(raw, "warehouse")
	if unmasked.IsMasked {
		t.Errorf("Expected IsMasked to be false for warehouse")
	}
	if unmasked.Address != raw.Address {
		t.Errorf("Expected unmasked address, got %q", unmasked.Address)
	}
}
