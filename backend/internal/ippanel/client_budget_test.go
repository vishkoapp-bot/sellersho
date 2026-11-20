package ippanel

import (
	"context"
	"errors"
	"github.com/drup/platform/internal/dependencybudget"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func budgetFixture(t *testing.T, max int) {
	t.Helper()
	// PERF-03 G3: OTP sends run through otpBudget, admin reads through
	// sharedBudget, but these tests predate the split and their fixture
	// must govern whichever gate the call under test uses. Point BOTH
	// at the same fixture executor (the pre-G3 single-gate topology);
	// the G3 split itself is covered by perf03_otp_isolation_test.go,
	// which uses the real separate executors.
	oldShared, oldOTP := sharedBudget, otpBudget
	p := dependencybudget.DefaultPolicy(max, 100)
	p.BackoffMin = time.Millisecond
	p.BackoffMax = 2 * time.Millisecond
	fixture := dependencybudget.New(p)
	sharedBudget = fixture
	otpBudget = fixture
	t.Cleanup(func() { sharedBudget = oldShared; otpBudget = oldOTP })
}
func TestIPPanelReadsRetryButSMSSendsNeverReplay(t *testing.T) {
	for _, safe := range []bool{true, false} {
		t.Run(map[bool]string{true: "credit", false: "sms"}[safe], func(t *testing.T) {
			budgetFixture(t, 2)
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(503)
					return
				}
				w.Write([]byte(`{"meta":{"status":true},"data":{"balance":10}}`))
			}))
			defer s.Close()
			c := New(s.URL, "synthetic", "synthetic")
			if safe {
				data, err := c.Credit(context.Background())
				if err != nil || calls.Load() != 2 || data["balance"] != float64(10) {
					t.Fatalf("credit retry %d %v", calls.Load(), err)
				}
			} else {
				_, err := c.SendPattern(context.Background(), "09120000000", "synthetic", nil)
				if !dependencybudget.IsAmbiguous(err) || calls.Load() != 1 {
					t.Fatalf("SMS replay/false rejection %d %v", calls.Load(), err)
				}
			}
		})
	}
}
func TestIPPanelIncompleteAcknowledgementsStayUnknown(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"meta":{"status":true},"data":{}}`} {
		t.Run(body, func(t *testing.T) {
			budgetFixture(t, 2)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
			defer s.Close()
			_, err := New(s.URL, "synthetic", "synthetic").SendPattern(context.Background(), "09120000000", "synthetic", nil)
			if !dependencybudget.IsAmbiguous(err) {
				t.Fatal("incomplete SMS reply became definitive", err)
			}
		})
	}
}

func TestIPPanelConflictingOrHTTPFailureAcknowledgementsStayUnknown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"false_with_message_reference", 200, `{"meta":{"status":false},"data":{"message_outbox_ids":["issued"]}}`},
		{"false_with_bulk_reference", 200, `{"meta":{"status":false},"data":{"bulk_id":"issued"}}`},
		{"unvalidated_http_failure", 400, `{"error":"synthetic proxy failure"}`},
		{"http_rate_limit", 429, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budgetFixture(t, 2)
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer s.Close()
			_, err := New(s.URL, "synthetic", "synthetic").SendPattern(context.Background(), "09120000000", "synthetic", nil)
			if !dependencybudget.IsAmbiguous(err) || calls.Load() != 1 {
				t.Fatalf("uncertain acknowledgement permits cleanup/replay calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}
func TestIPPanelProviderDeadlineReleasesSharedCapacity(t *testing.T) {
	budgetFixture(t, 1)
	var wait atomic.Bool
	wait.Store(true)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if wait.Load() {
			<-r.Context().Done()
			return
		}
		io.WriteString(w, `{"meta":{"status":true},"data":{"message_outbox_ids":["synthetic"]}}`)
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := New(s.URL, "synthetic", "synthetic").SendPattern(ctx, "09120000000", "synthetic", nil)
	if !errors.Is(err, context.DeadlineExceeded) || !dependencybudget.IsAmbiguous(err) {
		t.Fatal("SMS deadline not ambiguous", err)
	}
	wait.Store(false)
	if _, err := New(s.URL, "synthetic", "synthetic").SendPattern(context.Background(), "09120000000", "synthetic", nil); err != nil {
		t.Fatal("capacity did not return", err)
	}
}
func TestFreshIPPanelClientsShareOneAdmissionGate(t *testing.T) {
	budgetFixture(t, 2)
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		io.WriteString(w, `{"meta":{"status":true},"data":{"message_outbox_ids":["synthetic"]}}`)
	}))
	defer s.Close()
	done := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, err := New(s.URL, "synthetic", "synthetic").SendPattern(context.Background(), "09120000000", "synthetic", nil)
			done <- err
		}()
	}
	<-entered
	<-entered
	overloaded := 0
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if errors.Is(err, dependencybudget.ErrOverloaded) {
				overloaded++
			}
		case <-time.After(200 * time.Millisecond):
			t.Error("provider calls queued instead of rejecting")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if overloaded != 2 || calls.Load() != 2 {
		t.Fatalf("fresh clients bypassed shared gate calls=%d rejects=%d", calls.Load(), overloaded)
	}
}
