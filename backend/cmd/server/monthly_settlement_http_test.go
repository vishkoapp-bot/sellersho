package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drup/platform/internal/repository"
)

func TestFIN002ManualRunResumesDurableIncompleteCycle(t *testing.T) {
	db := opsHTTPDB(t)
	for _, q := range []string{
		`UPDATE platform_settings SET settlement_day=10`,
		`INSERT INTO monthly_settlement_cycles(period,settlement_day,status,progress_initialized) VALUES('2026-09',10,'running',true)`,
		`INSERT INTO stores(id,name,slug,owner_name,owner_type) SELECT 'fin002-'||n,'synthetic','fin002-'||n,'synthetic','dropshipper' FROM generate_series(1,31) n`,
		`INSERT INTO monthly_settlement_progress(cycle_period,store_id) SELECT '2026-09',id FROM stores WHERE id LIKE 'fin002-%'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	call := func() *httptest.ResponseRecorder {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		r := httptest.NewRequest("POST", "/api/v1/finance/settlements/monthly/run", strings.NewReader(`{"period":"2026-09"}`)).WithContext(ctx)
		w := httptest.NewRecorder()
		handleT01MonthlyRun(w, r, db)
		return w
	}
	first := call()
	if first.Code != 202 {
		t.Fatalf("incomplete cycle refused instead of resumed: %d %s", first.Code, first.Body.String())
	}
	var progress struct {
		Status                                          string
		CompletedStores, PendingStores, CandidateStores int
	}
	if err := json.Unmarshal(first.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Status != "running" || progress.CandidateStores != 31 || progress.CompletedStores > 25 || progress.PendingStores == 0 {
		t.Fatalf("partial batch falsely completed: %+v", progress)
	}
	second := call()
	if second.Code != 200 {
		t.Fatalf("remaining batch failed: %d %s", second.Code, second.Body.String())
	}
	if err := json.Unmarshal(second.Body.Bytes(), &progress); err != nil || progress.Status != "completed" || progress.CompletedStores != 31 || progress.PendingStores != 0 {
		t.Fatalf("completed progress invalid %+v %v", progress, err)
	}
	if third := call(); third.Code != 409 {
		t.Fatalf("sealed cycle rearmed: %d", third.Code)
	}
}

func TestFIN002ManualWorkerAndPayoutShareReservationAuthority(t *testing.T) {
	db := opsHTTPDB(t)
	// Synthetic accepted released-profit snapshot, only in the guarded clone.
	// This verifies reservation authority, not inventory/payment capacity.
	for _, q := range []string{
		`UPDATE platform_settings SET settlement_day=10`,
		`INSERT INTO stores(id,name,slug,owner_name,owner_type,owner_user_id) VALUES('fin002-money','synthetic','fin002-money','synthetic','dropshipper',(SELECT id FROM users ORDER BY id LIMIT 1))`,
		`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,status,payment_status,dropshipper_profit,profit_released,profit_released_at)
 VALUES('fin002-profit','FIN002-PROFIT','fin002-money','synthetic',2000000,2000000,'synthetic','09120000000','synthetic','synthetic','synthetic','1234567890','FIN002-TRACK','delivered_confirmed','paid',2000000,true,NOW())`,
		`INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status,available_balance) VALUES('fin002-bank','fin002-money','seller','synthetic','synthetic-bank','synthetic-bank',1,'rejected',2000000)`,
		`INSERT INTO monthly_settlement_cycles(period,settlement_day,status,progress_initialized) VALUES('2026-11',10,'running',true)`,
		`INSERT INTO monthly_settlement_progress(cycle_period,store_id) VALUES('2026-11','fin002-money')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := repository.NewFinanceRepository(db)
	before, err := repo.GetStoreBalance(ctx, "fin002-money")
	if err != nil || before.AvailableAmount != 2000000 {
		t.Fatalf("invalid accepted fixture: %+v %v", before, err)
	}
	var ledgerBefore int
	if err := db.QueryRow(`SELECT count(*) FROM ledger_entries`).Scan(&ledgerBefore); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			switch i {
			case 0:
				request := httptest.NewRequest("POST", "/api/v1/finance/settlements/monthly/run", strings.NewReader(`{"period":"2026-11"}`)).WithContext(ctx)
				w := httptest.NewRecorder()
				handleT01MonthlyRun(w, request, db)
				if w.Code != 200 && w.Code != 202 && w.Code != 409 {
					t.Errorf("manual race %d %s", w.Code, w.Body.String())
				}
			case 1:
				if _, err := repo.RunMonthlySettlement(ctx, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), "worker"); err != nil {
					t.Error(err)
				}
			case 2:
				_, err := repo.RequestStorePayout(ctx, "fin002-money", "bank", "bank", 2000000)
				if err != nil && !errors.Is(err, repository.ErrStoreInsufficientBalance) {
					t.Error(err)
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if _, err := db.Exec(`UPDATE monthly_settlement_progress SET next_attempt_at=NOW() WHERE state<>'done'`); err != nil {
		t.Fatal(err)
	}
	cycle, err := repo.RunMonthlySettlementForPeriod(ctx, "2026-11", "resume")
	if err != nil || cycle.Status != "completed" {
		t.Fatalf("unfinished manual/worker batch %+v %v", cycle, err)
	}
	var reserved int64
	var ledgerAfter int
	if err := db.QueryRow(`SELECT COALESCE(sum(amount),0) FROM payout_requests WHERE store_id='fin002-money' AND status='pending'`).Scan(&reserved); err != nil || reserved != 2000000 {
		t.Fatalf("manual+worker+payout reserved %d from2m: %v", reserved, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM ledger_entries`).Scan(&ledgerAfter); err != nil || ledgerAfter != ledgerBefore {
		t.Fatalf("reservation invented posted money: %d ->%d %v", ledgerBefore, ledgerAfter, err)
	}
}
