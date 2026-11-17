package lifecycle

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

type Closer func(context.Context) error

// Process has one total grace and an idempotent teardown shared by startup errors
// and normal signal termination. Request contexts deliberately do not inherit the
// signal context: Shutdown lets admitted work finish before Close cancels it.
type Process struct {
	State       *State
	Grace       time.Duration
	Server      *http.Server
	StopWorker  Closer
	StopMetrics Closer
	Closers     []Closer
	once        sync.Once
	done        chan struct{}
	err         error
}

func (p *Process) Serve(ctx context.Context, listener net.Listener, startWorker func()) error {
	if ctx.Err() != nil {
		listener.Close()
		return errors.Join(ctx.Err(), p.Shutdown())
	}
	if startWorker != nil {
		startWorker()
	}
	p.State.Initialize()
	served := make(chan error, 1)
	go func() { served <- p.Server.Serve(listener) }()
	var err error
	select {
	case <-ctx.Done():
	case err = <-served:
	}
	drainErr := p.Shutdown()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, drainErr)
}

func (p *Process) Shutdown() error {
	// Protect initialization of the shared completion channel as well as teardown.
	p.once.Do(func() {
		if p.State != nil {
			p.State.Drain()
		}
		p.done = make(chan struct{})
		go func() { defer close(p.done); p.err = p.shutdown() }()
	})
	<-p.done
	return p.err
}
func (p *Process) shutdown() error {
	grace := p.Grace
	if grace <= 0 {
		grace = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	var failures []error
	if p.Server != nil {
		httpCtx, httpCancel := context.WithTimeout(ctx, min(14*time.Second, grace*70/100))
		err := p.Server.Shutdown(httpCtx)
		httpCancel()
		if err != nil {
			failures = append(failures, errors.New("HTTP drain exceeded budget"))
			p.Server.Close()
		}
	}
	if p.StopWorker != nil {
		workerCtx, workerCancel := context.WithTimeout(ctx, min(5*time.Second, grace*25/100))
		if bounded(workerCtx, p.StopWorker) != nil {
			failures = append(failures, errors.New("worker drain exceeded budget"))
		}
		workerCancel()
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(ctx, min(time.Second, grace*5/100))
	defer cleanupCancel()
	if p.StopMetrics != nil && bounded(cleanupCtx, p.StopMetrics) != nil {
		failures = append(failures, errors.New("metrics cleanup exceeded budget"))
	}
	// Initiate every close even if an earlier closer blocks. A dependency cannot
	// prevent another dependency from being closed or hold process exit past grace.
	results := make(chan error, len(p.Closers))
	for _, closeResource := range p.Closers {
		go func(c Closer) { results <- c(cleanupCtx) }(closeResource)
	}
	for range p.Closers {
		select {
		case err := <-results:
			if err != nil {
				failures = append(failures, errors.New("dependency cleanup failed"))
			}
		case <-cleanupCtx.Done():
			return errors.Join(append(failures, errors.New("dependency cleanup exceeded budget"))...)
		}
	}
	return errors.Join(failures...)
}
func bounded(ctx context.Context, closeResource Closer) error {
	done := make(chan error, 1)
	go func() { done <- closeResource(ctx) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
