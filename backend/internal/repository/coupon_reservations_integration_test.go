package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/payment"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFIN007RetryClassification(t *testing.T) {
	for _, code := range []pq.ErrorCode{"40001", "40P01", "55P03"} {
		err := fmt.Errorf("guard: %w", &pq.Error{Code: code})
		if !couponTransactionRetryable(err) {
			t.Fatalf("retryable SQLSTATE %s lost", code)
		}
		for _, terminal := range []error{context.Canceled, context.DeadlineExceeded, ErrCouponExhausted, ErrCouponInvalid, ErrCouponChanged, ErrCouponEvidence, ErrTransactionLockOrder} {
			if couponTransactionRetryable(errors.Join(terminal, err)) {
				t.Fatalf("terminal %v retried with SQLSTATE %s", terminal, code)
			}
		}
	}
}

func TestFIN007CouponEvidenceSQLState(t *testing.T) {
	r, _, o, _ := fin007Fixture(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.CreateOrder(ctx, o); err != nil {
		t.Fatal(err)
	}
	blocker, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err = blocker.ExecContext(ctx, `LOCK TABLE order_coupon_reservations IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func(*sql.Tx) error{
		"lock":       func(tx *sql.Tx) error { return lockOrderCouponTx(ctx, tx, &TransactionLockSet{}, o.ID) },
		"dispatch":   func(tx *sql.Tx) error { return assertCouponDispatchAllowedTx(ctx, tx, o.ID) },
		"confirm":    func(tx *sql.Tx) error { _, e := confirmOrderCouponTx(ctx, tx, o.ID, true); return e },
		"cancel":     func(tx *sql.Tx) error { return cancelOrderCouponTx(ctx, tx, o.ID, "customer_cancel") },
		"transition": func(tx *sql.Tx) error { return transitionOrderCouponTx(ctx, tx, o.ID, "review_hold", "fixture") },
	} {
		t.Run(name, func(t *testing.T) {
			tx, e := r.db.BeginTx(ctx, nil)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback()
			if _, e = tx.ExecContext(ctx, `SET LOCAL lock_timeout='20ms'`); e != nil {
				t.Fatal(e)
			}
			e = call(tx)
			var pg *pq.Error
			if !errors.Is(e, ErrCouponEvidence) || !errors.As(e, &pg) || pg.Code != "55P03" {
				t.Fatalf("evidence acquisition lost SQLSTATE: %v", e)
			}
		})
	}
}

func TestFIN007RetryBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := couponRetryPause(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retry continued: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := couponRetryPause(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("retry widened budget: %v", err)
	}
}

func TestFIN007CreationRetryRollback(t *testing.T) {
	for _, keyed := range []bool{false, true} {
		for _, persistent := range []bool{false, true} {
			t.Run(fmt.Sprintf("keyed=%v/persistent=%v", keyed, persistent), func(t *testing.T) {
				r, f, o, coupon := fin007Fixture(t, 1)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				seq := "fin007_retry_" + strings.ReplaceAll(uuid.NewString(), "-", "")
				condition := "n<=2"
				if persistent {
					condition = "TRUE"
				}
				opsExec(t, r.db, `CREATE SEQUENCE `+seq+`;CREATE FUNCTION `+seq+`() RETURNS TRIGGER LANGUAGE plpgsql AS $$ DECLARE n bigint; BEGIN IF NEW.id='`+o.ID+`' THEN n:=nextval('`+seq+`');IF `+condition+` THEN RAISE EXCEPTION 'fixture lock timeout' USING ERRCODE='55P03';END IF;END IF;RETURN NEW;END $$;CREATE TRIGGER `+seq+` BEFORE INSERT ON orders FOR EACH ROW EXECUTE FUNCTION `+seq+`()`)
				defer r.db.Exec(`DROP TRIGGER ` + seq + ` ON orders;DROP FUNCTION ` + seq + `();DROP SEQUENCE ` + seq)
				var err error
				if keyed {
					hash := strings.Repeat("b", 64)
					if _, err = r.BeginCheckoutAttempt(ctx, f.user, *o.IdempotencyKey, hash); err != nil {
						t.Fatal(err)
					}
					_, _, err = r.CreateCheckoutOrderWithShipments(ctx, o, nil, hash)
				} else {
					err = r.CreateOrder(ctx, o)
				}
				var attempts, orders, count int
				if e := r.db.QueryRow(`SELECT last_value,(SELECT count(*) FROM orders WHERE id=$1),(SELECT used_count FROM seller_discount_codes WHERE id=$2) FROM `+seq, o.ID, coupon).Scan(&attempts, &orders, &count); e != nil {
					t.Fatal(e)
				}
				want := 1
				if persistent {
					want = 0
					var pg *pq.Error
					if !errors.As(err, &pg) || pg.Code != "55P03" {
						t.Fatalf("retry failure lost SQLSTATE: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if attempts != 3 || orders != want || count != want {
					t.Fatalf("partial retry effects attempts/orders/count=%d/%d/%d", attempts, orders, count)
				}
			})
		}
	}
}

func fin007Evidence(o *domain.Order, id string, revision, amount int64) {
	v := reflect.ValueOf(o).Elem()
	for name, value := range map[string]interface{}{"CouponID": id, "CouponRevision": revision, "CouponDiscountAmount": &amount} {
		f := v.FieldByName(name)
		if f.IsValid() {
			f.Set(reflect.ValueOf(value))
		}
	}
}
func fin007Fixture(t *testing.T, max int) (*OrderRepository, opsFixture, domain.Order, string) {
	t.Helper()
	r, f, o := checkoutOrderFixture(t)
	coupon := "coupon-" + uuid.NewString()
	var limit interface{}
	if max > 0 {
		limit = max
	}
	opsExec(t, r.db, `INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at,max_uses) VALUES($1,$2,'SAVE','fixed',100,NOW()-INTERVAL '1 day',NOW()+INTERVAL '1 day',$3)`, coupon, f.store, limit)
	o.DiscountCode = "SAVE"
	o.SellerDiscountAmount = 100
	o.DiscountAmount = 100
	o.PayableAmount -= 100
	o.GatewayAmount = o.PayableAmount
	fin007Evidence(&o, coupon, 1, 100)
	return r, f, o, coupon
}
func fin007Fresh(o domain.Order) domain.Order {
	o.ID = "fin007-" + uuid.NewString()[:12]
	o.OrderNumber = o.ID
	o.TrackingCode = o.ID
	k := uuid.NewString()
	o.IdempotencyKey = &k
	return o
}
func fin007State(t *testing.T, db *sql.DB, order, state string, count int) {
	t.Helper()
	var got string
	var occupied int
	if err := db.QueryRow(`SELECT r.state,c.used_count FROM order_coupon_reservations r JOIN seller_discount_codes c ON c.id=r.coupon_id WHERE r.order_id=$1`, order).Scan(&got, &occupied); err != nil || got != state || occupied != count {
		t.Fatalf("state=%s count=%d want=%s/%d err=%v", got, occupied, state, count, err)
	}
}
func TestFIN007LastUse100(t *testing.T) {
	for _, max := range []int{1, 0} {
		t.Run(fmt.Sprint(max), func(t *testing.T) {
			r, f, template, coupon := fin007Fixture(t, max)
			r.db.SetMaxOpenConns(25)
			r.db.SetMaxIdleConns(10)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			const n = 100
			start := make(chan struct{})
			result := make(chan error, n)
			var wg sync.WaitGroup
			for i := 0; i < n; i++ {
				o := fin007Fresh(template)
				hash := strings.Repeat("a", 64)
				if _, err := r.BeginCheckoutAttempt(ctx, f.user, *o.IdempotencyKey, hash); err != nil {
					t.Fatal(err)
				}
				wg.Add(1)
				go func(o domain.Order) {
					defer wg.Done()
					<-start
					_, _, err := r.CreateCheckoutOrderWithShipments(ctx, o, nil, hash)
					result <- err
				}(o)
			}
			close(start)
			wg.Wait()
			close(result)
			accepted, exhausted := 0, 0
			for err := range result {
				if err == nil {
					accepted++
				} else if strings.Contains(err.Error(), "coupon_exhausted") {
					exhausted++
				} else {
					t.Errorf("unexpected admission error: %v", err)
				}
			}
			want := n
			if max == 1 {
				want = 1
			}
			if accepted != want || exhausted != n-want {
				t.Fatalf("accepted=%d exhausted=%d want=%d/%d", accepted, exhausted, want, n-want)
			}
			var rows, count, completed int
			if err := r.db.QueryRow(`SELECT (SELECT count(*) FROM order_coupon_reservations WHERE coupon_id=$1),(SELECT used_count FROM seller_discount_codes WHERE id=$1),(SELECT count(*) FROM checkout_attempts a JOIN orders o ON o.id=a.order_id WHERE o.store_id=$2)`, coupon, f.store).Scan(&rows, &count, &completed); err != nil || rows != want || count != want || completed != want {
				t.Fatalf("atomic admission rows=%d count=%d completed=%d err=%v", rows, count, completed, err)
			}
		})
	}
}
func TestFIN007CheckoutReplay(t *testing.T) {
	r, f, o, coupon := fin007Fixture(t, 1)
	ctx := context.Background()
	hash := strings.Repeat("a", 64)
	if _, err := r.BeginCheckoutAttempt(ctx, f.user, *o.IdempotencyKey, hash); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.CreateCheckoutOrderWithShipments(ctx, o, nil, hash); err != nil {
		t.Fatal(err)
	}
	opsExec(t, r.db, `UPDATE seller_discount_codes SET is_active=false,ends_at=NOW()-INTERVAL '1 second' WHERE id=$1`, coupon)
	for i := 0; i < 5; i++ {
		id, created, err := r.CreateCheckoutOrderWithShipments(ctx, o, nil, hash)
		if err != nil || created || id != o.ID {
			t.Fatalf("response loss replay %s %v %v", id, created, err)
		}
	}
	if _, _, err := r.CreateCheckoutOrderWithShipments(ctx, o, nil, strings.Repeat("b", 64)); err == nil {
		t.Fatal("changed fingerprint accepted")
	}
	fin007State(t, r.db, o.ID, "reserved", 1)
	fresh := fin007Fresh(o)
	fin007Evidence(&fresh, "", 0, 100)
	if err := r.CreateOrder(ctx, fresh); err == nil {
		t.Fatal("direct invalid coupon snapshot accepted")
	}
}
func TestFIN007AdmissionRevalidation(t *testing.T) {
	for _, mode := range []string{"revision", "inactive", "expired", "store", "amount", "zero"} {
		t.Run(mode, func(t *testing.T) {
			r, _, o, coupon := fin007Fixture(t, 1)
			switch mode {
			case "revision":
				opsExec(t, r.db, `UPDATE seller_discount_codes SET value=200 WHERE id=$1`, coupon)
			case "inactive":
				opsExec(t, r.db, `UPDATE seller_discount_codes SET is_active=false WHERE id=$1`, coupon)
			case "expired":
				opsExec(t, r.db, `UPDATE seller_discount_codes SET ends_at=NOW()-INTERVAL '1 second' WHERE id=$1`, coupon)
			case "store":
				o.DiscountCode = "WRONG"
			case "amount":
				fin007Evidence(&o, coupon, 1, 200)
			case "zero":
				fin007Evidence(&o, coupon, 1, 0)
			}
			err := r.CreateOrder(context.Background(), o)
			if err == nil {
				t.Fatal("stale or invalid coupon accepted")
			}
			var n int
			if e := r.db.QueryRow(`SELECT count(*) FROM orders WHERE id=$1`, o.ID).Scan(&n); e != nil || n != 0 {
				t.Fatalf("rejected admission leaked order %d %v", n, e)
			}
		})
	}
}
func TestFIN007PaymentLifecycle(t *testing.T) {
	for _, mode := range []string{"receipt", "legacy", "cancel", "expiry", "inquiry", "failed", "unknown", "late"} {
		t.Run(mode, func(t *testing.T) {
			r, f, o, _ := fin007Fixture(t, 1)
			ctx := context.Background()
			var replacement domain.Order
			if err := r.CreateOrder(ctx, o); err != nil {
				t.Fatal(err)
			}

			if mode == "expiry" {
				opsExec(t, r.db, `UPDATE orders SET payment_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, o.ID)
				if _, err := r.expireUnpaidOrder(ctx, o.ID); err != nil {
					t.Fatal(err)
				}
				fin007State(t, r.db, o.ID, "released", 0)
				return
			}
			if mode == "cancel" || mode == "late" {
				if err := r.CancelOrder(ctx, o.ID, "fixture", true); err != nil {
					t.Fatal(err)
				}
				fin007State(t, r.db, o.ID, "released", 0)
				if mode == "cancel" {
					return
				}
				replacement = fin007Fresh(o)
				if err := r.CreateOrder(ctx, replacement); err != nil {
					t.Fatalf("released capacity could not be reused: %v", err)
				}
				fin007State(t, r.db, replacement.ID, "reserved", 1)
			}
			token := "token-" + uuid.NewString()
			if err := r.CreatePaymentTransaction(ctx, PaymentTransaction{ID: uuid.NewString(), OrderID: o.ID, OrderNumber: o.OrderNumber, GatewayToken: token, Amount: o.PayableAmount}); err != nil {
				t.Fatal(err)
			}

			if mode == "failed" {
				if err := r.RecordPaymentFailure(ctx, token); err != nil {
					t.Fatal(err)
				}
				if err := r.CancelOrder(ctx, o.ID, "fixture", true); err != nil {
					t.Fatal(err)
				}
				fin007State(t, r.db, o.ID, "review_hold", 1)
				return
			}
			if mode == "legacy" {
				wh := seedCanonicalWarehouseStock(t, r.db, f.product, f.supplier, 100)
				seedCanonicalWarehouseOrderOrigins(t, r.db, o.ID, wh)
				_, err := r.MarkPaymentPaid(ctx, token, "legacy", "mask")
				if err != nil && err != ErrInsufficientInventory {
					t.Fatal(err)
				}
				fin007State(t, r.db, o.ID, "review_hold", 1)
				return
			}
			id, err := r.BeginPaymentVerification(ctx, token, "simulated")
			if err != nil {
				t.Fatal(err)
			}

			if mode == "unknown" {
				if err = r.RecordPaymentVerificationUnknown(ctx, id, "provider_unavailable"); err != nil {
					t.Fatal(err)
				}
				fin007State(t, r.db, o.ID, "review_hold", 1)
				return
			}
			if mode == "inquiry" {
				if err = r.CancelOrder(ctx, o.ID, "fixture", true); err != nil {
					t.Fatal(err)
				}
				fin007State(t, r.db, o.ID, "review_hold", 1)
				return
			}
			result := payment.VerificationResult{Success: true, RRN: "simulated-" + uuid.NewString()[:8], VerifiedAt: time.Now(), VerifiedAmount: o.PayableAmount}
			for i := 0; i < 2; i++ {
				if err = r.RecordPaymentVerification(ctx, id, result); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "late" {
				fin007State(t, r.db, o.ID, "released", 1)
				fin007State(t, r.db, replacement.ID, "reserved", 1)
				var receipts, obligations int
				var captured bool
				if err = r.db.QueryRow(`SELECT (SELECT count(*) FROM order_payment_receipts WHERE order_id=$1),(SELECT count(*) FROM order_refund_obligations WHERE order_id=$1 AND purpose='cancellation' AND amount_toman=$2),inventory_captured FROM orders WHERE id=$1`, o.ID, o.PayableAmount).Scan(&receipts, &obligations, &captured); err != nil || receipts != 1 || obligations != 1 || captured {
					t.Fatalf("late money/capacity/stock preservation %d/%d/%v: %v", receipts, obligations, captured, err)
				}
				if _, err = r.MarkPaymentPaid(ctx, token, "simulated-ref", "mask"); err != ErrPaymentExpired {
					t.Fatalf("late admission %v", err)
				}
				return
			}
			fin007State(t, r.db, o.ID, "consumed", 1)
			if err = r.CancelOrder(ctx, o.ID, "fixture", true); err != nil {
				t.Fatal(err)
			}
			fin007State(t, r.db, o.ID, "consumed", 1)
		})
	}
}
func TestFIN007LifecycleCompatibility(t *testing.T) {
	r, _, o, coupon := fin007Fixture(t, 1)
	ctx := context.Background()
	if err := r.CreateOrder(ctx, o); err != nil {
		t.Fatal(err)
	}
	if err := r.SetPaymentStatus(ctx, o.ID, "paid"); err == nil {
		t.Fatal("generic paid assertion bypassed receipt")
	}
	if err := r.UpdateOrderFulfillment(ctx, o.ID, "cancelled", ""); err == nil {
		t.Fatal("generic cancellation bypassed reservation")
	}
	if err := NewStoreRepository(r.db).IncrementDiscountCodeUse(ctx, coupon); err == nil {
		t.Fatal("independent increment accepted")
	}
	opsExec(t, r.db, `UPDATE orders SET customer_full_name='metadata' WHERE id=$1`, o.ID)
	plain := fin007Fresh(o)
	plain.DiscountCode = ""
	fin007Evidence(&plain, "", 0, 0)
	if err := r.CreateOrder(ctx, plain); err != nil {
		t.Fatal(err)
	}
	if err := r.SetPaymentStatus(ctx, plain.ID, "paid"); err != nil {
		t.Fatalf("plain setter changed: %v", err)
	}
	if err := r.UpdateOrderFulfillment(ctx, plain.ID, "cancelled", ""); err != nil {
		t.Fatalf("plain fulfillment changed: %v", err)
	}
}

func TestFIN007AdmissionFaults(t *testing.T) {
	for _, table := range []string{"orders", "order_items", "order_item_origins", "supplier_shipments", "order_coupon_reservations", "seller_discount_codes", "order_coupon_events", "checkout_attempts"} {
		t.Run(table, func(t *testing.T) {
			r, f, o, coupon := fin007Fixture(t, 1)
			ctx := context.Background()
			wh := seedCanonicalWarehouseStock(t, r.db, f.product, f.supplier, 100)
			for i := range o.Items {
				o.Items[i].Routing = "warehouse"
				o.Items[i].SupplierID = f.supplier
				o.Items[i].WarehouseID = &wh
			}
			hash := strings.Repeat("a", 64)
			if _, err := r.BeginCheckoutAttempt(ctx, f.user, *o.IdempotencyKey, hash); err != nil {
				t.Fatal(err)
			}
			fn := "fin007_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			field := "order_id"
			value := o.ID
			switch table {
			case "orders":
				field = "id"
			case "seller_discount_codes":
				field = "id"
				value = coupon
			case "checkout_attempts":
				field = "attempt_id"
				value = *o.IdempotencyKey
			}
			opsExec(t, r.db, `CREATE FUNCTION `+fn+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.`+field+`='`+value+`' THEN RAISE EXCEPTION 'FIN007 injected fault'; END IF; RETURN NEW; END $$`)
			opsExec(t, r.db, `CREATE TRIGGER `+fn+` BEFORE INSERT OR UPDATE ON `+table+` FOR EACH ROW EXECUTE FUNCTION `+fn+`()`)
			defer func() { opsExec(t, r.db, `DROP TRIGGER `+fn+` ON `+table); opsExec(t, r.db, `DROP FUNCTION `+fn+`()`) }()
			sh := []domain.SupplierShipment{{ID: uuid.NewString(), OrderID: o.ID, SupplierID: f.supplier}}
			if _, _, err := r.CreateCheckoutOrderWithShipments(ctx, o, sh, hash); err == nil || !strings.Contains(err.Error(), "injected fault") {
				t.Fatalf("fault %s not observed %v", table, err)
			}
			var rows, count, lines, shipments int
			if err := r.db.QueryRow(`SELECT (SELECT count(*) FROM orders WHERE id=$1),(SELECT used_count FROM seller_discount_codes WHERE id=$2),(SELECT count(*) FROM order_items WHERE order_id=$1),(SELECT count(*) FROM supplier_shipments WHERE order_id=$1)`, o.ID, coupon).Scan(&rows, &count, &lines, &shipments); err != nil || rows != 0 || count != 0 || lines != 0 || shipments != 0 {
				t.Fatalf("partial admission order/count/line/shipment=%d/%d/%d/%d %v", rows, count, lines, shipments, err)
			}
		})
	}
}
func TestFIN007CreationParentLockProtocol(t *testing.T) {
	r, f, o, coupon := fin007Fixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Rollback()
	if err = lockCouponCreationParentsTx(ctx, a, o, nil, false); err != nil {
		t.Fatal(err)
	}
	// A same-store creator and a distinct coupon can progress while the first
	// creator retains every compatible principal guard. No lock upgrade occurs.
	other := fin007Fresh(o)
	otherCoupon := "coupon-" + uuid.NewString()
	opsExec(t, r.db, `INSERT INTO seller_discount_codes(id,store_id,code,mode,value,starts_at,ends_at) SELECT $1,store_id,'OTHER',mode,value,starts_at,ends_at FROM seller_discount_codes WHERE id=$2`, otherCoupon, coupon)
	other.DiscountCode = "OTHER"
	fin007Evidence(&other, otherCoupon, 1, 100)
	if err = r.CreateOrder(ctx, other); err != nil {
		t.Fatalf("compatible same-store creator blocked %v", err)
	}
	// NOWAIT FOR UPDATE conflicts with KEY SHARE, proving the creation protocol
	// did not obtain or upgrade to an exclusive principal guard.
	b, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Rollback()
	if _, err = b.ExecContext(ctx, `SELECT id FROM stores WHERE id=$1 FOR UPDATE NOWAIT`, f.store); err == nil {
		t.Fatal("principal key-share absent")
	}
	b.Rollback()
	// Duplicate private identity aborts; no second slot or subordinate effects.
	if err = r.CreateOrder(ctx, other); err == nil {
		t.Fatal("duplicate private row accepted")
	}
	fin007State(t, r.db, other.ID, "reserved", 1)
}
func fin007WaitBlocked(t *testing.T, db *sql.DB, ctx context.Context, pid int) {
	t.Helper()
	for {
		var blocked bool
		if err := db.QueryRowContext(ctx, `SELECT cardinality(pg_blocking_pids($1))>0`, pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
}
func TestFIN007PaymentExpiryRaces(t *testing.T) {
	for _, first := range []string{"receipt", "expiry", "inquiry"} {
		t.Run(first, func(t *testing.T) {
			r, f, o, _ := fin007Fixture(t, 1)
			r.db.SetMaxOpenConns(5)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := r.CreateOrder(ctx, o); err != nil {
				t.Fatal(err)
			}
			token := "tok-" + uuid.NewString()
			if err := r.CreatePaymentTransaction(ctx, PaymentTransaction{ID: uuid.NewString(), OrderID: o.ID, OrderNumber: o.OrderNumber, GatewayToken: token, Amount: o.PayableAmount}); err != nil {
				t.Fatal(err)
			}
			var verification string
			var err error
			if first == "receipt" {
				verification, err = r.BeginPaymentVerification(ctx, token, "simulated")
				if err != nil {
					t.Fatal(err)
				}
			}
			if first == "expiry" {
				opsExec(t, r.db, `UPDATE orders SET payment_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, o.ID)
			}
			conn, err := r.db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			blocker, err := conn.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			// Block only the principal. The winning REAL entry point first holds the
			// order and payment, then waits here; the losing REAL entry point queues on
			// that order. Releasing this barrier deterministically orders both methods.
			if _, err = blocker.ExecContext(ctx, `SELECT id FROM stores WHERE id=$1 FOR UPDATE`, f.store); err != nil {
				t.Fatal(err)
			}
			winner, loser := make(chan error, 1), make(chan error, 1)
			go func() {
				switch first {
				case "receipt":
					winner <- r.RecordPaymentVerification(ctx, verification, payment.VerificationResult{Success: true, RRN: "race-" + uuid.NewString()[:8], VerifiedAt: time.Now(), VerifiedAmount: o.PayableAmount})
				case "inquiry":
					_, e := r.BeginPaymentVerification(ctx, token, "simulated")
					winner <- e
				case "expiry":
					_, e := r.expireUnpaidOrder(ctx, o.ID)
					winner <- e
				}
			}()
			wait := func(pattern string) {
				t.Helper()
				for {
					var n int
					if err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1`, pattern).Scan(&n); err != nil {
						t.Fatal(err)
					}
					if n > 0 {
						return
					}
					if ctx.Err() != nil {
						t.Fatal(ctx.Err())
					}
				}
			}
			wait("SELECT id FROM stores%FOR UPDATE")
			if first == "expiry" {
				go func() { _, e := r.BeginPaymentVerification(ctx, token, "simulated"); loser <- e }()
			} else {
				go func() { loser <- r.CancelOrder(ctx, o.ID, "fixture", true) }()
			}
			wait("%FROM orders%FOR UPDATE%")
			if err = blocker.Commit(); err != nil {
				t.Fatal(err)
			}
			if err = <-winner; err != nil {
				t.Fatal(err)
			}
			if err = <-loser; err != nil {
				t.Fatal(err)
			}
			state, count := "review_hold", 1
			if first == "receipt" {
				state = "consumed"
			}
			if first == "expiry" {
				state = "released"
				count = 0
			}
			fin007State(t, r.db, o.ID, state, count)
		})
	}
}

func TestFIN007LifecycleFaults(t *testing.T) {
	for _, table := range []string{"order_payment_receipts", "order_coupon_reservations", "order_coupon_events", "orders", "order_refund_obligations"} {
		t.Run(table, func(t *testing.T) {
			r, _, o, _ := fin007Fixture(t, 1)
			ctx := context.Background()
			if err := r.CreateOrder(ctx, o); err != nil {
				t.Fatal(err)
			}
			token := "tok-" + uuid.NewString()
			if err := r.CreatePaymentTransaction(ctx, PaymentTransaction{ID: uuid.NewString(), OrderID: o.ID, OrderNumber: o.OrderNumber, GatewayToken: token, Amount: o.PayableAmount}); err != nil {
				t.Fatal(err)
			}
			id, err := r.BeginPaymentVerification(ctx, token, "gateway")
			if err != nil {
				t.Fatal(err)
			}
			result := payment.VerificationResult{Success: true, RRN: "fault-" + uuid.NewString()[:8], VerifiedAt: time.Now(), VerifiedAmount: o.PayableAmount}
			cancelling := table == "orders" || table == "order_refund_obligations"
			if cancelling {
				if err = r.RecordPaymentVerification(ctx, id, result); err != nil {
					t.Fatal(err)
				}
			}
			fn := "fin007_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			field := "order_id"
			if table == "orders" {
				field = "id"
			}
			opsExec(t, r.db, `CREATE FUNCTION `+fn+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.`+field+`='`+o.ID+`' THEN RAISE EXCEPTION 'FIN007 lifecycle fault'; END IF; RETURN NEW; END $$`)
			opsExec(t, r.db, `CREATE TRIGGER `+fn+` BEFORE INSERT OR UPDATE ON `+table+` FOR EACH ROW EXECUTE FUNCTION `+fn+`()`)
			if cancelling {
				err = r.CancelOrder(ctx, o.ID, "fixture", true)
			} else {
				err = r.RecordPaymentVerification(ctx, id, result)
			}
			if err == nil || !strings.Contains(err.Error(), "lifecycle fault") {
				t.Fatalf("lifecycle fault %s unobserved %v", table, err)
			}
			opsExec(t, r.db, `DROP TRIGGER `+fn+` ON `+table)
			opsExec(t, r.db, `DROP FUNCTION `+fn+`()`)
			state := "reserved"
			receipts := 0
			if cancelling {
				state = "consumed"
				receipts = 1
			}
			fin007State(t, r.db, o.ID, state, 1)
			var n int
			if err = r.db.QueryRow(`SELECT count(*) FROM order_payment_receipts WHERE order_id=$1`, o.ID).Scan(&n); err != nil || n != receipts {
				t.Fatalf("durable receipt boundary %d %v", n, err)
			}
			if cancelling {
				err = r.CancelOrder(ctx, o.ID, "fixture", true)
			} else {
				err = r.RecordPaymentVerification(ctx, id, result)
			}
			if err != nil {
				t.Fatalf("fault recovery %v", err)
			}
			fin007State(t, r.db, o.ID, "consumed", 1)
		})
	}
}

func TestFIN007AdmissionContextCancellation(t *testing.T) {
	parent, stop := context.WithTimeout(context.Background(), 17*time.Second)
	defer stop()
	bounded, cancel := couponAdmissionContext(parent)
	defer cancel()
	want, _ := parent.Deadline()
	got, _ := bounded.Deadline()
	if !got.Equal(want) {
		t.Fatal("caller budget replaced")
	}
	fallback, finish := couponAdmissionContext(context.Background())
	defer finish()
	deadline, ok := fallback.Deadline()
	if !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) < 4*time.Second {
		t.Fatal("unbounded caller fallback absent")
	}
	r, _, o, coupon := fin007Fixture(t, 1)
	blocker, err := r.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err = blocker.Exec(`SELECT id FROM seller_discount_codes WHERE id=$1 FOR UPDATE`, coupon); err != nil {
		t.Fatal(err)
	}
	ctx, abort := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.CreateOrder(ctx, o) }()
	observe, stopObserve := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopObserve()
	for {
		var waiting bool
		if err = r.db.QueryRowContext(observe, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT id FROM seller_discount_codes%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if observe.Err() != nil {
			t.Fatal(observe.Err())
		}
	}
	abort()
	if err = <-done; err == nil {
		t.Fatal("caller cancellation admitted order")
	}
	blocker.Rollback()
	var n int
	if err = r.db.QueryRow(`SELECT count(*) FROM orders WHERE id=$1`, o.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("cancelled admission leaked %d %v", n, err)
	}
}

func TestFIN007CreationEarlyGuardCancellation(t *testing.T) {
	for _, phase := range []string{"parent", "attempt"} {
		t.Run(phase, func(t *testing.T) {
			r, f, o, _ := fin007Fixture(t, 1)
			hash := strings.Repeat("a", 64)
			if phase == "attempt" {
				if _, err := r.BeginCheckoutAttempt(context.Background(), f.user, *o.IdempotencyKey, hash); err != nil {
					t.Fatal(err)
				}
			}
			blocker, err := r.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			query := `SELECT id FROM stores WHERE id=$1 FOR UPDATE`
			id := f.store
			match := "SELECT id FROM stores%FOR KEY SHARE"
			if phase == "attempt" {
				query = `SELECT attempt_id FROM checkout_attempts WHERE attempt_id=$1 FOR UPDATE`
				id = *o.IdempotencyKey
				match = "SELECT state,%checkout_attempts%FOR UPDATE"
			}
			if _, err = blocker.Exec(query, id); err != nil {
				t.Fatal(err)
			}
			ctx, abort := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				if phase == "attempt" {
					_, _, e := r.CreateCheckoutOrderWithShipments(ctx, o, nil, hash)
					done <- e
				} else {
					done <- r.CreateOrder(ctx, o)
				}
			}()
			observe, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			for {
				var waiting bool
				if err = r.db.QueryRowContext(observe, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1)`, match).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				if observe.Err() != nil {
					t.Fatal(observe.Err())
				}
			}
			abort()
			if err = <-done; err == nil {
				t.Fatal("early guard ignored caller cancellation")
			}
			blocker.Rollback()
			var n int
			if err = r.db.QueryRow(`SELECT count(*) FROM orders WHERE id=$1`, o.ID).Scan(&n); err != nil || n != 0 {
				t.Fatalf("early guard leaked order %d %v", n, err)
			}
		})
	}
}

func TestFIN007CreationFKCatalog(t *testing.T) {
	db := operationsDB(t)
	expected := map[string]string{"orders.customer_user_id": "users", "orders.store_id": "stores", "orders.referrer_seller_id": "stores", "order_items.order_id": "orders", "order_items.product_id": "products", "order_items.variant_id": "product_variants", "order_item_origins.order_id": "orders", "order_item_origins.order_item_id": "order_items", "order_item_origins.supplier_id": "suppliers", "order_item_origins.warehouse_id": "warehouses", "order_item_origins.supplier_warehouse_id": "supplier_warehouses", "supplier_shipments.order_id": "orders", "supplier_shipments.supplier_id": "suppliers"}
	rows, err := db.Query(`SELECT c.conrelid::regclass::text||'.'||a.attname,c.confrelid::regclass::text FROM pg_constraint c JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=ANY(c.conkey) WHERE c.contype='f' AND c.conrelid IN ('orders'::regclass,'order_items'::regclass,'order_item_origins'::regclass,'supplier_shipments'::regclass)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var field, parent string
		if err = rows.Scan(&field, &parent); err != nil {
			t.Fatal(err)
		}
		if expected[field] != parent {
			t.Fatalf("undiscovered creation FK parent %s -> %s", field, parent)
		}
		seen[field] = true
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for field := range expected {
		if !seen[field] {
			t.Fatalf("fixture missing real FK %s", field)
		}
	}
}
func TestFIN007CreationOriginParentsAndDrift(t *testing.T) {
	r, f, o, coupon := fin007Fixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	vid := uuid.NewString()
	opsExec(t, r.db, `INSERT INTO product_variants(id,product_id,spnc,variant_name) VALUES($1,$2,$3,'fixture')`, vid, f.product, "FIN"+uuid.NewString()[:8])
	wh := seedCanonicalWarehouseStock(t, r.db, f.product, f.supplier, 100)
	swh := "swh-" + uuid.NewString()[:8]
	opsExec(t, r.db, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,'fixture','x','x')`, swh, f.supplier)
	for i := range o.Items {
		o.Items[i].VariantID = &vid
		o.Items[i].Routing = "warehouse"
		o.Items[i].SupplierID = f.supplier
		o.Items[i].WarehouseID = &wh
		o.Items[i].OriginSplits = []domain.OrderItemOriginSplit{{WarehouseID: &wh, SupplierWarehouseID: &swh, Quantity: o.Items[i].Quantity}}
	}
	a, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Rollback()
	if err = lockCouponCreationParentsTx(ctx, a, o, nil, false); err != nil {
		t.Fatal(err)
	}
	if err = lockCouponCreationParentsTx(ctx, a, o, nil, true); err != nil {
		t.Fatal(err)
	}
	b, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Rollback()
	// NO KEY UPDATE is compatible only with KEY SHARE among these modes. This
	// proves the creator never upgraded any existing FK parent to SHARE/UPDATE.
	for _, l := range []TransactionRowLock{{Table: "stores", ID: f.store}, {Table: "suppliers", ID: f.supplier}, {Table: "users", ID: f.user}, {Table: "products", ID: f.product}, {Table: "product_variants", ID: vid}, {Table: "warehouses", ID: wh}, {Table: "supplier_warehouses", ID: swh}} {
		if _, err = b.ExecContext(ctx, `SELECT id FROM `+l.Table+` WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, l.ID); err != nil {
			t.Fatalf("creation upgraded %s: %v", l.Table, err)
		}
	}
	b.Rollback()
	a.Rollback()
	other := opsFixtureNew(t, r.db)
	opsExec(t, r.db, `UPDATE products SET supplier_id=$2 WHERE id=$1`, f.product, other.supplier)
	if err = r.CreateOrder(ctx, o); err == nil || !strings.Contains(err.Error(), "coupon_changed") {
		t.Fatalf("new earlier supplier parent acquired late: %v", err)
	}
	var count, orders int
	if err = r.db.QueryRow(`SELECT used_count,(SELECT count(*) FROM orders WHERE id=$2) FROM seller_discount_codes WHERE id=$1`, coupon, o.ID).Scan(&count, &orders); err != nil || count != 0 || orders != 0 {
		t.Fatalf("drift rollback count=%d orders=%d err=%v", count, orders, err)
	}
}
