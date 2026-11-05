package repository

import (
	"testing"

	"github.com/drup/platform/internal/pricing"
)

// TestDecidePriceIncreaseMatrix pins the recorded decision matrix
// (migrations 202610310003 + 202610310006) at the pure-function level.
func TestDecidePriceIncreaseMatrix(t *testing.T) {
	unlocked := pricing.DefaultPlatformPricingSettings()
	unlocked.IncreaseLocked = false
	locked := pricing.DefaultPlatformPricingSettings() // locked, cap 10
	grant30 := 30.0

	cases := []struct {
		name       string
		settings   *pricing.PlatformPricingSettings
		grantCap   *float64
		agreement  bool
		pct        float64
		wantApply  bool
		wantReason string
		wantCap    float64
		wantExc    bool
	}{
		{"grant under cap while locked applies", locked, &grant30, false, 20, true, "", 30, true},
		{"grant under cap beats agreement", locked, &grant30, true, 20, true, "", 30, true},
		{"grant over cap queues as exception_cap", locked, &grant30, false, 50, false, "exception_cap", 30, true},
		{"agreement queues at any pct", unlocked, nil, true, 1, false, "agreement", 10, false},
		{"locked queues below cap", locked, nil, false, 5, false, "locked", 10, false},
		{"locked queues above cap as locked", locked, nil, false, 50, false, "locked", 10, false},
		{"unlocked below cap auto-applies", unlocked, nil, false, 5, true, "", 10, false},
		{"unlocked at cap auto-applies", unlocked, nil, false, 10, true, "", 10, false},
		{"unlocked above cap queues", unlocked, nil, false, 50, false, "above_cap", 10, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := decidePriceIncrease(tc.settings, tc.grantCap, tc.agreement, tc.pct)
			if d.Apply != tc.wantApply || d.Reason != tc.wantReason || d.CapPct != tc.wantCap || d.IsException != tc.wantExc {
				t.Fatalf("decide(%v, grant=%v, agreement=%v, pct=%v) = %+v, want apply=%v reason=%q cap=%v exc=%v",
					tc.settings.IncreaseLocked, tc.grantCap, tc.agreement, tc.pct, d, tc.wantApply, tc.wantReason, tc.wantCap, tc.wantExc)
			}
		})
	}
}

// TestPriceIncreaseFailClosedDefaults: the fallback used when the
// settings row cannot be loaded must be the schema defaults — locked
// with a 10% cap — so a settings failure can never auto-apply an
// increase.
func TestPriceIncreaseFailClosedDefaults(t *testing.T) {
	s := pricing.DefaultPlatformPricingSettings()
	if !s.IncreaseLocked || s.IncreaseCapPct != 10 {
		t.Fatalf("schema defaults drifted: locked=%v cap=%v", s.IncreaseLocked, s.IncreaseCapPct)
	}
	d := decidePriceIncrease(s, nil, false, 1)
	if d.Apply || d.Reason != "locked" {
		t.Fatalf("fail-closed default must queue even a 1%% increase as locked, got %+v", d)
	}
}
