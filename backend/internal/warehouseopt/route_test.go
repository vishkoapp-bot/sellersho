package warehouseopt

import (
	"math"
	"testing"
)

func TestExactTourAndInput(t *testing.T) {
	r, e := Optimize(Point{}, []Stop{{ID: "c", Point: Point{X: 10, Y: 10}}, {ID: "a", Point: Point{X: 10}}, {ID: "b", Point: Point{Y: 10}}})
	if e != nil || r.Distance != 40 || len(r.Stops) != 3 {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = Optimize(Point{X: math.NaN()}, nil); e == nil {
		t.Fatal("accepted NaN")
	}
	if _, e = Optimize(Point{}, []Stop{{ID: "x"}, {ID: "x"}}); e == nil {
		t.Fatal("duplicate")
	}
}
func TestHeuristicVisitsEveryBin(t *testing.T) {
	s := []Stop{}
	for i := 0; i < 40; i++ {
		s = append(s, Stop{ID: string(rune('a' + i)), Point: Point{X: float64(i % 7), Y: float64(i / 7)}})
	}
	r, e := Optimize(Point{}, s)
	if e != nil || r.Algorithm != "nearest-neighbour-2opt" || len(r.Stops) != 40 {
		t.Fatalf("%+v %v", r, e)
	}
	seen := map[string]bool{}
	for _, v := range r.Stops {
		if seen[v.ID] {
			t.Fatal("repeat")
		}
		seen[v.ID] = true
	}
}
