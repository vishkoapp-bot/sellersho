package repository

// PERF-02 list-pagination contract (see docs/architecture/list-pagination-contract.md).
//
// Every list that used to return an unbounded slice now pages by a stable
// keyset cursor over (created_at DESC, id DESC) — or (date DESC, id DESC)
// for the ledger, whose sort key is the entry date. The cursor is opaque:
// base64url of "<RFC3339Nano created_at>\n<id>". A page never returns more
// than MaxListLimit rows; the HTTP layer rejects a non-integer or negative
// limit with 400 and clamps an over-max limit to MaxListLimit.
//
// Cursor stability semantics (same as the FIN-009 statement cursor): rows
// created after the page was read sort BEFORE the cursor, so a concurrent
// insert is never duplicated and never shifts a page — it is simply seen
// on a fresh first page. Rows that sort before the cursor are the only
// ones a walking client can miss, which is the documented behaviour.

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

const (
	// DefaultListLimit is the page size when the caller passes no limit (or 0).
	DefaultListLimit = 50
	// MaxListLimit is the largest page any newly bounded list returns.
	MaxListLimit = 100
	// DefaultReportWindowDays is the report window when the caller passes no dates.
	DefaultReportWindowDays = 31
	// MaxReportWindowDays is the largest report window (366 days = one leap year).
	MaxReportWindowDays = 366
)

// ListPageOptions is the parsed, validated form of ?limit=&cursor=.
type ListPageOptions struct {
	// Limit caps the entries in one page (default 50, clamped to 1..100).
	Limit int
	// Cursor is the opaque page token returned as NextCursor by the
	// previous page; empty starts at the first page.
	Cursor string
}

// ListPage is one page of a keyset-paged list.
type ListPage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
	HasMore    bool   `json:"hasMore"`
	Limit      int    `json:"limit"`
}

// NormalizeListLimit applies the PERF-02 contract: missing/0 -> default 50,
// over-max -> clamp to MaxListLimit. A negative limit reaching here is a
// caller bug (the HTTP layer rejects it with 400); it is treated as default
// rather than silently returning an unbounded list.
func NormalizeListLimit(limit int) int {
	if limit <= 0 {
		return DefaultListLimit
	}
	if limit > MaxListLimit {
		return MaxListLimit
	}
	return limit
}

// EncodeCreatedAtCursor builds the opaque cursor for the last row of a page
// from its sort key (created_at DESC, id DESC).
func EncodeCreatedAtCursor(createdAt time.Time, id string) string {
	raw := createdAt.Format(time.RFC3339Nano) + "\n" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCreatedAtCursor parses a page cursor back into its sort key.
func DecodeCreatedAtCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor: %w", err)
	}
	parts := strings.SplitN(string(raw), "\n", 2)
	if len(parts) != 2 || parts[1] == "" {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor: %w", err)
	}
	return createdAt, parts[1], nil
}

// nextCreatedAtCursor returns the cursor for the next page when a full page
// was read: the sort key of the last item. Empty when the page is short
// (no more rows) or empty. createdAt/id extract the sort key of one item.
func NextCreatedAtCursor[T any](items []T, limit int, createdAt func(T) time.Time, id func(T) string) (string, bool) {
	if len(items) == 0 || len(items) < limit {
		return "", false
	}
	last := items[len(items)-1]
	return EncodeCreatedAtCursor(createdAt(last), id(last)), true
}

// ValidateReportWindow enforces the PERF-02 report window contract:
// start <= end and (end - start) <= MaxReportWindowDays. Dates are
// YYYY-MM-DD; empty strings are left to the caller to default.
func ValidateReportWindow(start, end string) error {
	if start == "" || end == "" {
		return nil
	}
	s, err := time.Parse("2006-01-02", start)
	if err != nil {
		return fmt.Errorf("invalid start date %q: expected YYYY-MM-DD", start)
	}
	e, err := time.Parse("2006-01-02", end)
	if err != nil {
		return fmt.Errorf("invalid end date %q: expected YYYY-MM-DD", end)
	}
	if e.Before(s) {
		return fmt.Errorf("end date must not be before start date")
	}
	if e.Sub(s) > time.Duration(MaxReportWindowDays)*24*time.Hour {
		return fmt.Errorf("report window must not exceed %d days", MaxReportWindowDays)
	}
	return nil
}

// DefaultReportWindow returns the default 31-day window ending today (UTC
// calendar dates, matching the YYYY-MM-DD window parameters): start is
// 30 days ago, end is today.
func DefaultReportWindow() (start, end string) {
	now := time.Now().UTC()
	end = now.Format("2006-01-02")
	start = now.AddDate(0, 0, -(DefaultReportWindowDays - 1)).Format("2006-01-02")
	return start, end
}
