// Package jobrunner supplies fixed, cancellable, nonqueued job admission.
package jobrunner

import (
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/drup/platform/internal/observability"
	"github.com/google/uuid"
)

var ErrBusy = errors.New("job busy")
var ErrStopped = errors.New("worker stopped")
var ErrMetadata = errors.New("job outcome unavailable")
var ErrUnknownJob = errors.New("unknown job")

type Result struct {
	Affected, Confirmed int64
	Disabled            bool
}
type Outcome struct {
	Job, Owner string
	Started    time.Time
	Duration   time.Duration
	Result     Result
	ErrorCode  string
}
type Store interface {
	Acquire(context.Context, string, string, time.Duration) (bool, error)
	Complete(context.Context, Outcome) error
	Release(context.Context, string, string) error
}
type Job struct {
	ID              string
	Period, Timeout time.Duration
	Expiry          bool
	Run             func(context.Context) (Result, error)
}
type Runner struct {
	mu               sync.Mutex
	store            Store
	jobs             []Job
	active           map[string]bool
	started, stopped bool
	cancel           context.CancelFunc
	root             context.Context
	wg               sync.WaitGroup
	expiry, shared   chan struct{}
}

func New(store Store, jobs []Job) *Runner {
	root, cancel := context.WithCancel(context.Background())
	return &Runner{root: root, cancel: cancel, store: store, jobs: append([]Job(nil), jobs...), active: map[string]bool{}, expiry: make(chan struct{}, 1), shared: make(chan struct{}, 2)}
}
func (r *Runner) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.stopped {
		return
	}
	r.started = true
	for _, j := range r.jobs {
		r.wg.Add(1)
		go r.loop(j)
	}
}
func (r *Runner) loop(j Job) {
	defer r.wg.Done()
	delay := time.Duration(0)
	backoff := 2 * time.Second
	for {
		timer := time.NewTimer(delay)
		select {
		case <-r.root.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		result, err := r.Run(r.root, j.ID)
		switch {
		case errors.Is(err, ErrStopped):
			return
		case errors.Is(err, ErrBusy):
			delay = time.Second + time.Duration(rand.Int64N(int64(4*time.Second)))
		case err != nil:
			log.Printf("[WARN] background job %s: %s", j.ID, ErrorCode(err))
			if (result.Affected > 0 || result.Confirmed > 0) && !errors.Is(err, ErrMetadata) && !errors.Is(err, context.Canceled) {
				// A bounded pass can commit useful work before its deadline. Keep
				// draining that backlog without treating progress as an outage.
				delay, backoff = 2*time.Second, 2*time.Second
			} else {
				delay = backoff
				backoff = min(60*time.Second, backoff*2)
			}
		default:
			delay = j.Period
			backoff = 2 * time.Second
		}
	}
}
func (r *Runner) Stop(ctx context.Context) error {
	r.mu.Lock()
	r.stopped = true
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *Runner) Run(parent context.Context, id string) (Result, error) {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return Result{}, ErrStopped
	}
	var job Job
	found := false
	for _, j := range r.jobs {
		if j.ID == id {
			job = j
			found = true
			break
		}
	}
	if !found {
		r.mu.Unlock()
		return Result{}, ErrUnknownJob
	}
	lane := r.shared
	if job.Expiry {
		lane = r.expiry
	}
	if r.active[id] {
		r.mu.Unlock()
		return Result{}, ErrBusy
	}
	select {
	case lane <- struct{}{}:
	default:
		r.mu.Unlock()
		return Result{}, ErrBusy
	}
	r.active[id] = true
	r.wg.Add(1)
	root := r.root
	r.mu.Unlock()
	defer func() { <-lane; r.mu.Lock(); delete(r.active, id); r.mu.Unlock(); r.wg.Done() }()
	parent = observability.NewContext(parent)
	ctx, cancel := context.WithTimeout(parent, job.Timeout)
	defer cancel()
	if root != nil {
		stop := context.AfterFunc(root, cancel)
		defer stop()
	}
	owner := uuid.NewString()
	started := time.Now()
	acquired, err := r.store.Acquire(ctx, id, owner, job.Timeout+2*time.Second)
	if err != nil {
		return Result{}, err
	}
	if !acquired {
		return Result{}, ErrBusy
	}
	result, runErr := invoke(ctx, job.Run)
	// D-05: the operation IS the job ID — one distinct operation per
	// job in the duration histogram. There is no job named
	// "reconciliation"; the old shared label made five different jobs
	// indistinguishable. Unknown IDs collapse to "other" in Observe.
	operation := job.ID
	outcome := "success"
	if runErr != nil {
		outcome = "error"
	}
	observability.LogOperation(ctx, "worker", operation, outcome, time.Since(started))
	// Cancellation is detached ONLY for bounded metadata, never for business work.
	metadata, metadataCancel := context.WithTimeout(context.WithoutCancel(parent), time.Second)
	defer metadataCancel()
	o := Outcome{Job: id, Owner: owner, Started: started, Duration: time.Since(started), Result: result, ErrorCode: ErrorCode(runErr)}
	recordErr := r.store.Complete(metadata, o)
	if recordErr != nil && metadata.Err() == nil {
		recordErr = r.store.Complete(metadata, o)
	}
	releaseErr := r.store.Release(metadata, id, owner)
	if recordErr != nil || releaseErr != nil {
		log.Printf("[ERROR] background job %s outcome recording failed", id)
		return result, errors.Join(runErr, ErrMetadata)
	}
	return result, runErr
}
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, ErrBusy):
		return "busy"
	case errors.Is(err, ErrMetadata):
		return "metadata_failed"
	default:
		return "job_failed"
	}
}

// Panics at a callback boundary are failures with safe fixed error codes.
func invoke(ctx context.Context, run func(context.Context) (Result, error)) (result Result, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("job callback failed")
		}
	}()
	return run(ctx)
}
