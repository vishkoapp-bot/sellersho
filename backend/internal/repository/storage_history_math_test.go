package repository

import (
	"testing"

	"github.com/drup/platform/internal/pricing"
)

func TestFIN006StorageRatePreservesWholeTomanHalfUp(t *testing.T) {
	// Two half-Toman components round once to1, not independently to2.
	if got, err := storageFeeUnitDayToman(1, 1, 1, 1, StorageFeeSettings{PerKgDay: 500, PerM3Day: 500000000}); err != nil || got != 1 {
		t.Fatalf("combined half-components=%d want1 err=%v", got, err)
	}
	// This boundary is exactly representable as integer Toman but a float sum
	// loses the half before the legacy second rounding operation.
	want := pricing.MaxTransportMoney
	if got, err := storageFeeUnitDayToman(1000, 1, 1, 1, StorageFeeSettings{PerKgDay: want - 1, PerM3Day: 500000000}); err != nil || got != want {
		t.Fatalf("whole-Toman half-up=%d want%d err=%v", got, want, err)
	}
}

func TestFIN006StorageRateRejectsUnknownNegativeAndOverflow(t *testing.T) {
	for _, c := range []struct {
		g, l, w, h int
		settings   StorageFeeSettings
	}{
		{0, 1, 1, 1, DefaultStorageFeeSettings()},
		{1, 0, 1, 1, DefaultStorageFeeSettings()},
		{1, 1, 1, 1, StorageFeeSettings{PerKgDay: -1}},
		{1, 1, 1, 1, StorageFeeSettings{PerM3Day: -1}},
		{1000000000, 1000000, 1000000, 1000000, StorageFeeSettings{PerKgDay: pricing.MaxTransportMoney, PerM3Day: pricing.MaxTransportMoney}},
	} {
		if _, err := storageFeeUnitDayToman(c.g, c.l, c.w, c.h, c.settings); err == nil {
			t.Fatalf("unsafe fee tuple accepted: %+v", c)
		}
	}
	if got, err := storageFeeUnitDayToman(1500, 500, 400, 300, DefaultStorageFeeSettings()); err != nil || got != 3480 {
		t.Fatalf("default unit rate=%d err=%v", got, err)
	}
	if got, err := storageFeeUnitDayToman(1, 1, 1, 1, StorageFeeSettings{}); err != nil || got != 0 {
		t.Fatalf("zero tariff=%d err=%v", got, err)
	}
}
