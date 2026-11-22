package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// TestT01AdminOverviewTrendsDateScan (Batch 4a, D5): the trends case used to
// SELECT TO_CHAR(d::date, 'YYYY-MM-DD') AS day and scan the text value into
// a time.Time, so every row errored and the handler returned HTTP 500. The
// fix selects d::date so lib/pq can scan into time.Time. Seed 2 paid
// non-cancelled orders on distinct days + 1 cancelled order on one of those
// days: the endpoint must return 200, 30 points, non-zero gmv on exactly the
// right days, and the cancelled order must contribute nothing.
func TestT01AdminOverviewTrendsDateScan(t *testing.T) {
	db := supplierFlowDB(t)
	// customer_user_id is a FK to users: use a real seeded user id, not a
	// random UUID (a random UUID fails the FK check).
	f := supplierFlowSeed(t, db)
	ids := []string{}
	mkOrder := func(amount int64, daysAgo int, status string) {
		t.Helper()
		id := "t01-trends-" + uuid.NewString()[:8]
		ids = append(ids, id)
		if _, err := db.Exec(`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status,status,fulfillment_method,created_at) VALUES($1,$1,'store-central','Audit seller',$2,$2,'Audit buyer','09123456789','Tehran','Tehran','Audit address','1234512345',$1,$3,'paid',$4,'supplier_direct', CURRENT_DATE - ($5 * INTERVAL '1 day'))`, id, amount, f.user, status, daysAgo); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, id := range ids {
			db.Exec(`DELETE FROM orders WHERE id=$1`, id)
		}
	})
	mkOrder(100000, 2, "processing")
	mkOrder(250000, 1, "processing")
	mkOrder(999999, 2, "cancelled")

	w := httptest.NewRecorder()
	handleT01AdminOverviewSub(w, httptest.NewRequest("GET", "/api/v1/admin/overview/trends?days=30", nil), db, "trends")
	if w.Code != 200 {
		t.Fatalf("trends status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Data []struct {
			GMV    int64 `json:"gmv"`
			Orders int64 `json:"orders"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 30 {
		t.Fatalf("expected 30 points, got %d", len(body.Data))
	}
	// Series is oldest first: CURRENT_DATE-29 .. CURRENT_DATE, so the order
	// 2 days ago is index 27 and the order 1 day ago is index 28.
	for i, p := range body.Data {
		switch i {
		case 27:
			if p.GMV != 100000 || p.Orders != 1 {
				t.Fatalf("day-2 point = %+v, want {gmv 100000 orders 1}", p)
			}
		case 28:
			if p.GMV != 250000 || p.Orders != 1 {
				t.Fatalf("day-1 point = %+v, want {gmv 250000 orders 1}", p)
			}
		case 29:
			// Today: other tests in a full-package run may seed today's
			// orders against the same disposable DB; not asserted here.
		default:
			if p.GMV != 0 || p.Orders != 0 {
				t.Fatalf("point %d = %+v, want zero (cancelled order must contribute nothing and no other day has sales)", i, p)
			}
		}
	}
}
