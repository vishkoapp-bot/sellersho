package repository

import (
	"context"
	"os"
	"testing"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestMoneyPolicySnapshotUsesCurrentSchema(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable current schema")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var storeID, productID string
	if err = db.QueryRow(`SELECT id FROM stores ORDER BY id LIMIT 1`).Scan(&storeID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT id FROM products ORDER BY id LIMIT 1`).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	id := "money-" + uuid.NewString()
	order := domain.Order{ID: id, OrderNumber: id[:25], TrackingCode: id, StoreID: storeID, StoreName: "money policy fixture", PaymentMethod: "cash", PaymentStatus: "pending", Status: "processing", ReferrerSellerID: &storeID, TotalAmount: 101, PayableAmount: 101,
		Items: []domain.OrderItem{{ProductID: productID, Quantity: 1, WholesaleCost: 101, FinalUnitPrice: 101, PricingPolicyVersion: pricing.MoneyPolicyVersion}}}
	if err = NewOrderRepository(db).CreateOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM orders WHERE id=$1`, id)
	var version string
	var unit int64
	if err = db.QueryRow(`SELECT pricing_policy_version,final_unit_price FROM order_items WHERE order_id=$1`, id).Scan(&version, &unit); err != nil {
		t.Fatal(err)
	}
	if version != pricing.MoneyPolicyVersion || unit != 101 {
		t.Fatalf("snapshot %q/%d", version, unit)
	}
	loaded, err := NewOrderRepository(db).GetOrderByID(context.Background(), id)
	if err != nil || loaded == nil || len(loaded.Items) != 1 || loaded.Items[0].PricingPolicyVersion != pricing.MoneyPolicyVersion {
		t.Fatalf("policy read round trip failed: %+v %v", loaded, err)
	}
}

func TestMoneyPolicyMigrationPreservesHistoricalSnapshots(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable current schema")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `CREATE TEMP TABLE order_items (id TEXT,final_unit_price BIGINT,wholesale_cost BIGINT,platform_fee BIGINT,seller_margin BIGINT); INSERT INTO order_items VALUES('paid-history',100,149,-49,0)`); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, `DROP TABLE pg_temp.order_items`)
	up, err := os.ReadFile("../../migrations/202611140001_money_policy_version.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	var version string
	var price, cost, fee, margin int64
	if err = conn.QueryRowContext(ctx, `SELECT pricing_policy_version,final_unit_price,wholesale_cost,platform_fee,seller_margin FROM pg_temp.order_items`).Scan(&version, &price, &cost, &fee, &margin); err != nil {
		t.Fatal(err)
	}
	if version != "legacy-unversioned" || price != 100 || cost != 149 || fee != -49 || margin != 0 {
		t.Fatalf("historical amounts rewritten %q/%d/%d/%d/%d", version, price, cost, fee, margin)
	}
}

func TestMoneyPolicyReadOnlyAuditReportsSyntheticHistory(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable current schema")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var productID, storeID string
	if err = db.QueryRow(`SELECT id FROM products ORDER BY id LIMIT 1`).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT id FROM stores ORDER BY id LIMIT 1`).Scan(&storeID); err != nil {
		t.Fatal(err)
	}
	id := "money-audit-" + uuid.NewString()
	order := domain.Order{ID: id, OrderNumber: id[:25], TrackingCode: id, StoreID: storeID, StoreName: "synthetic historical fixture", PaymentMethod: "cash", PaymentStatus: "paid", Status: "processing", ReferrerSellerID: &storeID, TotalAmount: 100, PayableAmount: 100,
		Items: []domain.OrderItem{{ProductID: productID, Quantity: 1, WholesaleCost: 149, PlatformFee: -49, FinalUnitPrice: 100}}}
	if err = NewOrderRepository(db).CreateOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM orders WHERE id=$1`, id)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO platform_pct_products(product_id,pct) VALUES($1,1.001) ON CONFLICT(product_id) DO UPDATE SET pct=1.001`, productID); err != nil {
		t.Fatal(err)
	}
	report, err := os.ReadFile("../../../scripts/audit-money-policy.sql")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(string(report))
	if err != nil {
		t.Fatal(err)
	}
	badInput, badHistory := false, false
	for rows.Next() {
		var source, record, field, value, reason string
		if err = rows.Scan(&source, &record, &field, &value, &reason); err != nil {
			t.Fatal(err)
		}
		if source == "platform_pct_products" && record == productID && reason == "unsupported_percent_precision" {
			badInput = true
			t.Logf("synthetic audit: %s %s=%s %s", source, field, value, reason)
		}
		if source == "historical_order_items" && reason == "negative_platform_fee" {
			badHistory = true
			t.Logf("synthetic audit: %s %s=%s %s", source, field, value, reason)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if !badInput || !badHistory {
		t.Fatalf("audit missed synthetic input/history: %v/%v", badInput, badHistory)
	}
	var price, fee int64
	if err = tx.QueryRow(`SELECT final_unit_price,platform_fee FROM order_items WHERE order_id=$1`, id).Scan(&price, &fee); err != nil {
		t.Fatal(err)
	}
	if price != 100 || fee != -49 {
		t.Fatal("read-only audit changed historical snapshots")
	}
}
