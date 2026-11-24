package repository

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSpncxRetryCapture(t *testing.T) {
	ctx := context.Background()
	// The pre-strict fixture and expected failure queue mode are deterministic.
	previousStrict := spncxStrict
	spncxStrict = false
	t.Cleanup(func() { spncxStrict = previousStrict })

	const order = "retry-order"
	const product = "retry-product"
	const spnc = "RETRYCAP"
	seed := func(t *testing.T, mode string, stock int, paymentStatus, orderStatus string) (*sql.DB, string) {
		t.Helper()
		db, schema, _, _ := skuInventoryFixture(t)
		tx := skuTestTx(t, db, schema)
		defer tx.Rollback()
		variant := uuid.NewString()
		exec := func(query string, args ...any) {
			t.Helper()
			if _, err := tx.Exec(query, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO products(id,supplier_id,stock,schema_version,product_type,origin_supplier_warehouse_id) VALUES($1,'s',$2,0,'legacy','sw1')`, product, stock)
		exec(`INSERT INTO product_variants(id,product_id,spnc,stock,inventory_mode) VALUES($1,$2,$3,$4,$5)`, variant, product, spnc, stock, mode)
		if mode == "tracked" {
			if err := SeedTrackedSKUInventoryTx(ctx, tx, product, "s"); err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE inventory_balances SET supplier_warehouse_id='sw1' WHERE product_id=$1 AND status='sellable'`, product)
		} else {
			exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,supplier_warehouse_id,owner_type,location_type,location_key,status,quantity,reconciled)
				VALUES('retry-source',$1,'s','sw1','supplier','supplier','supplier-warehouse:sw1','sellable',$2,TRUE)`, product, stock)
		}
		exec(`INSERT INTO orders(id) VALUES($1)`, order)
		exec(`UPDATE orders SET status=$2 WHERE id=$1`, order, orderStatus)
		exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,$2,$3,$4,2)`, uuid.NewString(), order, product, variant)
		exec(`INSERT INTO payment_transactions(id,order_id,gateway_token,status,amount,rrn,card_mask)
			VALUES($1,$1,$1,$2,1000,'test-rrn','test-mask')`, order, paymentStatus)
		exec(`INSERT INTO spncx_failed_captures(order_id,failure,mode,at)
			VALUES($1,'insufficient_inventory','pre_strict',TIMESTAMPTZ '2000-01-01 00:00:00Z')`, order)
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return skuScopedDB(t, schema), variant
	}

	assertInventory := func(t *testing.T, db *sql.DB, variant string, wantStock, wantUnits, wantSold, wantLinks, wantCaptures int) {
		t.Helper()
		var stock, units, sold, links, captures int
		if err := db.QueryRow(`SELECT v.stock,
			(SELECT COUNT(*) FROM product_units WHERE variant_id=v.id),
			(SELECT COUNT(*) FROM product_units WHERE variant_id=v.id AND status='sold'),
			(SELECT COUNT(*) FROM order_item_units WHERE order_id=$2),
			(SELECT COUNT(*) FROM order_inventory_captures WHERE order_id=$2)
			FROM product_variants v WHERE v.id=$1`, variant, order).Scan(&stock, &units, &sold, &links, &captures); err != nil {
			t.Fatal(err)
		}
		if stock != wantStock || units != wantUnits || sold != wantSold || links != wantLinks || captures != wantCaptures {
			t.Fatalf("stock/units/sold/links/captures = %d/%d/%d/%d/%d, want %d/%d/%d/%d/%d",
				stock, units, sold, links, captures, wantStock, wantUnits, wantSold, wantLinks, wantCaptures)
		}
	}
	assertQueue := func(t *testing.T, db *sql.DB, wantFailure, wantMode string, wantRefreshed bool) {
		t.Helper()
		var failure, mode string
		var refreshed bool
		err := db.QueryRow(`SELECT failure,mode,at>TIMESTAMPTZ '2000-01-01 00:00:00Z' FROM spncx_failed_captures WHERE order_id=$1`, order).Scan(&failure, &mode, &refreshed)
		if err != nil || failure != wantFailure || mode != wantMode || refreshed != wantRefreshed {
			t.Fatalf("queue failure=%q mode=%q refreshed=%t err=%v", failure, mode, refreshed, err)
		}
	}

	t.Run("success and double retry", func(t *testing.T) {
		db, variant := seed(t, "tracked", 2, "pending", "processing")
		repo := NewOrderRepository(db)
		result, err := repo.RetryFailedCapture(ctx, order)
		wantCodes := []string{spnc + "-X0001", spnc + "-X0002"}
		if err != nil || result == nil || result.OrderID != order || !result.Captured || !slices.Equal(result.UnitCodes, wantCodes) {
			t.Fatalf("retry = %+v, %v, want captured order and FIFO codes %v", result, err, wantCodes)
		}
		var paymentStatus, orderPaymentStatus, rrn, cardMask string
		var captured, queued bool
		if err := db.QueryRow(`SELECT p.status,o.payment_status,p.rrn,p.card_mask,o.inventory_captured,
			EXISTS(SELECT 1 FROM spncx_failed_captures WHERE order_id=o.id)
			FROM orders o JOIN payment_transactions p ON p.order_id=o.id WHERE o.id=$1`, order).
			Scan(&paymentStatus, &orderPaymentStatus, &rrn, &cardMask, &captured, &queued); err != nil {
			t.Fatal(err)
		}
		if paymentStatus != "paid" || orderPaymentStatus != "paid" || !captured || queued || rrn != "test-rrn" || cardMask != "test-mask" {
			t.Fatalf("payment=%q order payment=%q captured=%t queued=%t rrn=%q card mask=%q", paymentStatus, orderPaymentStatus, captured, queued, rrn, cardMask)
		}
		assertInventory(t, db, variant, 0, 2, 2, 2, 1)

		if _, err := repo.RetryFailedCapture(ctx, order); !errors.Is(err, ErrRetryCaptureAlreadyCompleted) {
			t.Fatalf("double retry error = %v, want ErrRetryCaptureAlreadyCompleted", err)
		}
		assertInventory(t, db, variant, 0, 2, 2, 2, 1)
	})

	for _, tc := range []struct{ name, paymentStatus, orderStatus string }{
		{"refunded payment", "refunded", "processing"},
		{"cancelled order", "pending", "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, variant := seed(t, "tracked", 2, tc.paymentStatus, tc.orderStatus)
			if _, err := NewOrderRepository(db).RetryFailedCapture(ctx, order); !errors.Is(err, ErrRetryPaymentNotRetryable) {
				t.Fatalf("retry error = %v, want ErrRetryPaymentNotRetryable", err)
			}
			assertInventory(t, db, variant, 2, 2, 0, 0, 0)
			assertQueue(t, db, "insufficient_inventory", "pre_strict", false)
		})
	}

	t.Run("failed retry leaves payment pending", func(t *testing.T) {
		db, variant := seed(t, "tracked", 0, "pending", "processing")
		if _, err := db.Exec(`UPDATE spncx_failed_captures SET failure='unit_cap_exceeded',mode='strict' WHERE order_id=$1`, order); err != nil {
			t.Fatal(err)
		}
		_, err := NewOrderRepository(db).RetryFailedCapture(ctx, order)
		var failure *RetryCaptureFailedError
		if !errors.As(err, &failure) || failure.Failure != "insufficient_inventory" {
			t.Fatalf("retry error = %v, want insufficient_inventory", err)
		}
		var status string
		if err := db.QueryRow(`SELECT status FROM payment_transactions WHERE order_id=$1`, order).Scan(&status); err != nil || status != "pending" {
			t.Fatalf("payment status = %q, %v, want pending", status, err)
		}
		assertInventory(t, db, variant, 0, 0, 0, 0, 0)
		assertQueue(t, db, "insufficient_inventory", "pre_strict", true)
	})

	t.Run("pre-strict gap audit without queue precondition", func(t *testing.T) {
		db, _ := seed(t, "legacy", 2, "pending", "processing")
		if _, err := db.Exec(`DELETE FROM spncx_failed_captures WHERE order_id=$1`, order); err != nil {
			t.Fatal(err)
		}
		result, err := NewOrderRepository(db).RetryFailedCapture(ctx, order)
		if err != nil || result == nil || !result.Captured || result.UnitCodes == nil || len(result.UnitCodes) != 0 {
			t.Fatalf("quantity-only retry = %+v, %v, want capture with empty unit codes", result, err)
		}
		var gap bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM inventory_movements
			WHERE metadata->>'order_id'=$1 AND metadata->>'unit_coverage_gap'='true'
			  AND metadata->>'requested'='2' AND metadata->>'allocated'='0' AND metadata->>'shortfall'='2')`, order).Scan(&gap); err != nil || !gap {
			t.Fatalf("coverage gap audit = %t, %v", gap, err)
		}
	})

	t.Run("order not found", func(t *testing.T) {
		_, schema, _, _ := skuInventoryFixture(t)
		db := skuScopedDB(t, schema)
		if _, err := NewOrderRepository(db).RetryFailedCapture(ctx, order); !errors.Is(err, ErrRetryOrderNotFound) {
			t.Fatalf("retry error = %v, want ErrRetryOrderNotFound", err)
		}
	})

	// Coupon-backed retry: the FIN007 fences (migration 202611170001) reject
	// any UPDATE of payment_transactions.status or
	// orders.payment_status/inventory_captured on a coupon-backed order
	// without the tx-local fin007_writer flag, and the shipment fence
	// requires the reservation to leave 'reserved'. The retry must mirror
	// the MarkPaymentPaid prologue (lockOrderFinanceTx + confirmOrderCouponTx)
	// or it captures and then fails finalization with 500. The fence
	// functions below are copied verbatim from the migration so this test
	// proves the retry against the real triggers, not a stub.
	t.Run("coupon-backed retry succeeds", func(t *testing.T) {
		db, variant := seed(t, "tracked", 2, "pending", "processing")
		exec := func(query string, args ...any) {
			t.Helper()
			if _, err := db.Exec(query, args...); err != nil {
				t.Fatal(err)
			}
		}
		// IF NOT EXISTS: the fixture applies real migrations into the scratch
		// schema, so order_payment_receipts / verifications may already
		// exist with the prod shape; the other four tables do not.
		for _, ddl := range []string{
			`CREATE TABLE IF NOT EXISTS stores(id TEXT PRIMARY KEY)`,
			`CREATE TABLE IF NOT EXISTS seller_discount_codes(id TEXT PRIMARY KEY, used_count BIGINT NOT NULL DEFAULT 0)`,
			`CREATE TABLE IF NOT EXISTS order_coupon_reservations(order_id TEXT PRIMARY KEY, coupon_id TEXT NOT NULL, store_id TEXT NOT NULL, code TEXT NOT NULL, seller_discount_amount BIGINT NOT NULL, coupon_discount_amount BIGINT, coupon_revision BIGINT, state TEXT NOT NULL, evidence_kind TEXT NOT NULL, reason TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp())`,
			`CREATE TABLE IF NOT EXISTS order_coupon_events(order_id TEXT NOT NULL, event_kind TEXT NOT NULL, prior_state TEXT, new_state TEXT NOT NULL, reason TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(order_id,event_kind))`,
			`CREATE TABLE IF NOT EXISTS order_payment_receipts(id TEXT PRIMARY KEY, order_id TEXT NOT NULL, verification_id TEXT, payment_id TEXT, amount_toman BIGINT, customer_id TEXT, funding_kind TEXT, currency TEXT)`,
			`CREATE TABLE IF NOT EXISTS order_payment_verifications(id TEXT PRIMARY KEY, order_id TEXT NOT NULL, amount_toman BIGINT, customer_id TEXT, funding_kind TEXT)`,
		} {
			exec(ddl)
		}
		exec(`INSERT INTO stores(id) VALUES('test-store')`)
		exec(`INSERT INTO seller_discount_codes(id, used_count) VALUES('coupon-1', 1)`)
		// Before the fences are installed: the order fence forbids changing
		// discount_code on an existing order, so the coupon must be set now.
		exec(`UPDATE orders SET discount_code='SAVE10', store_id='test-store' WHERE id=$1`, order)
		exec(`INSERT INTO order_coupon_reservations(order_id,coupon_id,store_id,code,seller_discount_amount,coupon_discount_amount,coupon_revision,state,evidence_kind,reason)
			VALUES($1,'coupon-1','test-store','SAVE10',100,100,1,'reserved','admission','checkout')`, order)
		for _, ddl := range []string{
			`CREATE FUNCTION fin007_order_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE backed boolean;
BEGIN
 backed:=btrim(COALESCE(CASE WHEN TG_OP='DELETE' THEN OLD.discount_code ELSE NEW.discount_code END,''))<>'';
 IF TG_OP='UPDATE' THEN
  backed:=backed OR btrim(COALESCE(OLD.discount_code,''))<>'';
  IF backed AND ((NOT OLD.inventory_captured AND NEW.inventory_captured) OR (NEW.status IN ('shipped','delivered') AND OLD.status NOT IN ('shipped','delivered','delivered_unconfirmed','delivered_confirmed','delivery_disputed'))) AND NOT EXISTS(SELECT 1 FROM order_coupon_reservations WHERE order_id=OLD.id) THEN RAISE EXCEPTION 'FIN007 unresolved coupon cannot admit fulfillment' USING ERRCODE='55000'; END IF;
  IF backed AND (OLD.store_id,OLD.discount_code,OLD.seller_discount_amount,OLD.coupon_discount_amount,OLD.total_amount,OLD.discount_amount,OLD.payable_amount) IS DISTINCT FROM (NEW.store_id,NEW.discount_code,NEW.seller_discount_amount,NEW.coupon_discount_amount,NEW.total_amount,NEW.discount_amount,NEW.payable_amount) THEN RAISE EXCEPTION 'FIN007 immutable order coupon snapshot' USING ERRCODE='55000'; END IF;
  IF NOT ((OLD.status='cancelled') IS DISTINCT FROM (NEW.status='cancelled') OR OLD.payment_status IS DISTINCT FROM NEW.payment_status OR OLD.inventory_captured IS DISTINCT FROM NEW.inventory_captured) THEN RETURN NEW; END IF;
 END IF;
 IF backed AND current_setting('sellersho.fin007_writer',true) IS DISTINCT FROM 'v1' THEN RAISE EXCEPTION 'FIN007-writer-required' USING ERRCODE='55000'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END $$`,
			`CREATE TRIGGER fin007_order_fence BEFORE INSERT OR UPDATE OR DELETE ON orders FOR EACH ROW EXECUTE FUNCTION fin007_order_fence()`,
			`CREATE FUNCTION fin007_payment_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE oid text;
BEGIN
 IF TG_TABLE_NAME='order_payment_verification_events' THEN
  SELECT order_id INTO oid FROM order_payment_verifications WHERE id=NEW.verification_id;
 ELSE oid:=NEW.order_id; END IF;
 IF TG_OP='UPDATE' AND TG_TABLE_NAME='payment_transactions' AND (to_jsonb(OLD)-ARRAY['updated_at','card_mask','rrn'])=(to_jsonb(NEW)-ARRAY['updated_at','card_mask','rrn']) THEN RETURN NEW; END IF;
 IF EXISTS(SELECT 1 FROM orders WHERE id=oid AND btrim(COALESCE(discount_code,''))<>'') AND current_setting('sellersho.fin007_writer',true) IS DISTINCT FROM 'v1' THEN RAISE EXCEPTION 'FIN007-writer-required' USING ERRCODE='55000'; END IF;
 RETURN NEW;
END $$`,
			`CREATE TRIGGER fin007_payment_fence BEFORE INSERT OR UPDATE ON payment_transactions FOR EACH ROW EXECUTE FUNCTION fin007_payment_fence()`,
		} {
			exec(ddl)
		}
		// Control: without the writer flag the fence must fire, proving this
		// test would catch a retry that skipped the confirm prologue.
		ctrl, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		_, ctrlErr := ctrl.Exec(`UPDATE payment_transactions SET status='paid' WHERE order_id=$1`, order)
		_ = ctrl.Rollback()
		if ctrlErr == nil || !strings.Contains(ctrlErr.Error(), "FIN007-writer-required") {
			t.Fatalf("fence control error = %v, want FIN007-writer-required", ctrlErr)
		}
		result, err := NewOrderRepository(db).RetryFailedCapture(ctx, order)
		wantCodes := []string{spnc + "-X0001", spnc + "-X0002"}
		if err != nil || result == nil || !result.Captured || !slices.Equal(result.UnitCodes, wantCodes) {
			t.Fatalf("coupon retry = %+v, %v, want captured order and FIFO codes %v", result, err, wantCodes)
		}
		var paymentStatus, orderPaymentStatus, couponState string
		var queued bool
		if err := db.QueryRow(`SELECT p.status,o.payment_status,r.state,
			EXISTS(SELECT 1 FROM spncx_failed_captures WHERE order_id=o.id)
			FROM orders o JOIN payment_transactions p ON p.order_id=o.id
			JOIN order_coupon_reservations r ON r.order_id=o.id WHERE o.id=$1`, order).
			Scan(&paymentStatus, &orderPaymentStatus, &couponState, &queued); err != nil {
			t.Fatal(err)
		}
		// No receipt rows exist, so the coupon is admitted to review_hold
		// (not consumed) — either state lets the order ship.
		if paymentStatus != "paid" || orderPaymentStatus != "paid" || couponState != "review_hold" || queued {
			t.Fatalf("payment=%q order payment=%q coupon=%q queued=%t", paymentStatus, orderPaymentStatus, couponState, queued)
		}
		assertInventory(t, db, variant, 0, 2, 2, 2, 1)
	})
}
