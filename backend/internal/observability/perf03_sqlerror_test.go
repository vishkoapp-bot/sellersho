package observability

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lib/pq"
)

// PERF-03 G7: SQL failures are captured as value-free SQLSTATE codes
// (never the message, which can carry parameter values) in both the
// log path and /metrics.
func TestPERF03SQLErrorCodeIsValueFree(t *testing.T) {
	if got := sqlErrorCode(&pq.Error{Code: "40001"}); got != "40001" {
		t.Fatalf("pq 40001: got %q", got)
	}
	if got := sqlErrorCode(&pq.Error{Code: "23505"}); got != "23505" {
		t.Fatalf("pq 23505: got %q", got)
	}
	if got := sqlErrorCode(errors.New("boom")); got != "unknown" {
		t.Fatalf("generic error: got %q, want unknown", got)
	}
	if got := sqlErrorCode(nil); got != "" {
		t.Fatalf("nil error: got %q, want empty", got)
	}
}

func TestPERF03SQLErrorCounterInMetrics(t *testing.T) {
	m := New()
	m.RecordSQLError("40001")
	m.RecordSQLError("40001")
	m.RecordSQLError("23505")
	m.RecordSQLError("")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	out := rec.Body.String()
	if !strings.Contains(out, `sellersho_sql_errors_total{code="40001"} 2`) {
		t.Fatalf("want 40001 count 2, got:\n%s", out)
	}
	if !strings.Contains(out, `sellersho_sql_errors_total{code="23505"} 1`) {
		t.Fatalf("want 23505 count 1, got:\n%s", out)
	}
	if strings.Contains(out, `code=""`) {
		t.Fatalf("empty code must never be emitted, got:\n%s", out)
	}
}
