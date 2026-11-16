package jobrunner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStopCancelsManualRunBeforeStart(t *testing.T) {
	began := make(chan struct{})
	done := make(chan error, 1)
	r := New(&memoryStore{}, []Job{{ID: "manual", Timeout: time.Second, Run: func(ctx context.Context) (Result, error) { close(began); <-ctx.Done(); return Result{}, ctx.Err() }}})
	go func() { _, err := r.Run(context.Background(), "manual"); done <- err }()
	<-began
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := r.Stop(ctx); err != nil {
		t.Errorf("Stop did not cancel pre-Start manual business work: %v", err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("manual outcome %v, want cancellation", err)
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.Start()
	if _, err := r.Run(context.Background(), "manual"); !errors.Is(err, ErrStopped) {
		t.Fatal("late admission", err)
	}
}
