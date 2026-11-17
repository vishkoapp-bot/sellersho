package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
)

// Explicit parent-first fixture transaction, never a pooled/global capability.
func fin005ReturnFixtureExec(t *testing.T, db *sql.DB, order, query string, args ...any) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = lockOrderFinanceTx(context.Background(), tx, order); err != nil {
		t.Fatal(err)
	}
	if err = markDisputeWriterTx(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// Older tests own exclusive non-public schemas. Install the actual additive
// migration after their historical rows, without weakening public guards.
func installFIN005PrivateFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if schema == "public" {
		t.Fatal("private schema required")
	}
	// Current eligibility reads retained physical shipment facts even on orders
	// with no legs. Install the actual relation contract for older private fixtures.
	shipmentDDL, err := os.ReadFile("../../migrations/000011_direct_dropship.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	shipmentSQL := string(shipmentDDL)
	start := strings.Index(shipmentSQL, "CREATE TABLE IF NOT EXISTS supplier_shipments (")
	if start < 0 {
		t.Fatal("canonical supplier shipment relation missing")
	}
	end := strings.Index(shipmentSQL[start:], ";")
	if end < 0 {
		t.Fatal("canonical supplier shipment relation incomplete")
	}
	if _, err = db.Exec(shipmentSQL[start : start+end+1]); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE disputes ADD COLUMN IF NOT EXISTS order_number TEXT,ADD COLUMN IF NOT EXISTS store_id TEXT,ADD COLUMN IF NOT EXISTS type TEXT,ADD COLUMN IF NOT EXISTS responsible_party TEXT,ADD COLUMN IF NOT EXISTS resolution_date TIMESTAMPTZ`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS disputed_at TIMESTAMPTZ,ADD COLUMN IF NOT EXISTS confirmed_at TIMESTAMPTZ,ADD COLUMN IF NOT EXISTS return_window_ends_at TIMESTAMPTZ,ADD COLUMN IF NOT EXISTS profit_released_at TIMESTAMPTZ,ADD COLUMN IF NOT EXISTS referral_commission_released BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE order_item_origins ADD COLUMN IF NOT EXISTS order_id TEXT,ADD COLUMN IF NOT EXISTS quantity INT`,
		`CREATE TABLE IF NOT EXISTS return_items(id UUID PRIMARY KEY,return_id UUID REFERENCES customer_returns(id),order_item_id TEXT,qty INT,qc_status TEXT,received_at TIMESTAMPTZ)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"202611050006_customer_return_financial_events.up.sql", "202611160002_dispute_eligibility.up.sql"} {
		b, err := os.ReadFile("../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(string(b)); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}
