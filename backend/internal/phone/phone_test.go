package phone

import "testing"

func TestNormalizeIranianMobile(t *testing.T) {
	for _, value := range []string{"09 12-3456789", "+989123456789", "989123456789", "9123456789"} {
		if got := Normalize(value); got != "09123456789" {
			t.Fatalf("Normalize(%q) = %q", value, got)
		}
	}
	if !IsValid("+989123456789") {
		t.Fatal("expected valid Iranian mobile")
	}
	if IsValid("02112345678") {
		t.Fatal("landline must not be accepted as mobile")
	}
}
