package requestbudget

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOversizedBodiesCannotReachMutation(t *testing.T) {
	cases := []struct {
		path, method string
		limit        int
	}{
		{"/api/v1/orders", "POST", 2 << 20},
		{"/api/v1/upload/product-image", "POST", 10 << 20},
		{"/api/v1/supplier/products/p/images", "POST", 48 << 20},
		{"/api/v1/supplier/products/p/variants/v/images", "POST", 48 << 20},
		{"/api/v1/supplier/products/p/images/reorder", "POST", 2 << 20},
		{"/api/v1/upload/product-image", "PUT", 2 << 20},
	}
	for _, c := range cases {
		for _, chunked := range []bool{false, true} {
			t.Run(c.path+c.method+map[bool]string{true: "chunked", false: "known"}[chunked], func(t *testing.T) {
				called := false
				h := New(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(201) }))
				r := httptest.NewRequest(c.method, c.path, io.LimitReader(strings.NewReader(strings.Repeat("x", c.limit+1)), int64(c.limit+1)))
				r.Header.Set("Content-Type", "multipart/form-data")
				if chunked {
					r.ContentLength = -1
				} else {
					r.ContentLength = int64(c.limit + 1)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != 413 || called {
					t.Fatalf("oversize admitted mutation: status=%d called=%v", w.Code, called)
				}
			})
		}
	}
}

func TestChunkedOversizeRejectsBeforeClientFinishesBody(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(New(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Store(true); w.WriteHeader(201) })))
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	start := time.Now()
	fmt.Fprintf(conn, "POST /api/v1/orders HTTP/1.1\r\nHost: fixture\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n", (2<<20)+1)
	if _, err = io.WriteString(conn, strings.Repeat("x", (2<<20)+1)); err != nil {
		t.Fatal(err)
	}
	// Deliberately leave the chunk terminator and final chunk unsent.
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal("oversize response waited for the unfinished body", err)
	}
	defer response.Body.Close()
	if response.StatusCode != 413 || called.Load() || time.Since(start) > time.Second {
		t.Fatal("streamed oversize reached mutation or delayed rejection", response.StatusCode, called.Load())
	}
}

func TestHealthBypassesHeldApplicationAdmission(t *testing.T) {
	cfg := DefaultConfig()
	cfg.APIConcurrent = 1
	began, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	h := New(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/orders" {
			close(began)
			<-release
		}
		w.WriteHeader(200)
	}))
	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/orders", nil))
	<-began
	for _, path := range []string{"/api/live", "/api/ready", "/api/health"} {
		w := httptest.NewRecorder()
		start := time.Now()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || time.Since(start) > 100*time.Millisecond {
			t.Fatal("health blocked on application admission", path, w.Code)
		}
	}
}
