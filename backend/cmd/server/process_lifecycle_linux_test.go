package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/jobrunner"
	"github.com/drup/platform/internal/lifecycle"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/requestbudget"
	"github.com/drup/platform/internal/testdb"
)

// Only the owned Linux fixture runner opts into this subprocess. Production has
// no barrier endpoints, test authentication, injected sleep job or baseline mode.
func TestLifecycleProcessHelper(t *testing.T) {
	if os.Getenv("SELLERSHO_PROCESS_HELPER") != "1" {
		return
	}
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	rp := &gap13Repos{db: db, orderRepo: repository.NewOrderRepository(db), catalogRepo: repository.NewCatalogRepository(db), storeRepo: repository.NewStoreRepository(db), shipmentRepo: repository.NewSupplierShipmentRepository(db), pricingRepo: repository.NewPricingRepository(db), supplierRepo: repository.NewSupplierRepository(db)}
	s := lifecycle.New(db.PingContext, nil, "test", true)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/live", s.Live)
	mux.HandleFunc("/read", func(w http.ResponseWriter, r *http.Request) {
		var n int
		err := db.QueryRowContext(r.Context(), `SELECT count(*) FROM checkout_attempts`).Scan(&n)
		if err != nil {
			http.Error(w, "read canceled", 503)
			return
		}
		w.Write([]byte("read completed"))
	})
	mux.HandleFunc("/api/v1/orders", func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), userClaimsKey, &auth.UserClaims{UserID: gap13UserID, Mobile: "09120000000", Role: "customer", Capabilities: []string{"customer"}})
		handleCreateOrder(w, r.WithContext(ctx), ctx, rp.orderRepo, rp.catalogRepo, rp.storeRepo, rp.shipmentRepo, rp.pricingRepo, rp.supplierRepo, db)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("SELLERSHO_PROCESS_PORT"))
	if err != nil {
		t.Fatal("fixture listener")
	}
	server := newHTTPServer("", s.Admission(requestbudget.New(requestbudget.DefaultConfig())(mux)))
	if os.Getenv("SELLERSHO_LIFECYCLE_BASELINE") == "1" { // Original ListenAndServe path had no signal drain.
		s.Initialize()
		if err = server.Serve(listener); err != nil {
			t.Fatal("baseline serve")
		}
		return
	}
	signalCtx, stopSignal := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stopSignal()
	runner := jobrunner.New(repository.JobStore{DB: db}, []jobrunner.Job{{ID: "ops01-process-sleep", Timeout: 30 * time.Second, Period: time.Hour, Run: func(ctx context.Context) (jobrunner.Result, error) {
		_, err := db.ExecContext(ctx, `SELECT pg_sleep(25)`)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return jobrunner.Result{}, err
	}}})
	manualDone := make(chan struct{})
	process := &lifecycle.Process{State: s, Grace: 3 * time.Second, Server: server, StopWorker: func(ctx context.Context) error {
		if err := runner.Stop(ctx); err != nil {
			return err
		}
		select {
		case <-manualDone:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, Closers: []lifecycle.Closer{func(context.Context) error { return db.Close() }}}
	if err = process.Serve(signalCtx, listener, func() {
		go func() { defer close(manualDone); runner.Run(context.Background(), "ops01-process-sleep") }()
	}); err != nil {
		t.Fatal(err)
	}
}

type processResponse struct {
	code int
	body string
	err  error
}

func processRequest(client *http.Client, method, url, body string) processResponse {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return processResponse{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return processResponse{err: err}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return processResponse{resp.StatusCode, string(b), err}
}

func TestProcessSIGTERMCheckoutRestartConservation(t *testing.T) {
	if os.Getenv("SELLERSHO_LINUX_PROCESS_FIXTURE") != "1" {
		t.Skip("owned Linux process fixture required")
	}
	rp := checkoutHTTPFixture(t)
	body := checkoutBody(t, rp, "process-terminate-replay")
	snapshot := func() []int64 {
		t.Helper()
		out := make([]int64, 4)
		for i, q := range []string{`SELECT count(*) FROM ledger_entries`, `SELECT count(*) FROM payment_transactions`, `SELECT count(*) FROM inventory_movements`, `SELECT stock FROM products WHERE id='p-direct-both'`} {
			if err := rp.db.QueryRow(q).Scan(&out[i]); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	initial := snapshot()
	start := func() (*exec.Cmd, *bytes.Buffer, string) {
		t.Helper()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		_, port, _ := net.SplitHostPort(listener.Addr().String())
		listener.Close()
		cmd := exec.Command(os.Args[0], "-test.run=^TestLifecycleProcessHelper$", "-test.timeout=20s")
		cmd.Env = append(os.Environ(), "SELLERSHO_PROCESS_HELPER=1", "SELLERSHO_PROCESS_PORT="+port)
		out := &bytes.Buffer{}
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Start(); err != nil {
			t.Fatal("process start")
		}
		t.Cleanup(func() {
			if cmd.ProcessState == nil {
				cmd.Process.Kill()
				cmd.Wait()
			}
		})
		base := "http://127.0.0.1:" + port
		client := &http.Client{Timeout: 200 * time.Millisecond}
		until := time.Now().Add(5 * time.Second)
		for time.Now().Before(until) {
			if r := processRequest(client, "GET", base+"/api/live", ""); r.err == nil && r.code == 200 {
				return cmd, out, base
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("child did not become live")
		return nil, nil, ""
	}
	stop := func(cmd *exec.Cmd, out *bytes.Buffer) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("graceful child failed: %v (output withheld)", err)
			}
		case <-time.After(4 * time.Second):
			cmd.Process.Kill()
			<-done
			t.Fatal("process shutdown exceeded total grace")
		}
	}
	cmd, out, base := start()
	// Both real requests are held after HTTP admission by an owned SQL lock.
	tx, err := rp.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`LOCK TABLE checkout_attempts IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 6 * time.Second}
	reads, orders := make(chan processResponse, 1), make(chan processResponse, 1)
	go func() { reads <- processRequest(client, "GET", base+"/read", "") }()
	go func() { orders <- processRequest(client, "POST", base+"/api/v1/orders", body) }()
	until := time.Now().Add(3 * time.Second)
	blocked := 0
	for time.Now().Before(until) {
		if err = rp.db.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if blocked < 2 {
		t.Fatal("requests never reached real SQL lock after admission")
	}
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal("send SIGTERM")
	}
	time.Sleep(50 * time.Millisecond)
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	read, order := <-reads, <-orders
	if read.err != nil || read.code != 200 || read.body != "read completed" || order.err != nil || order.code != 201 {
		t.Fatalf("SIGTERM lost admitted read/checkout: read=%d err=%v order=%d err=%v response=%s", read.code, read.err, order.code, order.err, order.body)
	}
	var created struct{ ID string }
	if err = json.Unmarshal([]byte(order.body), &created); err != nil || created.ID == "" {
		t.Fatal("missing durable order response")
	}
	stop(cmd, out)
	if os.Getenv("SELLERSHO_LIFECYCLE_BASELINE") == "1" {
		t.Fatal("baseline unexpectedly survived signal")
	}
	var canceled int
	if err = rp.db.QueryRow(`SELECT count(*) FROM background_job_state WHERE job='ops01-process-sleep' AND owner IS NULL AND last_error='cancelled'`).Scan(&canceled); err != nil || canceled != 1 {
		t.Fatal("manual worker did not cancel and finalize before dependency close", canceled, err)
	}
	after := snapshot()
	if fmt.Sprint(initial) != fmt.Sprint(after) {
		t.Fatal("unpaid checkout changed payment/ledger/stock", initial, after)
	}
	cmd, out, base = start()
	replay := processRequest(client, "POST", base+"/api/v1/orders", body)
	if replay.err != nil || replay.code != 200 || !strings.Contains(replay.body, created.ID) {
		t.Fatal("restart replay lost original order", replay.code, replay.err)
	}
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	stop(cmd, out)
	for _, q := range []string{`SELECT count(*) FROM orders`, `SELECT count(*) FROM order_items`, `SELECT count(*) FROM checkout_attempts`} {
		var n int
		if err = rp.db.QueryRow(q).Scan(&n); err != nil || n != 1 {
			t.Fatal("replay duplicated logical checkout", q, n, err)
		}
	}
	if got := snapshot(); fmt.Sprint(got) != fmt.Sprint(initial) {
		t.Fatal("restart replay changed money or stock", initial, got)
	}
	t.Logf("real SIGTERM completed read+checkout; restart preserved one order/item/attempt; ledger/payment/movement/stock snapshots %v", initial)
}
