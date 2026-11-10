package warehouseopt

import (
	"errors"
	"math"
	"sort"
)

// Coordinates are warehouse metres, with Manhattan travel along aisles.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

func (p Point) Valid() bool {
	return !math.IsNaN(p.X+p.Y+p.Z) && !math.IsInf(p.X+p.Y+p.Z, 0) && math.Abs(p.X) <= 100000 && math.Abs(p.Y) <= 100000 && math.Abs(p.Z) <= 100000
}
func Distance(a, b Point) float64 { return math.Abs(a.X-b.X) + math.Abs(a.Y-b.Y) + math.Abs(a.Z-b.Z) }

type Stop struct {
	ID string `json:"binCode"`
	Point
	Items []string `json:"items"`
}
type Route struct {
	Stops         []Stop  `json:"stops"`
	Distance      float64 `json:"distanceMeters"`
	Algorithm     string  `json:"algorithm"`
	ReturnToStart bool    `json:"returnToStart"`
}

// Optimize solves small tours exactly with Held-Karp, and bounds resource use
// for larger batches with nearest-neighbour plus 2-opt (never called optimal).
func Optimize(start Point, stops []Stop) (Route, error) {
	result := Route{Stops: []Stop{}, ReturnToStart: true, Algorithm: "held-karp"}
	if !start.Valid() || len(stops) > 200 {
		return result, errors.New("invalid route or more than 200 bins")
	}
	seen := map[string]bool{}
	for _, s := range stops {
		if s.ID == "" || seen[s.ID] || !s.Point.Valid() {
			return result, errors.New("invalid or duplicate bin")
		}
		seen[s.ID] = true
	}
	if len(stops) == 0 {
		return result, nil
	}
	nodes := append([]Stop(nil), stops...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	n := len(nodes)
	path := []int{}
	if n <= 14 {
		size := (1 << n) * n
		dp := make([]float64, size)
		prev := make([]int, size)
		for i := range dp {
			dp[i] = math.Inf(1)
			prev[i] = -1
		}
		for i := 0; i < n; i++ {
			dp[(1<<i)*n+i] = Distance(start, nodes[i].Point)
		}
		for mask := 1; mask < (1 << n); mask++ {
			for last := 0; last < n; last++ {
				if mask&(1<<last) == 0 {
					continue
				}
				for next := 0; next < n; next++ {
					if mask&(1<<next) != 0 {
						continue
					}
					idx := (mask|(1<<next))*n + next
					v := dp[mask*n+last] + Distance(nodes[last].Point, nodes[next].Point)
					if v < dp[idx] {
						dp[idx] = v
						prev[idx] = last
					}
				}
			}
		}
		mask := (1 << n) - 1
		last := 0
		best := math.Inf(1)
		for i := 0; i < n; i++ {
			v := dp[mask*n+i] + Distance(nodes[i].Point, start)
			if v < best {
				best = v
				last = i
			}
		}
		for mask != 0 {
			path = append(path, last)
			p := prev[mask*n+last]
			mask &^= 1 << last
			last = p
		}
		for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
			path[i], path[j] = path[j], path[i]
		}
	} else {
		result.Algorithm = "nearest-neighbour-2opt"
		used := make([]bool, n)
		p := start
		for len(path) < n {
			best := -1
			d := math.Inf(1)
			for i := 0; i < n; i++ {
				if !used[i] && Distance(p, nodes[i].Point) < d {
					best = i
					d = Distance(p, nodes[i].Point)
				}
			}
			used[best] = true
			path = append(path, best)
			p = nodes[best].Point
		}
		for pass := 0; pass < 20; pass++ {
			improved := false
			for i := 0; i < n-1; i++ {
				for j := i + 1; j < n; j++ {
					a, b := start, start
					if i > 0 {
						a = nodes[path[i-1]].Point
					}
					if j+1 < n {
						b = nodes[path[j+1]].Point
					}
					if Distance(a, nodes[path[j]].Point)+Distance(nodes[path[i]].Point, b)+1e-9 < Distance(a, nodes[path[i]].Point)+Distance(nodes[path[j]].Point, b) {
						for l, r := i, j; l < r; l, r = l+1, r-1 {
							path[l], path[r] = path[r], path[l]
						}
						improved = true
					}
				}
			}
			if !improved {
				break
			}
		}
	}
	p := start
	for _, i := range path {
		result.Stops = append(result.Stops, nodes[i])
		result.Distance += Distance(p, nodes[i].Point)
		p = nodes[i].Point
	}
	result.Distance += Distance(p, start)
	return result, nil
}
