// Package dependencybudget bounds dependency calls without detached work or queues.
package dependencybudget

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

var ErrOverloaded = errors.New("provider capacity unavailable")
var ErrAmbiguous = errors.New("provider outcome unknown")

type callError struct {
	cause     error
	ambiguous bool
}

func (e *callError) Error() string {
	if e.ambiguous {
		return "provider outcome unknown"
	}
	if errors.Is(e.cause, context.DeadlineExceeded) {
		return "provider deadline exceeded"
	}
	if errors.Is(e.cause, context.Canceled) {
		return "provider request cancelled"
	}
	return "provider request failed"
}
func (e *callError) Unwrap() error        { return e.cause }
func (e *callError) Is(target error) bool { return target == ErrAmbiguous && e.ambiguous }
func IsAmbiguous(err error) bool          { return errors.Is(err, ErrAmbiguous) }
func MarkAmbiguous(err error) error {
	if err == nil || IsAmbiguous(err) {
		return err
	}
	return &callError{cause: err, ambiguous: true}
}

type Policy struct {
	MaxInFlight            int
	Rate                   float64
	Burst                  int
	Budget                 time.Duration
	ReadAttemptTimeout     time.Duration
	MaxReadAttempts        int
	BackoffMin, BackoffMax time.Duration
	Observe                func(context.Context, string, time.Duration)
}

func DefaultPolicy(inflight, rate int) Policy {
	return Policy{MaxInFlight: inflight, Rate: float64(rate), Burst: rate, Budget: 5 * time.Second, ReadAttemptTimeout: 2 * time.Second, MaxReadAttempts: 2, BackoffMin: 100 * time.Millisecond, BackoffMax: 250 * time.Millisecond}
}

type Executor struct {
	policy  Policy
	slots   chan struct{}
	mu      sync.Mutex
	tokens  float64
	updated time.Time
}

func New(p Policy) *Executor {
	if p.MaxInFlight <= 0 || p.Rate <= 0 || p.Burst <= 0 || p.Budget <= 0 || p.ReadAttemptTimeout <= 0 || p.MaxReadAttempts < 1 || p.MaxReadAttempts > 2 || p.BackoffMin < 0 || p.BackoffMax < p.BackoffMin {
		panic("invalid dependency budget")
	}
	return &Executor{policy: p, slots: make(chan struct{}, p.MaxInFlight), tokens: float64(p.Burst), updated: time.Now()}
}

type Reply struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func (e *Executor) takeToken() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	e.tokens += now.Sub(e.updated).Seconds() * e.policy.Rate
	e.updated = now
	if e.tokens > float64(e.policy.Burst) {
		e.tokens = float64(e.policy.Burst)
	}
	if e.tokens < 1 {
		return false
	}
	e.tokens--
	return true
}
func classify(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, ErrOverloaded):
		return "overloaded"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case IsAmbiguous(err):
		return "unknown"
	default:
		return "error"
	}
}
func (e *Executor) Do(parent context.Context, client *http.Client, makeRequest func(context.Context) (*http.Request, error), safeRead bool, maxBody int64) (reply Reply, err error) {
	start := time.Now()
	defer func() {
		if e.policy.Observe != nil {
			e.policy.Observe(parent, classify(err), time.Since(start))
		}
	}()
	if parent.Err() != nil {
		return reply, &callError{cause: parent.Err()}
	}
	if maxBody < 1 {
		return reply, &callError{cause: errors.New("invalid body limit")}
	}
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	default:
		return reply, ErrOverloaded
	}
	ctx, cancel := context.WithTimeout(parent, e.policy.Budget)
	defer cancel()
	// Redirects can replay a POST or add uncounted outbound requests. Keep the
	// supplied transport and timeout, but own every dispatch in this loop.
	boundedClient := *client
	boundedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	attempts := 1
	if safeRead {
		attempts = e.policy.MaxReadAttempts
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if ctx.Err() != nil {
			return Reply{}, &callError{cause: ctx.Err()}
		}
		if !e.takeToken() {
			return Reply{}, ErrOverloaded
		}
		duration := e.policy.Budget
		if safeRead {
			duration = e.policy.ReadAttemptTimeout
		}
		child, stop := context.WithTimeout(ctx, duration)
		req, buildErr := makeRequest(child)
		if buildErr != nil {
			stop()
			return Reply{}, &callError{cause: buildErr}
		}
		req = req.WithContext(child)
		response, callErr := boundedClient.Do(req)
		retry := false
		var body []byte
		if callErr == nil {
			body, callErr = io.ReadAll(io.LimitReader(response.Body, maxBody+1))
			response.Body.Close()
			if callErr == nil && int64(len(body)) > maxBody {
				stop()
				return Reply{}, &callError{cause: errors.New("provider response exceeded bound"), ambiguous: !safeRead}
			}
			if callErr == nil {
				reply = Reply{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: body}
				if response.StatusCode < 200 || response.StatusCode >= 300 {
					callErr = fmt.Errorf("provider HTTP status %d", response.StatusCode)
					retry = response.StatusCode == 408 || response.StatusCode == 429 || response.StatusCode >= 500
				}
			} else {
				retry = true
			}
		} else {
			retry = true
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
		}
		stop()
		if callErr == nil {
			return reply, nil
		}
		if ctx.Err() != nil {
			callErr = ctx.Err()
		}
		// Once dispatched, an HTTP failure alone cannot prove a mutation did not
		// happen. Provider adapters may recognize explicit business rejections
		// only from a complete successful-HTTP acknowledgement.
		unknown := !safeRead
		err = &callError{cause: callErr, ambiguous: unknown}
		if !safeRead || !retry || attempt+1 >= attempts || ctx.Err() != nil {
			return reply, err
		}
		capDelay := e.policy.BackoffMin * time.Duration(1<<uint(attempt+1))
		if capDelay > e.policy.BackoffMax {
			capDelay = e.policy.BackoffMax
		}
		delay := e.policy.BackoffMin
		if capDelay > delay {
			delay += time.Duration(rand.Int64N(int64(capDelay-delay) + 1))
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Reply{}, &callError{cause: ctx.Err()}
		case <-timer.C:
		}
	}
	return reply, err
}
