package main

import (
	"context"
	"github.com/drup/platform/internal/config"
	"github.com/drup/platform/internal/lifecycle"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/drup/platform/internal/testredis"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func TestHealthBoundsPoolExhaustion(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	h := legacyHealthHandler(&repository.PostgresDB{DB: db}, nil, &config.Config{Environment: "test"})
	done := make(chan struct{})
	w := httptest.NewRecorder()
	go func() { defer close(done); h.ServeHTTP(w, httptest.NewRequest("GET", "/api/health", nil)) }()
	select {
	case <-done:
		if w.Code != 503 {
			t.Fatal("dependency down health", w.Code)
		}
	case <-time.After(650 * time.Millisecond):
		conn.Close()
		<-done
		t.Fatal("pool-exhausted health exceeded 500ms plus scheduling allowance")
	}
}

func TestRealDependenciesPoolFloodAndDownReadiness(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cache := testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))
	s := lifecycle.New(db.PingContext, func(ctx context.Context) error { return cache.Ping(ctx).Err() }, "test", true)
	s.Initialize()
	check := func(want int) {
		t.Helper()
		w := httptest.NewRecorder()
		start := time.Now()
		s.Ready(w, httptest.NewRequest("GET", "/api/ready", nil))
		if w.Code != want || time.Since(start) > 650*time.Millisecond {
			t.Fatal("readiness", w.Code, time.Since(start))
		}
	}
	check(200)
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before := db.Stats().WaitCount
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); check(503) }()
	deadline := time.Now().Add(time.Second)
	for db.Stats().WaitCount == before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); check(503) }()
	}
	wg.Wait()
	if got := db.Stats().WaitCount - before; got != 1 {
		t.Fatal("readiness flood multiplied SQL pool waits", got)
	}
	conn.Close()
	check(200)
	cache.Close()
	check(503)
	db.Close()
	check(503)
	w := httptest.NewRecorder()
	s.Live(w, httptest.NewRequest("GET", "/api/live", nil))
	if w.Code != 200 {
		t.Fatal("dependency outage changed liveness")
	}
}
