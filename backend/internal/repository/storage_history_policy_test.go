package repository

import (
	"testing"
	"time"
)

func TestFIN006ReceiptHistoryKeepsClosedAndPartialQuantityDays(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, storageFeeTehranLoc)
	day := func(n int) time.Time { return tehranDay(base.AddDate(0, 0, n)) }
	cases := []struct {
		name string
		h    storageReceiptHistory
		want int64
	}{
		{"one old unit", storageReceiptHistory{Quantity: 1, HeldFrom: base, BillingStart: base, FreeDays: 30, UnitDay: 3480}, 34800},
		{"nine fresh units", storageReceiptHistory{Quantity: 9, HeldFrom: base.AddDate(0, 0, 40), BillingStart: base.AddDate(0, 0, 40), FreeDays: 30, UnitDay: 3480}, 0},
		{"closed today", storageReceiptHistory{Quantity: 1, HeldFrom: base, BillingStart: base, FreeDays: 30, UnitDay: 3480, Departures: []storageQuantityDeparture{{Quantity: 1, At: base.AddDate(0, 0, 40)}}}, 34800},
		{"half departed at day35 boundary", storageReceiptHistory{Quantity: 10, HeldFrom: base, BillingStart: base, FreeDays: 30, UnitDay: 3480, Departures: []storageQuantityDeparture{{Quantity: 5, At: base.AddDate(0, 0, 35)}}}, 261000},
		{"half departed during day35", storageReceiptHistory{Quantity: 10, HeldFrom: base, BillingStart: base, FreeDays: 30, UnitDay: 3480, Departures: []storageQuantityDeparture{{Quantity: 5, At: base.AddDate(0, 0, 35).Add(12 * time.Hour)}}}, 278400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var total int64
			for n := 0; n < 40; n++ {
				_, amount, err := storageReceiptDayAmount(c.h, day(n))
				if err != nil {
					t.Fatal(err)
				}
				total += amount
			}
			if total != c.want {
				t.Fatalf("receipt quantity-day total=%d want%d", total, c.want)
			}
		})
	}
}

func TestFIN006TransferKeepsAgeAndHasOneDayBoundaryOwner(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, storageFeeTehranLoc)
	transfer := base.AddDate(0, 0, 35).Add(12 * time.Hour)
	source := storageReceiptHistory{Quantity: 1, HeldFrom: base, BillingStart: base, FreeDays: 30, UnitDay: 3480, Departures: []storageQuantityDeparture{{Quantity: 1, At: transfer}}}
	destination := storageReceiptHistory{Quantity: 1, HeldFrom: transfer, BillingStart: base, FreeDays: 30, UnitDay: 3480}
	for n := 30; n < 40; n++ {
		date := tehranDay(base.AddDate(0, 0, n))
		_, from, err := storageReceiptDayAmount(source, date)
		if err != nil {
			t.Fatal(err)
		}
		_, to, err := storageReceiptDayAmount(destination, date)
		if err != nil {
			t.Fatal(err)
		}
		if from+to != 3480 {
			t.Fatalf("transfer day%d fee=%d+%d want3480", n, from, to)
		}
	}
}

func TestFIN006UnknownOrImpossibleHistoryCannotProduceDebt(t *testing.T) {
	if storageFeeTehranLoc.String() != "Asia/Tehran" {
		t.Fatal("storage billing silently fell back from Tehran")
	}
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, storageFeeTehranLoc)
	for _, h := range []storageReceiptHistory{
		{Quantity: 0, HeldFrom: now, BillingStart: now, UnitDay: 1},
		{Quantity: 1, HeldFrom: now, BillingStart: now, UnitDay: -1},
		{Quantity: 1, HeldFrom: now, BillingStart: now, FreeDays: -1},
		{Quantity: 1, HeldFrom: now, BillingStart: now, Departures: []storageQuantityDeparture{{Quantity: 2, At: now}}},
		{Quantity: 1, HeldFrom: now, BillingStart: now, Departures: []storageQuantityDeparture{{Quantity: 1, At: now.Add(-time.Hour)}}},
	} {
		if _, _, err := storageReceiptDayAmount(h, tehranDay(now)); err == nil {
			t.Fatalf("invalid receipt history accepted: %+v", h)
		}
	}
}
