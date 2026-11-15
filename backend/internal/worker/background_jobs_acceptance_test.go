package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/jobrunner"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/drup/platform/internal/testredis"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

func jobDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEAM09_WORKER_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable database required")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(12)
	return db
}
func jobExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

type jobFixture struct{ user, store, supplier, product, prefix string }

func seedJobFixture(t *testing.T, db *sql.DB) jobFixture {
	t.Helper()
	suffix := uuid.NewString()[:8]
	f := jobFixture{uuid.NewString(), "js-" + suffix, "ju-" + suffix, "jp-" + suffix, "jo-" + suffix + "-"}
	jobExec(t, db, `INSERT INTO users(id,mobile,full_name,role) VALUES($1,$2,'job test','supplier')`, f.user, fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9))
	jobExec(t, db, `INSERT INTO suppliers(id,name,owner_user_id,approval_status) VALUES($1,'job supplier',$2,'approved')`, f.supplier, f.user)
	jobExec(t, db, `INSERT INTO stores(id,name,slug,owner_name,owner_user_id,active_plan_id,status) VALUES($1,'job store',$1,'job',$2,'standard','active')`, f.store, f.user)
	jobExec(t, db, `INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,warehouse_name,category_slug,length_mm,width_mm,height_mm,weight_grams) VALUES($1,'job product',$1,$2,2000,1000,0,10000,'job warehouse',(SELECT slug FROM categories LIMIT 1),100,100,100,100)`, f.product, f.supplier)
	return f
}
func seedJobOrders(t *testing.T, db *sql.DB, f jobFixture, start, n int) {
	t.Helper()
	jobExec(t, db, `INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,payment_expires_at)
 SELECT $1||g::text,$1||g::text,$2,'job',2000,2000,'job','09123456789','x','x','x','1234512345',$1||g::text,clock_timestamp()-INTERVAL '2 minutes' FROM generate_series($3::int,$4::int)g`, f.prefix, f.store, start, start+n-1)
	jobExec(t, db, `INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price) SELECT md5($1||g::text)::uuid,$1||g::text,$2,1,1000,0,1000,2000 FROM generate_series($3::int,$4::int)g`, f.prefix, f.product, start, start+n-1)
}

func TestJobsBurstSustainedTwoWorkersSlowIsolationAndHTTP(t *testing.T) {
	db := jobDB(t)
	f := seedJobFixture(t, db)
	seedJobOrders(t, db, f, 1, 260)
	cache := &repository.RedisClient{Client: testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))}
	for i := 1; i <= 260; i++ {
		if err := cache.HoldInventoryUntil(context.Background(), f.prefix+fmt.Sprint(i), f.product, 1, 10000, time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	var slowRuns atomic.Int32
	build := func() *jobrunner.Runner {
		w := NewBackgroundWorker(db)
		w.SetOrderRepository(repository.NewOrderRepository(db))
		w.SetFinanceRepository(repository.NewFinanceRepository(db))
		w.SetRedisClient(cache)
		jobs := w.jobs()
		for i := range jobs {
			if jobs[i].ID == "monthly-settlement" {
				jobs[i].Run = func(ctx context.Context) (jobrunner.Result, error) {
					slowRuns.Add(1)
					<-ctx.Done()
					return jobrunner.Result{}, ctx.Err()
				}
			}
		}
		return jobrunner.New(repository.JobStore{DB: db}, jobs)
	}
	a, b := build(), build()
	a.Start()
	b.Start()
	defer a.Stop(context.Background())
	defer b.Stop(context.Background())
	catalog := repository.NewCatalogRepository(db)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := catalog.GetProductByID(r.Context(), f.product)
		if err != nil {
			t.Logf("catalog read error: %v", err)
			http.Error(w, "read failed", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(p)
	}))
	defer server.Close()
	client := &http.Client{Timeout: 2 * time.Second}
	var latencies []time.Duration
	maxInUse := 0
	maxOldest := int64(0)
	started := time.Now()
	nextArrival := started
	created := 260
	deadline := started.Add(75 * time.Second)
	for time.Now().Before(deadline) {
		now := time.Now()
		if now.Sub(started) < 30*time.Second && !now.Before(nextArrival) {
			seedJobOrders(t, db, f, created+1, 1)
			created++
			nextArrival = nextArrival.Add(250 * time.Millisecond)
		}
		before := time.Now()
		res, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatal(res.StatusCode)
		}
		latencies = append(latencies, time.Since(before))
		if n := db.Stats().InUse; n > maxInUse {
			maxInUse = n
		}
		var pending int
		var oldest int64
		if err := db.QueryRow(`SELECT COUNT(*),COALESCE(EXTRACT(EPOCH FROM clock_timestamp()-MIN(payment_expires_at))::bigint,0) FROM orders WHERE store_id=$1 AND status IN ('pending','processing')`, f.store).Scan(&pending, &oldest); err != nil {
			t.Fatal(err)
		}
		if oldest > maxOldest {
			maxOldest = oldest
		}
		if oldest > 300 {
			t.Fatalf("expiry SLO exceeded: %d seconds", oldest)
		}
		if now.Sub(started) >= 30*time.Second && pending == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var pending, stock, cancelled, ledger int
	_ = db.QueryRow(`SELECT count(*) FROM orders WHERE store_id=$1 AND status IN ('pending','processing')`, f.store).Scan(&pending)
	_ = db.QueryRow(`SELECT count(*) FROM orders WHERE store_id=$1 AND status='cancelled'`, f.store).Scan(&cancelled)
	_ = db.QueryRow(`SELECT stock FROM products WHERE id=$1`, f.product).Scan(&stock)
	_ = db.QueryRow(`SELECT count(*) FROM ledger_entries WHERE related_order_or_dispute LIKE $1`, f.prefix+"%").Scan(&ledger)
	if pending != 0 || cancelled != created || stock != 10000 || ledger != 0 || slowRuns.Load() < 1 {
		var human, automatic int
		var earliestHuman, earliestAuto sql.NullTime
		_ = db.QueryRow(`SELECT count(*),min(r.next_retry_at) FROM order_expiry_review_state r JOIN orders o ON o.id=r.order_id WHERE o.store_id=$1 AND o.status IN ('pending','processing')`, f.store).Scan(&human, &earliestHuman)
		_ = db.QueryRow(`SELECT count(*),min(r.next_retry_at) FROM background_job_candidate_retry r JOIN orders o ON o.id=r.candidate WHERE r.job='unpaid-order-expiry' AND o.store_id=$1 AND o.status IN ('pending','processing')`, f.store).Scan(&automatic, &earliestAuto)
		t.Logf("pending diagnostic human_review=%d earliest=%v automatic_retry=%d earliest=%v", human, earliestHuman, automatic, earliestAuto)
		t.Fatalf("pending=%d canceled=%d/%d stock=%d ledger=%d slow=%d", pending, cancelled, created, stock, ledger, slowRuns.Load())
	}
	held, err := cache.HasInventoryHold(context.Background(), f.prefix+"1", f.product, 1)
	if err != nil || held {
		t.Fatal("hold not released", held, err)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	t.Logf("LOCAL ONLY burst=260 sustained_created=%d over30s oldest_ready_max=%ds drain=%s HTTP_requests=%d p50=%s p95=%s max=%s observed_DB_in_use=%d open=%d", created-260, maxOldest, time.Since(started), len(latencies), latencies[len(latencies)/2], latencies[len(latencies)*95/100], latencies[len(latencies)-1], maxInUse, db.Stats().OpenConnections)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = a.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err = b.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

// This subprocess is deliberately killed after committed work. It never releases
// ownership, and no provider or production connection is reachable from this test.
func TestJobsCrashChild(t *testing.T) {
	if os.Getenv("SELLERSHO_JOB_CRASH_CHILD") != "owned-job01" {
		t.Skip("parent-only helper")
	}
	db := jobDB(t)
	ok, err := (repository.JobStore{DB: db}).Acquire(context.Background(), "unpaid-order-expiry", "crashed-process", time.Second)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	_, _ = repository.NewOrderRepository(db).ExpireUnpaidOrders(context.Background())
	select {}
}
func TestJobsCrashRestartAndMidBatchCancellation(t *testing.T) {
	db := jobDB(t)
	f := seedJobFixture(t, db)
	seedJobOrders(t, db, f, 1, 60)
	// Slow only owned order updates so the parent can interrupt a real SQL batch.
	jobExec(t, db, `CREATE FUNCTION job01_slow_expiry() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id LIKE 'jo-%' AND NEW.status='cancelled' THEN PERFORM pg_sleep(0.025); END IF; RETURN NEW; END $$`)
	jobExec(t, db, `CREATE TRIGGER job01_slow_expiry BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION job01_slow_expiry()`)
	child := exec.Command(os.Args[0], "-test.run=^TestJobsCrashChild$", "-test.timeout=30s")
	child.Env = append(os.Environ(), "SELLERSHO_JOB_CRASH_CHILD=owned-job01")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	until := time.Now().Add(5 * time.Second)
	committed := 0
	for time.Now().Before(until) {
		_ = db.QueryRow(`SELECT count(*) FROM orders WHERE store_id=$1 AND status='cancelled'`, f.store).Scan(&committed)
		if committed >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if committed < 3 {
		t.Fatal("child made no committed progress")
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	// After the crash, choose two actual remaining candidates. Signal a real
	// first commit and a blocked next update before cancelling; no90ms timing guess.
	rows, err := db.Query(`SELECT id FROM orders WHERE store_id=$1 AND status IN ('pending','processing') ORDER BY payment_expires_at,id COLLATE "C" LIMIT 2`, f.store)
	if err != nil {
		t.Fatal(err)
	}
	var pendingIDs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		pendingIDs = append(pendingIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(pendingIDs) != 2 {
		t.Fatal("need two remaining candidates", pendingIDs, err)
	}
	jobExec(t, db, `CREATE FUNCTION job01_block_next_expiry() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id=TG_ARGV[0] AND NEW.status='cancelled' THEN PERFORM pg_advisory_xact_lock(90261178); END IF; RETURN NEW; END $$`)
	jobExec(t, db, `CREATE TRIGGER job01_block_next_expiry BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION job01_block_next_expiry('`+pendingIDs[1]+`')`)
	barrier, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback()
	if _, err = barrier.Exec(`SELECT pg_advisory_xact_lock(90261178)`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	type batchResult struct {
		orders []repository.ExpiredOrder
		err    error
	}
	done := make(chan batchResult, 1)
	go func() {
		orders, e := repository.NewOrderRepository(db).ExpireUnpaidOrders(ctx)
		done <- batchResult{orders, e}
	}()
	until = time.Now().Add(5 * time.Second)
	atBarrier := false
	firstCommitted := false
	for time.Now().Before(until) {
		if err = db.QueryRow(`SELECT status='cancelled' FROM orders WHERE id=$1`, pendingIDs[0]).Scan(&firstCommitted); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid=90261178 AND NOT granted)`).Scan(&atBarrier); err != nil {
			t.Fatal(err)
		}
		if firstCommitted && atBarrier {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	_ = barrier.Rollback()
	result := <-done
	if !firstCommitted || !atBarrier || result.err == nil || len(result.orders) == 0 {
		t.Fatalf("mid-batch cancellation lacked actual committed/barrier evidence committed=%v barrier=%v returned=%d err=%v", firstCommitted, atBarrier, len(result.orders), result.err)
	}
	t.Logf("mid-batch cancel after committed first candidate and real next SQL barrier: committed_results=%d", len(result.orders))
	// A crashed owner's lease expires by the server clock; do not assume a wall
	// sleep establishes expiry. Recovery attempts keep the real5s cadence.
	until = time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		var expired bool
		if err = db.QueryRow(`SELECT lease_until<=clock_timestamp() FROM background_job_state WHERE job='unpaid-order-expiry'`).Scan(&expired); err != nil {
			t.Fatal(err)
		}
		if expired {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	worker := NewBackgroundWorker(db)
	worker.SetOrderRepository(repository.NewOrderRepository(db))
	until = time.Now().Add(20 * time.Second)
	nextPass := time.Now()
	for time.Now().Before(until) {
		var remaining int
		if err = db.QueryRow(`SELECT count(*) FROM orders WHERE store_id=$1 AND status IN ('pending','processing')`, f.store).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining == 0 {
			break
		}
		if !time.Now().Before(nextPass) {
			attemptStarted := time.Now()
			_, err = worker.runner().Run(context.Background(), "unpaid-order-expiry")
			if err != nil {
				var automatic bool
				if e := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM background_job_candidate_retry b JOIN orders o ON o.id=b.candidate WHERE b.job='unpaid-order-expiry' AND b.reason='transient' AND o.store_id=$1 AND o.status IN ('pending','processing'))`, f.store).Scan(&automatic); e != nil {
					t.Fatal(e)
				}
				if !expectedExpiryBudgetFailure(err, automatic || time.Since(attemptStarted) >= 3*time.Second) {
					t.Fatal(err)
				}
			}
			nextPass = time.Now().Add(5 * time.Second)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var cancelled, stock, ledger int
	_ = db.QueryRow(`SELECT count(*) FROM orders WHERE store_id=$1 AND status='cancelled'`, f.store).Scan(&cancelled)
	_ = db.QueryRow(`SELECT stock FROM products WHERE id=$1`, f.product).Scan(&stock)
	_ = db.QueryRow(`SELECT count(*) FROM ledger_entries WHERE related_order_or_dispute LIKE $1`, f.prefix+"%").Scan(&ledger)
	if cancelled != 60 || stock != 10000 || ledger != 0 {
		t.Fatal(cancelled, stock, ledger)
	}
	var owner sql.NullString
	if err = db.QueryRow(`SELECT owner FROM background_job_state WHERE job='unpaid-order-expiry'`).Scan(&owner); err != nil || owner.Valid {
		t.Fatal("stuck owner", owner, err)
	}
}

// ErrTxDone requires actual automatic-retry or elapsed-job-budget evidence.
// Joined metadata/business failures are never discarded as a budget outcome.
func expectedExpiryBudgetFailure(err error, contextEvidence bool) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, part := range joined.Unwrap() {
			if !expectedExpiryBudgetFailure(part, contextEvidence) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return expectedExpiryBudgetFailure(wrapped.Unwrap(), contextEvidence)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, sql.ErrTxDone) {
		return contextEvidence
	}
	var state interface{ SQLState() string }
	return errors.As(err, &state) && state.SQLState() == "57014"
}
