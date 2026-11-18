package requestbudget

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// OBS-01 D-06: admission rejects are countable per reason and slot
// occupancy is reported per lane.
type stubReporter struct {
	mu       sync.Mutex
	rejects  map[string]int
	inflight map[string]int
}

func (s *stubReporter) RecordReject(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rejects == nil {
		s.rejects = map[string]int{}
	}
	s.rejects[reason]++
}
func (s *stubReporter) SetAdmissionInflight(lane string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight == nil {
		s.inflight = map[string]int{}
	}
	s.inflight[lane] = n
}

func TestRejectsCountedSeparatelyFromOther5xx(t *testing.T) {
	rep := &stubReporter{}
	c := DefaultConfig()
	c.APIConcurrent = 1
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	h := NewWithReporter(c, rep)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-release; w.WriteHeader(200) }))
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/products", nil))
		done <- w.Code
	}()
	<-entered
	// Occupancy reported while the single slot is held.
	rep.mu.Lock()
	if rep.inflight["api"] != 1 {
		t.Fatalf("inflight api = %d, want 1", rep.inflight["api"])
	}
	rep.mu.Unlock()
	// Second request is rejected as overloaded, not queued.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/orders", nil))
	if w.Code != 503 {
		t.Fatalf("overload status %d, want 503", w.Code)
	}
	rep.mu.Lock()
	if rep.rejects["request_overloaded"] != 1 {
		t.Fatalf("request_overloaded rejects = %d, want 1", rep.rejects["request_overloaded"])
	}
	rep.mu.Unlock()
	close(release)
	if <-done != 200 {
		t.Fatal("admitted request failed")
	}
	rep.mu.Lock()
	if rep.inflight["api"] != 0 {
		t.Fatalf("inflight api = %d after release, want 0", rep.inflight["api"])
	}
	rep.mu.Unlock()
}

func TestOversizeBodyCountsAsReject(t *testing.T) {
	rep := &stubReporter{}
	h := NewWithReporter(DefaultConfig(), rep)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	// 2 MiB non-upload limit is 2 MiB; send 3 MiB.
	body := strings.NewReader(strings.Repeat("x", 3<<20))
	req := httptest.NewRequest("POST", "/api/v1/orders", body)
	req.ContentLength = 3 << 20
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 413 {
		t.Fatalf("oversize status %d, want 413", w.Code)
	}
	rep.mu.Lock()
	defer rep.mu.Unlock()
	if rep.rejects["request_body_too_large"] != 1 {
		t.Fatalf("request_body_too_large rejects = %d, want 1", rep.rejects["request_body_too_large"])
	}
	if rep.rejects["request_overloaded"] != 0 {
		t.Fatalf("oversize body miscounted as overload: %+v", rep.rejects)
	}
}

func TestNilReporterDisablesReporting(t *testing.T) {
	// New() without a reporter must behave exactly as before.
	c := DefaultConfig()
	c.APIConcurrent = 1
	h := New(c)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(50 * time.Millisecond); w.WriteHeader(200) }))
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/products", nil))
		done <- w.Code
	}()
	time.Sleep(10 * time.Millisecond)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/products", nil))
	if w.Code != 503 {
		t.Fatalf("nil-reporter overload status %d, want 503", w.Code)
	}
	if <-done != 200 {
		t.Fatal("admitted request failed")
	}
}
