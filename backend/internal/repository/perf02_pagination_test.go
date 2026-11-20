package repository

// PERF-02 Step 13 regression tests: same-name tenant isolation, cursor
// stability, totals-vs-reference, dense-product bounds, invalid limits.
// Run via the test-isolated runner (one package + -run), never against a
// shared database:
//
//	~/workspace/.dev/test-isolated-native.sh -- go test ./internal/repository -run '^TestPerf02' -count=1 -v
import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPerf02SameNameTenantIsolation(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	a := opsFixtureNew(t, db)
	b := opsFixtureNew(t, db)
	// Two stores, the SAME name, different slugs and owners. Scoping
	// must be by store_id only — a name match must never leak rows.
	opsExec(t, db, `UPDATE stores SET name='same-name store' WHERE id IN ($1,$2)`, a.store, b.store)

	aOrders := []string{}
	bOrders := []string{}
	for i := 0; i < 2; i++ {
		id, _ := opsOrder(t, db, a, true)
		aOrders = append(aOrders, id)
		id, _ = opsOrder(t, db, b, true)
		bOrders = append(bOrders, id)
	}
	// One return + one payout per store.
	// customer_returns INSERTs go through the FIN005 writer fence
	// (lockOrderFinanceTx + markDisputeWriterTx); see
	// dispute_fixture_test.go.
	fin005ReturnFixtureExec(t, db, aOrders[0], `INSERT INTO customer_returns(id,order_id,customer_id,status,reason,return_code) VALUES($1,$2,$3,'requested','test','RC-A')`, uuid.NewString(), aOrders[0], a.user)
	fin005ReturnFixtureExec(t, db, bOrders[0], `INSERT INTO customer_returns(id,order_id,customer_id,status,reason,return_code) VALUES($1,$2,$3,'requested','test','RC-B')`, uuid.NewString(), bOrders[0], b.user)
	opsExec(t, db, `INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status) VALUES($1,$2,'seller',$2,'IR000000000000000000000000','test',1000,'pending')`, uuid.NewString(), a.store)
	opsExec(t, db, `INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status) VALUES($1,$2,'seller',$2,'IR000000000000000000000000','test',2000,'pending')`, uuid.NewString(), b.store)

	orderRepo := NewOrderRepository(db)
	finRepo := NewFinanceRepository(db)

	// Orders: each store's page holds exactly its own orders.
	for _, tc := range []struct {
		store string
		want  []string
	}{
		{a.store, aOrders},
		{b.store, bOrders},
	} {
		page, err := orderRepo.GetOrdersByStoreIDPage(ctx, tc.store, ListPageOptions{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != len(tc.want) {
			t.Fatalf("store %s: got %d orders, want %d", tc.store, len(page.Items), len(tc.want))
		}
		got := map[string]bool{}
		for _, o := range page.Items {
			got[o.ID] = true
		}
		for _, id := range tc.want {
			if !got[id] {
				t.Fatalf("store %s: missing order %s", tc.store, id)
			}
		}
	}

	// Returns: same-name stores must not see each other's returns.
	for _, tc := range []struct {
		store string
		order string
	}{
		{a.store, aOrders[0]},
		{b.store, bOrders[0]},
	} {
		page, err := orderRepo.ListCustomerReturnsByStorePage(ctx, tc.store, ListPageOptions{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].OrderID != tc.order {
			t.Fatalf("store %s: returns = %+v, want exactly order %s", tc.store, page.Items, tc.order)
		}
	}

	// Payouts: ownership-checked by (store_id, owner_user_id); a
	// same-name store owned by someone else must not leak.
	for _, tc := range []struct {
		owner, store string
		amount       int64
	}{
		{a.user, a.store, 1000},
		{b.user, b.store, 2000},
	} {
		page, err := finRepo.GetStorePayoutsPage(ctx, tc.owner, tc.store, ListPageOptions{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].Amount != tc.amount {
			t.Fatalf("store %s: payouts = %+v, want exactly amount %d", tc.store, page.Items, tc.amount)
		}
	}
	if _, err := finRepo.GetStorePayoutsPage(ctx, a.user, b.store, ListPageOptions{Limit: 50}); err != ErrStoreForbidden {
		t.Fatalf("cross-owner payout page: err = %v, want ErrStoreForbidden", err)
	}
}

func TestPerf02CursorStability(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	f := opsFixtureNew(t, db)
	repo := NewOrderRepository(db)
	now := time.Now().UTC().Truncate(time.Second)

	// 6 orders with distinct created_at, 1 minute apart.
	ids := []string{}
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("cur-%d-%s", i, uuid.NewString()[:8])
		created := now.Add(time.Duration(-(6 - i)) * time.Minute)
		opsExec(t, db, `INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status,created_at) VALUES($1,$1,$2,'ops',2000,2000,'ops','09123456789','x','x','x','1234512345',$1,$3,'paid',$4)`, id, f.store, f.user, created)
		ids = append(ids, id)
	}
	// Page 1 (limit 2): the two newest.
	page1, err := repo.GetOrdersByStoreIDPage(ctx, f.store, ListPageOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Items) != 2 || !page1.HasMore || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v", page1)
	}
	// Insert mid-pagination: 2 NEWER rows (sort before the cursor — must
	// never appear on later pages) and 1 OLDER row (sorts after everything
	// — must appear on a later page).
	newer1 := "cur-newer1-" + uuid.NewString()[:8]
	newer2 := "cur-newer2-" + uuid.NewString()[:8]
	older := "cur-older-" + uuid.NewString()[:8]
	opsExec(t, db, `INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status,created_at) VALUES($1,$1,$2,'ops',2000,2000,'ops','09123456789','x','x','x','1234512345',$1,$3,'paid',$4)`, newer1, f.store, f.user, now.Add(time.Minute))
	opsExec(t, db, `INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status,created_at) VALUES($1,$1,$2,'ops',2000,2000,'ops','09123456789','x','x','x','1234512345',$1,$3,'paid',$4)`, newer2, f.store, f.user, now.Add(2*time.Minute))
	opsExec(t, db, `INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status,created_at) VALUES($1,$1,$2,'ops',2000,2000,'ops','09123456789','x','x','x','1234512345',$1,$3,'paid',$4)`, older, f.store, f.user, now.Add(-time.Hour))

	seen := map[string]int{}
	for _, o := range page1.Items {
		seen[o.ID]++
	}
	cursor := page1.NextCursor
	for cursor != "" {
		page, err := repo.GetOrdersByStoreIDPage(ctx, f.store, ListPageOptions{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range page.Items {
			seen[o.ID]++
		}
		cursor = page.NextCursor
	}
	for _, id := range ids {
		if seen[id] != 1 {
			t.Fatalf("order %s seen %d times, want exactly 1", id, seen[id])
		}
	}
	if seen[older] != 1 {
		t.Fatalf("older mid-pagination insert seen %d times, want 1", seen[older])
	}
	if seen[newer1] != 0 || seen[newer2] != 0 {
		t.Fatalf("newer mid-pagination inserts leaked into later pages: %v", seen)
	}
	if len(seen) != 7 {
		t.Fatalf("walked %d distinct orders, want 7 (6 + older insert)", len(seen))
	}
}

func TestPerf02StatementTotalsMatchReference(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	f := opsFixtureNew(t, db)
	tehran, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	month := time.Now().In(tehran).Format("2006-01")
	now := time.Now().UTC()

	// 3 released orders (profit 1000/2000/3000), 1 paid payout (500),
	// 1 ledger adjustment credit (700) — all inside the current month.
	for i, profit := range []int64{1000, 2000, 3000} {
		id := fmt.Sprintf("stmt-%d-%s", i, uuid.NewString()[:8])
		opsExec(t, db, `INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status,status,profit_released,profit_released_at,dropshipper_profit,created_at) VALUES($1,$1,$2,'ops',5000,5000,'ops','09123456789','x','x','x','1234512345',$1,$3,'paid','delivered',TRUE,$4,$5,$4)`, id, f.store, f.user, now, profit)
	}
	opsExec(t, db, `INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status,processed_at,created_at) VALUES($1,$2,'seller',$2,'IR000000000000000000000000','test',500,'paid',$3,$3)`, uuid.NewString(), f.store, now)
	opsExec(t, db, `INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator,related_order_or_dispute,created_at) VALUES($1,$2,'store_payable:'||$3,'seller','credit',700,'سند تعدیل تست','test','', $2)`, uuid.NewString(), now, f.store)

	finRepo := NewFinanceRepository(db)
	full, err := finRepo.GetStoreStatement(ctx, f.store, month)
	if err != nil {
		t.Fatal(err)
	}
	if full.TotalCredit != 6700 {
		t.Fatalf("reference totalCredit = %d, want 6700 (1000+2000+3000+700)", full.TotalCredit)
	}
	if full.TotalDebit != 500 {
		t.Fatalf("reference totalDebit = %d, want 500", full.TotalDebit)
	}
	if full.TotalEntries != len(full.Entries) {
		t.Fatalf("reference totalEntries = %d, len(entries) = %d", full.TotalEntries, len(full.Entries))
	}

	// Walk the pages (limit 2) and concatenate. Totals repeat on every
	// page (each page computes them in its own repeatable-read tx), so
	// they must EQUAL the reference on every page, not accumulate.
	var walked []string
	cursor := ""
	pages := 0
	for {
		page, err := finRepo.GetStoreStatementPage(ctx, f.store, month, StatementPageOptions{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if page.TotalCredit != full.TotalCredit || page.TotalDebit != full.TotalDebit || page.TotalEntries != full.TotalEntries {
			t.Fatalf("page %d totals credit=%d debit=%d entries=%d, want %d/%d/%d", pages, page.TotalCredit, page.TotalDebit, page.TotalEntries, full.TotalCredit, full.TotalDebit, full.TotalEntries)
		}
		for _, e := range page.Entries {
			walked = append(walked, e.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pages > 10 {
			t.Fatal("statement paging did not terminate")
		}
	}
	// The last page's totals are the reference totals; entry sets equal.
	refIDs := []string{}
	for _, e := range full.Entries {
		refIDs = append(refIDs, e.ID)
	}
	sort.Strings(walked)
	sort.Strings(refIDs)
	if len(walked) != len(refIDs) {
		t.Fatalf("walked %d entries, reference has %d", len(walked), len(refIDs))
	}
	for i := range walked {
		if walked[i] != refIDs[i] {
			t.Fatalf("entry mismatch at %d: %s vs %s", i, walked[i], refIDs[i])
		}
	}
}

func TestPerf02ReportPageCountsMatchTotal(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	f := opsFixtureNew(t, db)
	// 5 products under one supplier (fixture product + 4 more).
	opsExec(t, db, `INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,length_mm,width_mm,height_mm,weight_grams,measurement_status,warehouse_name) SELECT 'perf02-prod-'||g,'perf02 product '||g,'perf02-prod-'||g,$1,2000,1000,0,100,100,100,100,100,'verified','ops warehouse' FROM generate_series(1,4) g`, f.supplier)
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM products`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	repo := NewSupplierRepository(db)
	for _, full := range []bool{false, true} {
		seen := map[string]int{}
		cursor := ""
		var reportedTotal int
		for {
			var rows []HistoricalInventoryReportRow
			var err error
			if full {
				rows, reportedTotal, cursor, err = repo.GetHistoricalInventoryReportCursorPage(ctx, 2, cursor)
			} else {
				rows, reportedTotal, cursor, err = repo.GetHistoricalInventoryReportSummaryCursorPage(ctx, 2, cursor)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				seen[r.ProductID]++
				if !full {
					// Summary rows ship counts, not evidence payloads.
					if len(r.LocationBreakdown) != 0 || len(r.ActiveReservations) != 0 || len(r.Movements) != 0 || len(r.MeasurementEvidence) != 0 {
						t.Fatalf("summary row %s ships evidence lists", r.ProductID)
					}
				}
			}
			if cursor == "" {
				break
			}
		}
		if reportedTotal != total {
			t.Fatalf("full=%v: reported total %d, want COUNT(*) %d", full, reportedTotal, total)
		}
		if len(seen) != total {
			t.Fatalf("full=%v: walked %d distinct products, want %d", full, len(seen), total)
		}
		for id, n := range seen {
			if n != 1 {
				t.Fatalf("full=%v: product %s seen %d times", full, id, n)
			}
		}
	}
}

func TestPerf02DenseProductReviewBounded(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	f := opsFixtureNew(t, db)
	product := "perf02-dense-" + uuid.NewString()[:8]
	opsExec(t, db, `INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,length_mm,width_mm,height_mm,weight_grams,measurement_status,warehouse_name) VALUES($1,'dense product',$1,$2,2000,1000,0,100,100,100,100,100,'verified','ops warehouse')`, product, f.supplier)
	opsExec(t, db, `INSERT INTO inventory_movements(id,product_id,quantity,reference_type,reference_id,created_at,idempotency_key) SELECT 'mov-dense-'||g,$1,1,'inbound_received','ref-'||g, now() - (g||' seconds')::interval, 'idem-'||g FROM generate_series(1,5005) g`, product)
	opsExec(t, db, `INSERT INTO inventory_reservations(id,product_id,location_key,quantity,reference_type,reference_id,status,idempotency_key,created_at) SELECT 'res-dense-'||g,$1,'loc-1',1,'order','ref-'||g,'active','resk-'||g, now() - (g||' seconds')::interval FROM generate_series(1,5001) g`, product)

	repo := NewSupplierRepository(db)
	start := time.Now()
	row, err := repo.GetHistoricalInventoryReportRow(ctx, product)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("dense single-row review took %v, want < 2s", elapsed)
	}
	if len(row.Movements) > MaxHistoricalNestedEvidence {
		t.Fatalf("dense row ships %d movements, want <= %d", len(row.Movements), MaxHistoricalNestedEvidence)
	}
	if len(row.ActiveReservations) > MaxHistoricalNestedEvidence {
		t.Fatalf("dense row ships %d reservations, want <= %d", len(row.ActiveReservations), MaxHistoricalNestedEvidence)
	}
	if row.EvidenceTotal < 10006 {
		t.Fatalf("dense row evidenceTotal = %d, want >= 10006 (5005 movements + 5001 reservations)", row.EvidenceTotal)
	}
	if !row.EvidenceTruncated {
		t.Fatal("dense row EvidenceTruncated = false, want true")
	}

	// The summary page reports the same counts without building JSONB.
	found := false
	cursor := ""
	for {
		rows, total, next, err := repo.GetHistoricalInventoryReportSummaryCursorPage(ctx, MaxHistoricalReportLimit, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if total < 1 {
			t.Fatal("summary total is 0")
		}
		for _, r := range rows {
			if r.ProductID == product {
				found = true
				if r.EvidenceTotal < 10006 || !r.EvidenceTruncated {
					t.Fatalf("summary dense row: evidenceTotal=%d truncated=%v", r.EvidenceTotal, r.EvidenceTruncated)
				}
				if len(r.Movements) != 0 || len(r.ActiveReservations) != 0 {
					t.Fatal("summary dense row ships evidence lists")
				}
			}
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if !found {
		t.Fatalf("dense product %s not found in summary pages", product)
	}
}

func TestPerf02LimitNormalization(t *testing.T) {
	if got := NormalizeListLimit(0); got != DefaultListLimit {
		t.Fatalf("NormalizeListLimit(0) = %d, want %d", got, DefaultListLimit)
	}
	if got := NormalizeListLimit(50); got != 50 {
		t.Fatalf("NormalizeListLimit(50) = %d, want 50", got)
	}
	if got := NormalizeListLimit(101); got != MaxListLimit {
		t.Fatalf("NormalizeListLimit(101) = %d, want %d", got, MaxListLimit)
	}
	if got := NormalizeListLimit(1000000); got != MaxListLimit {
		t.Fatalf("NormalizeListLimit(1000000) = %d, want %d", got, MaxListLimit)
	}
	// A negative limit reaching the repository is a caller bug; it must
	// degrade to the default, never to an unbounded list.
	if got := NormalizeListLimit(-5); got != DefaultListLimit {
		t.Fatalf("NormalizeListLimit(-5) = %d, want %d", got, DefaultListLimit)
	}
	// Cursor round-trip + invalid cursors.
	now := time.Now().UTC().Truncate(time.Second)
	enc := EncodeCreatedAtCursor(now, "abc-123")
	decTime, decID, err := DecodeCreatedAtCursor(enc)
	if err != nil || !decTime.Equal(now) || decID != "abc-123" {
		t.Fatalf("cursor round-trip: %v %v %v", decTime, decID, err)
	}
	for _, bad := range []string{"", "!!!not-base64!!!", "aGVsbG8"} {
		if _, _, err := DecodeCreatedAtCursor(bad); err == nil {
			t.Fatalf("DecodeCreatedAtCursor(%q) = nil error, want error", bad)
		}
	}
}
