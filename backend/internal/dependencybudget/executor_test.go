package dependencybudget

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testPolicy() Policy {
	return Policy{MaxInFlight: 2, Rate: 100, Burst: 100, Budget: time.Second, ReadAttemptTimeout: 200 * time.Millisecond, MaxReadAttempts: 2, BackoffMin: time.Millisecond, BackoffMax: 2 * time.Millisecond}
}
func requestAt(method, url string) func(context.Context) (*http.Request, error) {
	return func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, method, url, strings.NewReader("synthetic"))
	}
}
func TestSafeReadRetriesButUncertainMutationDoesNot(t *testing.T) {
	for _, safe := range []bool{true, false} {
		t.Run(map[bool]string{true: "read", false: "mutation"}[safe], func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(503)
					w.Write([]byte("SECRET"))
					return
				}
				w.Write([]byte("ok"))
			}))
			defer s.Close()
			e := New(testPolicy())
			reply, err := e.Do(context.Background(), s.Client(), requestAt("POST", s.URL), safe, 100)
			if safe {
				if err != nil || calls.Load() != 2 || string(reply.Body) != "ok" {
					t.Fatalf("read retry %d %v", calls.Load(), err)
				}
			} else {
				if !IsAmbiguous(err) || calls.Load() != 1 {
					t.Fatalf("mutation replayed or falsely rejected: %d %v", calls.Load(), err)
				}
				if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), s.URL) {
					t.Fatal("executor error leaked payload/URL")
				}
			}
		})
	}
}
func TestProviderRedirectsCannotBypassAttemptOrRateBudgets(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect, http.StatusSeeOther} {
		for _, safe := range []bool{false, true} {
			var initial, redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				redirected.Add(1)
				io.WriteString(w, "unexpected redirect target")
			}))
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				initial.Add(1)
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
			}))
			_, err := New(testPolicy()).Do(context.Background(), origin.Client(), requestAt("POST", origin.URL), safe, 100)
			origin.Close()
			target.Close()
			if err == nil || initial.Load() != 1 || redirected.Load() != 0 || (!safe && !IsAmbiguous(err)) {
				t.Errorf("redirect escaped budget status=%d safe=%v initial=%d target=%d err=%v", status, safe, initial.Load(), redirected.Load(), err)
			}
		}
	}
}

func TestParentDeadlineCoversProviderHeadersAndBody(t *testing.T) {
	for _, body := range []bool{false, true} {
		t.Run(map[bool]string{false: "headers", true: "body"}[body], func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				if body {
					w.Header().Set("Content-Length", "10000")
					w.Write([]byte("{"))
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer s.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := New(testPolicy()).Do(ctx, s.Client(), requestAt("POST", s.URL), false, 20000)
			if !errors.Is(err, context.DeadlineExceeded) || !IsAmbiguous(err) || time.Since(start) > 300*time.Millisecond {
				t.Fatalf("deadline not propagated: %v", err)
			}
		})
	}
}
func TestOverloadNeverQueuesAndCapacityReturns(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-release; w.Write([]byte("ok")) }))
	defer s.Close()
	e := New(testPolicy())
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := e.Do(context.Background(), s.Client(), requestAt("GET", s.URL), true, 100)
			done <- err
		}()
	}
	<-entered
	<-entered
	start := time.Now()
	for i := 0; i < 20; i++ {
		if _, err := e.Do(context.Background(), s.Client(), requestAt("POST", s.URL), false, 100); !errors.Is(err, ErrOverloaded) || IsAmbiguous(err) {
			t.Fatalf("local overload is not definite/no-send: %v", err)
		}
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("overload queued requests")
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Do(context.Background(), s.Client(), requestAt("GET", s.URL), true, 100); err != nil {
		t.Fatal("capacity was not released", err)
	}
}
func TestRateBudgetCountsAttempts(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer s.Close()
	p := testPolicy()
	p.Rate = 0.000001
	p.Burst = 2
	e := New(p)
	e.Do(context.Background(), s.Client(), requestAt("GET", s.URL), true, 100)
	if _, err := e.Do(context.Background(), s.Client(), requestAt("POST", s.URL), false, 100); !errors.Is(err, ErrOverloaded) {
		t.Fatal("exhausted rate budget admitted mutation", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected provider attempts: %d", calls.Load())
	}
}
func TestCancellationInterruptsBackoff(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer s.Close()
	p := testPolicy()
	p.BackoffMin = 200 * time.Millisecond
	p.BackoffMax = 400 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := New(p).Do(ctx, s.Client(), requestAt("GET", s.URL), true, 100)
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("backoff ignored parent: %d %v", calls.Load(), err)
	}
}
func TestResponseBoundAndNoSendCancellation(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, "12345") }))
	defer s.Close()
	e := New(testPolicy())
	if _, err := e.Do(context.Background(), s.Client(), requestAt("POST", s.URL), false, 3); !IsAmbiguous(err) {
		t.Fatal("oversized mutation reply treated as known", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Do(ctx, s.Client(), requestAt("POST", s.URL), false, 100); !errors.Is(err, context.Canceled) || IsAmbiguous(err) {
		t.Fatal("pre-send cancellation misclassified", err)
	}
	if calls.Load() != 1 {
		t.Fatal("cancelled call reached provider")
	}
}
