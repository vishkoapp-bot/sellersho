package main

import "net/http"

// healthStatus maps the already-measured Postgres/Redis liveness into
// the /api/health top-level status and HTTP code (Team 10 live parity).
//
// Healthy case is unchanged: status "ok", HTTP 200, with the caller
// still reporting postgres:"up" and redis:"up" in the payload.
// Degraded case: any dependency down -> status "degraded" and HTTP 503,
// so a monitor reading only "status" (or only the HTTP code) can no
// longer mistake a DB/Redis outage for a healthy service.
//
// U61 CAVEAT: live DB-down behaviour is unverified -- Phase 1
// deliberately never stopped the live database to test it. This is a
// documented honesty fix for the source gap, not a claim of live parity.
func healthStatus(postgresUp, redisUp bool) (string, int) {
	if postgresUp && redisUp {
		return "ok", http.StatusOK
	}
	return "degraded", http.StatusServiceUnavailable
}
