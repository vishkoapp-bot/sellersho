package repository

import (
	"math"
	"testing"
)

func TestFinanceSignedBalanceArithmetic(t *testing.T) {
	for _, tc := range []struct {
		name            string
		credits, debits []int64
		want            int64
		overflow        bool
	}{
		{"refund debt", []int64{10000000}, []int64{10000000, 2000000}, -2000000, false},
		{"signed adjustment", []int64{10000000, -3000000}, []int64{2000000}, 5000000, false},
		{"intermediate overflow cancels", []int64{math.MaxInt64, 1}, []int64{math.MaxInt64}, 1, false},
		{"credit overflow", []int64{math.MaxInt64, 1}, nil, 0, true},
		{"debt overflow", nil, []int64{math.MaxInt64, 2}, 0, true},
		{"minimum signed adjustment", []int64{math.MinInt64}, nil, 0, true},
		{"transport maximum", []int64{9007199254740991}, nil, 9007199254740991, false},
		{"transport debt maximum", nil, []int64{9007199254740991}, -9007199254740991, false},
		{"transport credit overflow", []int64{9007199254740991, 1}, nil, 0, true},
		{"transport debt overflow", nil, []int64{9007199254740991, 1}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := signedBalanceSum(tc.credits, tc.debits)
			if (err != nil) != tc.overflow || got != tc.want {
				t.Fatalf("got %d err=%v want %d overflow=%v", got, err, tc.want, tc.overflow)
			}
		})
	}
}
