package tapin

import "sort"

// Box is one entry of the platform-managed postal-box catalogue
// (table tapin_boxes, migration 202611050008). Tapin's API exposes
// no boxes endpoint (verified in the capture), so the catalogue is
// ours: admins maintain it, and PickBox chooses from it.
type Box struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	LengthCm       int    `json:"lengthCm"`
	WidthCm        int    `json:"widthCm"`
	HeightCm       int    `json:"heightCm"`
	MaxWeightGrams *int   `json:"maxWeightGrams,omitempty"`
	IsActive       bool   `json:"isActive"`
	Sort           int    `json:"sort"`
}

// VolumeCm3 is the box's internal volume proxy for comparisons.
func (b Box) VolumeCm3() int64 {
	return int64(b.LengthCm) * int64(b.WidthCm) * int64(b.HeightCm)
}

func sortedDims(a, b, c int) [3]int {
	d := []int{a, b, c}
	sort.Ints(d)
	return [3]int{d[0], d[1], d[2]}
}

// fits reports whether a parcel of the given dims (any orientation)
// and weight fits the box: the component-wise comparison of sorted
// dims, plus the box's optional max weight.
func (b Box) fits(lengthCm, widthCm, heightCm, weightGrams int) bool {
	bd := sortedDims(b.LengthCm, b.WidthCm, b.HeightCm)
	pd := sortedDims(lengthCm, widthCm, heightCm)
	if pd[0] > bd[0] || pd[1] > bd[1] || pd[2] > bd[2] {
		return false
	}
	if weightGrams > 0 && b.MaxWeightGrams != nil && weightGrams > *b.MaxWeightGrams {
		return false
	}
	return true
}

// PickBox chooses the catalogue box for a parcel.
//
// Rules (owner's recorded box rule — nearest fit, configured
// default as the fallback):
//   - Only active boxes participate.
//   - Unknown parcel dims (any dimension <= 0): the default box.
//   - Otherwise the smallest-volume box the parcel fits (dims in any
//     orientation; max weight honoured when set). Ties break by
//     lower Sort, then lower ID, so the pick is deterministic.
//   - Nothing fits: the largest-volume active box, flagged oversize
//     — better a too-big box than no box, and the flag lets callers
//     warn.
//   - No active boxes at all, or the default is missing when needed:
//     ok is false and the caller falls back to the platform default
//     id semantics ('4').
func PickBox(catalogue []Box, lengthCm, widthCm, heightCm, weightGrams int, defaultID string) (pick Box, oversize bool, ok bool) {
	active := make([]Box, 0, len(catalogue))
	for _, b := range catalogue {
		if b.IsActive {
			active = append(active, b)
		}
	}
	if len(active) == 0 {
		return Box{}, false, false
	}
	if defaultID == "" {
		defaultID = "4"
	}
	byPreference := func(boxes []Box, i, j int) bool {
		vi, vj := boxes[i].VolumeCm3(), boxes[j].VolumeCm3()
		if vi != vj {
			return vi < vj
		}
		if boxes[i].Sort != boxes[j].Sort {
			return boxes[i].Sort < boxes[j].Sort
		}
		return boxes[i].ID < boxes[j].ID
	}
	if lengthCm <= 0 || widthCm <= 0 || heightCm <= 0 {
		for _, b := range active {
			if b.ID == defaultID {
				return b, false, true
			}
		}
		return Box{}, false, false
	}
	fitting := make([]Box, 0, len(active))
	for _, b := range active {
		if b.fits(lengthCm, widthCm, heightCm, weightGrams) {
			fitting = append(fitting, b)
		}
	}
	if len(fitting) > 0 {
		sort.SliceStable(fitting, func(i, j int) bool { return byPreference(fitting, i, j) })
		return fitting[0], false, true
	}
	sort.SliceStable(active, func(i, j int) bool {
		vi, vj := active[i].VolumeCm3(), active[j].VolumeCm3()
		if vi != vj {
			return vi > vj // largest first for the oversize fallback
		}
		return byPreference(active, i, j)
	})
	return active[0], true, true
}
