package observability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Delivery-path note (OBS-01 C-01): the Prometheus rules in
// ops/observability/alerts.yml are fixtures/visualization ONLY — there is
// no Alertmanager, no Grafana contact point, and no other delivery route
// for them anywhere in the stack. The ONLY operator delivery path is this
// in-app poller (DeliverAlerts -> notification_outbox -> the admin-panel
// notification inbox). Every Prometheus rule that must reach an operator
// therefore needs a matching condition in AlertSnapshot.conditions();
// the SLO-breach rules (SellershoLatencySLOBreachRead/Mutation) are
// mirrored by the slo_breach condition below, with the same thresholds.
// Do not describe the Prometheus rules as "alerts" to the operator
// unless a real delivery route for them exists.
type AlertSnapshot struct {
	Requests, Errors                                  int64
	DependencyLost, JobStale, BackupFailed, DiskKnown bool
	DiskFreeRatio                                     float64
	// SLOKnown reports whether the poller measured latency quantiles over
	// the last poll window (see sloFromHTTPDelta). The four quantiles are
	// seconds, per class, over that window; they are only meaningful when
	// SLOKnown is true (too few requests in the window means unknown,
	// never a fabricated healthy value and never a breach).
	SLOKnown                                               bool
	SLOReadP95, SLOReadP99, SLOMutationP95, SLOMutationP99 float64
	// FIN-013 finance alert inputs (audit §5): FinanceInvariantNew is
	// the count of NEW invariant failures + reconciliation differences
	// in the last poll window (a delta, so the condition recovers when
	// the failures stop — a cumulative total could never recover);
	// FinanceBalanceMismatch is the live mismatch gauge (paid orders
	// without a ledger entry, paid-cancelled orders without an
	// obligation); FinanceUnknownOldest is the oldest unknown
	// payment/refund age in seconds (FinanceUnknownKnown false when
	// neither queue query succeeded — unknown, never a fabricated 0).
	FinanceInvariantNew     int64
	FinanceBalanceMismatch  float64
	FinanceUnknownOldest    float64
	FinanceUnknownKnown     bool
}

// Latency SLO bounds (CAP-01, agreed): read p95 <= 300 ms / p99 <=
// 1000 ms; mutation p95 <= 750 ms / p99 <= 2000 ms. These are the SAME
// bounds as the Prometheus rules SellershoLatencySLOBreachRead/Mutation
// (ops/observability/alerts.yml); keep the two in sync. opclass is a
// method proxy (GET/HEAD/OPTIONS = read, everything else = mutation);
// see metrics.go.
const (
	sloReadP95     = 0.3
	sloReadP99     = 1.0
	sloMutationP95 = 0.75
	sloMutationP99 = 2.0
)

// Finance unknown-outcome staleness bound (FIN-013 §5, a proposal for
// the finance reviewer to confirm): an unknown payment or refund older
// than 1 hour needs a human — the reconciler retries on its own
// schedule, but an outcome unknown for an hour means the provider
// never answered and the money is neither confirmed nor failed.
const financeUnknownStaleSeconds = 3600

var alertTitles = map[string]string{"api_errors": "خطای بیش از حد درخواست‌های سایت", "dependency_loss": "قطع ارتباط پایگاه داده یا کش", "job_stale": "توقف اجرای موفق کار زمان‌بندی‌شده", "disk_pressure": "کمبود فضای دیسک سرور", "backup_failure": "خطا یا تأخیر نسخه پشتیبان", "slo_breach": "نقض حد آستانه تأخیر سرویس (SLO)", "finance_invariant": "نقض پایستگی مالی یا عدم تطابق تراز", "finance_unknown_stale": "پرداخت یا استرداد با نتیجه نامشخص قدیمی"}

func (s AlertSnapshot) conditions() map[string]bool {
	return map[string]bool{"api_errors": s.Requests > 0 && float64(s.Errors)/float64(s.Requests) > .001, "dependency_loss": s.DependencyLost, "job_stale": s.JobStale, "disk_pressure": s.DiskKnown && s.DiskFreeRatio < .15, "backup_failure": s.BackupFailed, "slo_breach": s.SLOKnown && (s.SLOReadP95 > sloReadP95 || s.SLOReadP99 > sloReadP99 || s.SLOMutationP95 > sloMutationP95 || s.SLOMutationP99 > sloMutationP99), "finance_invariant": s.FinanceInvariantNew > 0 || s.FinanceBalanceMismatch > 0, "finance_unknown_stale": s.FinanceUnknownKnown && s.FinanceUnknownOldest > financeUnknownStaleSeconds}
}

// diskPressureBand maps a free ratio to a severity band. Band 0 is
// healthy (>= 15% free, the alert threshold). Bands 1-4 are worsening
// incident levels; crossing into a higher band while the incident is
// active re-notifies the operator (C-11) instead of the incident going
// silent until recovery. Improving within the incident (e.g. 8% -> 12%)
// keeps the worst band seen, so a later worsening past it still
// re-notifies; recovery resets the band to 0.
func diskPressureBand(ratio float64) int {
	switch {
	case ratio < 0.02:
		return 4
	case ratio < 0.05:
		return 3
	case ratio < 0.10:
		return 2
	case ratio < 0.15:
		return 1
	default:
		return 0
	}
}

// DeliverAlerts commits incident state and admin inbox rows atomically.
// Repeats and process restarts produce one notification per incident,
// rearmed on recovery — or, for disk_pressure only, on worsening into a
// higher severity band (see diskPressureBand). A worsening that happens
// inside the 15-minute cooldown is remembered as durable
// delivery_pending state and delivered when the cooldown expires, the
// same mechanism that already covers a suppressed new incident. At most
// one notification per code/admin per 15 minutes, even if the monitored
// signal repeatedly crosses its threshold.
// If PostgreSQL is unavailable, pending incidents stay in process memory and are
// retried after recovery. Durable delivery during total DB outage needs an
// independent monitoring host; this method never claims that guarantee.
func (m *Registry) DeliverAlerts(ctx context.Context, s AlertSnapshot) error {
	m.alertMu.Lock()
	defer m.alertMu.Unlock()
	conditions := s.conditions()
	for code, active := range conditions {
		if active {
			m.pending[code] = true
		}
	}
	m.mu.Lock()
	db := m.db
	m.mu.Unlock()
	if db == nil {
		return fmt.Errorf("alert database unavailable")
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return fmt.Errorf("alert transaction unavailable")
	}
	defer tx.Rollback()
	// Fixed iteration order avoids deadlocks between collectors/processes.
	// FIN-013 §13.8 RULE — read before adding a code here: any future
	// alert code MUST widen the observability_alert_state_code_check
	// CHECK in the same migration. DeliverAlerts upserts EVERY code in
	// this list on every tick, so a code the CHECK does not allow
	// violates the constraint, fails the whole alert transaction, and
	// NO alert of any kind is delivered (disk, SLO, job-stale
	// included) — that is exactly how the finance_invariant /
	// finance_unknown_stale codes broke all alert delivery until
	// migration 202611200004 widened the CHECK.
	for _, code := range []string{"api_errors", "dependency_loss", "job_stale", "disk_pressure", "backup_failure", "slo_breach", "finance_invariant", "finance_unknown_stale"} {
		active := conditions[code] || m.pending[code]
		// The severity band is only meaningful for disk_pressure; every
		// other code stores 0. While the disk incident stays active the
		// stored band only ever grows (GREATEST); recovery resets it to 0
		// so the next incident starts from band 1 again.
		band := 0
		if code == "disk_pressure" && s.DiskKnown {
			band = diskPressureBand(s.DiskFreeRatio)
		}
		var changed string
		e = tx.QueryRowContext(ctx, `INSERT INTO observability_alert_state(code,active,delivery_pending,severity_band) VALUES($1,$2,$2,$3) ON CONFLICT(code) DO UPDATE SET delivery_pending=observability_alert_state.delivery_pending OR (EXCLUDED.active AND NOT observability_alert_state.active) OR (EXCLUDED.active AND observability_alert_state.code='disk_pressure' AND EXCLUDED.severity_band>observability_alert_state.severity_band),active=EXCLUDED.active,severity_band=CASE WHEN EXCLUDED.active THEN GREATEST(observability_alert_state.severity_band,EXCLUDED.severity_band) ELSE 0 END,updated_at=now() RETURNING code`, code, active, band).Scan(&changed)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return fmt.Errorf("alert state write failed")
		}
		// At most one notification/code/admin per 15 minutes, even if the
		// monitored signal repeatedly crosses its threshold.
		var due string
		e = tx.QueryRowContext(ctx, `UPDATE observability_alert_state SET notified_at=now(),delivery_pending=false WHERE code=$1 AND delivery_pending AND notified_at<now()-INTERVAL '15 minutes' RETURNING code`, code).Scan(&due)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return fmt.Errorf("alert cooldown write failed")
		}

		_, e = tx.ExecContext(ctx, `INSERT INTO notification_outbox(recipient_user_id,kind,title,body,payload) SELECT u.id,'operations_alert',$2,$3,jsonb_build_object('alertCode',$1::text,'tab','jobs') FROM users u WHERE u.is_active AND (u.role='admin' OR EXISTS(SELECT 1 FROM user_capabilities c WHERE c.user_id=u.id AND c.capability='super_admin' AND c.status='active'))`, code, alertTitles[code], "هشدار عملیاتی سایت؛ داشبورد ظرفیت و وضعیت سرویس را بررسی کنید.")
		if e != nil {
			return fmt.Errorf("admin alert delivery failed")
		}
	}
	if e = tx.Commit(); e != nil {
		return fmt.Errorf("admin alert commit failed")
	}
	for code := range m.pending {
		delete(m.pending, code)
	}
	return nil
}
