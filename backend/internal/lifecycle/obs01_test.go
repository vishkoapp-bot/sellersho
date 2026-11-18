package lifecycle

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// OBS-01 D-06: drain 503s count as service_draining rejects.
type stubRejectReporter struct{ counts map[string]int }

func (s *stubRejectReporter) RecordReject(reason string) {
	if s.counts == nil {
		s.counts = map[string]int{}
	}
	s.counts[reason]++
}

func TestDrainRejectCountsAsReject(t *testing.T) {
	rep := &stubRejectReporter{}
	s := New(nil, nil, "test", true)
	s.SetRejectReporter(rep)
	s.Initialize()
	s.Drain()
	h := s.Admission(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/orders", nil))
	if w.Code != 503 {
		t.Fatalf("drain status %d, want 503", w.Code)
	}
	if rep.counts["service_draining"] != 1 {
		t.Fatalf("service_draining rejects = %d, want 1", rep.counts["service_draining"])
	}
	// Reads still pass during drain and are not rejects.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/orders", nil))
	if w.Code != 200 {
		t.Fatalf("drain GET status %d, want 200", w.Code)
	}
	if rep.counts["service_draining"] != 1 {
		t.Fatalf("GET during drain counted as reject: %+v", rep.counts)
	}
}
