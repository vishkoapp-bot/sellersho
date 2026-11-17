package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func openParcelHTTPDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable database required")
	}
	admin, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "parcel_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(`CREATE SCHEMA ` + pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := testdb.Open(t, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, e := admin.Exec(`DROP SCHEMA ` + pq.QuoteIdentifier(schema) + ` CASCADE`); e != nil {
			t.Error(e)
		}
		admin.Close()
	})
	for _, ddl := range []string{
		`CREATE TABLE suppliers(id VARCHAR(50) PRIMARY KEY);INSERT INTO suppliers VALUES('supplier')`,
		`CREATE TABLE supplier_return_requests(id VARCHAR(60) PRIMARY KEY,supplier_id TEXT,status TEXT);INSERT INTO supplier_return_requests VALUES('return','supplier','packed')`,
		`CREATE TABLE supplier_return_parcels(id VARCHAR(80) PRIMARY KEY,return_request_id TEXT,final_charge BIGINT,payment_method TEXT,status TEXT,confirmed_by TEXT,updated_at TIMESTAMPTZ,final_charge_source TEXT,final_charge_reference TEXT);INSERT INTO supplier_return_parcels VALUES('parcel','return',120000,'sender_pays','draft',NULL,now(),'manual','bill1')`,
		`CREATE TABLE ledger_entries(id TEXT PRIMARY KEY,date TIMESTAMPTZ,target_account TEXT,target_type TEXT,type TEXT,amount BIGINT,reason TEXT,operator TEXT,related_order_or_dispute TEXT)`,
		`CREATE TABLE supplier_return_financial_events(id VARCHAR(80) PRIMARY KEY,return_request_id TEXT,parcel_id TEXT,supplier_id TEXT,operation TEXT,amount BIGINT,payment_method TEXT,ledger_entry_id TEXT,idempotency_key TEXT UNIQUE,reason TEXT,actor_user_id TEXT,event_identity TEXT UNIQUE,created_at TIMESTAMPTZ DEFAULT now())`,
	} {
		if _, err = db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../../migrations/202611160001_parcel_charge_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestParcelChargeHTTP(t *testing.T) {
	db := openParcelHTTPDB(t)
	repo := repository.NewSupplierRepository(db)
	if err := repo.ConfirmReturnParcelCharge(context.Background(), "return", "parcel", "actor", "original"); err != nil {
		t.Fatal(err)
	}
	service := auth.NewJWTService(strings.Repeat("a", 32))
	// Same role list as the production warehouse route.
	handler := RequireRoles(service, "admin", "warehouse_staff", "logistics_specialist")(func(w http.ResponseWriter, r *http.Request) {
		handleCorrectReturnCharge(w, r, r.Context(), "return", "parcel", repo)
	})
	call := func(role, key, body string, ctx context.Context) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/warehouse/returns/return/parcels/parcel/correct-charge", strings.NewReader(body))
		if ctx != nil {
			r = r.WithContext(ctx)
		}
		r.Header.Set("Idempotency-Key", key)
		if role != "" {
			token, err := service.GenerateToken("actor", "09123456789", "Actor", role, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	body := `{"expectedAmount":120000,"finalAmount":90000,"source":"manual","reference":"bill2","reason":"carrier rebate"}`
	for _, c := range []struct {
		role, key, body string
		want            int
	}{
		{"", "key", body, 401}, {"supplier", "key", body, 403}, {"warehouse_staff", "", body, 400}, {"warehouse_staff", strings.Repeat("x", 129), body, 400}, {"warehouse_staff", "key", "{", 400}, {"warehouse_staff", "key", `{"finalAmount":0}`, 400}, {"warehouse_staff", "key", strings.Replace(body, "120000", "120000.5", 1), 400}, {"warehouse_staff", "key", body + " {}", 400}, {"warehouse_staff", "key", strings.Replace(body, `"manual"`, `"unknown"`, 1), 400},
	} {
		if w := call(c.role, c.key, c.body, nil); w.Code != c.want {
			t.Fatalf("%s expected%d got%d %s", c.body, c.want, w.Code, w.Body.String())
		}
	}
	first := call("warehouse_staff", "key", body, nil)
	if first.Code != 200 {
		t.Fatalf("correction %d %s", first.Code, first.Body.String())
	}
	replay := call("warehouse_staff", "key", body, nil)
	if replay.Code != 200 || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay %d %s", replay.Code, replay.Body.String())
	}
	if w := call("warehouse_staff", "key", strings.Replace(body, "90000", "80000", 1), nil); w.Code != 409 {
		t.Fatalf("conflict%d", w.Code)
	}
	if w := call("warehouse_staff", "fresh", body, nil); w.Code != 409 {
		t.Fatalf("stale%d", w.Code)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT id FROM suppliers WHERE id='supplier' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	w := call("warehouse_staff", "busy", strings.Replace(strings.Replace(body, "90000", "80000", 1), "120000", "90000", 1), ctx)
	if w.Code != 503 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("busy %d %s", w.Code, w.Body.String())
	}
}
func TestParcelChargeHTTPMissingClaims(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/correct-charge", strings.NewReader("{}"))
	handleCorrectReturnCharge(w, r, r.Context(), "return", "parcel", nil)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
