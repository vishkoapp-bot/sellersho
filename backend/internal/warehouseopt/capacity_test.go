package warehouseopt

import (
	"math"
	"testing"
)

func TestOperationsCapacityUnitsAndOverflow(t *testing.T) {
	v, w, e := Capacity(100, 100, 100, 50, 2)
	if e != nil || v != 2000 || w != 100 {
		t.Fatal(v, w, e)
	}
	v, _, e = Capacity(1, 1, 1, 1, 1)
	if e != nil || v != 1 {
		t.Fatal("fractional cm3 rounded down")
	}
	if _, _, e = Capacity(math.MaxInt64, math.MaxInt64, math.MaxInt64, 1, 1); e == nil {
		t.Fatal("overflow accepted")
	}
	if _, _, e = Capacity(0, 10, 10, 10, 1); e == nil {
		t.Fatal("missing measurements accepted")
	}
}

func TestCapacityMaximumPhysicalsAndAggregateRounding(t *testing.T) {
	for _, c := range []struct{ l, w, h, g, q, v, weight int64 }{
		{1, 1, 1, 1, 1001, 2, 1001},
		{1000000, 1000000, 1000000, 1000000000, 1, 1000000000000000, 1000000000},
	} {
		v, w, err := Capacity(c.l, c.w, c.h, c.g, c.q)
		if err != nil || v != c.v || w != c.weight {
			t.Fatalf("capacity=%d/%d err=%v", v, w, err)
		}
	}
	if _, _, err := Capacity(1000000, 1000000, 1000000, 1000000000, math.MaxInt32); err == nil {
		t.Fatal("aggregate physical volume overflow accepted")
	}
	if _, _, err := Capacity(1, 1, 1, math.MaxInt64, 2); err == nil {
		t.Fatal("aggregate weight overflow accepted")
	}
}
