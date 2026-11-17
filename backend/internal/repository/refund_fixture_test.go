package repository

import (
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
)

// Legacy fixtures use exclusive disposable schemas. Install the actual current
// refund migration and its guards rather than omitting money tables in production.
func installRefundFixtureSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, q := range []string{
		`ALTER TABLE payment_transactions ADD COLUMN IF NOT EXISTS amount BIGINT,ADD COLUMN IF NOT EXISTS order_number TEXT,ADD COLUMN IF NOT EXISTS gateway_token TEXT,ADD COLUMN IF NOT EXISTS status TEXT,ADD COLUMN IF NOT EXISTS rrn TEXT,ADD COLUMN IF NOT EXISTS card_mask TEXT,ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
		`CREATE UNIQUE INDEX IF NOT EXISTS payment_transactions_one_active_per_order ON payment_transactions(order_id) WHERE status IN ('pending','paid')`,
		`CREATE TABLE IF NOT EXISTS disputes(id TEXT PRIMARY KEY,order_id TEXT,status TEXT,resolution_action TEXT,claim_amount BIGINT)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	oldRefund, err := os.ReadFile("../../migrations/202610250007_refunds_table.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(oldRefund)); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../migrations/202611150004_order_refund_principal.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
}

func removeEmptyRefundFixtureSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	var exists bool
	if err := db.QueryRow(`SELECT to_regclass('order_refund_obligations') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		return
	}
	down, err := os.ReadFile("../../migrations/202611150004_order_refund_principal.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
}

// This explicit local test receipt represents the fixture's successful sale.
// It uses the same SQL source/customer/currency guards and never calls a gateway.
// Once certified, later price/catalog edits cannot rewrite the captured source.
func seedVerifiedRefundFixturePayment(t *testing.T, db *sql.DB, orderID string) {
	t.Helper()
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM order_payment_receipts WHERE order_id=$1)`, orderID).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		return
	}
	var amount int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(final_unit_price::numeric*quantity),0) FROM order_items WHERE order_id=$1`, orderID).Scan(&amount); err != nil || amount <= 0 {
		t.Fatalf("fixture must provide a positive captured sale amount: %d %v", amount, err)
	}
	var paymentID string
	err := db.QueryRow(`SELECT id FROM payment_transactions WHERE order_id=$1 ORDER BY created_at,id LIMIT 1`, orderID).Scan(&paymentID)
	if err == sql.ErrNoRows {
		paymentID = "fixture-pay-" + uuid.NewString()[:12]
		if _, err = db.Exec(`INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) SELECT $1,id,order_number,$1,$3,'paid' FROM orders WHERE id=$2`, paymentID, orderID, amount); err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
		t.Fatal(err)
	} else if _, err = db.Exec(`UPDATE payment_transactions SET amount=$2 WHERE id=$1`, paymentID, amount); err != nil {
		t.Fatal(err)
	}
	verification := "fixture-v-" + uuid.NewString()
	if _, err = db.Exec(`INSERT INTO order_payment_verifications(id,payment_id,order_id,customer_id,amount_toman,currency,funding_kind) SELECT $1,p.id,p.order_id,o.customer_user_id,p.amount,'TOMAN','gateway' FROM payment_transactions p JOIN orders o ON o.id=p.order_id WHERE p.id=$2`, verification, paymentID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO order_payment_receipts(id,verification_id,funding_kind,payment_id,order_id,customer_id,amount_toman,currency,provider_reference,verified_at) SELECT $1,id,funding_kind,payment_id,order_id,customer_id,amount_toman,currency,$3,NOW() FROM order_payment_verifications WHERE id=$2`, "fixture-receipt-"+uuid.NewString(), verification, "fixture-"+uuid.NewString()[:12]); err != nil {
		t.Fatal(err)
	}
}
