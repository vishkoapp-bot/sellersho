package main

// PERF-02 Step 13: the single invalid-limit policy (Step 11 contract §1)
// at the HTTP layer: non-integer/negative -> 400, over-max -> clamp to 100
// with the effective limit echoed, missing/0 -> default 50, bad cursor is
// carried through for the repository to reject.
import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
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
	// N1: a start-only window is start..today and must obey the same
	// 366-day max as an explicit window — a bare 2020 start used to be
	// honored silently as a ~6-year window.
	if _, _, err := defaultReportWindowBounds("2020-01-01", ""); err == nil {
		t.Fatal("start-only 6-year window accepted, want error")
	}
	tenDaysAgo := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")
	start, end, err = defaultReportWindowBounds(tenDaysAgo, "")
	if err != nil {
		t.Fatalf("start-only 10-day window rejected: %v", err)
	}
	if start != tenDaysAgo || end != time.Now().UTC().Format("2006-01-02") {
		t.Fatalf("start-only window = %q..%q, want %q..today", start, end, tenDaysAgo)
	}
	// A future start collapses to a single-day window, not an error.
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	start, end, err = defaultReportWindowBounds(tomorrow, "")
	if err != nil || start != end || start != tomorrow {
		t.Fatalf("future start-only window = %q..%q err %v", start, end, err)
	}
}

// TestPerf02ReportWindowErrorMapping pins the N1 ledger facet: every error
// repository.ValidateReportWindow can return is a client error (400), never
// a 500 — including when it surfaces from the repository layer after the
// HTTP helper (isReportWindowError is the backstop mapping in
// handleGetFilteredLedger).
func TestPerf02ReportWindowErrorMapping(t *testing.T) {
	for _, tc := range [][2]string{
		{"2020-01-01", "2027-01-01"}, // over 366 days
		{"2026-06-01", "2026-01-01"}, // start after end
		{"not-a-date", "2026-01-01"}, // malformed start
		{"2026-01-01", "not-a-date"}, // malformed end
	} {
		err := repository.ValidateReportWindow(tc[0], tc[1])
		if err == nil {
			t.Fatalf("ValidateReportWindow(%q, %q) = nil error, want error", tc[0], tc[1])
		}
		if !isReportWindowError(err) {
			t.Fatalf("isReportWindowError(%v) = false, want true", err)
		}
	}
	// Non-window errors must not be reclassified as 400.
	if isReportWindowError(nil) || isReportWindowError(errors.New("pq: connection refused")) || isReportWindowError(errors.New("invalid cursor: bad base64")) {
		t.Fatal("isReportWindowError classified a non-window error as a window error")
	}
}

func TestPerf02SellerReportStartOnlyWindowReturns400(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewStoreRepository(db)
	fin := repository.NewFinanceRepository(db)
	owner := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role,is_active) VALUES($1,$2,'Report window test','seller',true)`, owner, "09"+owner[:13]); err != nil {
		t.Fatal(err)
	}
	store := domain.Store{ID: "s-" + uuid.NewString(), Name: "Report window test", Slug: "s-" + uuid.NewString()[:20], OwnerType: "dropshipper", OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8]}
	if err := repo.CreateStore(ctx, store); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM stores WHERE id=$1`, store.ID)
		db.Exec(`DELETE FROM users WHERE id=$1`, owner)
	})
	get := func(query string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/stores/"+store.Slug+"/report"+query, nil)
		r = r.WithContext(context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: owner, Role: "seller"}))
		w := httptest.NewRecorder()
		handleSellerStore(w, r, repo, fin, store.Slug, "report", "")
		return w
	}
	daysAgo := func(n int) string { return time.Now().UTC().AddDate(0, 0, -n).Format("2006-01-02") }
	cases := []struct {
		query string
		want  int
	}{
		{"?start=2020-01-01", http.StatusBadRequest},                // N1: start-only 6-year window
		{"?start=2020-01-01&end=2027-01-01", http.StatusBadRequest}, // explicit over-wide window
		{"?start=" + daysAgo(10), http.StatusOK},                    // start-only valid window
		{"?start=2020-01-01&end=2020-01-05", http.StatusOK},         // explicit short window, long ago
		{"?end=2020-01-05", http.StatusOK},                          // end-only, derived window
		{"", http.StatusOK},                                         // default window
	}
	for _, tc := range cases {
		if w := get(tc.query); w.Code != tc.want {
			t.Fatalf("GET report%s: got %d, want %d: %s", tc.query, w.Code, tc.want, w.Body.String())
		}
	}
}

func TestPerf02LedgerDateFromOnlyWindowReturns400(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	fin := repository.NewFinanceRepository(db)
	get := func(query string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/finance/ledger"+query, nil)
		w := httptest.NewRecorder()
		handleGetFilteredLedger(w, r, r.Context(), fin)
		return w
	}
	daysAgo := func(n int) string { return time.Now().UTC().AddDate(0, 0, -n).Format("2006-01-02") }
	cases := []struct {
		query string
		want  int
	}{
		{"?dateFrom=2020-01-01", http.StatusBadRequest},                   // N1 facet: was 500 via repo validation
		{"?dateFrom=2020-01-01&dateTo=2027-01-01", http.StatusBadRequest}, // explicit over-wide window
		{"?dateFrom=" + daysAgo(30), http.StatusOK},                       // dateFrom-only valid window
		{"?dateFrom=2020-01-01&dateTo=2020-01-05", http.StatusOK},         // explicit short window, long ago
		{"?dateTo=2020-01-05", http.StatusOK},                             // dateTo-only, derived window
		{"", http.StatusOK},                                               // default window
	}
	for _, tc := range cases {
		if w := get(tc.query); w.Code != tc.want {
			t.Fatalf("GET ledger%s: got %d, want %d: %s", tc.query, w.Code, tc.want, w.Body.String())
		}
	}
}
