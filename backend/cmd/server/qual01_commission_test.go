package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestQual01CommissionUTCWindow(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	// Keep the session's timezone different from the wire calendar.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`SET TIME ZONE 'Asia/Tehran'`); err != nil {
		t.Fatal(err)
	}
	// Use a historical window, separate from migration seed orders.
	// The range starts Nov 17 00:00 UTC and ends Nov 20 00:00 UTC.
	now := time.Date(2023, 11, 19, 18, 0, 0, 0, time.UTC)
	seed := func(created time.Time, profit int64, released any, payment, status string) {
		t.Helper()
		id := uuid.NewString()
		short := id[:18]
		_, err := db.Exec(`INSERT INTO orders
			(id,order_number,store_name,total_amount,payable_amount,customer_full_name,
			 customer_phone,customer_province,customer_city,customer_address,customer_postal_code,
			 tracking_code,platform_profit,profit_released,payment_status,status,created_at)
			VALUES($1,$2,'Fixture',1000,1000,'Fixture','09000000000','Fixture','Fixture','Fixture','0000000000',
			       $2,$3,$4,$5,$6,$7)`, id, short, profit, released, payment, status, created)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := db.Exec(`DELETE FROM orders WHERE id=$1`, id); err != nil {
				t.Error(err)
			}
		})
	}
	start := time.Date(2023, 11, 17, 0, 0, 0, 0, time.UTC)
	seed(start.Add(-time.Second), 400, true, "paid", "processing")
	seed(start, 17, true, "paid", "processing")
	seed(now.Add(-19*time.Hour), 19, nil, "paid", "processing")
	seed(now, 200, true, "paid", "cancelled")
	seed(now, 300, true, "pending", "pending")
	seed(start.AddDate(0, 0, 3), 500, true, "paid", "processing")
	type bucket struct {
		Date       string
		Commission int64
	}
	var got struct {
		Success                               bool
		Days                                  int
		Total, ReleasedTotal, UnreleasedTotal int64
		OrderCount                            int64
		Trend                                 []bucket
	}
	invoke := func(at time.Time) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/finance/treasury/commission?days=3", nil)
		res := httptest.NewRecorder()
		handleT01TreasuryCommission(res, req, db, at)
		return res
	}
	res := invoke(now)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Success || got.Days != 3 || got.Total != 36 || got.ReleasedTotal != 17 || got.UnreleasedTotal != 19 || got.OrderCount != 2 {
		t.Fatalf("wrong commission totals: %+v", got)
	}
	want := []bucket{{"2023-11-17", 17}, {"2023-11-18", 19}, {"2023-11-19", 0}}
	if !slices.Equal(got.Trend, want) {
		t.Fatalf("trend=%v want %v", got.Trend, want)
	}
	if res := invoke(now.AddDate(-1, 0, 0)); res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &got) != nil || got.Total != 0 || got.OrderCount != 0 {
		t.Fatalf("empty window should report zero: status=%d body=%s", res.Code, res.Body.String())
	}
	if _, err := db.Exec(`SET search_path TO pg_catalog`); err != nil {
		t.Fatal(err)
	}
	res = invoke(now)
	if _, err := db.Exec(`RESET search_path`); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("failed report read returned %d", res.Code)
	}
}
