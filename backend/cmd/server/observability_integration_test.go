package main

import (
	"context"
	"encoding/json"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/testdb"
	"net/http"
	"os"
	"testing"
)

func TestOperationsAlertsReachProtectedAdminInbox(t *testing.T) {
	db, e := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	f := &supplierNotificationsFixture{db: db, jwtService: auth.NewJWTService("obs-admin-local-test")}
	admin := f.seedRoleUser(t, "09999000101", "admin")
	customer := f.seedRoleUser(t, "09999000102", "customer")
	m := observability.New()
	m.SetDB(db)
	ctx := context.Background()
	if e = m.DeliverAlerts(ctx, observability.AlertSnapshot{}); e != nil {
		t.Fatal(e)
	}
	s := observability.AlertSnapshot{Requests: 100, Errors: 1, DependencyLost: true, JobStale: true, DiskKnown: true, DiskFreeRatio: .01, BackupFailed: true}
	if e = m.DeliverAlerts(ctx, s); e != nil {
		t.Fatal(e)
	}
	get := func(w http.ResponseWriter, r *http.Request) { handleAdminNotifications(w, r, db) }
	read := func(w http.ResponseWriter, r *http.Request) { handleAdminNotificationsRead(w, r, db) }
	token := f.token(t, admin, "09999000101", "admin")
	code, body := f.callRole(t, "GET", "/api/v1/admin/notifications", token, "", []string{"admin", "finance", "support_specialist"}, get)
	var env notificationsEnvelope
	if e = json.Unmarshal([]byte(body), &env); e != nil || code != 200 || len(env.Notifications) != 5 || env.UnreadCount != 5 {
		t.Fatalf("admin inbox %d %s %v", code, body, e)
	}
	for _, n := range env.Notifications {
		if n.Kind != "operations_alert" {
			t.Fatal("wrong kind")
		}
	}
	code, _ = f.callRole(t, "GET", "/api/v1/admin/notifications", f.token(t, customer, "09999000102", "customer"), "", []string{"admin", "finance", "support_specialist"}, get)
	if code != 403 {
		t.Fatalf("unauthorized read %d", code)
	}
	code, body = f.callRole(t, "POST", "/api/v1/admin/notifications/read", token, "", []string{"admin", "finance", "support_specialist"}, read)
	if code != 200 {
		t.Fatalf("mark read %d %s", code, body)
	}
	_, body = f.callRole(t, "GET", "/api/v1/admin/notifications", token, "", []string{"admin", "finance", "support_specialist"}, get)
	json.Unmarshal([]byte(body), &env)
	if env.UnreadCount != 0 {
		t.Fatal("unread did not clear")
	}
}
