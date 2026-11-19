package observability

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestPERF04RedisOperationSeriesAreDistinct (G-11): the PERF-04 per-script
// operation names each produce their own series instead of collapsing to
// "eval" (the hook's view) or "other" (an unregistered name).
func TestPERF04RedisOperationSeriesAreDistinct(t *testing.T) {
	m := New()
	ops := []string{"hold", "release", "has_hold", "otp_reserve", "otp_verify", "eval"}
	for _, op := range ops {
		m.Observe("redis", op, "success", time.Millisecond)
	}
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	s := w.Body.String()
	for _, op := range ops {
		if !strings.Contains(s, `operation="`+op+`"`) {
			t.Fatalf("operation %q has no series in /metrics output", op)
		}
	}
	if strings.Contains(s, `operation="other"`) {
		t.Fatal("a PERF-04 operation collapsed to \"other\"")
	}
}
