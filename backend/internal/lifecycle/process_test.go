package lifecycle

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadinessSingleProbeAndIndependentLiveness(t *testing.T) {
	var calls atomic.Int32
	began := make(chan struct{})
	s := New(func(ctx context.Context) error { calls.Add(1); close(began); <-ctx.Done(); return ctx.Err() }, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }, "test", true)
	s.Initialize()
	first := make(chan struct{})
	go func() {
		defer close(first)
		s.Ready(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/ready", nil))
	}()
	<-began
	for i := 0; i < 30; i++ {
		w := httptest.NewRecorder()
		started := time.Now()
		s.Ready(w, httptest.NewRequest("GET", "/api/ready", nil))
		if w.Code != 503 || time.Since(started) > 50*time.Millisecond {
			t.Fatal("probe contention queued", w.Code)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("probe concurrency multiplied work", calls.Load())
	}
	w := httptest.NewRecorder()
	s.Live(w, httptest.NewRequest("GET", "/api/live", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	select {
	case <-first:
	case <-time.After(650 * time.Millisecond):
		t.Fatal("readiness exceeded total deadline")
	}
	s.Drain()
	w = httptest.NewRecorder()
	s.Live(w, httptest.NewRequest("GET", "/api/live", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, method := range []string{"GET", "HEAD", "OPTIONS", "POST", "PUT", "PATCH", "DELETE"} {
		w := httptest.NewRecorder()
		s.Admission(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, httptest.NewRequest(method, "/", nil))
		if method == "GET" || method == "HEAD" || method == "OPTIONS" {
			if w.Code != 204 {
				t.Fatal("safe read rejected", method)
			}
		} else if w.Code != 503 || w.Header().Get("Retry-After") != "1" {
			t.Fatal("new mutation admitted", method, w.Code)
		}
	}
}

func TestSignalDrainPreservesAdmittedRequestAndOrdersCleanup(t *testing.T) {
	signal, cancel := context.WithCancel(context.Background())
	defer cancel()
	began, release := make(chan struct{}), make(chan struct{})
	var workerStopped atomic.Bool
	var closes atomic.Int32
	s := New(nil, nil, "test", true)
	server := &http.Server{Handler: s.Admission(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(began)
		select {
		case <-release:
			w.Write([]byte("completed"))
		case <-r.Context().Done():
			t.Error("signal prematurely canceled admitted HTTP")
		}
	}))}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &Process{State: s, Grace: time.Second, Server: server, StopWorker: func(context.Context) error { workerStopped.Store(true); return nil }, Closers: []Closer{func(context.Context) error {
		if !workerStopped.Load() {
			t.Error("dependency closed before worker")
		}
		closes.Add(1)
		return nil
	}}}
	served := make(chan error, 1)
	go func() { served <- p.Serve(signal, listener, nil) }()
	response := make(chan string, 1)
	go func() {
		r, e := http.Get("http://" + listener.Addr().String())
		if e != nil {
			response <- "error"
			return
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		response <- string(b)
	}()
	<-began
	cancel()
	time.Sleep(20 * time.Millisecond)
	close(release)
	if got := <-response; got != "completed" {
		t.Fatal("accepted request lost", got)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	if err := p.Shutdown(); err != nil || closes.Load() != 1 {
		t.Fatal("non-idempotent cleanup", err, closes.Load())
	}
}

func TestForcedDrainAndBlockingCloserStayWithinTotalGrace(t *testing.T) {
	began := make(chan struct{})
	canceled := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(began); <-r.Context().Done(); close(canceled) })}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)
	go func() {
		r, e := http.Get("http://" + listener.Addr().String())
		if e == nil {
			r.Body.Close()
		}
	}()
	<-began
	unblock := make(chan struct{})
	defer close(unblock)
	var workerStopped, closed atomic.Bool
	p := &Process{State: New(nil, nil, "test", true), Grace: 200 * time.Millisecond, Server: server, StopWorker: func(ctx context.Context) error { <-ctx.Done(); workerStopped.Store(true); return ctx.Err() }, Closers: []Closer{func(ctx context.Context) error { closed.Store(true); <-unblock; return nil }}}
	start := time.Now()
	err = p.Shutdown()
	if err == nil || time.Since(start) > 350*time.Millisecond || !closed.Load() {
		t.Fatal("unbounded or missing cleanup", err, time.Since(start), closed.Load())
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("force close did not cancel HTTP")
	}
}

func TestCanceledStartupClosesWithoutStartingWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var started, stopped, metrics, closed atomic.Bool
	p := &Process{State: New(nil, nil, "test", true), Grace: time.Second, StopWorker: func(context.Context) error { stopped.Store(true); return nil }, StopMetrics: func(context.Context) error {
		if !stopped.Load() {
			t.Error("metrics stopped before worker drain")
		}
		metrics.Store(true)
		return nil
	}, Closers: []Closer{func(context.Context) error {
		if !metrics.Load() {
			t.Error("dependency closed before metrics")
		}
		closed.Store(true)
		return nil
	}}}
	if err = p.Serve(ctx, listener, func() { started.Store(true) }); err == nil || started.Load() || !closed.Load() {
		t.Fatal("canceled startup escaped cleanup", err, started.Load(), closed.Load())
	}
	if err = p.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if conn, err := net.DialTimeout("tcp", listener.Addr().String(), 100*time.Millisecond); err == nil {
		conn.Close()
		t.Fatal("canceled startup retained listener")
	}
}
