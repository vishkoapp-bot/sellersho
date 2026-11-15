package jobrunner

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A deterministic metadata boundary: the engine, timers, callbacks, admission
// and cancellation are real. PostgreSQL ownership is tested in repository.
type memoryStore struct {
	mu       sync.Mutex
	owners   map[string]string
	outcomes []Outcome
	fail     bool
}

func (s *memoryStore) Acquire(ctx context.Context, job, owner string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owners == nil {
		s.owners = map[string]string{}
	}
	if s.owners[job] != "" {
		return false, nil
	}
	s.owners[job] = owner
	return true, nil
}
func (s *memoryStore) Complete(ctx context.Context, o Outcome) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("metadata unavailable")
	}
	s.outcomes = append(s.outcomes, o)
	return nil
}
func (s *memoryStore) Release(ctx context.Context, job, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owners[job] == owner {
		delete(s.owners, job)
	}
	return ctx.Err()
}

func TestExpiryProgressesWhileOtherJobsBlock(t *testing.T) {
	s := &memoryStore{}
	slowStarted := make(chan struct{}, 2)
	expiry := make(chan struct{}, 8)
	r := New(s, []Job{
		{ID: "slow-a", Period: time.Hour, Timeout: time.Second, Run: func(ctx context.Context) (Result, error) {
			slowStarted <- struct{}{}
			<-ctx.Done()
			return Result{}, ctx.Err()
		}},
		{ID: "slow-b", Period: time.Hour, Timeout: time.Second, Run: func(ctx context.Context) (Result, error) {
			slowStarted <- struct{}{}
			<-ctx.Done()
			return Result{}, ctx.Err()
		}},
		{ID: "expiry", Expiry: true, Period: 10 * time.Millisecond, Timeout: time.Second, Run: func(ctx context.Context) (Result, error) { expiry <- struct{}{}; return Result{Affected: 1}, nil }},
	})
	r.Start()
	defer r.Stop(context.Background())
	for i := 0; i < 2; i++ {
		select {
		case <-slowStarted:
		case <-time.After(time.Second):
			t.Fatal("slow job never started")
		}
	}
	for i := 0; i < 3; i++ {
		select {
		case <-expiry:
		case <-time.After(300 * time.Millisecond):
			t.Fatal("expiry starved by slow jobs")
		}
	}
}

func TestStopCancelsDrainsAndPreventsRestart(t *testing.T) {
	var calls atomic.Int32
	began := make(chan struct{})
	r := New(&memoryStore{}, []Job{{ID: "work", Period: time.Millisecond, Timeout: time.Hour, Run: func(ctx context.Context) (Result, error) {
		calls.Add(1)
		close(began)
		<-ctx.Done()
		return Result{}, ctx.Err()
	}}})
	r.Start()
	r.Start()
	<-began
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := r.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	r.Start()
	time.Sleep(10 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestCanceledCallbackRecordsCommittedProgressWithFreshDeadline(t *testing.T) {
	s := &memoryStore{}
	r := New(s, []Job{{ID: "work", Period: time.Hour, Timeout: 10 * time.Millisecond, Run: func(ctx context.Context) (Result, error) { <-ctx.Done(); return Result{Affected: 7}, ctx.Err() }}})
	result, err := r.Run(context.Background(), "work")
	if !errors.Is(err, context.DeadlineExceeded) || result.Affected != 7 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.outcomes) != 1 || s.outcomes[0].Result.Affected != 7 || s.outcomes[0].ErrorCode != "deadline_exceeded" {
		t.Fatalf("lost outcome: %+v", s.outcomes)
	}
}

func TestFailedMetadataIsReturnedAndSameJobCannotOverlap(t *testing.T) {
	s := &memoryStore{fail: true}
	began := make(chan struct{})
	unblock := make(chan struct{})
	r := New(s, []Job{{ID: "work", Timeout: time.Second, Period: time.Hour, Run: func(context.Context) (Result, error) { close(began); <-unblock; return Result{Affected: 2}, nil }}})
	done := make(chan error, 1)
	go func() { _, err := r.Run(context.Background(), "work"); done <- err }()
	<-began
	if _, err := r.Run(context.Background(), "work"); !errors.Is(err, ErrBusy) {
		t.Fatalf("overlap err=%v", err)
	}
	close(unblock)
	if err := <-done; !errors.Is(err, ErrMetadata) {
		t.Fatalf("metadata failure hidden: %v", err)
	}
}

func TestCallbackPanicRecordsFailureAndFreesSlot(t *testing.T) {
	s := &memoryStore{}
	var calls atomic.Int32
	r := New(s, []Job{{ID: "panic", Period: time.Hour, Timeout: time.Second, Run: func(context.Context) (Result, error) {
		if calls.Add(1) == 1 {
			panic("private provider payload")
		}
		return Result{Affected: 1}, nil
	}}})
	if _, err := r.Run(context.Background(), "panic"); err == nil {
		t.Fatal("panic not reported")
	}
	if _, err := r.Run(context.Background(), "panic"); err != nil {
		t.Fatal("slot not recovered", err)
	}
	if len(s.outcomes) != 2 || s.outcomes[0].ErrorCode != "job_failed" {
		t.Fatal(s.outcomes)
	}
}

func TestStopBudgetBoundsNonCooperativeCallback(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	r := New(&memoryStore{}, []Job{{ID: "slow", Period: time.Hour, Timeout: time.Hour, Run: func(context.Context) (Result, error) { close(started); <-release; return Result{}, nil }}})
	r.Start()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := r.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("drain ignored stop budget", err)
	}
	close(release)
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
