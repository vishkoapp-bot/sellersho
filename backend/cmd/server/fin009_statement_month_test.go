package main

import (
	"testing"
	"time"
)

// FIN-009 G-11 (D-04): the default statement month derives from
// Asia/Tehran, not UTC and not server-local time. Q-02 answered by the
// owner Nov 19: TEHRAN — the statement period is a Tehran calendar
// month (time.ParseInLocation("2006-01", ..., Asia/Tehran) in the
// repository defines the window), so the default must be Tehran too.
// Tehran is UTC+03:30 year-round, no DST since 2022.
//
// Test name retained as TestFIN009DefaultStatementMonthIsUTC for
// continuity with the round-1 fix (where the default was UTC pending
// Q-02); only the expectations are re-scoped to Tehran.
func TestFIN009DefaultStatementMonthIsUTC(t *testing.T) {
	tehran := time.FixedZone("Asia/Tehran", 3*3600+1800)
	// 2026-11-01 00:30 Tehran == 2026-10-31 21:00 UTC: the handler
	// default must be the Tehran month (2026-11), not the UTC month.
	if got := defaultStatementMonth(time.Date(2026, 11, 1, 0, 30, 0, 0, tehran)); got != "2026-11" {
		t.Errorf("defaultStatementMonth(2026-11-01 00:30 Tehran)=%q, want 2026-11 (Tehran month)", got)
	}
	// 2026-11-01 04:00 Tehran == 2026-11-01 00:30 UTC: November in both.
	if got := defaultStatementMonth(time.Date(2026, 11, 1, 4, 0, 0, 0, tehran)); got != "2026-11" {
		t.Errorf("defaultStatementMonth(2026-11-01 04:00 Tehran)=%q, want 2026-11", got)
	}
	// 2026-10-31 23:30 UTC == 2026-11-01 03:00 Tehran: November.
	if got := defaultStatementMonth(time.Date(2026, 10, 31, 23, 30, 0, 0, time.UTC)); got != "2026-11" {
		t.Errorf("defaultStatementMonth(2026-10-31 23:30 UTC)=%q, want 2026-11 (03:00 Tehran)", got)
	}
	// 2026-10-31 20:00 UTC == 2026-10-31 23:30 Tehran: October.
	if got := defaultStatementMonth(time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC)); got != "2026-10" {
		t.Errorf("defaultStatementMonth(2026-10-31 20:00 UTC)=%q, want 2026-10 (23:30 Tehran)", got)
	}
	// A UTC instant on the 1st at 00:00 is 03:30 Tehran: November.
	if got := defaultStatementMonth(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); got != "2026-11" {
		t.Errorf("defaultStatementMonth(2026-11-01 00:00 UTC)=%q, want 2026-11", got)
	}
}
