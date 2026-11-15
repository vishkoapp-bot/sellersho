package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/repository"
)

// Compiled only by test_monthly_legacy_compat.py into the actual previous
// committed source. It deliberately references no new repository interface.
func TestFIN002LegacyWritersPreserveIncompleteCycles(t *testing.T) {
	db := opsHTTPDB(t)
	for _, q := range []string{
		`UPDATE platform_settings SET settlement_day=10`,
		`INSERT INTO stores(id,name,slug,owner_name,owner_type) VALUES('fin002-legacy-done','synthetic','fin002-legacy-done','synthetic','dropshipper'),('fin002-legacy-pending','synthetic','fin002-legacy-pending','synthetic','dropshipper')`,
		`INSERT INTO monthly_settlement_cycles(period,settlement_day,status,total_stores,total_gross,total_net,progress_initialized) VALUES('2026-11',10,'running',1,2000000,2000000,true)`,
		`INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status,available_balance) VALUES('fin002-legacy-payout','fin002-legacy-done','seller','synthetic','synthetic','synthetic',2000000,'processing',2000000)`,
		`INSERT INTO monthly_settlement_items(id,cycle_period,store_id,gross_amount,fee_amount,net_amount,payout_request_id,status) VALUES('fin002-legacy-item','2026-11','fin002-legacy-done',2000000,0,2000000,'fin002-legacy-payout','pending_payout')`,
		`INSERT INTO monthly_settlement_progress(cycle_period,store_id,state,reason) VALUES('2026-11','fin002-legacy-done','done','reserved'),('2026-11','fin002-legacy-pending','pending','')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() string {
		var out string
		if err := db.QueryRow(`SELECT json_build_object('cycles',(SELECT json_agg(c ORDER BY period) FROM monthly_settlement_cycles c),
 'work',(SELECT json_agg(w ORDER BY cycle_period,store_id) FROM monthly_settlement_progress w),
 'items',(SELECT json_agg(i ORDER BY id) FROM monthly_settlement_items i),
 'payouts',(SELECT json_agg(p ORDER BY id) FROM payout_requests p))::text`).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, state := range []string{"running", "blocked"} {
		if _, err := db.Exec(`UPDATE monthly_settlement_cycles SET status=$1 WHERE period='2026-11'`, state); err != nil {
			t.Fatal(err)
		}
		before := snapshot()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		cycle, err := repository.NewFinanceRepository(db).RunMonthlySettlement(ctx, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), "legacy-worker")
		if err != nil || cycle == nil || cycle.Status != state {
			cancel()
			t.Fatalf("legacy scheduled writer rejected additive schema: %+v %v", cycle, err)
		}
		request := httptest.NewRequest("POST", "/api/v1/finance/settlements/monthly/run", strings.NewReader(`{"period":"2026-11"}`)).WithContext(ctx)
		response := httptest.NewRecorder()
		handleT01MonthlyRun(response, request, db)
		cancel()
		if response.Code != 409 {
			t.Fatalf("legacy manual writer rearmed incomplete period: %d %s", response.Code, response.Body.String())
		}
		if snapshot() != before {
			t.Fatal("legacy writer changed unfinished progress or money")
		}
	}
}
