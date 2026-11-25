package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestSellerFinanceCommandReplay(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	owner := uuid.NewString()
	storeID := "retry-" + uuid.NewString()
	otherID := "retry-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role) VALUES($1,$2,'Finance retry','seller')`, owner, "f"+owner[:12]); err != nil {
		t.Fatal(err)
	}
	stores := repository.NewStoreRepository(db)
	fin := repository.NewFinanceRepository(db)
	for _, id := range []string{storeID, otherID} {
		if err := stores.CreateStore(ctx, domain.Store{ID: id, Name: "Finance retry", Slug: id, OwnerType: "dropshipper", OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8]}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO referral_bonuses(id,referrer_store_id,referred_store_id,amount,status,released_at) VALUES($1,$2,$3,20000000,'released',NOW())`, uuid.NewString(), storeID, otherID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE platform_settings SET early_withdrawal_fee_pct=10 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	bban := "0170000000000000000000"
	n, _ := new(big.Int).SetString(bban+"182700", 10)
	check := 98 - new(big.Int).Mod(n, big.NewInt(97)).Int64()
	iban := fmt.Sprintf("IR%02d%s", check, bban)
	post := func(action, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/stores/"+storeID+"/"+action, strings.NewReader(body))
		r.Header.Set("Idempotency-Key", key)
		r = r.WithContext(context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: owner, Role: "seller"}))
		w := httptest.NewRecorder()
		handleSellerStore(w, r, stores, fin, storeID, action, "")
		return w
	}
	for _, tc := range []struct{ action, body, changed, table string }{
		{"services", `{"service":"standard"}`, `{"service":"vip"}`, "seller_service_purchases"},
		{"payouts", fmt.Sprintf(`{"amount":2000000,"shaba":"%s"}`, iban), fmt.Sprintf(`{"amount":3000000,"shaba":"%s"}`, iban), "payout_requests"},
		{"early-withdrawals", fmt.Sprintf(`{"amount":2000000,"shaba":"%s"}`, iban), fmt.Sprintf(`{"amount":3000000,"shaba":"%s"}`, iban), "seller_early_withdrawals"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			for _, key := range []string{"", strings.Repeat("x", 129)} {
				if w := post(tc.action, key, tc.body); w.Code != 400 {
					t.Fatalf("invalid request key accepted status%d", w.Code)
				}
			}
			var before int
			if err := db.QueryRow(`SELECT COUNT(*) FROM `+tc.table+` WHERE store_id=$1`, storeID).Scan(&before); err != nil || before != 0 {
				t.Fatalf("invalid-key effects%d err%v", before, err)
			}
			first := post(tc.action, "one-"+tc.action, tc.body)
			if first.Code != 200 && first.Code != 201 {
				t.Fatalf("first %d %s", first.Code, first.Body.String())
			}
			if tc.action == "services" {
				if _, err := db.Exec(`UPDATE stores SET active_plan_id='vip',plan_expires_at=NOW()+INTERVAL '900 days' WHERE id=$1`, storeID); err != nil {
					t.Fatal(err)
				}
			}
			second := post(tc.action, "one-"+tc.action, tc.body)
			if second.Code != first.Code {
				t.Fatalf("replay %d %s", second.Code, second.Body.String())
			}
			if tc.action == "services" && first.Body.String() != second.Body.String() {
				t.Fatalf("service recovery changed original acknowledgment: %s / %s", first.Body.String(), second.Body.String())
			}
			if tc.action == "services" {
				statement, err := fin.GetStoreStatement(ctx, storeID, time.Now().UTC().Format("2006-01"))
				if err != nil {
					t.Fatal(err)
				}
				if statement.TotalDebit != 890000 || statement.TotalCredit != 20000000 || statement.ClosingAvailable != 19110000 {
					t.Fatalf("migrated statement %+v", statement)
				}
			}

			if tc.action != "services" {
				var a, b map[string]interface{}
				_ = json.Unmarshal(first.Body.Bytes(), &a)
				_ = json.Unmarshal(second.Body.Bytes(), &b)
				if a["id"] != b["id"] {
					t.Fatalf("replay created %v and %v", a["id"], b["id"])
				}
			}
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM `+tc.table+` WHERE store_id=$1`, storeID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("effects=%d err=%v", count, err)
			}
			if w := post(tc.action, "one-"+tc.action, tc.changed); w.Code != 409 {
				t.Fatalf("changed payload status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}

	// Fault before commit must leave neither the reservation nor its request
	// acknowledgement behind; retrying the same key then commits exactly once.
	if _, err := db.Exec(`CREATE FUNCTION finance_test_record_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.client_key='before-commit' THEN RAISE EXCEPTION 'simulated durable record failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER finance_test_record_failure BEFORE INSERT ON finance_command_requests FOR EACH ROW EXECUTE FUNCTION finance_test_record_failure()`); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"amount":2000000,"shaba":"%s"}`, iban)
	if w := post("payouts", "before-commit", body); w.Code != 500 {
		t.Fatalf("fault response%d", w.Code)
	}
	var effects int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payout_requests WHERE store_id=$1`, storeID).Scan(&effects); err != nil || effects != 1 {
		t.Fatalf("failed transaction effects=%d err=%v", effects, err)
	}
	if _, err := db.Exec(`DROP TRIGGER finance_test_record_failure ON finance_command_requests; DROP FUNCTION finance_test_record_failure()`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if w := post("payouts", "before-commit", body); w.Code != 201 {
			t.Fatalf("retry%d status%d body%s", i, w.Code, w.Body.String())
		}
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM payout_requests WHERE store_id=$1`, storeID).Scan(&effects); err != nil || effects != 2 {
		t.Fatalf("retried effects=%d err=%v", effects, err)
	}
}

func TestSupplierFinanceCommandReplay(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	supplier := "retry-supplier-" + uuid.NewString()[:12]
	if _, err := db.Exec(`INSERT INTO suppliers(id,name,warehouse_city,approval_status) VALUES($1,'Finance retry','Tehran','approved')`, supplier); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator) VALUES($1,NOW(),'supplier_payable:' || $2,'supplier','credit',10000000,'سند تعدیل retry fixture','test')`, uuid.NewString(), supplier); err != nil {
		t.Fatal(err)
	}
	bban := "0170000000000000000000"
	n, _ := new(big.Int).SetString(bban+"182700", 10)
	check := 98 - new(big.Int).Mod(n, big.NewInt(97)).Int64()
	iban := fmt.Sprintf("IR%02d%s", check, bban)
	handler := NewSupplierHandlers(repository.NewSupplierRepository(db), nil, false)
	postKey := func(amount int64, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/supplier/settlement/request", strings.NewReader(fmt.Sprintf(`{"amount":%d,"shaba":"%s","account_holder":"Fixture holder"}`, amount, iban)))
		r.Header.Set("Idempotency-Key", key)
		r = r.WithContext(context.WithValue(context.Background(), supplierIDKey, supplier))
		w := httptest.NewRecorder()
		handler.HandleRequestSettlement(w, r)
		return w
	}
	for _, key := range []string{"", strings.Repeat("x", 129)} {
		if w := postKey(2000000, key); w.Code != 400 {
			t.Fatalf("invalid supplier request key accepted%d", w.Code)
		}
	}
	var before int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payout_requests WHERE supplier_id=$1`, supplier).Scan(&before); err != nil || before != 0 {
		t.Fatalf("invalid-key effects%d err%v", before, err)
	}
	post := func(amount int64) *httptest.ResponseRecorder { return postKey(amount, "supplier-retry-key") }
	first, second := post(2000000), post(2000000)
	if first.Code != 201 || second.Code != 201 {
		t.Fatalf("responses%d/%d", first.Code, second.Code)
	}
	var a, b map[string]interface{}
	_ = json.Unmarshal(first.Body.Bytes(), &a)
	_ = json.Unmarshal(second.Body.Bytes(), &b)
	if a["id"] != b["id"] {
		t.Fatalf("replay created%v/%v", a["id"], b["id"])
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payout_requests WHERE supplier_id=$1`, supplier).Scan(&count); err != nil || count != 1 {
		t.Fatalf("effects%d err%v", count, err)
	}
	if w := post(3000000); w.Code != 409 {
		t.Fatalf("changed payload%d", w.Code)
	}
	if _, err := db.Exec(`UPDATE suppliers SET approval_status='suspended' WHERE id=$1`, supplier); err != nil {
		t.Fatal(err)
	}
	if w := post(2000000); w.Code != 201 {
		t.Fatalf("committed replay while suspended%d", w.Code)
	}
}

func TestSellerEarlyWithdrawalFloorFeesAndReplay(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	owner := uuid.NewString()
	storeID := "early-floor-" + uuid.NewString()
	otherID := "early-floor-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role) VALUES($1,$2,'Early withdrawal fixture','seller')`, owner, "f"+owner[:12]); err != nil {
		t.Fatal(err)
	}
	stores := repository.NewStoreRepository(db)
	fin := repository.NewFinanceRepository(db)
	for _, id := range []string{storeID, otherID} {
		if err := stores.CreateStore(ctx, domain.Store{ID: id, Name: "Early withdrawal fixture", Slug: id, OwnerType: "dropshipper", OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8]}); err != nil {
			t.Fatal(err)
		}
	}
	const funded int64 = 3000000
	if _, err := db.Exec(`INSERT INTO referral_bonuses(id,referrer_store_id,referred_store_id,amount,status,released_at) VALUES($1,$2,$3,$4,'released',NOW())`, uuid.NewString(), storeID, otherID, funded); err != nil {
		t.Fatal(err)
	}
	settings, err := fin.GetSettlementSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	setFloor := func(floor int64) {
		t.Helper()
		if _, err := db.Exec(`UPDATE platform_settings SET early_withdrawal_minimum_toman=$1 WHERE id=1`, floor); err != nil {
			t.Fatal(err)
		}
	}
	if settings.EarlyWithdrawalMinimumToman == nil || *settings.EarlyWithdrawalMinimumToman != 1000000 {
		t.Fatal("expected the migration's default withdrawal floor")
	}
	t.Cleanup(func() { setFloor(*settings.EarlyWithdrawalMinimumToman) })
	setFee := func(fee any) {
		t.Helper()
		if _, err := db.Exec(`UPDATE platform_settings SET early_withdrawal_fee_pct=$1 WHERE id=1`, fee); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { setFee(settings.EarlyWithdrawalFeePct) })
	setFee(nil)

	const iban = "IR060170000000000000000000"
	post := func(key string, amount int64, shaba string, status int, message string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1/stores/"+storeID+"/early-withdrawals", strings.NewReader(fmt.Sprintf(`{"amount":%d,"shaba":"%s"}`, amount, shaba)))
		r.Header.Set("Idempotency-Key", key)
		r = r.WithContext(context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: owner, Role: "seller"}))
		w := httptest.NewRecorder()
		handleSellerStore(w, r, stores, fin, storeID, "early-withdrawals", "")
		if w.Code != status || !strings.Contains(w.Body.String(), message) {
			t.Fatalf("%s status=%d body=%s, want %d with %q", key, w.Code, w.Body.String(), status, message)
		}
		return w
	}
	assertHolds := func(wantCount int, wantGross int64) {
		t.Helper()
		rows, err := fin.ListEarlyWithdrawals(ctx, storeID)
		if err != nil {
			t.Fatal(err)
		}
		balance, err := fin.GetStoreBalance(ctx, storeID)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != wantCount || balance.EarlyWithdrawalPendingGross != wantGross || balance.AvailableAmount != funded-wantGross {
			t.Fatalf("count=%d pending=%d available=%d, want %d, %d, %d", len(rows), balance.EarlyWithdrawalPendingGross, balance.AvailableAmount, wantCount, wantGross, funded-wantGross)
		}
	}
	readWithdrawal := func(w *httptest.ResponseRecorder) repository.EarlyWithdrawal {
		t.Helper()
		var row repository.EarlyWithdrawal
		if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if row.ID == "" || row.StoreID != storeID || row.Status != "pending" {
			t.Fatalf("invalid withdrawal acknowledgment: id=%q store=%q status=%q", row.ID, row.StoreID, row.Status)
		}
		return row
	}

	post("below-floor", 999999, iban, 400, "حداقل مبلغ برداشت زودهنگام ۱٬۰۰۰٬۰۰۰ تومان است.")
	post("over-balance", funded+1, iban, 400, "موجودی کیف پول برای این درخواست کافی نیست.")
	post("invalid-iban", 1000000, "IR000000000000000000000000", 400, "Invalid IBAN")
	post("unset-fee", 1000000, iban, 400, "کارمزد برداشت زودهنگام هنوز توسط مدیر تنظیم نشده است.")
	assertHolds(0, 0)

	setFee(0)
	first := readWithdrawal(post("at-floor", 1000000, iban, 201, ""))
	if first.GrossAmount != 1000000 || first.FeePct != 0 || first.FeeAmount != 0 || first.NetAmount != 1000000 {
		t.Fatalf("zero fee: gross=%d pct=%g fee=%d net=%d", first.GrossAmount, first.FeePct, first.FeeAmount, first.NetAmount)
	}
	assertHolds(1, 1000000)

	setFee(5.25)
	setFloor(2000000)
	post("below-raised-floor", 1500000, iban, 400, "حداقل مبلغ برداشت زودهنگام ۲٬۰۰۰٬۰۰۰ تومان است.")
	assertHolds(1, 1000000)
	replay := readWithdrawal(post("at-floor", 1000000, iban, 201, ""))
	if replay.ID != first.ID || replay.GrossAmount != first.GrossAmount || replay.FeePct != first.FeePct || replay.FeeAmount != first.FeeAmount || replay.NetAmount != first.NetAmount {
		t.Fatal("same-key replay changed the original withdrawal")
	}
	post("at-floor", 1000001, iban, 409, "")
	assertHolds(1, 1000000)

	setFloor(1000000)
	fractional := readWithdrawal(post("fractional-fee", 1000019, iban, 201, ""))
	if fractional.GrossAmount != 1000019 || fractional.FeePct != 5.25 || fractional.FeeAmount != 52500 || fractional.NetAmount != 947519 {
		t.Fatalf("fractional fee: gross=%d pct=%g fee=%d net=%d", fractional.GrossAmount, fractional.FeePct, fractional.FeeAmount, fractional.NetAmount)
	}
	assertHolds(2, 2000019)
}

func TestFinanceTreasuryDoesNotOmitUnreadSellerLiabilities(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE stores CASCADE`); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/v1/finance/treasury/cash-position", nil)
	response := httptest.NewRecorder()
	handleT01FinanceTreasury(response, request, db, "cash-position")
	if response.Code == 200 {
		t.Fatal("unread seller liabilities were silently treated as zero in treasury")
	}
}
