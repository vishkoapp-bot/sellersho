package main

// PERF-02 list-pagination HTTP contract (see
// docs/architecture/list-pagination-contract.md and
// backend/internal/repository/list_pagination.go).
//
//   - ?limit=: non-integer or negative -> 400; missing or 0 -> default 50;
//     over-max -> clamped to 100 and the effective limit echoed in the
//     response envelope (limit field).
//   - ?cursor=: opaque token from the previous page's nextCursor; malformed
//     -> 400. The repository validates it and returns an "invalid cursor"
//     error, mapped here to 400.

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/repository"
)

// parseListPage parses ?limit=&cursor= per the PERF-02 contract. The
// returned error is a client error (400) with a message safe to surface.
func parseListPage(r *http.Request) (repository.ListPageOptions, error) {
	opts := repository.ListPageOptions{Limit: repository.DefaultListLimit}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return opts, fmt.Errorf("limit must be a non-negative integer")
		}
		opts.Limit = repository.NormalizeListLimit(n)
	}
	opts.Cursor = r.URL.Query().Get("cursor")
	return opts, nil
}

// defaultReportWindowBounds applies the PERF-02 report-window contract to
// a (start, end) pair of YYYY-MM-DD dates: both empty -> the default 31-day
// window ending today; one empty -> the other bound extended by 30 days;
// a window over MaxReportWindowDays or start > end -> error.
func defaultReportWindowBounds(start, end string) (string, string, error) {
	for _, v := range []string{start, end} {
		if v != "" {
			if _, err := time.Parse("2006-01-02", v); err != nil {
				return "", "", fmt.Errorf("invalid date %q: expected YYYY-MM-DD", v)
			}
		}
	}
	if start == "" && end == "" {
		s, e := repository.DefaultReportWindow()
		return s, e, nil
	}
	if start == "" {
		e, _ := time.Parse("2006-01-02", end)
		return e.AddDate(0, 0, -(repository.DefaultReportWindowDays - 1)).Format("2006-01-02"), end, nil
	}
	if end == "" {
		today := time.Now().UTC().Format("2006-01-02")
		if today < start {
			return start, start, nil
		}
		return start, today, nil
	}
	if err := repository.ValidateReportWindow(start, end); err != nil {
		return "", "", err
	}
	return start, end, nil
}

// invalidCursorError reports whether err is a cursor-decoding failure from
// the repository layer (mapped to 400, not 500).
func isInvalidCursorError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "invalid cursor")
}
