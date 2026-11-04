package repository

import (
	"testing"
	"time"
)

func TestEffectiveSettlementDay(t *testing.T) {
	cases := []struct {
		day   int
		year  int
		month time.Month
		want  int
	}{
		{10, 2026, time.November, 10},
		{31, 2026, time.April, 30},
		{31, 2026, time.February, 28},
		{29, 2024, time.February, 29},
		{31, 2026, time.January, 31},
		{1, 2026, time.November, 1},
	}
	for _, c := range cases {
		if got := effectiveSettlementDay(c.day, c.year, c.month); got != c.want {
			t.Errorf("effectiveSettlementDay(%d, %d, %s) = %d, want %d", c.day, c.year, c.month, got, c.want)
		}
	}
}

func TestEarlyWithdrawalConstants(t *testing.T) {
	if EarlyWithdrawalMinimumToman != 1000000 {
		t.Errorf("EarlyWithdrawalMinimumToman = %d, want 1000000", EarlyWithdrawalMinimumToman)
	}
	if EarlyWithdrawalSLAHours != 24 {
		t.Errorf("EarlyWithdrawalSLAHours = %d, want 24", EarlyWithdrawalSLAHours)
	}
}

func TestEarlyWithdrawalFillDeadline(t *testing.T) {
	created := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	pending := EarlyWithdrawal{Status: "pending", CreatedAt: created}
	pending.fillDeadline()
	if pending.DeadlineAt == nil || !pending.DeadlineAt.Equal(created.Add(24*time.Hour)) {
		t.Errorf("pending deadline = %v, want %v", pending.DeadlineAt, created.Add(24*time.Hour))
	}
	approved := EarlyWithdrawal{Status: "approved", CreatedAt: created}
	approved.fillDeadline()
	if approved.DeadlineAt != nil {
		t.Errorf("approved deadline = %v, want nil", approved.DeadlineAt)
	}
}
