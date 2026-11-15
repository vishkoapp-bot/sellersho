package repository

import (
	"time"
	_ "time/tzdata"

	"github.com/drup/platform/internal/pricing"
)

const storageHistoryPolicy = "storage_calendar_v2"

type storageQuantityDeparture struct {
	Quantity int64
	At       time.Time
}

// One known receipt cohort. HeldFrom changes for a transfer destination while
// BillingStart and the frozen policy/rate retain the original receipt lineage.
// Departures are immutable physical events, including those of closed cohorts.
type storageReceiptHistory struct {
	Quantity     int64
	HeldFrom     time.Time
	BillingStart time.Time
	FreeDays     int
	UnitDay      int64
	Departures   []storageQuantityDeparture
}

// Day labels use the existing UTC DATE representation. Their actual boundary
// belongs to Tehran, not the host/container timezone. The caller assesses only
// completed calendar days and persists the unique cohort/day charge atomically.
func storageReceiptDayAmount(h storageReceiptHistory, day time.Time) (quantity, amount int64, err error) {
	if h.Quantity <= 0 || h.Quantity > 2147483647 || h.HeldFrom.IsZero() || h.BillingStart.IsZero() || h.FreeDays < 0 || h.FreeDays > 36500 || h.UnitDay < 0 || h.UnitDay > pricing.MaxTransportMoney {
		return 0, 0, pricing.ErrInvalidMoney
	}
	label := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	boundary := time.Date(label.Year(), label.Month(), label.Day(), 0, 0, 0, 0, storageFeeTehranLoc)
	remaining, departed := h.Quantity, int64(0)
	for _, event := range h.Departures {
		if event.Quantity <= 0 || event.Quantity > h.Quantity-departed || event.At.IsZero() || event.At.Before(h.HeldFrom) {
			return 0, 0, pricing.ErrInvalidMoney
		}
		departed += event.Quantity
		if !event.At.After(boundary) {
			remaining -= event.Quantity
		}
	}
	// Free-window and custody checks apply after validating the entire history;
	// corrupt histories must not advance a supposedly free/zero-fee checkpoint.
	if boundary.Before(h.HeldFrom) || label.Before(tehranDay(h.BillingStart).AddDate(0, 0, h.FreeDays)) {
		return 0, 0, nil
	}
	if err = pricing.AccumulateMoney(&amount, h.UnitDay, remaining); err != nil {
		return 0, 0, err
	}
	return remaining, amount, nil
}
