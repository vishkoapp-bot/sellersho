package main

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// staticFileHandler serves ./static (product-upload images and other
// backend-served files) with an ETag validator.
//
// EDGE-01 step 6 follow-up: the Go FileServer emits Last-Modified
// but no ETag, and nginx cannot synthesize an ETag for a proxied
// response (`etag on` was verified to be a no-op on proxy locations),
// so /static/ uploads had no ETag at all and conditional requests
// could only use If-Modified-Since. This wrapper adds a strong ETag
// derived from (modtime, size) and answers If-None-Match with 304
// without reading the file.
//
// The ETag is deliberately NOT content-hashed: upload URLs are stable
// but their content can change in place, so modtime+size changes
// whenever the file is replaced, and the short nginx Cache-Control
// (public, max-age=3600, must-revalidate) still bounds staleness.
// No Cache-Control is set here — nginx owns that header.
func staticFileHandler() http.Handler {
	return http.StripPrefix("/static/", withStaticETag("./static", http.FileServer(http.Dir("./static"))))
}

func withStaticETag(dir string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if rel == "" || rel == "." {
			next.ServeHTTP(w, r)
			return
		}
		// Same join FileServer's http.Dir uses, so the stat path is
		// exactly the file the server would open — no new traversal
		// surface.
		fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || fi.IsDir() {
			next.ServeHTTP(w, r)
			return
		}
		etag := fmt.Sprintf(`"%x-%x"`, fi.ModTime().UnixNano(), fi.Size())
		w.Header().Set("ETag", etag)
		if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// etagMatches reports whether an If-None-Match header matches etag
// under the weak comparison function RFC 7232 requires for
// If-None-Match (a W/ prefix on either side still matches), or is "*".
func etagMatches(inm, etag string) bool {
	for _, tag := range strings.Split(inm, ",") {
		tag = strings.TrimSpace(tag)
		tag = strings.TrimPrefix(tag, "W/")
		if tag == etag || tag == "*" {
			return true
		}
	}
	return false
}
