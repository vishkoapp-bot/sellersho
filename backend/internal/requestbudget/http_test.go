package requestbudget

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func shortConfig() Config {
	c := DefaultConfig()
	c.API = 25 * time.Millisecond
	c.Upload = 100 * time.Millisecond
	c.Domain = 15 * time.Millisecond
	c.WriteMargin = 20 * time.Millisecond
	return c
}
func TestDeadlineReturnsUnknownMutationWithoutStartingDetachedWork(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		h := New(shortConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
			w.WriteHeader(201)
			w.Write([]byte(`{"success":true}`))
		}))
		w := httptest.NewRecorder()
		start := time.Now()
		h.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/orders", nil))
		if w.Code != 504 || strings.Contains(w.Body.String(), "success") || time.Since(start) > 250*time.Millisecond {
			t.Fatalf("deadline response %d %s", w.Code, w.Body.String())
		}
		var response map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response["code"] != "request_timeout" || response["error"] == "request_timeout" {
			t.Fatal("customer-facing timeout must explain uncertainty, with a separate machine code")
		}
		if method == "POST" && !strings.Contains(w.Body.String(), `"outcome":"unknown"`) {
			t.Fatal("mutation timeout claimed a known failure")
		}
	}
}
func TestUploadBudgetAppliesOnlyToExactUploadCollections(t *testing.T) {
	cases := []struct {
		method, path string
		want         int
	}{{"POST", "/api/v1/upload/product-image", 200}, {"POST", "/api/v1/supplier/products/p/images", 200}, {"POST", "/api/v1/supplier/products/p/variants/v/images", 200}, {"GET", "/api/v1/supplier/products/p/images", 504}, {"POST", "/api/v1/supplier/products/p/images/reorder", 504}, {"POST", "/api/v1/orders", 504}}
	for _, c := range cases {
		t.Run(c.method+c.path, func(t *testing.T) {
			h := New(shortConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-time.After(50 * time.Millisecond):
					w.WriteHeader(200)
				case <-r.Context().Done():
					w.WriteHeader(500)
				}
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
			if w.Code != c.want {
				t.Fatalf("upload classification %d want%d", w.Code, c.want)
			}
		})
	}
}
func TestAdmissionRejectsImmediatelyThenReleases(t *testing.T) {
	c := DefaultConfig()
	c.APIConcurrent = 1
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	h := New(c)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-release; w.WriteHeader(200) }))
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/products", nil))
		done <- w.Code
	}()
	<-entered
	w := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/orders", nil))
	if w.Code != 503 || w.Header().Get("Retry-After") != "1" || time.Since(start) > 100*time.Millisecond {
		t.Fatal("admission queued or failed to reject", w.Code)
	}
	close(release)
	if <-done != 200 {
		t.Fatal("admitted request failed")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/products", nil))
	if w.Code != 200 {
		t.Fatal("capacity was not released")
	}
}
func TestParentDeadlineIsPreserved(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	deadline, _ := parent.Deadline()
	h := New(DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := r.Context().Deadline()
		if !ok || got.After(deadline) {
			t.Error("parent deadline extended")
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/orders", nil).WithContext(parent))
}
func TestStartedResponseIsAbortedOnDeadline(t *testing.T) {
	s := httptest.NewServer(New(shortConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"partial":`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})))
	defer s.Close()
	res, err := s.Client().Get(s.URL + "/api/v1/products")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if _, err = io.ReadAll(res.Body); err == nil {
		t.Fatal("partial response ended as clean success")
	}
}
func TestSlowRequestBodyHasSocketDeadline(t *testing.T) {
	s := httptest.NewServer(New(shortConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.ReadAll(r.Body); w.WriteHeader(500) })))
	defer s.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(s.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	io.WriteString(conn, "POST /api/v1/orders HTTP/1.1\r\nHost: test\r\nContent-Length: 100\r\n\r\nx")
	start := time.Now()
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 504 || time.Since(start) > 300*time.Millisecond {
		t.Fatalf("body read escaped route budget %d", res.StatusCode)
	}
}
