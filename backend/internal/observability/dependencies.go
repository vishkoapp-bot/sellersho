package observability

import (
	"context"
	"github.com/redis/go-redis/v9"
	"log"
	"net"
	"strings"
	"time"
)

// RedisHook measures all commands, including Lua scripts, without ever reading
// command arguments, keys or results. Pipelines are one bounded observation.
type RedisHook struct{ Registry *Registry }

func (h RedisHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}
func outcome(ctx context.Context, err error) string {
	if err == nil || err == redis.Nil {
		return "success"
	}
	if ctx.Err() == context.DeadlineExceeded {
		return "timeout"
	}
	if ctx.Err() == context.Canceled {
		return "canceled"
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		return "timeout"
	}
	return "error"
}
func (h RedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		h.Registry.Observe("redis", cmd.Name(), outcome(ctx, err), time.Since(start))
		return err
	}
}
func (h RedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		h.Registry.Observe("redis", "pipeline", outcome(ctx, err), time.Since(start))
		return err
	}
}
func (m *Registry) SetGauge(name string, value float64) {
	name = bounded(name, "postgres_up", "redis_up", "pg_connections", "pg_locked", "pg_deadlocks_total", "pg_transactions_total", "redis_pool_hits_total", "redis_pool_misses_total", "redis_pool_timeouts_total", "redis_pool_connections", "worker_expiry_last_success_timestamp_seconds", "worker_expiry_backlog", "worker_expiry_oldest_age_seconds", "backup_last_success_timestamp_seconds", "disk_free_ratio", "pg_active_connections", "pg_idle_connections")
	if name == "other" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges[name] = value
}
func (m *Registry) PollDependencies(ctx context.Context, client *redis.Client) {
	var previousRequests, previousErrors int64
	collect := func() {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		m.mu.Lock()
		db := m.db
		m.mu.Unlock()
		if db != nil {
			up := float64(1)
			if db.PingContext(c) != nil {
				up = 0
			}
			m.SetGauge("postgres_up", up)
			if up == 1 {
				var n, locked, active, idle float64
				err := db.QueryRowContext(c, `SELECT count(*),count(*) FILTER(WHERE wait_event_type='Lock'),count(*) FILTER(WHERE state='active'),count(*) FILTER(WHERE state='idle') FROM pg_stat_activity WHERE datname=current_database()`).Scan(&n, &locked, &active, &idle)
				if err == nil {
					m.SetGauge("pg_connections", n)
					m.SetGauge("pg_locked", locked)
					m.SetGauge("pg_active_connections", active)
					m.SetGauge("pg_idle_connections", idle)
				}
				var deadlocks, queries float64
				if db.QueryRowContext(c, `SELECT deadlocks,xact_commit+xact_rollback FROM pg_stat_database WHERE datname=current_database()`).Scan(&deadlocks, &queries) == nil {
					m.SetGauge("pg_deadlocks_total", deadlocks)
					m.SetGauge("pg_transactions_total", queries)
				}
				var last float64
				if db.QueryRowContext(c, `SELECT COALESCE(EXTRACT(EPOCH FROM MAX(started_at))::float8,0) FROM operations_job_runs WHERE job='unpaid-order-expiry' AND error=''`).Scan(&last) == nil {
					m.SetGauge("worker_expiry_last_success_timestamp_seconds", last)
					var backlog, oldest float64
					if db.QueryRowContext(c, `SELECT count(*),COALESCE(EXTRACT(EPOCH FROM (now()-MIN(payment_expires_at))),0)::float8 FROM orders WHERE payment_status='pending' AND status IN ('pending','processing') AND payment_expires_at<=now()`).Scan(&backlog, &oldest) == nil {
						m.SetGauge("worker_expiry_backlog", backlog)
						m.SetGauge("worker_expiry_oldest_age_seconds", oldest)
					}
				}
			}
		}
		if client != nil {
			up := float64(1)
			if client.Ping(c).Err() != nil {
				up = 0
			}
			m.SetGauge("redis_up", up)
			s := client.PoolStats()
			m.SetGauge("redis_pool_hits_total", float64(s.Hits))
			m.SetGauge("redis_pool_misses_total", float64(s.Misses))
			m.SetGauge("redis_pool_timeouts_total", float64(s.Timeouts))
			m.SetGauge("redis_pool_connections", float64(s.TotalConns))
		}
		ratio, known := diskFreeRatio()
		if known {
			m.SetGauge("disk_free_ratio", ratio)
		}
		m.mu.Lock()
		var requests, errors int64
		for key, s := range m.http {
			if strings.Contains(key, `class="api"`) {
				requests += s.Count
				if strings.Contains(key, `status="5`) {
					errors += s.Count
				}
			}
		}
		last, jobKnown := m.gauges["worker_expiry_last_success_timestamp_seconds"]
		backup, backupKnown := m.gauges["backup_last_success_timestamp_seconds"]
		snap := AlertSnapshot{Requests: requests - previousRequests, Errors: errors - previousErrors, DependencyLost: m.gauges["postgres_up"] == 0 || m.gauges["redis_up"] == 0, JobStale: jobKnown && time.Now().Unix()-int64(last) > 180, DiskKnown: known, DiskFreeRatio: ratio, BackupFailed: backupKnown && time.Now().Unix()-int64(backup) > 93600}
		m.mu.Unlock()
		previousRequests = requests
		previousErrors = errors
		if err := m.DeliverAlerts(c, snap); err != nil {
			log.Print("admin operations alert pending; database delivery unavailable")
		}
	}
	collect()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}
