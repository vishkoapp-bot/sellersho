package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestQual01TrackingDeliveryDoesNotReleaseMoney(t *testing.T) {
	for _, method := range []string{"supplier_direct", "warehouse"} {
		t.Run(method, func(t *testing.T) {
			db, _, order, payment := fin003HTTPPaidOrder(t)
			tracking := "qual01-" + uuid.NewString()
			if _, err := db.Exec(`UPDATE orders SET status='shipped',shipped_at=now(),fulfillment_method=$2,tracking_number=$3,dropshipper_profit=7000,platform_profit=1000 WHERE id=$1`, order, method, tracking); err != nil {
				t.Fatal(err)
			}
			var shipment string
			if method == "supplier_direct" {
				shipment = "qual01-" + uuid.NewString()
				if _, err := db.Exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,status,tracking_number,shipped_at,shipping_address,customer_phone,customer_name) VALUES($1,$2,(SELECT id FROM suppliers ORDER BY id LIMIT 1),'shipped',$3,now(),'{}','fixture','fixture')`, shipment, order, tracking); err != nil {
					t.Fatal(err)
				}
			}
			orders := repository.NewOrderRepository(db)
			shipments := repository.NewSupplierShipmentRepository(db)
			secret := uuid.NewString()
			for i := 0; i < 2; i++ {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/tracking/update", strings.NewReader(fmt.Sprintf(`{"trackingNumber":%q,"status":"delivered"}`, tracking)))
				r.Header.Set("X-Webhook-Secret", secret)
				w := httptest.NewRecorder()
				handleWebhookTrackingUpdate(w, r, r.Context(), shipments, orders, secret)
				if w.Code != http.StatusOK {
					t.Fatalf("delivery attempt %d: %d %s", i+1, w.Code, w.Body)
				}
				var preserved bool
				if err := db.QueryRow(`SELECT o.status='delivered' AND o.confirmed_at IS NULL AND o.return_window_ends_at IS NULL AND NOT o.profit_released
					AND o.dropshipper_profit=7000 AND o.platform_profit=1000 AND o.payment_status='paid' AND p.status='paid'
					AND NOT EXISTS(SELECT 1 FROM ledger_entries WHERE related_order_or_dispute=o.id)
					FROM orders o JOIN payment_transactions p ON p.order_id=o.id WHERE o.id=$1 AND p.id=$2`, order, payment).Scan(&preserved); err != nil || !preserved {
					t.Fatalf("delivery attempt %d changed protected funds or did not deliver: preserved=%v err=%v", i+1, preserved, err)
				}
			}
			if shipment != "" {
				var delivered bool
				if err := db.QueryRow(`SELECT status='delivered' AND delivered_at IS NOT NULL FROM supplier_shipments WHERE id=$1`, shipment).Scan(&delivered); err != nil || !delivered {
					t.Fatalf("supplier shipment was not delivered: %v %v", delivered, err)
				}
			}
		})
	}
}
