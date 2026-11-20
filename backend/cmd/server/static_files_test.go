package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEDGE01StaticETag (EDGE-01 step 6 follow-up): /static/ files
// carry a strong ETag and answer If-None-Match with 304; missing
// files 404 without an ETag; replacing a file changes its ETag.
func TestEDGE01StaticETag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.png"), []byte("fake-png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := http.StripPrefix("/static/", withStaticETag(dir, http.FileServer(http.Dir(dir))))
	get := func(path string, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w := get("/static/test.png", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", w.Code, w.Body)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("GET /static/test.png has no ETag")
	}
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Fatalf("ETag %q is not a quoted strong validator", etag)
	}
	if w.Header().Get("Last-Modified") == "" {
		t.Fatal("GET /static/test.png has no Last-Modified (FileServer regression)")
	}
	if w.Header().Get("Cache-Control") != "" {
		t.Fatalf("backend sets Cache-Control %q; nginx owns that header", w.Header().Get("Cache-Control"))
	}

	// Exact match -> 304, empty body.
	w = get("/static/test.png", map[string]string{"If-None-Match": etag})
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("If-None-Match exact = %d body %d bytes, want 304 empty", w.Code, w.Body.Len())
	}
	// Weak comparison (RFC 7232): W/"..." matches the strong ETag.
	w = get("/static/test.png", map[string]string{"If-None-Match": "W/" + etag})
	if w.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match weak = %d, want 304", w.Code)
	}
	// Star matches anything present.
	w = get("/static/test.png", map[string]string{"If-None-Match": "*"})
	if w.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match * = %d, want 304", w.Code)
	}
	// Non-matching tag -> 200 with the body.
	w = get("/static/test.png", map[string]string{"If-None-Match": `"no-such-etag"`})
	if w.Code != http.StatusOK || w.Body.Len() == 0 {
		t.Fatalf("If-None-Match wrong = %d body %d, want 200 with body", w.Code, w.Body.Len())
	}
	// Missing file -> 404, no ETag.
	w = get("/static/gone.png", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing = %d, want 404", w.Code)
	}
	if w.Header().Get("ETag") != "" {
		t.Fatalf("404 carries ETag %q", w.Header().Get("ETag"))
	}
	// Replacing the file changes the ETag (different size; the
	// mtime is also bumped past the previous value).
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "test.png"), []byte("fake-png-bytes-replaced-longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	w = get("/static/test.png", nil)
	if got := w.Header().Get("ETag"); got == etag {
		t.Fatalf("ETag unchanged after replace: %q", got)
	}
	// The OLD tag no longer matches -> 200.
	w = get("/static/test.png", map[string]string{"If-None-Match": etag})
	if w.Code != http.StatusOK {
		t.Fatalf("stale If-None-Match = %d, want 200", w.Code)
	}
	// HEAD keeps the ETag and serves no body.
	r := httptest.NewRequest(http.MethodHead, "/static/test.png", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("ETag") == "" || w.Body.Len() != 0 {
		t.Fatalf("HEAD = %d ETag %q body %d, want 200 + ETag + empty", w.Code, w.Header().Get("ETag"), w.Body.Len())
	}
}
