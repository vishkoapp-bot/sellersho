package observability

import (
	"context"
	"github.com/drup/platform/internal/testdb"
	"os"
	"testing"
)

func TestAdminAlertDeliveryDeduplicatedAndRearmed(t *testing.T) {
	db, e := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
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
