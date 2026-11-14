package main

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestOrderCancellationDispatchHTTPConflict(t *testing.T) {
	raw := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	base, err := testdb.Open(t, raw)
	if err != nil {
		t.Fatal(err)
	}
	schema := "inv01_http_" + uuid.NewString()[:8]
	if _, err := base.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	db, err := testdb.Open(t, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close(); base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); base.Close() }()
	for _, ddl := range []string{
		`CREATE TABLE orders(id text PRIMARY KEY,order_number text,customer_user_id uuid,status text,inventory_captured bool,shipped_at timestamptz)`,
		`CREATE TABLE supplier_shipments(id text PRIMARY KEY,order_id text,status text,shipped_at timestamptz,delivered_at timestamptz)`,
		`INSERT INTO orders VALUES('dispatch','ORDER1','00000000-0000-0000-0000-000000000001','processing',true,NULL)`,
		`INSERT INTO supplier_shipments VALUES('leg1','dispatch','shipped',now(),NULL),('leg2','dispatch','pending',NULL,NULL)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"customer", "admin"} {
		ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: "00000000-0000-0000-0000-000000000001", Role: role})
		req := httptest.NewRequest("POST", "/api/v1/orders/ORDER1/cancel", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		handleOrderCancel(w, req, ctx, "ORDER1", repository.NewOrderRepository(db))
		if w.Code != 409 {
			t.Fatalf("%s status=%d body=%s", role, w.Code, w.Body.String())
		}
	}
}
