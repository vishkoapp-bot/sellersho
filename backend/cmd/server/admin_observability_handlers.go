package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/observability"
)

// handleAdminRequestLookup serves the G-03 correlation-ID lookup:
// GET /api/v1/admin/observability/requests?request_id=<id>
//
// It returns the completed request's route, status, duration and
// operation spans from the in-memory ring (last 1000 completed
// requests; see backend/internal/observability/ring.go). The route in
// main.go guards it with RequireRoles(jwtService, "admin") — the same
// pattern as the admin inbox handlers — so a customer token gets 403
// and no token gets 401 from the middleware.
//
// An ID that is not in the ring (evicted, never seen, or still in
// flight) is a plain 404 with an honest message, never a fabricated
// breakdown. A restart wipes the ring; see
// docs/operations/correlation-lookup.md for retention bounds.
func handleAdminRequestLookup(w http.ResponseWriter, r *http.Request, reg *observability.Registry) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("request_id"))
	if id == "" {
		http.Error(w, `{"error":"request_id is required"}`, http.StatusBadRequest)
		return
	}
	rec, ok := reg.Lookup(id)
	if !ok {
		http.Error(w, `{"error":"request not found: the lookup holds only the last 1000 completed requests"}`, http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(rec)
}
