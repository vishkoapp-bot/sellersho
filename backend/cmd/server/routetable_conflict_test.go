package main

import (
	"net/http"
	"os"
	"regexp"
	"testing"
)

// TestRouteTableNoConflicts guards the FULL route table registered in
// main(). Handler-level suites boot only their own handler subsets, so a
// Go 1.22+ ServeMux pattern conflict in main() is invisible to them — and
// it is fatal: ServeMux panics at registration time when a new pattern
// overlaps an already-registered one and neither is more specific. That
// exact failure shipped once: the gap-11 route
// "/api/v1/admin/products/{productId}/price-exception" conflicted with the
// pre-existing subtree "/api/v1/admin/products/approve/" and the binary
// crash-looped on boot (both patterns match
// "/api/v1/admin/products/approve/price-exception").
//
// The test parses the pattern literals out of main.go (every mux.Handle /
// mux.HandleFunc call in this codebase takes a string literal as its first
// argument; the count assertion below fails loudly if that ever stops
// being true) and registers them, in source order, into a fresh ServeMux
// with panic recovery per pattern so one run reports EVERY conflict, not
// just the first.
func TestRouteTableNoConflicts(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	callRe := regexp.MustCompile(`mux\.Handle(?:Func)?\(`)
	litRe := regexp.MustCompile(`mux\.Handle(?:Func)?\(\s*"([^"]+)"`)

	calls := callRe.FindAllIndex(src, -1)
	matches := litRe.FindAllSubmatch(src, -1)
	if len(calls) != len(matches) {
		t.Fatalf("route extraction drift: main.go has %d mux.Handle* calls but only %d take a string-literal pattern; extend this extractor before trusting it", len(calls), len(matches))
	}
	if len(matches) < 200 {
		t.Fatalf("route extraction sanity: only %d patterns found; the real table has 270+, so extraction is broken", len(matches))
	}

	mux := http.NewServeMux()
	noop := func(w http.ResponseWriter, r *http.Request) {}
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		pattern := string(m[1])
		if seen[pattern] {
			t.Errorf("duplicate route pattern %q registered in main.go", pattern)
			continue
		}
		seen[pattern] = true
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("route pattern %q conflicts with an earlier registration: %v", pattern, r)
				}
			}()
			mux.HandleFunc(pattern, noop)
		}()
	}
}
