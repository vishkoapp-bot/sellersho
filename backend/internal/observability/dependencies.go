package observability

import (
	"context"
	"database/sql"
	"github.com/redis/go-redis/v9"
	"log"
	"math"
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
		d := time.Since(start)
		h.Registry.Observe("redis", cmd.Name(), outcome(ctx, err), d)
		// G-03: attach a Redis span to the request's pending ring
		// record. cmd.Name() is the bounded command name only —
		// arguments are never read (see the type comment above).
		h.Registry.RecordSpan(ctx, "redis", cmd.Name(), outcome(ctx, err), d)
		return err
	}
}
func (h RedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		d := time.Since(start)
		h.Registry.Observe("redis", "pipeline", outcome(ctx, err), d)
		// G-03: pipeline span, same as ProcessHook above.
		h.Registry.RecordSpan(ctx, "redis", "pipeline", outcome(ctx, err), d)
		return err
	}
}
func (m *Registry) SetGauge(name string, value float64) {
	// Per-job worker gauges (D-04): last-success timestamp, backlog
	// count and oldest-age for ALL six jobs. The legacy worker_expiry_*
	// names are kept as aliases of unpaid-order-expiry so existing
	// dashboard panels and gate G-02 keep working.
	name = bounded(name, "postgres_up", "redis_up", "pg_connections", "pg_locked", "pg_deadlocks_total", "pg_transactions_total", "redis_pool_hits_total", "redis_pool_misses_total", "redis_pool_timeouts_total", "redis_pool_connections", "worker_expiry_last_success_timestamp_seconds", "worker_expiry_backlog", "worker_expiry_oldest_age_seconds", "worker_unpaid_order_expiry_last_success_timestamp_seconds", "worker_unpaid_order_expiry_backlog", "worker_unpaid_order_expiry_oldest_age_seconds", "worker_ad_cleanup_last_success_timestamp_seconds", "worker_ad_cleanup_backlog", "worker_ad_cleanup_oldest_age_seconds", "worker_qc_response_deadline_last_success_timestamp_seconds", "worker_qc_response_deadline_backlog", "worker_qc_response_deadline_oldest_age_seconds", "worker_supplier_sla_last_success_timestamp_seconds", "worker_supplier_sla_backlog", "worker_supplier_sla_oldest_age_seconds", "worker_delivery_lock_releaser_last_success_timestamp_seconds", "worker_delivery_lock_releaser_backlog", "worker_delivery_lock_releaser_oldest_age_seconds", "worker_monthly_settlement_last_success_timestamp_seconds", "worker_monthly_settlement_backlog", "worker_monthly_settlement_oldest_age_seconds", "backup_last_success_timestamp_seconds", "disk_free_ratio", "pg_active_connections", "pg_idle_connections")
	if name == "other" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges[name] = value
}

// workerJob describes one scheduled job for the poller: its run-history
// source, its backlog query, and its staleness threshold (3*period+
// timeout — the same definition repository.BackgroundJobHealth uses for
// Stale, so the poller and the admin panel agree on what "stale" means).
type workerJob struct {
	id string
	// gaugePrefix is the metric-name slug, e.g. "worker_ad_cleanup".
	gaugePrefix string
	// staleAfter is the seconds a job may go without a successful run.
	staleAfter int64
	// historyTable/historyJobColumn select the last-success source:
	// operations_job_runs (job) for five jobs, job_runs (job_id) for
	// delivery-lock-releaser, which JobStore records there instead.
	historyTable, historyJobColumn string
	// backlogQuery returns (count, oldest-age-seconds) of ready work.
	// It mirrors the ready query in repository.jobBacklog for the same
	// job; that function lives in repository, which imports this
	// package, so the SQL is duplicated here by necessity — keep in sync.
	backlogQuery string
}

var workerJobs = []workerJob{
	{id: "unpaid-order-expiry", gaugePrefix: "worker_unpaid_order_expiry", staleAfter: 18, historyTable: "operations_job_runs", historyJobColumn: "job", backlogQuery: `SELECT count(*),COALESCE(EXTRACT(EPOCH FROM (now()-MIN(payment_expires_at))),0)::float8 FROM orders WHERE payment_status='pending' AND status IN ('pending','processing') AND payment_expires_at<=now()`},
	{id: "ad-cleanup", gaugePrefix: "worker_ad_cleanup", staleAfter: 32, historyTable: "operations_job_runs", historyJobColumn: "job", backlogQuery: `SELECT COUNT(*),COALESCE(GREATEST(0,EXTRACT(EPOCH FROM clock_timestamp()-MIN(due)))::float8,0) FROM (SELECT bucket+INTERVAL '10 minutes' AS due FROM ad_request_limits WHERE bucket<NOW()-INTERVAL '10 minutes' UNION ALL SELECT expires_at+INTERVAL '1 hour' AS due FROM ad_fraud_keys WHERE expires_at<NOW()-INTERVAL '1 hour') q`},
	{id: "qc-response-deadline", gaugePrefix: "worker_qc_response_deadline", staleAfter: 92, historyTable: "operations_job_runs", historyJobColumn: "job", backlogQuery: `SELECT COUNT(*),COALESCE(GREATEST(0,EXTRACT(EPOCH FROM clock_timestamp()-MIN(due)))::float8,0) FROM (SELECT response_due_at AS due FROM qc_discrepancies WHERE status='open' AND responded_at IS NULL AND response_due_at<=NOW() AND escalated_at IS NULL ORDER BY response_due_at LIMIT 1001) q`},
	{id: "supplier-sla", gaugePrefix: "worker_supplier_sla", staleAfter: 93, historyTable: "operations_job_runs", historyJobColumn: "job", backlogQuery: `SELECT COUNT(*),COALESCE(GREATEST(0,EXTRACT(EPOCH FROM clock_timestamp()-MIN(due)))::float8,0) FROM (SELECT COALESCE(m.assessed_at+INTERVAL '30 seconds',s.created_at) AS due FROM suppliers s LEFT JOIN supplier_sla_metrics m ON m.supplier_id=s.id WHERE (m.assessed_at IS NULL OR m.assessed_at<=NOW()-INTERVAL '30 seconds') AND NOT EXISTS(SELECT 1 FROM background_job_candidate_retry r WHERE r.job='supplier-sla' AND r.candidate=s.id AND r.next_retry_at>clock_timestamp()) ORDER BY due LIMIT 1001) q`},
	{id: "delivery-lock-releaser", gaugePrefix: "worker_delivery_lock_releaser", staleAfter: 190, historyTable: "job_runs", historyJobColumn: "job_id", backlogQuery: `SELECT COUNT(*),COALESCE(GREATEST(0,EXTRACT(EPOCH FROM clock_timestamp()-MIN(due)))::float8,0) FROM (SELECT due FROM (SELECT shipped_at+INTERVAL '20 days' AS due FROM orders o WHERE status IN ('shipped','delivered_unconfirmed') AND NOT profit_released AND shipped_at<=NOW()-INTERVAL '20 days' AND NOT EXISTS(SELECT 1 FROM background_job_candidate_retry r WHERE r.job='delivery-confirm' AND r.candidate=o.id AND r.next_retry_at>clock_timestamp()) UNION ALL SELECT return_window_ends_at AS due FROM orders o WHERE status='delivered_confirmed' AND NOT profit_released AND return_window_ends_at<=NOW() AND NOT EXISTS(SELECT 1 FROM background_job_candidate_retry r WHERE r.job='delivery-release' AND r.candidate=o.id AND r.next_retry_at>clock_timestamp())) q ORDER BY due LIMIT 1001) q`},
	{id: "monthly-settlement", gaugePrefix: "worker_monthly_settlement", staleAfter: 184, historyTable: "operations_job_runs", historyJobColumn: "job", backlogQuery: `SELECT COUNT(*),COALESCE(GREATEST(0,EXTRACT(EPOCH FROM clock_timestamp()-MIN(due)))::float8,0) FROM (SELECT p.next_attempt_at AS due FROM monthly_settlement_progress p JOIN monthly_settlement_cycles c ON c.period=p.cycle_period WHERE c.status<>'completed' AND p.state='pending' AND p.next_attempt_at<=NOW() ORDER BY p.next_attempt_at LIMIT 1001) q`},
}

// pollWorkerGauges publishes last-success / backlog / oldest-age gauges
// for ALL six jobs (D-04) and reports whether any job is stale (D-04
// alert input). A query that fails (e.g. a table missing pre-migration)
// leaves that job's gauges untouched instead of failing the whole poll.
// A job with no successful run on record (last == 0) counts as stale,
// matching the previous expiry-only behaviour at boot. monthly-settlement
// is skipped for staleness while settlement is unconfigured
// (settlement_day IS NULL), which is its designed inert default.
func (m *Registry) pollWorkerGauges(c context.Context, db *sql.DB) bool {
	settlementDisabled := false
	_ = db.QueryRowContext(c, `SELECT COALESCE((SELECT settlement_day IS NULL FROM platform_settings ORDER BY id LIMIT 1),TRUE)`).Scan(&settlementDisabled)
	stale := false
	now := time.Now().Unix()
	for _, j := range workerJobs {
		var last float64
		if err := db.QueryRowContext(c, `SELECT COALESCE(EXTRACT(EPOCH FROM MAX(started_at))::float8,0) FROM `+j.historyTable+` WHERE `+j.historyJobColumn+`=$1 AND error=''`, j.id).Scan(&last); err != nil {
			continue
		}
		m.SetGauge(j.gaugePrefix+"_last_success_timestamp_seconds", last)
		if j.backlogQuery != "" {
			var backlog, oldest float64
			if err := db.QueryRowContext(c, j.backlogQuery).Scan(&backlog, &oldest); err == nil {
				m.SetGauge(j.gaugePrefix+"_backlog", backlog)
				m.SetGauge(j.gaugePrefix+"_oldest_age_seconds", oldest)
			}
		}
		if j.id == "monthly-settlement" && settlementDisabled {
			continue
		}
		if last == 0 || now-int64(last) > j.staleAfter {
			stale = true
		}
	}
	// Legacy aliases: the expiry-only gauge names keep publishing the
	// unpaid-order-expiry values (dashboard panels + gate G-02).
	m.mu.Lock()
	last, ok := m.gauges["worker_unpaid_order_expiry_last_success_timestamp_seconds"]
	backlog, backlogOK := m.gauges["worker_unpaid_order_expiry_backlog"]
	oldest, oldestOK := m.gauges["worker_unpaid_order_expiry_oldest_age_seconds"]
	m.mu.Unlock()
	if ok {
		m.SetGauge("worker_expiry_last_success_timestamp_seconds", last)
	}
	if backlogOK {
		m.SetGauge("worker_expiry_backlog", backlog)
	}
	if oldestOK {
		m.SetGauge("worker_expiry_oldest_age_seconds", oldest)
	}
	return stale
}

// pollBackupSignal reads the verified backup-success timestamp published
// by the backup publisher (D-02: scripts/publish-backup-success.sh writes
// a row to backup_signals only after verifying the backup artifacts).
// On success it publishes sellersho_backup_last_success_timestamp_seconds.
// If the table is missing (pre-migration) or no row exists, the gauge
// stays unset and the backup state stays unknown — never fabricated.
func (m *Registry) pollBackupSignal(c context.Context, db *sql.DB) {
	var last float64
	if err := db.QueryRowContext(c, `SELECT EXTRACT(EPOCH FROM MAX(last_success_at))::float8 FROM backup_signals WHERE kind='nightly'`).Scan(&last); err == nil {
		m.SetGauge("backup_last_success_timestamp_seconds", last)
	}
}

// httpHistSnapshot is a copy of one HTTP histogram series' counts, taken
// under the registry lock so the next poll tick can diff it into a window.
type httpHistSnapshot struct {
	count   int64
	buckets []int64
}

// sloSummary is the per-class latency quantiles over one poll window, in
// seconds. known is false when no class had enough requests in the window
// to mean anything (unknown, never a fabricated healthy value).
type sloSummary struct {
	known                                      bool
	readP95, readP99, mutationP95, mutationP99 float64
}

// sloMinSamplesPerClass is the smallest per-class request count in a poll
// window for which a quantile means anything. A 15 s window with a handful
// of requests would let a single slow request trip the SLO alert; the
// Prometheus rules aggregate 5 minutes of traffic instead, so the poller
// requires a minimum sample before it calls a window a breach. Below the
// floor the class is treated as unknown (no alert either way). The
// thresholds themselves are the same as the Prometheus rules (see
// alerts.go); only the window (15 s vs 5 m) and this floor differ, and
// both differences are documented in docs/operations/observability.md.
const sloMinSamplesPerClass = 10

// histogramQuantile returns the smallest bucket bound whose cumulative
// count reaches q*N, or +Inf when the quantile lies past the last finite
// bucket (samples slower than every bound, i.e. in the +Inf bucket).
// counts are CUMULATIVE per-bucket counts in the Prometheus convention
// (counts[i] = samples with duration <= bounds[i]), as produced by
// Registry.add and by sloFromHTTPDelta's diffs — so the total is the
// last bucket's count plus inf, NOT the sum of the entries (summing
// cumulative buckets would inflate N and push every quantile to the
// last bucket).
func histogramQuantile(bounds []float64, counts []int64, inf int64, q float64) float64 {
	if len(counts) == 0 {
		return 0
	}
	total := counts[len(counts)-1] + inf
	if total <= 0 {
		return 0
	}
	rank := int64(math.Ceil(q * float64(total)))
	if rank < 1 {
		rank = 1
	}
	for i, c := range counts {
		if c >= rank {
			return bounds[i]
		}
	}
	return math.Inf(1)
}

// sloFromHTTPDelta diffs the current cumulative HTTP histogram series
// against the previous tick's snapshot and returns the per-class p95/p99
// over the window between the two ticks. prev == nil means "first tick":
// it baselines the snapshot and reports nothing, because there is no
// window yet (and boot-time cumulative history must not trip the alert).
// Only class="api" series feed the SLO, matching the Prometheus rules.
// A series that appears between ticks (a new route) counts its full
// current buckets as the window delta; bucket deltas are clamped at zero
// so a registry reset can never produce a negative count.
func sloFromHTTPDelta(prev map[string]httpHistSnapshot, cur map[string]*sample) (sloSummary, map[string]httpHistSnapshot) {
	next := make(map[string]httpHistSnapshot, len(cur))
	for key, s := range cur {
		b := make([]int64, len(s.Buckets))
		copy(b, s.Buckets)
		next[key] = httpHistSnapshot{count: s.Count, buckets: b}
	}
	var out sloSummary
	if prev == nil {
		return out, next
	}
	readCounts := make([]int64, len(buckets))
	mutCounts := make([]int64, len(buckets))
	var readTotal, mutTotal, readInf, mutInf int64
	for key, s := range cur {
		if !strings.Contains(key, `class="api"`) {
			continue
		}
		var dst []int64
		var total, inf *int64
		switch {
		case strings.Contains(key, `opclass="read"`):
			dst, total, inf = readCounts, &readTotal, &readInf
		case strings.Contains(key, `opclass="mutation"`):
			dst, total, inf = mutCounts, &mutTotal, &mutInf
		default:
			continue
		}
		p, ok := prev[key]
		last := len(s.Buckets) - 1
		curInf := s.Count - s.Buckets[last]
		var dCount, dInf int64
		if ok {
			prevInf := p.count - p.buckets[len(p.buckets)-1]
			dCount = s.Count - p.count
			dInf = curInf - prevInf
		} else {
			dCount, dInf = s.Count, curInf
		}
		if dCount < 0 {
			dCount = 0
		}
		if dInf < 0 {
			dInf = 0
		}
		*total += dCount
		*inf += dInf
		for i := range s.Buckets {
			var d int64
			if ok && i < len(p.buckets) {
				d = s.Buckets[i] - p.buckets[i]
			} else {
				d = s.Buckets[i]
			}
			if d < 0 {
				d = 0
			}
			dst[i] += d
		}
	}
	if readTotal >= sloMinSamplesPerClass {
		out.known = true
		out.readP95 = histogramQuantile(buckets, readCounts, readInf, 0.95)
		out.readP99 = histogramQuantile(buckets, readCounts, readInf, 0.99)
	}
	if mutTotal >= sloMinSamplesPerClass {
		out.known = true
		out.mutationP95 = histogramQuantile(buckets, mutCounts, mutInf, 0.95)
		out.mutationP99 = histogramQuantile(buckets, mutCounts, mutInf, 0.99)
	}
	return out, next
}

func (m *Registry) PollDependencies(ctx context.Context, client *redis.Client) {
	var previousRequests, previousErrors int64
	var prevHTTP map[string]httpHistSnapshot
	collect := func() {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		// jobStale is per-job now (D-04): true when ANY of the six jobs
		// has no successful run within its own threshold. It defaults
		// to false when the DB is unreachable, so a DB outage reports
		// dependency_loss, not a spurious job_stale.
		jobStale := false
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
				// Per-job worker gauges for all six jobs (D-04); the
				// return value feeds the job_stale alert condition.
				jobStale = m.pollWorkerGauges(c, db)
				// Verified backup-success signal (D-02).
				m.pollBackupSignal(c, db)
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
		backup, backupKnown := m.gauges["backup_last_success_timestamp_seconds"]
		// SLO breach over the window since the previous tick (C-01):
		// per-class p95/p99 from the HTTP histogram series the backend
		// already computes, same thresholds as the Prometheus rules.
		// The first tick only baselines (sloFromHTTPDelta returns
		// known=false), so boot-time cumulative history never trips it.
		slo, nextHTTP := sloFromHTTPDelta(prevHTTP, m.http)
		prevHTTP = nextHTTP
		snap := AlertSnapshot{Requests: requests - previousRequests, Errors: errors - previousErrors, DependencyLost: m.gauges["postgres_up"] == 0 || m.gauges["redis_up"] == 0, JobStale: jobStale, DiskKnown: known, DiskFreeRatio: ratio, BackupFailed: backupKnown && time.Now().Unix()-int64(backup) > 93600, SLOKnown: slo.known, SLOReadP95: slo.readP95, SLOReadP99: slo.readP99, SLOMutationP95: slo.mutationP95, SLOMutationP99: slo.mutationP99}
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
