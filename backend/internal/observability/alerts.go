package observability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AlertSnapshot is supplied only by trusted internal collectors. No public
// endpoint accepts metric values or alert bodies from request input.
type AlertSnapshot struct {
	Requests, Errors                                  int64
	DependencyLost, JobStale, BackupFailed, DiskKnown bool
	DiskFreeRatio                                     float64
}

var alertTitles = map[string]string{"api_errors": "خطای بیش از حد درخواست‌های سایت", "dependency_loss": "قطع ارتباط پایگاه داده یا کش", "job_stale": "توقف اجرای موفق کار زمان‌بندی‌شده", "disk_pressure": "کمبود فضای دیسک سرور", "backup_failure": "خطا یا تأخیر نسخه پشتیبان"}

func (s AlertSnapshot) conditions() map[string]bool {
	return map[string]bool{"api_errors": s.Requests > 0 && float64(s.Errors)/float64(s.Requests) > .001, "dependency_loss": s.DependencyLost, "job_stale": s.JobStale, "disk_pressure": s.DiskKnown && s.DiskFreeRatio < .15, "backup_failure": s.BackupFailed}
}

// DeliverAlerts commits incident state and admin inbox rows atomically. Repeats
// and process restarts produce one notification per incident, rearmed on recovery.
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
	for _, code := range []string{"api_errors", "dependency_loss", "job_stale", "disk_pressure", "backup_failure"} {
		active := conditions[code] || m.pending[code]
		var changed string
		e = tx.QueryRowContext(ctx, `INSERT INTO observability_alert_state(code,active,delivery_pending) VALUES($1,$2,$2) ON CONFLICT(code) DO UPDATE SET delivery_pending=observability_alert_state.delivery_pending OR (EXCLUDED.active AND NOT observability_alert_state.active),active=EXCLUDED.active,updated_at=now() RETURNING code`, code, active).Scan(&changed)
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
