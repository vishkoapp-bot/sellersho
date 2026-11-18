package observability

import (
	"context"
	"fmt"
	"github.com/drup/platform/internal/testdb"
	"github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHTTPBoundedLabelsAndCorrelation(t *testing.T) {
	m := New()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orders/", func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) == "" {
			t.Error("missing correlation")
		}
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(409)
		w.Write([]byte("no"))
	})
	h := m.HTTP(mux, mux)
	for i := 0; i < 1000; i++ {
		r := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/orders/private-%d?token=SECRET", i), strings.NewReader("abc"))
		r.Header.Set("Authorization", "SECRET")
		r.Header.Set("X-Request-ID", "untrusted\nSECRET")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if len(w.Header().Get("X-Request-ID")) != 32 {
			t.Fatal("unsafe request id")
		}
	}
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	s := w.Body.String()
	for _, v := range []string{"SECRET", "private-", "token", "Authorization"} {
		if strings.Contains(s, v) {
			t.Fatalf("leaked %s", v)
		}
	}
	for _, v := range []string{`route="/api/v1/orders/*"`, `status="409"`, ` 1000`, "sellersho_http_duration_seconds_bucket", "sellersho_http_request_bytes_total", "sellersho_http_response_bytes_total"} {
		if !strings.Contains(s, v) {
			t.Fatalf("missing %s", v)
		}
	}
	if len(m.http) > 1 {
		t.Fatal("unbounded routes")
	}
}
func TestMetricsListenerRejectsNonLoopback(t *testing.T) {
	// Pinned NEW contract (OBS-01 deploy-fix, 2026-11-18): the metrics
	// listener may bind literal loopback OR all interfaces (0.0.0.0) —
	// the safety boundary moved from the in-container bind to the host
	// loopback-only publish, because a loopback-bound socket is
	// unreachable at the container's eth0 IP and Docker publishing
	// forwards exactly there. Still rejected: a missing host, any
	// hostname (loopback names included — names are not a bind
	// guarantee), any specific non-loopback IP, and the IPv6
	// unspecified address (no IPv6 listener is configured anywhere).
	for _, a := range []string{":9090", "localhost:9090", "example.com:9090", "192.168.1.10:9090", "172.18.0.2:9090", "[::]:9090"} {
		if ValidateListenAddress(a) == nil {
			t.Fatalf("allowed %s", a)
		}
	}
	for _, a := range []string{"127.0.0.1:9090", "0.0.0.0:9090", "[::1]:9090"} {
		if e := ValidateListenAddress(a); e != nil {
			t.Fatalf("rejected %s: %v", a, e)
		}
	}
}

func TestMetricsListenerAllowsAllInterfacesBind(t *testing.T) {
	// The production bind (main.go) must actually listen on 0.0.0.0.
	m := New()
	srv, err := m.Start("0.0.0.0:0")
	if err != nil {
		t.Fatalf("Start(0.0.0.0:0) failed: %v", err)
	}
	defer srv.Close()
}
func TestUnknownOperationCannotGrowMetrics(t *testing.T) {
	m := New()
	for i := 0; i < 1000; i++ {
		m.Observe("redis", fmt.Sprint(i), "success", 0)
	}
	if len(m.ops) != 1 {
		t.Fatal("unbounded operations")
	}
}
func TestCancellationAndPanicAreRecorded(t *testing.T) {
	m := New()
	mux := http.NewServeMux()
	mux.HandleFunc("/panic", func(http.ResponseWriter, *http.Request) { panic("secret") })
	func() {
		defer func() { recover() }()
		m.HTTP(mux, mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/panic", nil))
	}()
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(w.Body.String(), `status="500"`) {
		t.Fatal("panic not recorded")
	}
}

func TestRedisHookRedactsArguments(t *testing.T) {
	m := New()
	h := RedisHook{Registry: m}
	cmd := redis.NewCmd(context.Background(), "eval", "SECRET", 0, "private-key")
	err := h.ProcessHook(func(context.Context, redis.Cmder) error { return context.DeadlineExceeded })(context.Background(), cmd)
	if err == nil {
		t.Fatal("error swallowed")
	}
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	s := w.Body.String()
	if strings.Contains(s, "SECRET") || strings.Contains(s, "private-key") || !strings.Contains(s, `operation="eval"`) {
		t.Fatal("script instrumentation invalid")
	}
}
func TestDependencyCollectionMigratedDatabase(t *testing.T) {
	db, e := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	m := New()
	m.SetDB(db)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.PollDependencies(ctx, nil); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.mu.Lock()
		_, ok := m.gauges["worker_expiry_last_success_timestamp_seconds"]
		m.mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("database collection missing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	for _, v := range []string{"sellersho_db_wait_seconds_total", "sellersho_pg_locked", "sellersho_postgres_up 1", "sellersho_worker_expiry_last_success_timestamp_seconds"} {
		if !strings.Contains(w.Body.String(), v) {
			t.Fatalf("missing %s", v)
		}
	}
}
