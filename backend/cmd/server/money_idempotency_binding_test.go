package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestMoneyIdempotencyBinding(t *testing.T) {
	db, _, order, payment := fin003HTTPPaidOrder(t)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner := uuid.NewString()
	exec(`INSERT INTO users(id,mobile,full_name,role) VALUES($1,$2,'Binding fixture','seller')`, owner, "b"+owner[:12])
	stores := repository.NewStoreRepository(db)
	store, referred := "binding-"+uuid.NewString(), "binding-"+uuid.NewString()
	for _, id := range []string{store, referred} {
		if err := stores.CreateStore(ctx, domain.Store{ID: id, Name: "Binding fixture", Slug: id, OwnerType: "dropshipper", OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8]}); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO referral_bonuses(id,referrer_store_id,referred_store_id,amount,status,released_at) VALUES($1,$2,$3,20000000,'released',NOW())`, uuid.NewString(), store, referred)
	var previousFee sql.NullFloat64
	if err := db.QueryRow(`SELECT early_withdrawal_fee_pct FROM platform_settings WHERE id=1`).Scan(&previousFee); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE platform_settings SET early_withdrawal_fee_pct=10 WHERE id=1`)
	t.Cleanup(func() { exec(`UPDATE platform_settings SET early_withdrawal_fee_pct=$1 WHERE id=1`, previousFee) })

	supplier := "binding-supplier-" + uuid.NewString()[:12]
	exec(`INSERT INTO suppliers(id,name,warehouse_city,approval_status) VALUES($1,'Binding fixture','Tehran','approved')`, supplier)
	exec(`INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator) VALUES($1,NOW(),'supplier_payable:' || $2,'supplier','credit',10000000,'سند تعدیل binding fixture','test')`, uuid.NewString(), supplier)

	bban := "0170000000000000000000"
	n, _ := new(big.Int).SetString(bban+"182700", 10)
	check := 98 - new(big.Int).Mod(n, big.NewInt(97)).Int64()
	iban := fmt.Sprintf("IR%02d%s", check, bban)
	sellerAmount := func(amount int) string { return fmt.Sprintf(`{"amount":%d,"shaba":%q}`, amount, iban) }
	supplierAmount := func(amount int) string {
		return fmt.Sprintf(`{"amount":%d,"shaba":%q,"account_holder":"Binding fixture"}`, amount, iban)
	}
	adjustAmount := func(amount int) string {
		return fmt.Sprintf(`{"targetKind":"seller","targetId":%q,"direction":"credit","amount":%d,"reason":"Binding fixture"}`, store, amount)
	}
	refundAmount := func(amount int) string { return fmt.Sprintf(`{"amount":%d,"reason":"Binding fixture"}`, amount) }

	finance := repository.NewFinanceRepository(db)
	seller := func(action string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			claims := &auth.UserClaims{UserID: owner, Role: "seller"}
			handleSellerStore(w, r.WithContext(context.WithValue(r.Context(), userClaimsKey, claims)), stores, finance, store, action, "")
		}
	}
	supplierHandler := NewSupplierHandlers(repository.NewSupplierRepository(db), nil, false)
	for _, tc := range []struct {
		name, path, body, changed, countSQL, principal string
		handler                                        http.HandlerFunc
	}{
		{"seller services", "/api/v1/stores/" + store + "/services", `{"service":"standard"}`, `{"service":"vip"}`, `SELECT count(*) FROM seller_service_purchases WHERE store_id=$1`, store, seller("services")},
		{"seller payouts", "/api/v1/stores/" + store + "/payouts", sellerAmount(2000000), sellerAmount(3000000), `SELECT count(*) FROM payout_requests WHERE store_id=$1`, store, seller("payouts")},
		{"seller early withdrawals", "/api/v1/stores/" + store + "/early-withdrawals", sellerAmount(2000000), sellerAmount(3000000), `SELECT count(*) FROM seller_early_withdrawals WHERE store_id=$1`, store, seller("early-withdrawals")},
		{"supplier settlement", "/api/v1/supplier/settlement/request", supplierAmount(2000000), supplierAmount(3000000), `SELECT count(*) FROM payout_requests WHERE supplier_id=$1`, supplier, func(w http.ResponseWriter, r *http.Request) {
			supplierHandler.HandleRequestSettlement(w, r.WithContext(context.WithValue(r.Context(), supplierIDKey, supplier)))
		}},
		{"finance adjustments", "/api/v1/finance/adjustments", adjustAmount(2000000), adjustAmount(3000000), `SELECT count(*) FROM finance_adjustments WHERE target_kind='seller' AND target_id=$1`, store, func(w http.ResponseWriter, r *http.Request) { wr69FinanceAdjustments(w, r, db) }},
		{"admin refund", "/api/v1/admin/payments/" + payment + "/refund", refundAmount(2000000), refundAmount(3000000), `SELECT count(*) FROM order_refund_obligations WHERE order_id=$1`, order, func(w http.ResponseWriter, r *http.Request) { wr69AdminPayments(w, r, db) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			post := func(key, body string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				r := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
				r.Header.Set("Idempotency-Key", key)
				tc.handler.ServeHTTP(w, r)
				return w
			}
			assertRows := func(want int) {
				t.Helper()
				var count int
				if err := db.QueryRow(tc.countSQL, tc.principal).Scan(&count); err != nil || count != want {
					t.Fatalf("effects=%d, want %d: %v", count, want, err)
				}
			}
			assertSuccess := func(w *httptest.ResponseRecorder) {
				t.Helper()
				if w.Code < 200 || w.Code > 202 {
					t.Fatalf("status=%d body=%s", w.Code, w.Body)
				}
			}
			const key = "weak-0.1847593191"
			first := post(key, tc.body)
			assertSuccess(first)
			second := post(key, tc.body)
			if second.Code != first.Code || second.Body.String() != first.Body.String() {
				t.Fatalf("replay changed result: first %d %s, second %d %s", first.Code, first.Body, second.Code, second.Body)
			}
			assertRows(1)
			if w := post(key, tc.changed); w.Code != http.StatusConflict {
				t.Fatalf("same key with changed payload: %d %s", w.Code, w.Body)
			}
			assertRows(1)
			assertSuccess(post("weak-0.3863904516", tc.changed))
			assertRows(2)
		})
	}
}

func TestCheckoutAttemptWeakKeyBinding(t *testing.T) {
	db, buyer, _, _ := fin003HTTPPaidOrder(t)
	ctx := context.Background()
	orders := repository.NewOrderRepository(db)
	const key = "weak-0.7496133087"
	hash := strings.Repeat("a", 64)
	if attempt, err := orders.BeginCheckoutAttempt(ctx, buyer, key, hash); err != nil || attempt.AttemptID != key {
		t.Fatalf("valid non-UUID key rejected: %+v %v", attempt, err)
	}
	if _, err := orders.BeginCheckoutAttempt(ctx, buyer, key, strings.Repeat("b", 64)); !errors.Is(err, repository.ErrCheckoutConflict) {
		t.Fatalf("changed payload: %v", err)
	}
	var attempts, orderCount int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM checkout_attempts WHERE customer_user_id=$1 AND attempt_id=$2 AND payload_hash=$3), (SELECT count(*) FROM orders WHERE customer_user_id=$1)`, buyer, key, hash).Scan(&attempts, &orderCount); err != nil || attempts != 1 || orderCount != 1 {
		t.Fatalf("original attempt count=%d buyer orders=%d: %v", attempts, orderCount, err)
	}
}
