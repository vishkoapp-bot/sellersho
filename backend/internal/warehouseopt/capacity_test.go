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
