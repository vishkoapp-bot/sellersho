// Package lifecycle owns process readiness, drain admission and bounded teardown.
package lifecycle

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

type Probe func(context.Context) error

// RejectReporter receives drain rejections (D-06). Same shape as
// requestbudget.RejectReporter, defined locally so lifecycle stays free
// of the observability import; production wires observability.Default.
type RejectReporter interface {
	RecordReject(reason string)
}

type State struct {
	initialized     atomic.Bool
	draining        atomic.Bool
	slot            chan struct{}
	postgres, redis Probe
	environment     string
	development     bool
	reporter        RejectReporter
}

func New(postgres, redis Probe, environment string, development bool) *State {
	return &State{slot: make(chan struct{}, 1), postgres: postgres, redis: redis, environment: environment, development: development}
}

// SetRejectReporter wires the D-06 drain-reject counter. Nil disables it.
func (s *State) SetRejectReporter(r RejectReporter) { s.reporter = r }
func (s *State) Initialize()                        { s.initialized.Store(true) }
func (s *State) Drain()                             { s.draining.Store(true) }
func (s *State) Live(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
func (s *State) Ready(w http.ResponseWriter, r *http.Request) {
	pg, redis := "down", "down"
	ready := s.initialized.Load() && !s.draining.Load()
	if ready {
		select {
		case s.slot <- struct{}{}:
			defer func() { <-s.slot }()
			ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
			defer cancel()
			if probe(ctx, s.postgres) == nil {
				pg = "up"
			}
			if probe(ctx, s.redis) == nil {
				redis = "up"
			}
		default:
			ready = false
		}
	}
	ready = ready && pg == "up" && redis == "up" && !s.draining.Load()
	status, code := "degraded", http.StatusServiceUnavailable
	if ready {
		status, code = "ok", http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"status": status, "engine": "Go Modular Monolith v1.0.0", "environment": s.environment, "isDev": s.development, "postgres": pg, "redis": redis, "timestamp": time.Now().UTC().Format(time.RFC3339)})
}
func probe(parent context.Context, p Probe) error {
	ctx, cancel := context.WithTimeout(parent, 250*time.Millisecond)
	defer cancel()
	if p == nil {
		return context.Canceled
	}
	return p(ctx)
}
func (s *State) Admission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.draining.Load() && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if s.reporter != nil {
				s.reporter.RecordReject("service_draining")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"code":"service_draining","error":"Service is restarting; retry with the same request identifier.","retryable":true}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
