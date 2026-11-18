package observability

import (
	"context"
	"github.com/drup/platform/internal/testdb"
	"os"
	"testing"
)

// OBS-01 C-01: the SLO breach reaches the admin inbox through the
// in-app poller — the same thresholds as the Prometheus rules
// (read p95 > 300 ms / p99 > 1000 ms; mutation p95 > 750 ms / p99 >
// 2000 ms), delivered via DeliverAlerts with the same dedup/rearm
// behavior as the other five codes. The Prometheus rules themselves
// have no delivery path (no Alertmanager, no contact point); this
// condition IS the delivery path for the SLO breach.
func TestSLOBreachDeliversExactlyOneAdminRow(t *testing.T) {
	db, e := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	// Fresh alert state: the disposable DB is shared by this package's
	// tests, and an earlier test may already hold slo_breach active.
	if _, e := db.Exec(`DELETE FROM observability_alert_state`); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	var admin, customer string
	if e = db.QueryRow(`INSERT INTO users(mobile,role,is_active,full_name) VALUES('09999000021','admin',true,'OBS SLO admin') RETURNING id`).Scan(&admin); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`INSERT INTO users(mobile,role,is_active,full_name) VALUES('09999000022','customer',true,'OBS SLO customer') RETURNING id`).Scan(&customer); e != nil {
		t.Fatal(e)
	}
	m := New()
	m.SetDB(db)
	count := func(code string) int {
		var n int
		if e := db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert' AND payload->>'alertCode'=$2`, admin, code).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	pushCooldown := func() {
		if _, e := db.Exec(`UPDATE observability_alert_state SET notified_at=now()-INTERVAL '16 minutes'`); e != nil {
			t.Fatal(e)
		}
	}
	// Read p95 breach (0.4 s > 0.3 s): exactly one row, admin only.
	// A repeat delivery deduplicates (same incident, cooldown).
	if e = m.DeliverAlerts(ctx, AlertSnapshot{SLOKnown: true, SLOReadP95: 0.4, SLOReadP99: 0.5, SLOMutationP95: 0.1, SLOMutationP99: 0.2}); e != nil {
		t.Fatal(e)
	}
	if e = m.DeliverAlerts(ctx, AlertSnapshot{SLOKnown: true, SLOReadP95: 0.4, SLOReadP99: 0.5, SLOMutationP95: 0.1, SLOMutationP99: 0.2}); e != nil {
		t.Fatal(e)
	}
	if n := count("slo_breach"); n != 1 {
		t.Fatalf("want exactly one slo_breach row, got %d", n)
	}
	var total, cust int
	db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert'`, admin).Scan(&total)
	db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert'`, customer).Scan(&cust)
	if total != 1 || cust != 0 {
		t.Fatalf("want 1 admin row / 0 customer rows, got %d / %d", total, cust)
	}
	// Within SLO after the cooldown expires: quiet, no new row.
	pushCooldown()
	if e = m.DeliverAlerts(ctx, AlertSnapshot{SLOKnown: true, SLOReadP95: 0.2, SLOReadP99: 0.5, SLOMutationP95: 0.1, SLOMutationP99: 0.2}); e != nil {
		t.Fatal(e)
	}
	if n := count("slo_breach"); n != 1 {
		t.Fatalf("within-SLO snapshot delivered a row: %d", n)
	}
	// Unknown (SLOKnown=false) with breaching-looking quantiles: the
	// quantiles mean nothing without a measured window, so no row.
	pushCooldown()
	if e = m.DeliverAlerts(ctx, AlertSnapshot{SLOKnown: false, SLOReadP95: 5, SLOReadP99: 9, SLOMutationP95: 8, SLOMutationP99: 9}); e != nil {
		t.Fatal(e)
	}
	if n := count("slo_breach"); n != 1 {
		t.Fatalf("unknown-SLO snapshot delivered a row: %d", n)
	}
	// Mutation p99-only breach (p95 fine, p99 2.5 s > 2 s): the p99 arm
	// fires per class, and recovery above re-armed the incident.
	pushCooldown()
	if e = m.DeliverAlerts(ctx, AlertSnapshot{SLOKnown: true, SLOReadP95: 0.2, SLOReadP99: 0.5, SLOMutationP95: 0.5, SLOMutationP99: 2.5}); e != nil {
		t.Fatal(e)
	}
	if n := count("slo_breach"); n != 2 {
		t.Fatalf("want second slo_breach row on mutation p99 breach, got %d", n)
	}
}

// OBS-01 C-11: a still-active disk-pressure incident re-notifies when
// free space worsens into a lower band (15% / 10% / 5% / 2%), instead
// of notifying once and going silent until recovery. Same-band
// worsening does NOT re-notify; worsening inside the 15-minute cooldown
// is remembered as durable delivery_pending and delivered when the
// cooldown expires; recovery resets the band so the next incident
// starts from band 1 again.
func TestDiskPressureRenotifiesOnWorseningBand(t *testing.T) {
	db, e := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	// Fresh alert state: the dedup test leaves disk_pressure active at
	// band 2 (ratio 0.1), which would mask every step below.
	if _, e := db.Exec(`DELETE FROM observability_alert_state`); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	var admin, customer string
	if e = db.QueryRow(`INSERT INTO users(mobile,role,is_active,full_name) VALUES('09999000031','admin',true,'OBS disk admin') RETURNING id`).Scan(&admin); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`INSERT INTO users(mobile,role,is_active,full_name) VALUES('09999000032','customer',true,'OBS disk customer') RETURNING id`).Scan(&customer); e != nil {
		t.Fatal(e)
	}
	m := New()
	m.SetDB(db)
	diskCount := func() int {
		var n int
		if e := db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert' AND payload->>'alertCode'='disk_pressure'`, admin).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	pushCooldown := func() {
		if _, e := db.Exec(`UPDATE observability_alert_state SET notified_at=now()-INTERVAL '16 minutes'`); e != nil {
			t.Fatal(e)
		}
	}
	deliver := func(ratio float64) {
		if e := m.DeliverAlerts(ctx, AlertSnapshot{DiskKnown: true, DiskFreeRatio: ratio}); e != nil {
			t.Fatal(e)
		}
	}
	// Band 1 (12% free): first notification, admin only.
	deliver(0.12)
	if n := diskCount(); n != 1 {
		t.Fatalf("want 1 disk_pressure row, got %d", n)
	}
	var cust int
	db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert'`, customer).Scan(&cust)
	if cust != 0 {
		t.Fatalf("customer received %d alerts", cust)
	}
	// Same-band worsening (12% -> 11%, still band 1) after the cooldown
	// expires: no re-notification.
	pushCooldown()
	deliver(0.11)
	if n := diskCount(); n != 1 {
		t.Fatalf("same-band worsening re-notified: %d", n)
	}
	// Worsening to band 2 (8% free) after the cooldown: re-notifies.
	pushCooldown()
	deliver(0.08)
	if n := diskCount(); n != 2 {
		t.Fatalf("want re-notification on band 1 -> 2, got %d", n)
	}
	// Worsening to band 3 (4% free) INSIDE the cooldown: remembered as
	// pending, not delivered yet.
	deliver(0.04)
	if n := diskCount(); n != 2 {
		t.Fatalf("worsening inside cooldown delivered early: %d", n)
	}
	// Cooldown expires: the remembered worsening is delivered without
	// any new signal — the same snapshot re-delivers from pending state.
	pushCooldown()
	deliver(0.04)
	if n := diskCount(); n != 3 {
		t.Fatalf("want pending worsening delivered after cooldown, got %d", n)
	}
	// Recovery resets the band: healthy, then a fresh band-1 incident
	// notifies again as a new incident.
	pushCooldown()
	deliver(0.9)
	if n := diskCount(); n != 3 {
		t.Fatalf("recovery delivered a row: %d", n)
	}
	deliver(0.12)
	if n := diskCount(); n != 4 {
		t.Fatalf("want new-incident notification after recovery, got %d", n)
	}
	var band int
	if e := db.QueryRow(`SELECT severity_band FROM observability_alert_state WHERE code='disk_pressure'`).Scan(&band); e != nil || band != 1 {
		t.Fatalf("want stored band 1 after recovery + fresh incident, got %d: %v", band, e)
	}
}

func TestAdminAlertDeliveryDeduplicatedAndRearmed(t *testing.T) {
	db, e := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	// Fresh alert state (F-1 pattern): the disposable DB is shared by
	// this package's tests, and TestDiskPressureRenotifiesOnWorseningBand
	// runs before this test in this file and leaves disk_pressure active
	// at band 1 with notified_at=now(). Without a reset, this test's
	// disk snapshot (ratio 0.1, band 2) would re-arm delivery_pending
	// via the band increase but the 15-minute cooldown would still
	// block the row, so the "five alerts" assertion would see four.
	if _, e := db.Exec(`DELETE FROM observability_alert_state`); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	var admin, customer string
	if e = db.QueryRow(`INSERT INTO users(mobile,role,is_active,full_name) VALUES('09999000001','admin',true,'OBS admin') RETURNING id`).Scan(&admin); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`INSERT INTO users(mobile,role,is_active,full_name) VALUES('09999000002','customer',true,'OBS customer') RETURNING id`).Scan(&customer); e != nil {
		t.Fatal(e)
	}
	s := AlertSnapshot{Requests: 1000, Errors: 2, DependencyLost: true, JobStale: true, DiskFreeRatio: .1, DiskKnown: true, BackupFailed: true}
	m := New()
	m.SetDB(db)
	for i := 0; i < 2; i++ {
		if e = m.DeliverAlerts(ctx, s); e != nil {
			t.Fatal(e)
		}
	}
	var count int
	if e = db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert'`, admin).Scan(&count); e != nil || count != 5 {
		t.Fatalf("want five alerts got %d: %v", count, e)
	}
	db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert'`, customer).Scan(&count)
	if count != 0 {
		t.Fatal("non-admin received alert")
	}
	if e = m.DeliverAlerts(ctx, AlertSnapshot{DiskKnown: true, DiskFreeRatio: .9}); e != nil {
		t.Fatal(e)
	}
	if e = m.DeliverAlerts(ctx, s); e != nil {
		t.Fatal(e)
	}
	db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert'`, admin).Scan(&count)
	if count != 5 {
		t.Fatalf("flapping bypassed cooldown: %d", count)
	}
	if _, e = db.Exec(`UPDATE observability_alert_state SET notified_at=now()-INTERVAL '16 minutes'`); e != nil {
		t.Fatal(e)
	}
	if e = m.DeliverAlerts(ctx, s); e != nil {
		t.Fatal(e)
	}
	db.QueryRow(`SELECT count(*) FROM notification_outbox WHERE recipient_user_id=$1 AND kind='operations_alert'`, admin).Scan(&count)
	if count != 10 {
		t.Fatalf("rearmed alert count %d", count)
	}
}
