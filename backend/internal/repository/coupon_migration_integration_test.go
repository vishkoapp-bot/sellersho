package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/migrations"
	"github.com/drup/platform/internal/payment"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFIN007MigrationHistoricalReservations(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	other := opsFixtureNew(t, db)
	up, err := os.ReadFile("../../migrations/202611170001_coupon_reservations.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/202611170001_coupon_reservations.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	run := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := tx.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	run(string(down))
	coupon := "h-" + uuid.NewString()[:8]
	run(`INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at,max_uses,used_count) VALUES($1,$2,'SAVE','fixed',10,NOW()-INTERVAL '2 days',NOW()+INTERVAL '2 days',1,9)`, coupon, f.store)
	run(`INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at) VALUES($1,$2,'SAVE','fixed',10,NOW()-INTERVAL '2 days',NOW()+INTERVAL '2 days')`, coupon+"-other", other.store)
	cases := []struct {
		name, status, payment, want string
		captured, deadline          bool
		amount                      int64
	}{
		{"paid", "processing", "paid", "consumed", false, true, 123}, {"refunded", "cancelled", "refunded", "consumed", false, true, 234}, {"captured", "processing", "pending", "consumed", true, true, 345},
		{"pending", "processing", "pending", "reserved", false, true, 456}, {"overdue", "pending", "pending", "reserved", false, true, 567}, {"cancelled", "cancelled", "pending", "released", false, true, 678},
		{"failed", "processing", "failed", "review_hold", false, true, 789}, {"unknown", "cancelled", "pending", "review_hold", false, true, 890}, {"no-deadline", "processing", "pending", "review_hold", false, false, 901}, {"zero", "processing", "pending", "review_hold", false, true, 0}, {"malformed", "processing", "pending", "review_hold", false, true, -1},
	}
	for _, c := range cases {
		id := coupon + "-" + c.name
		run(`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status,status,inventory_captured,seller_discount_amount,discount_code,payment_expires_at) VALUES($1,$1,$2,'history',2000,1000,'fixture','09123456789','x','x','x','1234512345',$1,$3,$4,$5,$6,$7,'SAVE',CASE WHEN $8 THEN NOW()-INTERVAL '1 hour' ELSE NULL END)`, id, f.store, f.user, c.payment, c.status, c.captured, c.amount, c.deadline)
		if c.name == "unknown" {
			pid := uuid.NewString()
			run(`INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES($1,$2,$2,$1,1000,'pending')`, pid, id)
			run(`INSERT INTO order_payment_verifications(id,payment_id,order_id,customer_id,amount_toman,currency,funding_kind) VALUES($1,$1,$2,$3,1000,'TOMAN','simulated')`, pid, id, f.user)
		}
	}

	for _, c := range []struct{ id, store, code string }{{coupon + "-missing", f.store, "MISSING"}, {coupon + "-ambiguous", f.store, "AMBIG"}, {coupon + "-scoped", other.store, "SAVE"}} {
		run(`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status,status,seller_discount_amount,discount_code,payment_expires_at) VALUES($1,$1,$2,'history',2000,1000,'fixture','09123456789','x','x','x','1234512345',$1,$3,'pending','processing',100,$4,NOW()+INTERVAL '1 hour')`, c.id, c.store, f.user, c.code)
	}
	for i, code := range []string{"AMBIG", " ambig "} {
		run(`INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at) VALUES($1,$2,$3,'fixed',10,NOW()-INTERVAL '1 day',NOW()+INTERVAL '1 day')`, fmt.Sprintf("%s-a%d", coupon, i), f.store, code)
	}
	var before string
	if err = tx.QueryRow(`SELECT jsonb_agg(jsonb_build_array(id,total_amount,payable_amount,seller_discount_amount) ORDER BY id)::text FROM orders WHERE store_id=$1`, f.store).Scan(&before); err != nil {
		t.Fatal(err)
	}
	run(string(up))
	var after string
	if err = tx.QueryRow(`SELECT jsonb_agg(jsonb_build_array(id,total_amount,payable_amount,seller_discount_amount) ORDER BY id)::text FROM orders WHERE store_id=$1`, f.store).Scan(&after); err != nil || before != after {
		t.Fatalf("history repriced %v", err)
	}
	for _, c := range cases {
		var state string
		var amount sql.NullInt64
		if err = tx.QueryRow(`SELECT state,coupon_discount_amount FROM order_coupon_reservations WHERE order_id=$1`, coupon+"-"+c.name).Scan(&state, &amount); err != nil || state != c.want || amount.Valid {
			t.Fatalf("history %s state=%s amount=%v err=%v", c.name, state, amount, err)
		}
	}
	var count, issues int
	if err = tx.QueryRow(`SELECT used_count,(SELECT count(*) FROM coupon_reconciliation_issues WHERE coupon_id=$1) FROM seller_discount_codes WHERE id=$1`, coupon).Scan(&count, &issues); err != nil || count != 10 || issues < 6 {
		t.Fatalf("rebuilt occupancy=%d issues=%d err=%v", count, issues, err)
	}

	var unresolved, scoped int
	if err = tx.QueryRow(`SELECT (SELECT count(*) FROM coupon_reconciliation_issues WHERE order_id IN ($1,$2) AND reason IN ('missing_identity','ambiguous_identity')),(SELECT used_count FROM seller_discount_codes WHERE id=$3)`, coupon+"-missing", coupon+"-ambiguous", coupon+"-other").Scan(&unresolved, &scoped); err != nil || unresolved != 2 || scoped != 1 {
		t.Fatalf("normalized identity/scoping issues=%d scoped=%d %v", unresolved, scoped, err)
	}
	// Deleting/recreating an unresolved normalized identity cannot bypass its
	// immutable issue. Roll back the rejected private order to its savepoint.
	run(`SAVEPOINT recreated`)
	run(`DELETE FROM seller_discount_codes WHERE id IN ($1,$2)`, coupon+"-a0", coupon+"-a1")
	run(`INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at) VALUES($1,$2,'AMBIG','fixed',100,NOW()-INTERVAL '1 day',NOW()+INTERVAL '1 day')`, coupon+"-recreated", f.store)
	o := domain.Order{ID: coupon + "-new", OrderNumber: coupon + "-new", StoreID: f.store, StoreName: "fixture", CustomerUserID: &f.user, TotalAmount: 1000, PayableAmount: 900, GatewayAmount: 900, PaymentMethod: "cash", PaymentStatus: "pending", Status: "processing", CustomerFullName: "fixture", CustomerPhone: "09123456789", CustomerProvince: "x", CustomerCity: "x", CustomerAddress: "x", CustomerPostalCode: "1234512345", TrackingCode: coupon + "-new", DiscountCode: "AMBIG", SellerDiscountAmount: 100}
	fin007Evidence(&o, coupon+"-recreated", 1, 100)
	if err = NewOrderRepository(db).insertOrderWithShipmentsTx(context.Background(), tx, o, nil, false); !errors.Is(err, ErrCouponInvalid) {
		t.Fatalf("recreated ambiguous identity admitted %v", err)
	}
	run(`ROLLBACK TO SAVEPOINT recreated`)
	// Force every deferred check before rolling this isolated historical rehearsal back.
	run(`SET CONSTRAINTS ALL IMMEDIATE`)
}
func TestFIN007MigrationIdentityAndFence(t *testing.T) {
	r, _, o, coupon := fin007Fixture(t, 1)
	ctx := context.Background()
	if err := r.CreateOrder(ctx, o); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE seller_discount_codes SET used_count=used_count+1 WHERE id=$1`,
		`DELETE FROM seller_discount_codes WHERE id=$1`,
		`UPDATE seller_discount_codes SET code='REBIND' WHERE id=$1`,
	} {
		if _, err := r.db.Exec(q, coupon); err == nil {
			t.Fatalf("coupon evidence bypass %s", q)
		}
	}
	for _, q := range []string{`DELETE FROM orders WHERE id=$1`, `UPDATE orders SET seller_discount_amount=200 WHERE id=$1`, `UPDATE order_coupon_reservations SET state='released' WHERE order_id=$1`, `DELETE FROM order_coupon_events WHERE order_id=$1`} {
		if _, err := r.db.Exec(q, o.ID); err == nil {
			t.Fatalf("order evidence bypass %s", q)
		}
	}
	for _, q := range []string{`UPDATE seller_discount_codes SET used_count=2 WHERE id=$1`, `UPDATE order_coupon_reservations SET state='consumed' WHERE coupon_id=$1`, `UPDATE order_coupon_reservations SET state='released' WHERE coupon_id=$1`, `UPDATE order_coupon_reservations SET coupon_discount_amount=99 WHERE coupon_id=$1`} {
		tx, err := r.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`SELECT set_config('sellersho.fin007_writer','v1',true)`); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(q, coupon)
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
		if err == nil {
			t.Fatalf("marked forged evidence accepted %s", q)
		}
	}
	fin007State(t, r.db, o.ID, "reserved", 1)
	down, err := os.ReadFile("../../migrations/202611170001_coupon_reservations.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := r.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(string(down)); err == nil || !strings.Contains(err.Error(), "populated evidence") {
		t.Fatalf("down lost evidence %v", err)
	}
}
func TestFIN007RuntimeLegacyForgery(t *testing.T) {
	r, _, o, coupon := fin007Fixture(t, 1)
	ctx := context.Background()
	if err := r.CreateOrder(ctx, o); err != nil {
		t.Fatal(err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	forged := fin007Fresh(o)
	if err = r.insertOrderWithShipmentsTx(ctx, tx, forged, nil, false); !errors.Is(err, ErrCouponExhausted) {
		t.Fatalf("expected full coupon, got %v", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO order_coupon_reservations(order_id,coupon_id,store_id,code,seller_discount_amount,coupon_discount_amount,state,evidence_kind,reason) VALUES($1,$2,$3,'SAVE',100,100,'consumed','legacy_snapshot','forged')`, forged.ID, coupon, o.StoreID)
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE seller_discount_codes SET used_count=used_count+1 WHERE id=$1`, coupon)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO order_coupon_events(order_id,event_kind,new_state,reason) VALUES($1,'legacy_snapshot','consumed','forged')`, forged.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err == nil {
		t.Fatal("marked runtime legacy snapshot bypassed capacity and receipt authority")
	}
	fin007State(t, r.db, o.ID, "reserved", 1)
}
func TestFIN007AdmissionNullAmountForgery(t *testing.T) {
	r, _, o, coupon := fin007Fixture(t, 5)
	ctx := context.Background()
	o.CouponDiscountAmount = nil
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = r.insertOrderWithShipmentsTx(ctx, tx, o, nil, false); !errors.Is(err, ErrCouponEvidence) {
		t.Fatalf("expected missing amount, got %v", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO order_coupon_reservations(order_id,coupon_id,store_id,code,seller_discount_amount,coupon_discount_amount,coupon_revision,state,evidence_kind,reason) VALUES($1,$2,$3,'SAVE',100,NULL,1,'reserved','admission','forged')`, o.ID, coupon, o.StoreID)
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE seller_discount_codes SET used_count=used_count+1 WHERE id=$1`, coupon)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO order_coupon_events(order_id,event_kind,new_state,reason) VALUES($1,'reserved','reserved','forged')`, o.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err == nil {
		t.Fatal("marked runtime admission accepted NULL coupon-only amount")
	}
}
func TestFIN007MigrationRunnerReplay(t *testing.T) {
	db := operationsDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		report, err := migrations.Run(ctx, db, migrations.Options{Dir: "../../migrations", LockTimeout: time.Second, MigrationTimeout: 5 * time.Second})
		if err != nil || len(report.Applied) != 0 {
			t.Fatalf("migration replay %v %v", report.Applied, err)
		}
	}
}

// This fixture installs the complete canonical prior schema in an owned schema,
// then invokes the canonical runner for FIN007. No production constraint is
// removed and no issue is manufactured through a runtime writer.
func fin007PriorSchema(t *testing.T) (*sql.DB, string) {
	t.Helper()
	base := operationsDB(t)
	schema := "fin007_legacy_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	opsExec(t, base, `CREATE SCHEMA `+schema)
	u, err := url.Parse(os.Getenv("OPERATIONS_TEST_DATABASE_URL"))
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
		if _, e := base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); e != nil {
			t.Error(e)
		}
	})
	// The shared disposable DB already owns uuid-ossp in public. Expose only
	// that extension function, never public relation fallback, to this schema.
	opsExec(t, db, `CREATE FUNCTION uuid_generate_v4() RETURNS UUID LANGUAGE SQL VOLATILE AS 'SELECT public.uuid_generate_v4()'`)
	prior := t.TempDir()
	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") && !strings.HasPrefix(entry.Name(), "202611170001_") {
			b, e := os.ReadFile(filepath.Join("../../migrations", entry.Name()))
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(prior, entry.Name()), b, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err = migrations.Run(ctx, db, migrations.Options{Dir: prior, LockTimeout: time.Second, MigrationTimeout: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	return db, schema
}

// Each distinct compensation has its own synthetic provider reference, as a
// real provider must. Replay of the same operation retains that reference.
type fin007HistoricalRefundProvider struct{}

func (f fin007HistoricalRefundProvider) Refund(_ context.Context, q payment.RefundRequest) (*payment.RefundOutcome, error) {
	return &payment.RefundOutcome{OperationKey: q.OperationKey, PaymentID: q.PaymentID, AmountToman: q.AmountToman, Currency: q.Currency, Destination: q.Destination, Status: "succeeded", ProviderReference: "fin007-" + q.OperationKey}, nil
}
func (f fin007HistoricalRefundProvider) LookupRefund(ctx context.Context, q payment.RefundRequest) (*payment.RefundOutcome, error) {
	return f.Refund(ctx, q)
}

func TestFIN007MigrationUnresolvedMoney(t *testing.T) {
	db, _ := fin007PriorSchema(t)
	f := opsFixtureNew(t, db)
	warehouse := seedCanonicalWarehouseStock(t, db, f.product, f.supplier, 100)
	ctx := context.Background()
	r := NewOrderRepository(db)
	for i, code := range []string{"AMBIG", " ambig "} {
		opsExec(t, db, `INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at) VALUES($1,$2,$3,'fixed',100,NOW()-INTERVAL '1 day',NOW()+INTERVAL '1 day')`, fmt.Sprintf("ambig-%d", i), f.store, code)
	}
	type legacy struct{ code, kind, id, token string }
	cases := []legacy{}
	for _, code := range []string{"MISSING", "AMBIG"} {
		for _, kind := range []string{"pending", "cancelled", "captured"} {
			id, token := opsOrder(t, db, f, false)
			if kind == "captured" {
				seedCanonicalWarehouseOrderOrigins(t, db, id, warehouse)
				if _, err := r.MarkPaymentPaid(ctx, token, "history-"+uuid.NewString()[:8], "mask"); err != nil {
					t.Fatal(err)
				}
				fin005ReturnFixtureExec(t, db, id, `UPDATE orders SET status='delivered_confirmed',shipped_at=NOW()-INTERVAL '1 day' WHERE id=$1`, id)
				for _, target := range []string{"shipped", "in_transit", "delivered", "failed", "pending"} {
					initial := "shipped"
					if target == "pending" {
						initial = "pending"
					}
					opsExec(t, db, `INSERT INTO supplier_shipments(id,order_id,supplier_id,shipping_address,customer_phone,customer_name,status,tracking_number,shipped_at) VALUES($1,$2,$3,'{}','09900000000','fixture',$4::text,$1,CASE WHEN $4::text='shipped' THEN NOW() ELSE NULL END)`, id+"-"+target, id, f.supplier, initial)
				}
			}
			opsExec(t, db, `UPDATE orders SET discount_code=$2,seller_discount_amount=100 WHERE id=$1`, id, code)
			if kind == "cancelled" {
				opsExec(t, db, `UPDATE orders SET status='cancelled' WHERE id=$1`, id)
			}
			cases = append(cases, legacy{code, kind, id, token})
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := migrations.Run(bounded, db, migrations.Options{Dir: "../../migrations", LockTimeout: time.Second, MigrationTimeout: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.code+"_"+c.kind, func(t *testing.T) {
			before := 0
			if err := db.QueryRow(`SELECT stock FROM products WHERE id=$1`, f.product).Scan(&before); err != nil {
				t.Fatal(err)
			}
			id, err := r.BeginPaymentVerification(ctx, c.token, "gateway")
			if err != nil {
				t.Fatalf("issue blocked inquiry %v", err)
			}
			result := payment.VerificationResult{Success: true, RRN: "legacy-" + uuid.NewString()[:8], VerifiedAt: time.Now(), VerifiedAmount: 2000}
			for i := 0; i < 2; i++ {
				if err = r.RecordPaymentVerification(ctx, id, result); err != nil {
					t.Fatalf("issue discarded known receipt %v", err)
				}
			}
			var receipts, stock int
			var status string
			var captured bool
			if err = db.QueryRow(`SELECT (SELECT count(*) FROM order_payment_receipts WHERE order_id=$1),(SELECT stock FROM products WHERE id=$2),status,inventory_captured FROM orders WHERE id=$1`, c.id, f.product).Scan(&receipts, &stock, &status, &captured); err != nil || receipts != 1 || stock != before {
				t.Fatalf("receipt/stock preservation %d %d/%d %v", receipts, stock, before, err)
			}
			if c.kind == "captured" {
				if status != "delivered_confirmed" || !captured {
					t.Fatal("existing fulfillment was undone")
				}
				if _, err = r.MarkPaymentPaid(ctx, c.token, result.RRN, "mask"); err != nil {
					t.Fatalf("captured paid replay changed %v", err)
				}
				shipments := NewSupplierShipmentRepository(db)
				t.Run("old_child_only_dispatch", func(t *testing.T) {
					oldTx, e := db.BeginTx(ctx, nil)
					if e != nil {
						t.Fatal(e)
					}
					_, e = oldTx.ExecContext(ctx, `UPDATE supplier_shipments SET status='shipped',carrier='post',tracking_number='old-new-track',shipped_at=NOW() WHERE id=$1`, c.id+"-pending")
					oldTx.Rollback()
					var pg *pq.Error
					if !errors.As(e, &pg) || pg.Code != "55000" {
						t.Fatalf("unmarked child-only old dispatch escaped fence: %v", e)
					}
				})
				for _, target := range []string{"shipped", "in_transit", "delivered", "failed"} {
					if _, err = shipments.UpdateShipmentStatus(ctx, c.id+"-"+target, target); err != nil {
						t.Fatalf("existing physical %s progress blocked: %v", target, err)
					}
				}
				if err = shipments.MarkShippedForSupplier(ctx, f.supplier, c.id+"-shipped", "post", c.id+"-shipped", nil); err != nil {
					t.Fatalf("existing shipped replay blocked: %v", err)
				}
				if err = shipments.MarkShippedForSupplier(ctx, f.supplier, c.id+"-pending", "post", "new-track", nil); !errors.Is(err, ErrCouponEvidence) {
					t.Fatalf("unresolved new physical dispatch admitted: %v", err)
				}
				if err = r.UpdateOrderFulfillment(ctx, c.id, "shipped", ""); !errors.Is(err, ErrCouponEvidence) {
					t.Fatalf("new dispatch from unresolved coupon %v", err)
				}
				if err = r.CancelOrder(ctx, c.id, "fixture", true); !errors.Is(err, ErrOrderPartiallyDispatched) {
					t.Fatalf("historical shipped cancellation changed %v", err)
				}
				ret := fin003ReceivedReturn(t, db, f, warehouse, c.id)
				if amount, _, e := r.RefundCustomerReturn(ctx, ret, f.user); e != nil || amount != 2000 {
					t.Fatalf("legitimate return blocked %d %v", amount, e)
				}
			} else {
				if status != "cancelled" || captured {
					t.Fatal("uncaptured unresolved order admitted fulfillment")
				}
				if err = r.CancelOrder(ctx, c.id, "fixture", true); err != nil {
					t.Fatalf("cancel replay blocked %v", err)
				}
				var obligation string
				if err = db.QueryRow(`SELECT id FROM order_refund_obligations WHERE order_id=$1 AND purpose='cancellation'`, c.id).Scan(&obligation); err != nil {
					t.Fatal(err)
				}
				fake := fin007HistoricalRefundProvider{}
				if state, e := r.ProcessOrderRefund(ctx, obligation, fake); e != nil || state != "succeeded" {
					t.Fatalf("legitimate compensation blocked %s %v", state, e)
				}
			}
		})
	}
	var rows, count, issues int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM order_coupon_reservations),(SELECT COALESCE(sum(used_count),0) FROM seller_discount_codes),(SELECT count(*) FROM coupon_reconciliation_issues WHERE reason IN ('missing_identity','ambiguous_identity'))`).Scan(&rows, &count, &issues); err != nil || rows != 0 || count != 0 || issues != len(cases) {
		t.Fatalf("invented coupon identity rows/count/issues %d/%d/%d %v", rows, count, issues, err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = markCouponWriterTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO coupon_reconciliation_issues(order_id,store_id,code,reason,source_snapshot) VALUES($1,$2,'MISSING','missing_identity','{}')`, cases[0].id, f.store); err == nil {
		t.Fatal("v1 marker forged migration-origin exception")
	}
}

func TestFIN007HistoricalAggregateProgress(t *testing.T) {
	db, _ := fin007PriorSchema(t)
	f := opsFixtureNew(t, db)
	ctx := context.Background()
	for i, code := range []string{"AMBIG", " ambig "} {
		opsExec(t, db, `INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at) VALUES($1,$2,$3,'fixed',100,NOW()-INTERVAL '1 day',NOW()+INTERVAL '1 day')`, fmt.Sprintf("aggregate-ambig-%d", i), f.store, code)
	}
	orders := map[string]string{}
	for _, code := range []string{"MISSING", "AMBIG"} {
		for _, state := range []string{"shipped", "processing"} {
			id, _ := opsOrder(t, db, f, true)
			if state == "shipped" {
				fin005ReturnFixtureExec(t, db, id, `UPDATE orders SET status='shipped',shipped_at=NOW()-INTERVAL '1 day' WHERE id=$1`, id)
			} else {
				opsExec(t, db, `UPDATE orders SET status='processing' WHERE id=$1`, id)
			}
			opsExec(t, db, `UPDATE orders SET discount_code=$2,seller_discount_amount=100 WHERE id=$1`, id, code)
			orders[code+state] = id
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := migrations.Run(bounded, db, migrations.Options{Dir: "../../migrations", LockTimeout: time.Second, MigrationTimeout: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	r := NewOrderRepository(db)
	for _, code := range []string{"MISSING", "AMBIG"} {
		t.Run(code, func(t *testing.T) {
			id := orders[code+"shipped"]
			for i := 0; i < 2; i++ {
				if err := r.UpdateOrderFulfillment(ctx, id, "delivered", id+"-track"); err != nil {
					t.Fatalf("existing aggregate delivery/replay blocked: %v", err)
				}
			}
			if err := r.UpdateOrderFulfillment(ctx, id, "shipped", "new-track"); !errors.Is(err, ErrCouponEvidence) {
				t.Fatalf("reverse dispatch admitted: %v", err)
			}
			for _, target := range []string{"shipped", "delivered"} {
				if err := r.UpdateOrderFulfillment(ctx, orders[code+"processing"], target, "new-track"); !errors.Is(err, ErrCouponEvidence) {
					t.Fatalf("never-dispatched %s admitted: %v", target, err)
				}
			}
		})
	}
}

func TestFIN007AdmissionDatabaseClock(t *testing.T) {
	for _, mode := range []string{"expired_backdated", "future_postdated", "valid_late_commit"} {
		t.Run(mode, func(t *testing.T) {
			r, _, o, coupon := fin007Fixture(t, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stamp := "clock_timestamp()-INTERVAL '36 hours'"
			switch mode {
			case "expired_backdated":
				opsExec(t, r.db, `UPDATE seller_discount_codes SET starts_at=clock_timestamp()-INTERVAL '2 days',ends_at=clock_timestamp()-INTERVAL '1 day' WHERE id=$1`, coupon)
			case "future_postdated":
				stamp = "clock_timestamp()+INTERVAL '36 hours'"
				opsExec(t, r.db, `UPDATE seller_discount_codes SET starts_at=clock_timestamp()+INTERVAL '1 day',ends_at=clock_timestamp()+INTERVAL '2 days' WHERE id=$1`, coupon)
			case "valid_late_commit":
				stamp = "clock_timestamp()"
				opsExec(t, r.db, `UPDATE seller_discount_codes SET ends_at=clock_timestamp()+INTERVAL '2 seconds' WHERE id=$1`, coupon)
			}
			if err := r.db.QueryRow(`SELECT revision FROM seller_discount_codes WHERE id=$1`, coupon).Scan(&o.CouponRevision); err != nil {
				t.Fatal(err)
			}
			tx, err := r.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if mode == "valid_late_commit" {
				if err = r.insertOrderWithShipmentsTx(ctx, tx, o, nil, false); err != nil {
					t.Fatal(err)
				}
				if _, err = tx.ExecContext(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM ends_at-clock_timestamp())+0.02)) FROM seller_discount_codes WHERE id=$1`, coupon); err != nil {
					t.Fatal(err)
				}
			} else {
				if err = r.insertOrderWithShipmentsTx(ctx, tx, o, nil, false); !errors.Is(err, ErrCouponInvalid) {
					t.Fatalf("expected invalid current terms: %v", err)
				}
				_, err = tx.ExecContext(ctx, `INSERT INTO order_coupon_reservations(order_id,coupon_id,store_id,code,seller_discount_amount,coupon_discount_amount,coupon_revision,state,evidence_kind,reason,created_at) VALUES($1,$2,$3,'SAVE',100,100,$4,'reserved','admission','forged',`+stamp+`)`, o.ID, coupon, o.StoreID, o.CouponRevision)
				if err == nil {
					_, err = tx.ExecContext(ctx, `UPDATE seller_discount_codes SET used_count=used_count+1 WHERE id=$1`, coupon)
				}
				if err == nil {
					_, err = tx.ExecContext(ctx, `INSERT INTO order_coupon_events(order_id,event_kind,new_state,reason) VALUES($1,'reserved','reserved','forged')`, o.ID)
				}
			}
			if err == nil {
				err = tx.Commit()
			}
			if mode == "valid_late_commit" {
				if err != nil {
					t.Fatalf("valid held slot rejected at late commit: %v", err)
				}
			} else if err == nil {
				t.Fatal("caller-controlled reservation timestamp bypassed current database window")
			}
		})
	}
	t.Run("lock_wait_crosses_end", func(t *testing.T) {
		r, _, o, coupon := fin007Fixture(t, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		blocker, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Rollback()
		if err = blocker.QueryRowContext(ctx, `UPDATE seller_discount_codes SET ends_at=clock_timestamp()+INTERVAL '1 second' WHERE id=$1 RETURNING revision`, coupon).Scan(&o.CouponRevision); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- r.CreateOrder(ctx, o) }()
		for {
			var waiting bool
			if err = r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM seller_discount_codes%FOR UPDATE%')`).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				break
			}
		}
		if _, err = blocker.ExecContext(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM ends_at-clock_timestamp())+0.02)) FROM seller_discount_codes WHERE id=$1`, coupon); err != nil {
			t.Fatal(err)
		}
		if err = blocker.Commit(); err != nil {
			t.Fatal(err)
		}
		if err = <-done; !errors.Is(err, ErrCouponInvalid) {
			t.Fatalf("post-lock expired admission: %v", err)
		}
		var n int
		if err = r.db.QueryRow(`SELECT count(*) FROM orders WHERE id=$1`, o.ID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("late waiter leaked order: %d %v", n, err)
		}
	})
	t.Run("marked_insert_before_counter_wait", func(t *testing.T) {
		r, _, o, coupon := fin007Fixture(t, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		blocker, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Rollback()
		if err = blocker.QueryRowContext(ctx, `UPDATE seller_discount_codes SET ends_at=clock_timestamp()+INTERVAL '2 seconds' WHERE id=$1 RETURNING revision`, coupon).Scan(&o.CouponRevision); err != nil {
			t.Fatal(err)
		}
		writer, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Rollback()
		o.CouponID = "" // Reject trusted evidence before the helper acquires coupon350.
		if err = r.insertOrderWithShipmentsTx(ctx, writer, o, nil, false); !errors.Is(err, ErrCouponEvidence) {
			t.Fatalf("private forged setup: %v", err)
		}
		done := make(chan error, 1)
		go func() {
			_, e := writer.ExecContext(ctx, `INSERT INTO order_coupon_reservations(order_id,coupon_id,store_id,code,seller_discount_amount,coupon_discount_amount,coupon_revision,state,evidence_kind,reason) VALUES($1,$2,$3,'SAVE',100,100,$4,'reserved','admission','forged-before-guard')`, o.ID, coupon, o.StoreID, o.CouponRevision)
			if e == nil {
				_, e = writer.ExecContext(ctx, `UPDATE seller_discount_codes SET used_count=used_count+1 WHERE id=$1`, coupon)
			}
			if e == nil {
				_, e = writer.ExecContext(ctx, `INSERT INTO order_coupon_events(order_id,event_kind,new_state,reason) VALUES($1,'reserved','reserved','forged-before-guard')`, o.ID)
			}
			if e == nil {
				e = writer.Commit()
			}
			done <- e
		}()
		for {
			var waiting bool
			if err = r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND (query LIKE 'INSERT INTO order_coupon_reservations%' OR query LIKE 'UPDATE seller_discount_codes SET used_count%'))`).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				break
			}
		}
		if _, err = blocker.ExecContext(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM ends_at-clock_timestamp())+0.02)) FROM seller_discount_codes WHERE id=$1`, coupon); err != nil {
			t.Fatal(err)
		}
		if err = blocker.Commit(); err != nil {
			t.Fatal(err)
		}
		if err = <-done; err == nil {
			t.Fatal("marked insert admitted before real coupon guard waited beyond expiry")
		}
	})
}

func TestFIN007HistoricalReleaseProof(t *testing.T) {
	db, _ := fin007PriorSchema(t)
	f := opsFixtureNew(t, db)
	ctx := context.Background()
	r := NewOrderRepository(db)
	coupon := "release-" + uuid.NewString()[:8]
	opsExec(t, db, `INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at) VALUES($1,$2,'SAVE','fixed',100,NOW()-INTERVAL '1 day',NOW()+INTERVAL '1 day')`, coupon, f.store)
	type history struct{ id, token string }
	orders := map[string]history{}
	for _, kind := range []string{"inquiry_hold", "receipt_hold", "eligible", "already_released"} {
		id, token := opsOrder(t, db, f, false)
		if kind == "inquiry_hold" || kind == "receipt_hold" {
			v, err := r.BeginPaymentVerification(ctx, token, "gateway")
			if err != nil {
				t.Fatal(err)
			}
			if kind == "receipt_hold" {
				if err = r.RecordPaymentVerification(ctx, v, payment.VerificationResult{Success: true, RRN: "hold-" + uuid.NewString()[:8], VerifiedAt: time.Now(), VerifiedAmount: 2000}); err != nil {
					t.Fatal(err)
				}
			}
		}
		if kind != "eligible" {
			if err := r.CancelOrder(ctx, id, "fixture", true); err != nil {
				t.Fatal(err)
			}
		}
		amount := 100
		if kind == "receipt_hold" {
			amount = 0
		}
		opsExec(t, db, `UPDATE orders SET discount_code='SAVE',seller_discount_amount=$2 WHERE id=$1`, id, amount)
		orders[kind] = history{id, token}
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := migrations.Run(bounded, db, migrations.Options{Dir: "../../migrations", LockTimeout: time.Second, MigrationTimeout: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"inquiry_hold", "receipt_hold"} {
		t.Run(kind, func(t *testing.T) {
			id := orders[kind].id
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err = lockOrderFinanceTx(ctx, tx, id); err != nil {
				t.Fatal(err)
			}
			_, err = tx.ExecContext(ctx, `UPDATE order_coupon_reservations SET state='released',reason='forged' WHERE order_id=$1`, id)
			if err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE seller_discount_codes SET used_count=used_count-1 WHERE id=$1`, coupon)
			}
			if err == nil {
				_, err = tx.ExecContext(ctx, `INSERT INTO order_coupon_events(order_id,event_kind,prior_state,new_state,reason) VALUES($1,'released','review_hold','released','forged')`, id)
			}
			if err == nil {
				err = tx.Commit()
			}
			if err == nil {
				t.Fatal("historical review hold released despite unresolved/verified money evidence")
			}
		})
	}
	t.Run("eligible_unpaid", func(t *testing.T) {
		id := orders["eligible"].id
		if err := r.CancelOrder(ctx, id, "fixture", true); err != nil {
			t.Fatal(err)
		}
		var state string
		if err := db.QueryRow(`SELECT state FROM order_coupon_reservations WHERE order_id=$1`, id).Scan(&state); err != nil || state != "released" {
			t.Fatalf("eligible legacy cancellation not released: %s %v", state, err)
		}
	})
	t.Run("already_released_late_receipt", func(t *testing.T) {
		o := orders["already_released"]
		v, err := r.BeginPaymentVerification(ctx, o.token, "gateway")
		if err != nil {
			t.Fatal(err)
		}
		if err = r.RecordPaymentVerification(ctx, v, payment.VerificationResult{Success: true, RRN: "late-" + uuid.NewString()[:8], VerifiedAt: time.Now(), VerifiedAmount: 2000}); err != nil {
			t.Fatalf("already-released historical receipt discarded: %v", err)
		}
		var state string
		var n int
		if err = db.QueryRow(`SELECT state,(SELECT count(*) FROM order_payment_receipts WHERE order_id=$1) FROM order_coupon_reservations WHERE order_id=$1`, o.id).Scan(&state, &n); err != nil || state != "released" || n != 1 {
			t.Fatalf("late receipt/state %s/%d %v", state, n, err)
		}
	})
}
