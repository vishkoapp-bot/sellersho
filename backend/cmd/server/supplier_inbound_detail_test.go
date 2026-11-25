package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestSupplierInboundDetailReturnsCompleteOwnedRecord(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	root, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "supplier_inbound_detail_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pq.QuoteIdentifier(schema)
	if _, err := root.Exec("CREATE SCHEMA " + quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.Exec("DROP SCHEMA " + quoted + " CASCADE") })
	conn, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	params := conn.Query()
	params.Set("search_path", schema)
	conn.RawQuery = params.Encode()
	db, err := testdb.Open(t, conn.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE inbound_shipments (
			id TEXT PRIMARY KEY, supplier_id TEXT NOT NULL, supplier_name TEXT NOT NULL,
			warehouse_name TEXT NOT NULL, items_count INT NOT NULL, cartons_count INT NOT NULL,
			carrier TEXT, truck_plate TEXT, status TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			received_at TIMESTAMPTZ, items JSONB, dispatch_at TIMESTAMPTZ, dispatched_at TIMESTAMPTZ
		);
		INSERT INTO inbound_shipments(id, supplier_id, supplier_name, warehouse_name, items_count, cartons_count, status, items, dispatch_at)
		VALUES ('inbound', 'owner', 'Fixture supplier', 'Fixture warehouse', 2, 1, 'scheduled', '[{"quantity":2}]', NOW());
	`); err != nil {
		t.Fatal(err)
	}
	h := &SupplierHandlers{supplierRepo: repository.NewSupplierRepository(db)}
	request := func(supplierID, inboundID string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/supplier/inbounds/"+inboundID, nil)
		r = r.WithContext(context.WithValue(r.Context(), supplierIDKey, supplierID))
		w := httptest.NewRecorder()
		h.HandleGetInboundByID(w, r)
		return w
	}
	response := request("owner", "inbound")
	if response.Code != http.StatusOK {
		t.Fatalf("owned inbound detail status = %d, want 200 (%s)", response.Code, response.Body)
	}
	var result repository.InboundShipment
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	var items []struct {
		Quantity int `json:"quantity"`
	}
	if err := json.Unmarshal(result.Items, &items); err != nil {
		t.Fatal(err)
	}
	if result.ID != "inbound" || result.SupplierID != "owner" || result.DispatchAt == nil || len(items) != 1 || items[0].Quantity != 2 {
		t.Fatalf("inbound record missing details: %+v", result)
	}
	for _, tc := range []struct{ supplier, inbound string }{
		{"other", "inbound"},
		{"owner", "missing"},
	} {
		response := request(tc.supplier, tc.inbound)
		if response.Code != http.StatusNotFound {
			t.Fatalf("unavailable inbound status = %d, want 404", response.Code)
		}
	}
}
