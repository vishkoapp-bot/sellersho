package main

// REL-01 (G9) — release contract endpoint.
//
// /api/version answers the browser/API contract-compatibility question
// at runtime: which exact commit this binary was built from, and the
// contract version it implements. The contract version is the sha256
// of the sorted mux pattern list in main.go (see
// scripts/release/contract_routes.py, which computes the same hash from
// source at release time).
//
// The values are injected by the release build via -ldflags:
//   -X main.ReleaseCommit=<commit> -X main.ReleaseContract=<routes-hash>
// A binary built any other way (go test, local dev) reports the
// defaults below, so "unknown" is never mistaken for a release build.
// The release validator (scripts/release/validate_release.py) and the
// smoke suite (scripts/release/smoke_release.py) compare this
// endpoint's output against the staged dist/contract.json; a mismatch
// fails the release before any traffic switch.

import (
	"encoding/json"
	"net/http"
)

// ReleaseCommit is the exact git commit the binary was built from.
// Set by -ldflags at release-build time; "unknown" otherwise.
var ReleaseCommit = "unknown"

// ReleaseContract is the contract version (routes hash) the binary
// implements. Set by -ldflags at release-build time; "unknown"
// otherwise.
var ReleaseContract = "unknown"

func handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"commit":          ReleaseCommit,
		"contractVersion": ReleaseContract,
		"routesHash":      ReleaseContract,
	})
}
