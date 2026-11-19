package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/drup/platform/internal/repository"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/drup/platform/internal/testdb"
)

func openFinanceHTTPDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DROP TABLE IF EXISTS seller_early_withdrawals,payout_requests,orders,referral_bonuses,seller_service_purchases,customer_return_financial_events,stores,ledger_entries,finance_adjustments CASCADE`,
		`CREATE TABLE stores(id TEXT PRIMARY KEY,owner_user_id TEXT,name TEXT DEFAULT 'Finance',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			slug VARCHAR(255) NOT NULL DEFAULT '',
			owner_name VARCHAR(255) NOT NULL DEFAULT '',
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			active_categories text[] NOT NULL DEFAULT '{}'::text[],
			active_plan_id character varying(20) NOT NULL DEFAULT 'trial'::character varying,
			addon_section_title character varying(200),
			contract_signature_code text,
			custom_domain character varying(150),
			custom_store_margin_override numeric(5,2),
			description text,
			is_margin_locked_by_admin boolean NOT NULL DEFAULT false,
			logo text NOT NULL DEFAULT ''::text,
			owner_phone text NOT NULL DEFAULT ''::text,
			owner_type character varying(20) NOT NULL DEFAULT 'dropshipper'::character varying,
			panel_config jsonb NOT NULL DEFAULT '{}'::jsonb,
			plan_expires_at timestamp with time zone,
			profit_margin_percent numeric(5,2) NOT NULL DEFAULT 12.00,
			referral_code character varying(30),
			referrer_seller_id character varying(50),
			rejection_reason text,
			related_section_title character varying(200),
			seller_deals_widget_config jsonb,
			signed_contract_version integer NOT NULL DEFAULT 1,
			status character varying(20) NOT NULL DEFAULT 'active'::character varying,
			tagline text,
			template_config jsonb NOT NULL DEFAULT '{}'::jsonb,
			template_id character varying(50),
			total_sales bigint NOT NULL DEFAULT 0,
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		`INSERT INTO stores(id) VALUES('adjust-store')`,
		`CREATE TABLE finance_adjustments(id TEXT PRIMARY KEY,target_kind TEXT,target_id TEXT,direction TEXT,amount BIGINT,reason TEXT,operator TEXT DEFAULT ''::character varying ,created_at TIMESTAMPTZ DEFAULT NOW(),request_key TEXT,request_fingerprint TEXT)`,
		`CREATE UNIQUE INDEX adjustment_request_key ON finance_adjustments(target_kind,target_id,request_key) WHERE request_key IS NOT NULL`,
		`CREATE TABLE ledger_entries(id TEXT PRIMARY KEY,date TIMESTAMPTZ DEFAULT now() ,target_account TEXT,target_type TEXT,type TEXT,amount BIGINT,reason TEXT,operator TEXT,related_order_or_dispute TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`CREATE TABLE seller_early_withdrawals(id TEXT PRIMARY KEY,store_id TEXT,gross_amount BIGINT,net_amount BIGINT,fee_amount BIGINT,shaba TEXT,bank_name TEXT,status TEXT DEFAULT 'pending'::text ,rejection_reason TEXT,processed_at TIMESTAMPTZ,payout_request_id TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			fee_pct numeric(5,2))`,
		`CREATE TABLE payout_requests(id TEXT PRIMARY KEY,store_id TEXT,supplier_id TEXT,recipient_type TEXT,recipient_name TEXT,shaba TEXT,bank_name TEXT,amount BIGINT,status TEXT DEFAULT 'pending'::character varying ,available_balance BIGINT DEFAULT 0 ,created_at TIMESTAMPTZ DEFAULT now() ,processed_at TIMESTAMPTZ,transfer_reference TEXT,transfer_confirmed_by TEXT,transfer_confirmed_at TIMESTAMPTZ,rejection_reason TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			payout_method character varying(30) NOT NULL DEFAULT 'manual_bank_transfer'::character varying,
			transfer_date timestamp with time zone)`,
		`CREATE TABLE orders(id VARCHAR(50) PRIMARY KEY,store_id TEXT,dropshipper_profit BIGINT DEFAULT 0 ,profit_released BOOLEAN DEFAULT false ,status TEXT DEFAULT 'processing'::character varying ,referrer_seller_id TEXT,referral_commission BIGINT DEFAULT 0 ,referral_commission_released BOOLEAN DEFAULT false ,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			order_number VARCHAR(50) NOT NULL DEFAULT '',
			payable_amount BIGINT NOT NULL DEFAULT 0,
			payment_status VARCHAR(20) NOT NULL DEFAULT 'pending',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			customer_user_id UUID,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			box_size character varying(80),
			carrier character varying(100),
			confirmed_at timestamp with time zone,
			coupon_discount_amount bigint,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			customer_address text,
			customer_city character varying(50),
			customer_full_name character varying(100),
			customer_phone character varying(20),
			customer_postal_code character varying(20),
			customer_province character varying(50),
			discount_amount bigint NOT NULL DEFAULT 0,
			discount_code text,
			dispute_reason text,
			disputed_at timestamp with time zone,
			fulfillment_method character varying(20) DEFAULT 'warehouse'::character varying,
			gateway_amount bigint NOT NULL DEFAULT 0,
			idempotency_key text,
			inventory_captured boolean NOT NULL DEFAULT false,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			packed_at timestamp with time zone,
			payment_expires_at timestamp with time zone DEFAULT (now() + '00:15:00'::interval),
			payment_method character varying(20) NOT NULL DEFAULT 'cash'::character varying,
			platform_addon_amount bigint NOT NULL DEFAULT 0,
			platform_profit bigint NOT NULL DEFAULT 0,
			profit_released_at timestamp with time zone,
			return_window_ends_at timestamp with time zone,
			seller_discount_amount bigint NOT NULL DEFAULT 0,
			shipped_at timestamp with time zone,
			shipping_cost bigint NOT NULL DEFAULT 0,
			shipping_label text,
			shipping_quote_id text,
			store_name character varying(100),
			supplier_addon_amount bigint NOT NULL DEFAULT 0,
			total_amount bigint,
			tracking_code character varying(50),
			tracking_number character varying(100),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			wallet_amount bigint NOT NULL DEFAULT 0,
			weight_grams integer)`,
		`CREATE TABLE referral_bonuses(referrer_store_id TEXT,amount BIGINT DEFAULT 100000 ,status TEXT DEFAULT 'locked'::character varying ,released_at TIMESTAMPTZ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			id character varying(50),
			referred_store_id character varying(50))`,
		`CREATE TABLE seller_service_purchases(store_id TEXT,amount BIGINT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			id uuid NOT NULL DEFAULT gen_random_uuid(),
			service_type character varying(30))`,
		`CREATE TABLE customer_return_financial_events(store_id TEXT,amount BIGINT,component TEXT,operation TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			actor_user_id character varying(80),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			id character varying(80),
			idempotency_key character varying(150),
			ledger_entry_id character varying(80),
			order_id text,
			reason text,
			return_id uuid,
			return_item_id uuid,
			supplier_id character varying(50))`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestFinanceAdjustmentConcurrentReplay(t *testing.T) {
	db := openFinanceHTTPDB(t)
	postKey := func(amount, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/finance/adjustments", strings.NewReader(`{"targetKind":"seller","targetId":"adjust-store","direction":"credit","amount":`+amount+`,"reason":"test"}`))
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		wr69FinanceAdjustmentCreate(w, r, db)
		return w
	}
	for _, key := range []string{"", strings.Repeat("x", 129)} {
		if w := postKey("3000000", key); w.Code != 400 {
			t.Fatalf("invalid adjustment key accepted%d", w.Code)
		}
	}
	var before int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ledger_entries`).Scan(&before); err != nil || before != 0 {
		t.Fatalf("invalid-key effects%d err%v", before, err)
	}
	post := func(amount string) *httptest.ResponseRecorder { return postKey(amount, "adjust-business-event") }
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- post("3000000") }()
	}
	wg.Wait()
	close(results)
	doc := ""
	for w := range results {
		if w.Code != 200 {
			t.Fatalf("same-key retry returned %d: %s", w.Code, w.Body.String())
		}
		var body struct {
			DocID string `json:"docId"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if doc != "" && doc != body.DocID {
			t.Fatalf("one request produced documents %s and %s", doc, body.DocID)
		}
		doc = body.DocID
	}
	if w := post("4000000"); w.Code != 409 {
		t.Fatalf("changed fingerprint returned %d", w.Code)
	}
	var n int
	var total int64
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*),SUM(amount) FROM ledger_entries WHERE target_type='seller'`).Scan(&n, &total); err != nil || n != 1 || total != 3000000 {
		t.Fatalf("effects=%d total=%d err=%v", n, total, err)
	}
}

func TestFinanceEarlyWithdrawalReservationTransitions(t *testing.T) {
	db := openFinanceHTTPDB(t)
	if _, err := db.Exec(`INSERT INTO orders(id,store_id,dropshipper_profit,profit_released,status) VALUES('earned','adjust-store',10000000,true,'delivered'); INSERT INTO seller_early_withdrawals(id,store_id,gross_amount,net_amount,fee_amount,shaba,bank_name,status) VALUES('early-one','adjust-store',2000000,1800000,200000,'bank','bank','pending'),('early-reject','adjust-store',2000000,1800000,200000,'bank','bank','pending')`); err != nil {
		t.Fatal(err)
	}
	check := func(want int64) {
		t.Helper()
		b, err := repository.NewFinanceRepository(db).GetStoreBalance(context.Background(), "adjust-store")
		if err != nil || b.AvailableAmount != want {
			t.Fatalf("balance=%+v err=%v want%d", b, err, want)
		}
	}
	review := func(id, status string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/early/"+id, strings.NewReader(`{"status":"`+status+`"}`))
		w := httptest.NewRecorder()
		handleT01EarlyWithdrawalReview(w, r, db, id)
		return w
	}
	check(6000000)
	var wg sync.WaitGroup
	results := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- review("early-one", "approved").Code }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for status := range results {
		if status == 200 {
			successes++
		} else if status != 409 {
			t.Fatalf("review status%d", status)
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d", successes)
	}
	check(6000000)
	if w := review("early-reject", "rejected"); w.Code != 200 {
		t.Fatalf("reject=%d", w.Code)
	}
	check(8000000)
	if w := review("early-reject", "approved"); w.Code != 409 {
		t.Fatalf("rejected replay=%d", w.Code)
	}
	var payout string
	if err := db.QueryRow(`SELECT payout_request_id FROM seller_early_withdrawals WHERE id='early-one'`).Scan(&payout); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewFinanceRepository(db).ConfirmManualTransfer(context.Background(), payout, "transfer", "test"); err != nil {
		t.Fatal(err)
	}
	check(8000000)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payout_requests`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("payouts=%d err=%v", n, err)
	}
}
