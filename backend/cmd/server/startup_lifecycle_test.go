package main

import (
	"context"
	"github.com/drup/platform/internal/testdb"
	"github.com/drup/platform/internal/testredis"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLifecycleStartupHelper(t *testing.T) {
	if os.Getenv("SELLERSHO_STARTUP_HELPER") != "1" {
		return
	}
	main()
}

func startupEnvironment(dsn, redisURL string) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "SystemRoot=" + os.Getenv("SystemRoot"), "APP_ENV=test", "SELLERSHO_STARTUP_HELPER=1", "DATABASE_URL=" + dsn, "REDIS_URL=" + redisURL, "STARTUP_TIMEOUT_SECONDS=2", "SHUTDOWN_GRACE_SECONDS=1", "PAYMENT_GATEWAY_MODE=mock", "MIGRATIONS_DIR=../../migrations"}
}

func TestStartupConfigAndMigrationSubprocessFailures(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, override, diagnostic string }{{"config", "SHUTDOWN_GRACE_SECONDS=secret-marker", "SHUTDOWN_GRACE_SECONDS"}, {"missing migrations", "MIGRATIONS_DIR=" + filepath.Join(t.TempDir(), "missing"), "migration validation failed"}, {"redis", "REDIS_URL=redis://:secret-marker@127.0.0.1:1/15", "Redis connection failed"}} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLifecycleStartupHelper$")
			cmd.Env = append(startupEnvironment(os.Getenv("SELLERSHO_TEST_DATABASE_URL"), "redis://127.0.0.1:1/15"), c.override)
			out, err := cmd.CombinedOutput()
			s := string(out)
			if err == nil || ctx.Err() != nil || strings.Contains(s, "panic:") || strings.Contains(s, "secret-marker") || !strings.Contains(s, c.diagnostic) {
				t.Fatalf("unsafe startup failure: %v %s", err, s)
			}
		})
	}
	// An actually held migration singleton must inherit startup cancellation.
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(context.Background(), `SELECT pg_advisory_lock(23438617066426957)`); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock_all()`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLifecycleStartupHelper$")
	cmd.Env = append(startupEnvironment(os.Getenv("SELLERSHO_TEST_DATABASE_URL"), "redis://127.0.0.1:1/15"), "MIGRATIONS_DIR=../../migrations", "STARTUP_TIMEOUT_SECONDS=1")
	started := time.Now()
	out, err := cmd.CombinedOutput()
	if err == nil || ctx.Err() != nil || time.Since(started) > 3*time.Second || !strings.Contains(string(out), "migration validation failed") {
		t.Fatalf("migration lock did not obey startup deadline: %v %s", err, out)
	}

}

func TestStartupBindFailureClosesDependenciesWithoutStartingWorker(t *testing.T) {
	assertStartupBindFailure(t, false)
}

func TestStartupMetricsBindFailureClosesDependenciesWithoutStartingWorker(t *testing.T) {
	assertStartupBindFailure(t, true)
}

func assertStartupBindFailure(t *testing.T, metrics bool) {
	t.Helper()
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("fixture URL")
	}
	q := u.Query()
	q.Set("application_name", "ops01_bind_failure")
	u.RawQuery = q.Encode()
	for _, pair := range []struct{ key, value string }{{"APP_ENV", "test"}, {"DATABASE_URL", u.String()}, {"REDIS_URL", os.Getenv("SELLERSHO_TEST_REDIS_URL")}, {"STARTUP_TIMEOUT_SECONDS", "3"}, {"SHUTDOWN_GRACE_SECONDS", "1"}, {"MIGRATIONS_DIR", "../../migrations"}} {
		t.Setenv(pair.key, pair.value)
	}
	bindAddress := ":0"
	if metrics {
		bindAddress = "127.0.0.1:9090"
	}
	listener, err := net.Listen("tcp", bindAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	if metrics {
		port = "0"
	}
	t.Setenv("PORT", port)
	var before, after int64
	if err = db.QueryRow(`SELECT COALESCE(sum(runs),0) FROM background_job_state`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	diagnostic := "HTTP listener bind failed"
	if metrics {
		diagnostic = "metrics listener bind failed"
	}
	if err = run(ctx); err == nil || !strings.Contains(err.Error(), diagnostic) {
		t.Fatal("bind failure", err)
	}
	if err = db.QueryRow(`SELECT COALESCE(sum(runs),0) FROM background_job_state`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("worker started before successful bind")
	}
	var sessions int
	if err = db.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE application_name='ops01_bind_failure'`).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatal("startup leaked SQL resources", sessions, err)
	}
}

func TestRequiredDatabaseFailureDoesNotContinueOrPanic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLifecycleStartupHelper$")
	// Build a minimal safe environment rather than inherit application credentials.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "SystemRoot=" + os.Getenv("SystemRoot"), "APP_ENV=test", "SELLERSHO_STARTUP_HELPER=1", "DATABASE_URL=postgres://fixture:withheld-marker@127.0.0.1:1/sellersho_test_unreachable?sslmode=disable", "REDIS_URL=redis://127.0.0.1:1/15", "STARTUP_TIMEOUT_SECONDS=1"}
	out, err := cmd.CombinedOutput()
	if err == nil || ctx.Err() != nil {
		t.Fatalf("required startup failure was not bounded/nonzero: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "panic:") || strings.Contains(s, "withheld-marker") || strings.Contains(s, "[WARN] PostgreSQL") {
		t.Fatalf("startup continued unsafely or exposed details: %s", s)
	}
	if !strings.Contains(s, "PostgreSQL") {
		t.Fatalf("missing actionable dependency diagnostic: %s", s)
	}
}
