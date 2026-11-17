package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/worker"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFIN005AcceptedCarrierFailureSurvivesVerdictAndWorker(t *testing.T) {
	for _, source := range []string{"supplier", "warehouse"} {
		for _, ordering := range []string{"carrier_first", "verdict_first", "retained_gap", "old_source_after_verdict"} {
			if source == "warehouse" && (ordering == "retained_gap" || ordering == "old_source_after_verdict") {
				continue
			}
			t.Run(source+"/"+ordering, func(t *testing.T) {
				db, actor, order, _ := fin003HTTPPaidOrder(t)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				tracking := "FIN005-TRACK-" + uuid.NewString()[:12]
				method := "supplier_direct"
				if source == "warehouse" {
					method = "warehouse"
				}
				if _, err := db.Exec(`UPDATE orders SET status='shipped',shipped_at=NOW()-INTERVAL '21 days',fulfillment_method=$2,tracking_number=$3 WHERE id=$1`, order, method, tracking); err != nil {
					t.Fatal(err)
				}
				shipment := "FIN005-SHIP-" + uuid.NewString()[:12]
				if source == "supplier" {
					if _, err := db.Exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,status,tracking_number,shipped_at,shipping_address,customer_phone,customer_name) VALUES($1,$2,(SELECT id FROM suppliers ORDER BY id LIMIT 1),'shipped',$3,NOW()-INTERVAL '21 days','{}','fixture','fixture')`, shipment, order, tracking); err != nil {
						t.Fatal(err)
					}
				}
				finance := repository.NewFinanceRepository(db)
				orders := repository.NewOrderRepository(db)
				id := "FIN005-CLAIM-" + uuid.NewString()[:12]
				if err := finance.CreateDispute(ctx, domain.Dispute{ID: id, OrderID: order, Type: "delivery_undelivered", ProductTitle: "fixture", ClaimAmount: 100, Reason: "fixture"}); err != nil {
					t.Fatal(err)
				}
				resolve := func() {
					t.Helper()
					if _, err := finance.ResolveDisputeWithOutcome(ctx, id, repository.DisputeResolutionInput{Action: "release_to_seller", ResponsibleParty: "seller", ActorUserID: actor}); err != nil {
						t.Fatal(err)
					}
				}
				if ordering == "verdict_first" || ordering == "old_source_after_verdict" {
					resolve()
				}
				webhook := func() {
					t.Helper()
					r := httptest.NewRequest("POST", "/", strings.NewReader(`{"trackingNumber":"`+tracking+`","status":"failed"}`))
					r.Header.Set("X-Webhook-Secret", "local-fixture")
					w := httptest.NewRecorder()
					handleWebhookTrackingUpdate(w, r, ctx, repository.NewSupplierShipmentRepository(db), orders, "local-fixture")
					if w.Code != 200 {
						t.Fatalf("accepted webhook %d %s", w.Code, w.Body.String())
					}
				}
				if ordering == "retained_gap" || ordering == "old_source_after_verdict" {
					// Retained accepted old-binary source after a process/propagation gap.
					if _, err := db.Exec(`UPDATE supplier_shipments SET status='failed',updated_at=NOW() WHERE id=$1`, shipment); err != nil {
						t.Fatal(err)
					}
				} else {
					webhook()
				}
				if ordering == "carrier_first" {
					var first time.Time
					if err := db.QueryRow(`SELECT disputed_at FROM orders WHERE id=$1`, order).Scan(&first); err != nil {
						t.Fatal(err)
					}
					webhook()
					var same bool
					if err := db.QueryRow(`SELECT disputed_at=$2 FROM orders WHERE id=$1`, order, first).Scan(&same); err != nil || !same {
						t.Fatalf("pending-claim duplicate refreshed independent hold %v %v", same, err)
					}
				}
				if ordering == "carrier_first" || ordering == "retained_gap" {
					resolve()
				}
				if err := worker.NewBackgroundWorker(db).RunDeliveryJob(ctx); err != nil {
					t.Fatal(err)
				}
				var protected bool
				var firstHold sql.NullTime
				if err := db.QueryRow(`SELECT (status='delivery_disputed' OR ($2='old_source_after_verdict' AND status='shipped')) AND confirmed_at IS NULL AND return_window_ends_at IS NULL AND NOT profit_released,disputed_at FROM orders WHERE id=$1`, order, ordering).Scan(&protected, &firstHold); err != nil || !protected || (!firstHold.Valid && ordering != "old_source_after_verdict") {
					t.Fatalf("accepted %s failure erased or auto-confirmed: protected=%v err=%v", source, protected, err)
				}
				if ordering != "retained_gap" && ordering != "old_source_after_verdict" {
					webhook()
					var same bool
					if err := db.QueryRow(`SELECT disputed_at=$2 FROM orders WHERE id=$1`, order, firstHold.Time).Scan(&same); err != nil || !same {
						t.Fatalf("duplicate failure rewrote hold %v %v", same, err)
					}
					var notices int
					if err := db.QueryRow(`SELECT COUNT(*) FROM notification_outbox WHERE kind='carrier_failed' AND payload->>'order_id'=$1`, order).Scan(&notices); err != nil || notices != 1 {
						t.Fatalf("duplicate notices %d %v", notices, err)
					}
				}
				out, err := finance.GetDisputeOutcome(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(out)
				if out.SellerReleaseState == "released" || out.SellerReleaseState == "awaiting_window" {
					t.Fatalf("untruthful independent failure outcome %s", raw)
				}
			})
		}
	}
}

func TestFIN005AcceptedShipmentFailureRollsBackWithOrderHold(t *testing.T) {
	db, _, order, _ := fin003HTTPPaidOrder(t)
	ctx := context.Background()
	tracking := "ATOMIC-" + uuid.NewString()
	shipment := "ATOMIC-" + uuid.NewString()
	if _, err := db.Exec(`UPDATE orders SET status='shipped',fulfillment_method='supplier_direct' WHERE id=$1`, order); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,status,tracking_number,shipping_address,customer_phone,customer_name) VALUES($1,$2,(SELECT id FROM suppliers ORDER BY id LIMIT 1),'shipped',$3,'{}','fixture','fixture')`, shipment, order, tracking); err != nil {
		t.Fatal(err)
	}
	id := "ATOMIC-" + uuid.NewString()
	if err := repository.NewFinanceRepository(db).CreateDispute(ctx, domain.Dispute{ID: id, OrderID: order, Type: "delivery_undelivered", ProductTitle: "fixture", Reason: "fixture", ClaimAmount: 100}); err != nil {
		t.Fatal(err)
	}
	function := "fin005_atomic_carrier_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := db.Exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id=%s AND NEW.disputed_at IS DISTINCT FROM OLD.disputed_at THEN RAISE EXCEPTION 'FIN005 carrier atomic fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER %s BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION %s()`, function, pq.QuoteLiteral(order), function, function)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(fmt.Sprintf(`DROP TRIGGER %s ON orders; DROP FUNCTION %s()`, function, function)); err != nil {
			t.Error(err)
		}
	})
	if _, err := repository.NewSupplierShipmentRepository(db).UpdateShipmentStatus(ctx, tracking, "failed"); err == nil || !strings.Contains(err.Error(), "FIN005 carrier atomic fault") {
		t.Fatalf("accepted source escaped order-hold transaction %v", err)
	}
	var clean bool
	if err := db.QueryRow(`SELECT s.status='shipped' AND o.status='delivery_disputed' AND o.disputed_at=e.hold_at AND NOT o.profit_released AND NOT EXISTS(SELECT 1 FROM notification_outbox WHERE kind='carrier_failed' AND payload->>'order_id'=o.id) FROM supplier_shipments s JOIN orders o ON o.id=s.order_id JOIN dispute_order_evidence e ON e.order_id=o.id WHERE s.id=$1`, shipment).Scan(&clean); err != nil || !clean {
		t.Fatalf("partial accepted failure or notification committed %v %v", clean, err)
	}
}

func TestFIN005RetainedCarrierFailuresCannotStarveDeliveryWorker(t *testing.T) {
	for _, stage := range []string{"auto_confirm", "release"} {
		t.Run(stage, func(t *testing.T) {
			db := opsHTTPDB(t)
			var bad []string
			seed := func(failed bool) string {
				t.Helper()
				id := "cf-" + uuid.NewString()[:20]
				user := uuid.NewString()
				if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name) VALUES($1,$2,'fixture')`, user, "c"+user[:12]); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO orders(id,order_number,store_id,store_name,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payable_amount,total_amount,payment_status,status,shipped_at,dropshipper_profit) VALUES($1,$1,(SELECT id FROM stores ORDER BY id LIMIT 1),'fixture','fixture','fixture','fixture','fixture','fixture','fixture',$1,$2,100,100,'paid','shipped',NOW()-INTERVAL '30 days',7000)`, id, user); err != nil {
					t.Fatal(err)
				}
				if stage == "release" {
					if _, err := db.Exec(`UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 day' WHERE id=$1`, id); err != nil {
						t.Fatal(err)
					}
				}
				if failed {
					if _, err := db.Exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,status,shipping_address,customer_phone,customer_name) VALUES($1,$1,(SELECT id FROM suppliers ORDER BY id LIMIT 1),'failed','{}','fixture','fixture')`, id); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := db.Exec(`UPDATE orders SET shipped_at=NOW()-INTERVAL '21 days',return_window_ends_at=CASE WHEN status='delivered_confirmed' THEN NOW()-INTERVAL '1 hour' ELSE NULL END WHERE id=$1`, id); err != nil {
						t.Fatal(err)
					}
				}
				return id
			}
			for i := 0; i < 100; i++ {
				bad = append(bad, seed(true))
			}
			eligible := seed(false)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			w := worker.NewBackgroundWorker(db)
			for i := 0; i < 2; i++ {
				if err := w.RunDeliveryJob(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var progressed bool
			if err := db.QueryRow(`SELECT status='delivered_confirmed' AND confirmed_at IS NOT NULL AND return_window_ends_at IS NOT NULL AND profit_released=$2 FROM orders WHERE id=$1`, eligible, stage == "release").Scan(&progressed); err != nil || !progressed {
				t.Fatalf("100 retained failures starved %s eligible order %v %v", stage, progressed, err)
			}
			var clean bool
			if err := db.QueryRow(`SELECT COUNT(*)=100 AND BOOL_AND(NOT profit_released AND ($2<>'auto_confirm' OR (status='shipped' AND confirmed_at IS NULL AND return_window_ends_at IS NULL))) FROM orders WHERE id=ANY($1)`, pq.Array(bad), stage).Scan(&clean); err != nil || !clean {
				t.Fatalf("retained failed sources gained confirmation/window/money %v %v", clean, err)
			}
		})
	}
}
