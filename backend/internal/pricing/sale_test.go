package pricing

import (
	"testing"
	"time"
)

func saleRes(finalPrice, baseCost, originalPrice int64) ReferenceResolution {
	return ReferenceResolution{
		FinalPrice:   finalPrice,
		BaseCost:     baseCost,
		OriginalPrice: originalPrice,
		SellerMargin: finalPrice - baseCost,
	}
}

func TestApplyVariantSale(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	i64 := func(v int64) *int64 { return &v }
	tm := func(v time.Time) *time.Time { return &v }

	// 1. Active sale between R and P: Price == S, OriginalPrice kept.
	res, on := ApplyVariantSale(saleRes(1000000, 800000, 1100000), i64(900000), now, nil, nil)
	if !on || res.FinalPrice != 900000 || res.OriginalPrice != 1100000 || res.SellerMargin != 100000 {
		t.Fatalf("active sale: on=%v res=%+v", on, res)
	}
	// 2. Sale >= P: no-op.
	if _, on := ApplyVariantSale(saleRes(1000000, 800000, 1100000), i64(1000000), now, nil, nil); on {
		t.Fatalf("sale >= P must be a no-op")
	}
	if _, on := ApplyVariantSale(saleRes(1000000, 800000, 1100000), i64(1200000), now, nil, nil); on {
		t.Fatalf("sale > P must be a no-op")
	}
	// 3. Sale below R: suspended, not clamped.
	res, on = ApplyVariantSale(saleRes(1000000, 800000, 1100000), i64(700000), now, nil, nil)
	if on || res.FinalPrice != 1000000 {
		t.Fatalf("sale < R must suspend: on=%v res=%+v", on, res)
	}
	// 4. Window: before from inactive; at from active; at to inactive; open-ended.
	if _, on := ApplyVariantSale(saleRes(1000000, 800000, 1100000), i64(900000), now, tm(future), nil); on {
		t.Fatalf("before saleFrom must be inactive")
	}
	if _, on := ApplyVariantSale(saleRes(1000000, 800000, 1100000), i64(900000), past, tm(past), tm(future)); !on {
		t.Fatalf("at saleFrom must be active")
	}
	if _, on := ApplyVariantSale(saleRes(1000000, 800000, 1100000), i64(900000), future, tm(past), tm(future)); on {
		t.Fatalf("at saleTo must be inactive ([from, to))")
	}
	if _, on := ApplyVariantSale(saleRes(1000000, 800000, 1100000), i64(900000), now, tm(past), nil); !on {
		t.Fatalf("null saleTo must be open-ended")
	}
	if _, on := ApplyVariantSale(saleRes(1000000, 800000, 1100000), i64(900000), now, nil, tm(future)); !on {
		t.Fatalf("null saleFrom must mean already started")
	}
	// 5. Nil sale: no-op.
	if _, on := ApplyVariantSale(saleRes(1000000, 800000, 1100000), nil, now, nil, nil); on {
		t.Fatalf("nil sale must be a no-op")
	}
	// 6. Sale equal to R: allowed (final == R, margin 0).
	res, on = ApplyVariantSale(saleRes(1000000, 800000, 1100000), i64(800000), now, nil, nil)
	if !on || res.FinalPrice != 800000 || res.SellerMargin != 0 {
		t.Fatalf("sale == R: on=%v res=%+v", on, res)
	}
}
