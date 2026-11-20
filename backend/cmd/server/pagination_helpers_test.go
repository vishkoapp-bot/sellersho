package main

// PERF-02 Step 13: the single invalid-limit policy (Step 11 contract §1)
// at the HTTP layer: non-integer/negative -> 400, over-max -> clamp to 100
// with the effective limit echoed, missing/0 -> default 50, bad cursor is
// carried through for the repository to reject.
import (
	"net/http/httptest"
	"testing"
)

func TestPerf02ParseListPageMatrix(t *testing.T) {
	cases := []struct {
		query   string
		want    int
		wantErr bool
	}{
		{"", 50, false},
		{"limit=0", 50, false},
		{"limit=1", 1, false},
		{"limit=50", 50, false},
		{"limit=100", 100, false},
		{"limit=101", 100, false},
		{"limit=1000000", 100, false},
		{"limit=-1", 0, true},
		{"limit=abc", 0, true},
		{"limit=1.5", 0, true},
		{"limit=", 50, false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/api/v1/orders?"+tc.query, nil)
		opts, err := parseListPage(req)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseListPage(%q) = no error, want 400", tc.query)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseListPage(%q) = error %v, want limit %d", tc.query, err, tc.want)
			continue
		}
		if opts.Limit != tc.want {
			t.Errorf("parseListPage(%q) limit = %d, want %d", tc.query, opts.Limit, tc.want)
		}
	}
}

func TestPerf02ParseListPageCursorPassthrough(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/orders?limit=25&cursor=YWJj", nil)
	opts, err := parseListPage(req)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Limit != 25 || opts.Cursor != "YWJj" {
		t.Fatalf("parseListPage = %+v, want limit 25 cursor YWJj", opts)
	}
	// A malformed cursor is not rejected here — the repository returns an
	// "invalid cursor" error and the handler maps it to 400.
	req = httptest.NewRequest("GET", "/api/v1/orders?cursor=!!!", nil)
	opts, err = parseListPage(req)
	if err != nil {
		t.Fatalf("parseListPage(bad cursor) = error %v, want passthrough", err)
	}
	if opts.Cursor != "!!!" {
		t.Fatalf("parseListPage(bad cursor) cursor = %q", opts.Cursor)
	}
}

func TestPerf02ReportWindowBounds(t *testing.T) {
	// Default window: 31 days ending today.
	start, end, err := defaultReportWindowBounds("", "")
	if err != nil {
		t.Fatal(err)
	}
	if start == "" || end == "" || start > end {
		t.Fatalf("default window = %q..%q", start, end)
	}
	// Over-max window rejected.
	if _, _, err := defaultReportWindowBounds("2026-01-01", "2027-06-01"); err == nil {
		t.Fatal("366+ day window accepted, want error")
	}
	// Start after end rejected.
	if _, _, err := defaultReportWindowBounds("2026-06-01", "2026-01-01"); err == nil {
		t.Fatal("start > end accepted, want error")
	}
	// Malformed date rejected.
	if _, _, err := defaultReportWindowBounds("not-a-date", ""); err == nil {
		t.Fatal("malformed date accepted, want error")
	}
	// One-sided bounds extend by 30 days.
	start, end, err = defaultReportWindowBounds("", "2026-10-31")
	if err != nil || start != "2026-10-01" || end != "2026-10-31" {
		t.Fatalf("one-sided end window = %q..%q err %v", start, end, err)
	}
}
