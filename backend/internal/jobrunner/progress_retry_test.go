package jobrunner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the actual loop, nonqueued admission, callbacks, cancellation and
// completed metadata. No delay-policy helper is mirrored in this regression.
func TestScheduledCommittedProgressDoesNotAccumulateErrorBackoff(t *testing.T) {
	for _, field := range []string{"affected", "confirmed"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			store := &memoryStore{}
			var committed atomic.Int32
			attempts := make(chan time.Time, 8)
			runner := New(store, []Job{{ID: "partial", Timeout: 10 * time.Millisecond, Period: time.Hour, Run: func(ctx context.Context) (Result, error) {
				began := time.Now()
				committed.Add(1) // Useful work commits before this attempt exhausts its budget.
				<-ctx.Done()
				attempts <- began
				result := Result{Affected: 1}
				if field == "confirmed" {
					result = Result{Confirmed: 1}
				}
				return result, ctx.Err()
			}}})
			runner.Start()
			deadline := time.NewTimer(7 * time.Second)
			defer deadline.Stop()
			var starts []time.Time
		wait:
			for len(starts) < 4 {
				select {
				case start := <-attempts:
					starts = append(starts, start)
				case <-deadline.C:
					break wait
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := runner.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			store.mu.Lock()
			outcomes := append([]Outcome(nil), store.outcomes...)
			store.mu.Unlock()
			if len(starts) < 4 {
				t.Fatalf("committed partial progress accumulated failure backoff: attempts=%d committed=%d outcomes=%d within7s, want4", len(starts), committed.Load(), len(outcomes))
			}
			for _, outcome := range outcomes {
				if outcome.ErrorCode != "deadline_exceeded" || outcome.Result.Affected+outcome.Result.Confirmed != 1 {
					t.Fatal("retry lost committed count or deadline outcome", outcome)
				}
			}
			t.Logf("four actual admitted callbacks persisted partial progress; start gaps %v, %v, %v", starts[1].Sub(starts[0]), starts[2].Sub(starts[1]), starts[3].Sub(starts[2]))
		})
	}
}

func TestZeroProgressAndMetadataFailuresKeepExponentialBackoff(t *testing.T) {
	for _, metadataFailure := range []bool{false, true} {
		name := "zero-progress"
		if metadataFailure {
			name = "metadata-failure"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &memoryStore{fail: metadataFailure}
			var attempts atomic.Int32
			runner := New(store, []Job{{ID: "failed", Timeout: 10 * time.Millisecond, Period: time.Hour, Run: func(ctx context.Context) (Result, error) {
				attempts.Add(1)
				<-ctx.Done()
				if metadataFailure {
					return Result{Affected: 1}, ctx.Err()
				}
				return Result{}, ctx.Err()
			}}})
			runner.Start()
			timer := time.NewTimer(7 * time.Second)
			defer timer.Stop()
			<-timer.C
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := runner.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			if got := attempts.Load(); got != 3 {
				t.Fatalf("zero-progress/metadata failure lost2s then4s exponential waits: attempts=%d want3 within7s", got)
			}
		})
	}
}
