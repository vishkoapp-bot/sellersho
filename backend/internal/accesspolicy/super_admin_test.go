package accesspolicy

import "testing"

func TestIsSuperAdminMobileUsesNormalizedExactAllowlist(t *testing.T) {
	tests := []struct {
		name   string
		mobile string
		want   bool
	}{
		{name: "first exact number", mobile: "09933070870", want: true},
		{name: "second exact number", mobile: "09127504005", want: true},
		{name: "international first number", mobile: "+989933070870", want: true},
		{name: "formatted second number", mobile: "09127-504-005", want: true},
		{name: "ordinary mobile", mobile: "09120000000", want: false},
		{name: "near miss", mobile: "09933070871", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSuperAdminMobile(tt.mobile); got != tt.want {
				t.Fatalf("IsSuperAdminMobile(%q) = %v, want %v", tt.mobile, got, tt.want)
			}
		})
	}
}
