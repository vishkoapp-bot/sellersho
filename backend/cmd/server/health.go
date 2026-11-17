package main

import (
	"context"
	"github.com/drup/platform/internal/config"
	"github.com/drup/platform/internal/database/seeder"
	"github.com/drup/platform/internal/lifecycle"
	"github.com/drup/platform/internal/repository"
	"net/http"
)

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

// newHealthState shares the production probe wiring with legacy health tests.
func newHealthState(db *repository.PostgresDB, redisClient *repository.RedisClient, cfg *config.Config) *lifecycle.State {
	var pg, cache lifecycle.Probe
	if db != nil {
		pg = db.DB.PingContext
	}
	if redisClient != nil {
		cache = func(ctx context.Context) error { return redisClient.Client.Ping(ctx).Err() }
	}
	return lifecycle.New(pg, cache, cfg.Environment, seeder.IsDevEnvironment(cfg.Environment))
}

func legacyHealthHandler(db *repository.PostgresDB, redisClient *repository.RedisClient, cfg *config.Config) http.HandlerFunc {
	state := newHealthState(db, redisClient, cfg)
	state.Initialize()
	return state.Ready
}
