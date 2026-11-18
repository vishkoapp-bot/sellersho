package main

import (
	"testing"

	"github.com/drup/platform/internal/repository"
)

// SEC-01 fix (critic F-04): a total Tapin re-encryption failure
// (token rows exist, some failed, none succeeded) must fail the
// boot, so a wrong TAPIN_TOKEN_KEY is visible at deploy time instead
// of passing health while every supplier profile 500s on decrypt.
// Partial failures keep booting (the key is fine; failed rows stay
// untouched for manual handling). Pure decision function — no DB.
func TestTapinReencryptTotalFailure(t *testing.T) {
	cases := []struct {
		name string
		rep  repository.ReencryptReport
		want bool
	}{
		{
			name: "all rows failed, none succeeded (key-level failure)",
			rep:  repository.ReencryptReport{Examined: 2, Failed: 2, FailedIDs: []string{"s1", "s2"}},
			want: true,
		},
		{
			name: "single row failed, nothing else",
			rep:  repository.ReencryptReport{Examined: 1, Failed: 1, FailedIDs: []string{"s1"}},
			want: true,
		},
		{
			name: "partial failure, one row re-encrypted",
			rep:  repository.ReencryptReport{Examined: 2, Encrypted: 1, Failed: 1, FailedIDs: []string{"s2"}},
			want: false,
		},
		{
			name: "partial failure, one row already encrypted",
			rep:  repository.ReencryptReport{Examined: 2, AlreadyEncrypted: 1, Failed: 1, FailedIDs: []string{"s2"}},
			want: false,
		},
		{
			name: "no token rows at all",
			rep:  repository.ReencryptReport{},
			want: false,
		},
		{
			name: "only empty rows examined",
			rep:  repository.ReencryptReport{Examined: 2, SkippedEmpty: 2},
			want: false,
		},
		{
			name: "all rows already encrypted, none failed",
			rep:  repository.ReencryptReport{Examined: 2, AlreadyEncrypted: 2},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tapinReencryptTotalFailure(tc.rep); got != tc.want {
				t.Fatalf("tapinReencryptTotalFailure(%+v) = %v, want %v", tc.rep, got, tc.want)
			}
		})
	}
}
