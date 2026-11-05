package tapin

import "testing"

func intPtr(v int) *int { return &v }

func catalogue() []Box {
	return []Box{
		{ID: "4", Title: "سایز ۴", LengthCm: 30, WidthCm: 20, HeightCm: 20, IsActive: true, Sort: 0},
		{ID: "3", Title: "سایز ۳", LengthCm: 20, WidthCm: 20, HeightCm: 15, IsActive: true, Sort: 1},
		{ID: "5", Title: "سایز ۵", LengthCm: 35, WidthCm: 25, HeightCm: 20, IsActive: true, Sort: 2},
		{ID: "9", Title: "غیرفعال", LengthCm: 10, WidthCm: 10, HeightCm: 10, IsActive: false, Sort: 3},
	}
}

func TestPickBoxUnknownDimsReturnsDefault(t *testing.T) {
	pick, oversize, ok := PickBox(catalogue(), 0, 0, 0, 0, "4")
	if !ok || oversize || pick.ID != "4" {
		t.Fatalf("pick = %+v oversize=%v ok=%v", pick, oversize, ok)
	}
	// Any single unknown dimension counts as unknown dims.
	pick, _, ok = PickBox(catalogue(), 25, 0, 10, 0, "5")
	if !ok || pick.ID != "5" {
		t.Fatalf("partial dims pick = %+v ok=%v, want default 5", pick, ok)
	}
}

func TestPickBoxSmallestFittingVolume(t *testing.T) {
	// 18x14x12 fits سایز ۳ (20x20x15) and bigger; smallest wins.
	pick, oversize, ok := PickBox(catalogue(), 18, 14, 12, 0, "4")
	if !ok || oversize || pick.ID != "3" {
		t.Fatalf("pick = %+v oversize=%v ok=%v, want box 3", pick, oversize, ok)
	}
}

func TestPickBoxOrientationFree(t *testing.T) {
	// Parcel 20x30x20 is box 4 rotated — sorted-dims comparison fits it.
	pick, _, ok := PickBox(catalogue(), 20, 30, 20, 0, "4")
	if !ok || pick.ID != "4" {
		t.Fatalf("pick = %+v ok=%v, want box 4", pick, ok)
	}
}

func TestPickBoxHonoursMaxWeight(t *testing.T) {
	boxes := catalogue()
	boxes[1].MaxWeightGrams = intPtr(1000) // سایز ۳ capped at 1kg
	// Parcel fits سایز ۳ dimensionally but weighs 2kg → next fit is ۴.
	pick, _, ok := PickBox(boxes, 18, 14, 12, 2000, "4")
	if !ok || pick.ID != "4" {
		t.Fatalf("pick = %+v ok=%v, want box 4 (weight cap)", pick, ok)
	}
	// Weight unknown (0) → cap not applied.
	pick, _, ok = PickBox(boxes, 18, 14, 12, 0, "4")
	if !ok || pick.ID != "3" {
		t.Fatalf("pick = %+v ok=%v, want box 3 (weight unknown)", pick, ok)
	}
}

func TestPickBoxOversizeFallback(t *testing.T) {
	pick, oversize, ok := PickBox(catalogue(), 90, 80, 70, 0, "4")
	if !ok || !oversize || pick.ID != "5" {
		t.Fatalf("pick = %+v oversize=%v ok=%v, want largest box 5 oversize", pick, oversize, ok)
	}
}

func TestPickBoxTieBreakBySortThenID(t *testing.T) {
	boxes := []Box{
		{ID: "b", Title: "b", LengthCm: 20, WidthCm: 20, HeightCm: 20, IsActive: true, Sort: 2},
		{ID: "a", Title: "a", LengthCm: 20, WidthCm: 20, HeightCm: 20, IsActive: true, Sort: 2},
		{ID: "c", Title: "c", LengthCm: 20, WidthCm: 20, HeightCm: 20, IsActive: true, Sort: 1},
	}
	pick, _, ok := PickBox(boxes, 10, 10, 10, 0, "")
	if !ok || pick.ID != "c" {
		t.Fatalf("pick = %+v, want c (lowest sort)", pick)
	}
	boxes[2].Sort = 5
	pick, _, _ = PickBox(boxes, 10, 10, 10, 0, "")
	if pick.ID != "a" {
		t.Fatalf("pick = %+v, want a (sort tie → lowest id)", pick)
	}
}

func TestPickBoxEmptyOrInactiveCatalogue(t *testing.T) {
	if _, _, ok := PickBox(nil, 10, 10, 10, 0, "4"); ok {
		t.Fatalf("empty catalogue must not pick")
	}
	inactive := []Box{{ID: "4", LengthCm: 30, WidthCm: 20, HeightCm: 20, IsActive: false}}
	if _, _, ok := PickBox(inactive, 10, 10, 10, 0, "4"); ok {
		t.Fatalf("inactive-only catalogue must not pick")
	}
}

func TestPickBoxMissingDefault(t *testing.T) {
	boxes := []Box{{ID: "7", Title: "x", LengthCm: 40, WidthCm: 40, HeightCm: 40, IsActive: true}}
	if _, _, ok := PickBox(boxes, 0, 0, 0, 0, "4"); ok {
		t.Fatalf("unknown dims with missing default must not pick")
	}
}
