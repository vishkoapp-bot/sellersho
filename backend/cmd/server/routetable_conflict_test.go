package main

import (
	"net/http"
	"os"
	"regexp"
	"strings"
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

// TestManualDispatchOrderingInvariants extends the route-table gate
// to the manual sub-path dispatch inside single mux patterns, which
// TestRouteTableNoConflicts cannot see (2026-11-13, variants P0/P1/P2
// critic finding 8). Those branches are plain if-chains on the URL
// suffix/shape, so they cannot be registered in a ServeMux — but the
// two failure classes that matter there are statically checkable:
//   - ORDER: a variant-scoped branch must precede the product-level
//     branch that would otherwise swallow the same suffix (a variant
//     images path also ends in "/images"); the check is the source
//     order of the two handler calls inside the
//     /api/v1/supplier/products/ block.
//   - DISJOINTNESS: the seller visibility branch (6 parts) and the
//     default-variant branch (4 parts) match on different part
//     counts, so no path can match both; likewise the variant DELETE
//     (5 parts, "variants"/"images") vs the product DELETE (3 parts,
//     "images").
// The assertions are source-position checks on main.go and fail
// loudly (like the extraction count above) if a refactor moves or
// renames a branch, so a human re-verifies the dispatch instead of
// the gate silently going blind. They do NOT execute the dispatch —
// a live boot test against real PG+Redis is still required before
// deploy (Nov 5 rule).
func TestManualDispatchOrderingInvariants(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(src)
	index := func(marker string) int {
		i := strings.Index(text, marker)
		if i < 0 {
			t.Fatalf("manual-dispatch marker %q not found in main.go; the dispatch was refactored — update this gate, do not delete it", marker)
		}
		return i
	}
	// Supplier block: every variant branch must precede its
	// product-level sibling. Markers include the call-site "(w, r,"
	// so a COMMENT that merely names the product handler (e.g. the
	// P0 ordering comment above the variant branch) cannot satisfy
	// — or break — the position check; bare handler names are not
	// unique in main.go.
	variantFirst := [][2]string{
		{"HandleReorderVariantImages(w, r,", "HandleReorderProductImages(w, r,"},
		{"HandleGetVariantImages(w, r,", "HandleGetProductImages(w, r,"},
		{"HandleUploadVariantImages(w, r,", "HandleUploadProductImages(w, r,"},
		{"HandleDeleteVariantImage(w, r,", "HandleDeleteProductImage(w, r,"},
	}
	for _, pair := range variantFirst {
		if index(pair[0]) > index(pair[1]) {
			t.Errorf("manual dispatch order: %s must precede %s in main.go (the variant branch would otherwise be swallowed by the product branch)", pair[0], pair[1])
		}
	}
	// Seller block: the two branches must exist with different part
	// counts (disjoint shapes). Scoped to the
	// /api/v1/seller/stores/ block so identically-shaped branches in
	// other blocks cannot satisfy the check by accident.
	sellerMarker := `mux.HandleFunc("/api/v1/seller/stores/",`
	sellerStart := index(sellerMarker)
	sellerRest := text[sellerStart+len(sellerMarker):]
	sellerEnd := strings.Index(sellerRest, "mux.HandleFunc(")
	if sellerEnd < 0 {
		t.Fatal("seller stores block has no following mux.HandleFunc; cannot scope the disjointness check")
	}
	seller := sellerRest[:sellerEnd]
	for _, want := range []string{
		`len(parts) == 6 && parts[1] == "products" && parts[3] == "variants" && parts[5] == "visibility"`,
		`len(parts) == 4 && parts[1] == "products" && parts[3] == "default-variant"`,
	} {
		if !strings.Contains(seller, want) {
			t.Errorf("seller stores dispatch is missing branch %q (visibility and default-variant must match on disjoint part counts)", want)
		}
	}
}
