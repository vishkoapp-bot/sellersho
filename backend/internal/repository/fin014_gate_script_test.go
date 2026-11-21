package repository

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// FIN-014 W8 — the release gate script and the snapshot scrub script
// are tested here by deliberate fail and pass, on synthetic data.
//
// Gate script contract (same as the BAK-01 restore checks): exit
// non-zero on failure, no success marker on failure; a stale marker
// is deleted at the start of every run.

func fin014ScriptPath(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(wd, "..", "..", "..", "scripts", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("script %s not found: %v", p, err)
	}
	return p
}

func fin014RunScript(t *testing.T, script string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	// The scripts shell out to the extracted psql, which needs the
	// extracted libpq (rootfs psql has no libpq.so.5 after the VM
	// replacement). Pass it explicitly — the go test process does
	// not reliably carry LD_LIBRARY_PATH into children.
	env := os.Environ()
	libpq := filepath.Join(os.Getenv("HOME"), "workspace", ".dev", "libpq-tmp", "usr", "lib", "x86_64-linux-gnu")
	env = append(env, "LD_LIBRARY_PATH="+libpq+":"+os.Getenv("LD_LIBRARY_PATH"))
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("running %s: %v", script, err)
		}
	}
	return string(out), code
}

func TestFIN014ReleaseGateScript(t *testing.T) {
	db := fin014FixtureDB(t)
	loadFIN014Fixture(t, db, "f14h-", "2026-10")
	report := fin014ReportForFixture(t, db, "f14h-")
	reportJSON, err := FIN014ReportJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	reportFile := filepath.Join(t.TempDir(), "fin014-report.json")
	if err := os.WriteFile(reportFile, reportJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	script := fin014ScriptPath(t, "fin014-release-gate.sh")
	marker := filepath.Join(t.TempDir(), "gate-marker.json")

	// Deliberate fail: the fixture has 10 certain defects and the
	// fail-closed default threshold is 0 — non-zero exit, no marker.
	out, code := fin014RunScript(t, script, "--report", reportFile, "--marker", marker)
	if code == 0 {
		t.Fatalf("gate passed on a report with 10 certain defects (default threshold 0); output:\n%s", out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("gate wrote a success marker on failure")
	}
	// A stale marker must not survive a failing run.
	if err := os.WriteFile(marker, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, code := fin014RunScript(t, script, "--report", reportFile, "--marker", marker); code == 0 {
		t.Fatalf("gate passed on a report with 10 certain defects (stale-marker run)")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("gate left a stale success marker standing after a failing run")
	}
	// Explicit override: the threshold is a parameter, not a default.
	out, code = fin014RunScript(t, script, "--report", reportFile, "--max-certain", "10", "--marker", marker)
	if code != 0 {
		t.Fatalf("gate failed with --max-certain 10 on a report with 10 certain defects; output:\n%s", out)
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("gate did not write the success marker on pass: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("success marker is not valid JSON: %v", err)
	}
	if m["certainCount"] != float64(10) || m["maxCertain"] != float64(10) || m["result"] != "pass" {
		t.Errorf("success marker contents unexpected: %s", raw)
	}
	if m["fin011Pinned"] != true {
		t.Errorf("success marker fin011Pinned=%v, want true (the pin must be recorded)", m["fin011Pinned"])
	}

	// Clean control: a prefix with no rows reports zero certain
	// defects and passes with the default threshold.
	clean := fin014ReportForFixture(t, db, "f14none-")
	if clean.CertainCount != 0 || clean.TotalRows != 0 {
		t.Fatalf("clean-control report certain=%d total=%d, want 0/0", clean.CertainCount, clean.TotalRows)
	}
	cleanJSON, err := FIN014ReportJSON(clean)
	if err != nil {
		t.Fatal(err)
	}
	cleanFile := filepath.Join(t.TempDir(), "fin014-report-clean.json")
	if err := os.WriteFile(cleanFile, cleanJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	cleanMarker := filepath.Join(t.TempDir(), "gate-marker-clean.json")
	if out, code := fin014RunScript(t, script, "--report", cleanFile, "--marker", cleanMarker); code != 0 {
		t.Fatalf("gate failed on the clean control fixture; output:\n%s", out)
	}
	if _, err := os.Stat(cleanMarker); err != nil {
		t.Errorf("gate did not write the success marker on the clean control: %v", err)
	}
}

func TestFIN014SnapshotScrubScript(t *testing.T) {
	db := fin014FixtureDB(t)
	loadFIN014Fixture(t, db, "f14x-", "2026-10")
	psql := filepath.Join(os.Getenv("HOME"), "workspace", ".dev", "psql-tmp", "usr", "lib", "postgresql", "16", "bin", "psql")
	if _, err := os.Stat(psql); err != nil {
		t.Skipf("psql not available at %s: %v", psql, err)
	}
	rawURL := os.Getenv("OPERATIONS_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("OPERATIONS_TEST_DATABASE_URL is not set")
	}
	script := fin014ScriptPath(t, "fin014-snapshot-scrub.sh")
	ctx := context.Background()
	_ = ctx

	// Dry run first: reports what it would scrub, changes nothing.
	before := fin014TableFingerprints(t, db)
	out, code := fin014RunScript(t, script, "--db-url", rawURL, "--skip-restore", "--dry-run", "--id-prefix", "f14x-", "--psql", psql)
	if code != 0 {
		t.Fatalf("scrub dry run failed; output:\n%s", out)
	}
	for _, want := range []string{"WOULD SCRUB: orders.customer_phone", "WOULD SCRUB: payout_requests.shaba", "WOULD SCRUB: users.mobile"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	afterDry := fin014TableFingerprints(t, db)
	for tbl, h := range before {
		if afterDry[tbl] != h {
			t.Errorf("table %s changed during the dry run", tbl)
		}
	}

	// Real scrub, scoped to this copy: PII goes, counts stay, the
	// manifest is written, and other copies are untouched.
	var ordersBefore int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orders WHERE id LIKE 'f14x-%'`).Scan(&ordersBefore); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(t.TempDir(), "scrub-manifest.json")
	out, code = fin014RunScript(t, script, "--db-url", rawURL, "--skip-restore", "--id-prefix", "f14x-", "--manifest", manifest, "--psql", psql)
	if code != 0 {
		t.Fatalf("scrub run failed; output:\n%s", out)
	}
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("scrub did not write the manifest: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("scrub manifest is not valid JSON: %v", err)
	}
	if m["result"] != "pass" {
		t.Errorf("scrub manifest result=%v, want pass: %s", m["result"], raw)
	}
	var phone, shaba string
	if err := db.QueryRow(`SELECT customer_phone FROM orders WHERE id='f14x-order-d7'`).Scan(&phone); err != nil || phone != "00000000000" {
		t.Errorf("scrubbed d7 phone=%q err=%v, want 00000000000", phone, err)
	}
	if err := db.QueryRow(`SELECT shaba FROM payout_requests WHERE id='f14x-payout-1'`).Scan(&shaba); err != nil || shaba != "SCRUBBED-IBAN" {
		t.Errorf("scrubbed payout shaba=%q err=%v, want SCRUBBED-IBAN", shaba, err)
	}
	var ordersAfter int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orders WHERE id LIKE 'f14x-%'`).Scan(&ordersAfter); err != nil || ordersAfter != ordersBefore {
		t.Errorf("orders count changed by the scrub: before=%d after=%d err=%v", ordersBefore, ordersAfter, err)
	}
	// Other copies untouched: the f14h- copy (loaded by the gate
	// test earlier in this file) still carries its synthetic phone.
	var otherPhone string
	if err := db.QueryRow(`SELECT customer_phone FROM orders WHERE id='f14h-order-d7'`).Scan(&otherPhone); err != nil || otherPhone != "09000000000" {
		t.Errorf("other copy's phone=%q err=%v, want 09000000000 (scrub must stay in scope)", otherPhone, err)
	}
}
