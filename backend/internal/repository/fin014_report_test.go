package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/lib/pq"
)

// FIN-014 W1 — the dry-run report (read-only) and its supervisor gate:
// two runs must be byte-identical and write nothing (proven by
// per-table row counts + id-list hashes before/after), and cursor
// paging must visit every row exactly once with stable totals.

var fin014ReadOnlyTables = []struct{ table, key string }{
	{"orders", "id"}, {"payment_transactions", "id"},
	{"order_payment_verifications", "id"}, {"order_payment_receipts", "id"},
	{"order_refund_obligations", "id"}, {"order_refund_events", "id"},
	{"ledger_entries", "id"}, {"payout_requests", "id"},
	{"monthly_settlement_cycles", "period"}, {"monthly_settlement_items", "id"},
	{"supplier_return_requests", "id"}, {"supplier_return_parcels", "id"},
	{"supplier_return_financial_events", "id"},
	{"seller_discount_codes", "id"}, {"order_coupon_reservations", "order_id"},
	{"warehouse_storage_positions", "id"}, {"warehouse_storage_receipt_lots", "id"},
	{"warehouse_storage_fee_charges", "id"},
	{"disputes", "id"}, {"dispute_order_evidence", "dispute_id"},
	{"dispute_financial_decisions", "dispute_id"}, {"audit_logs", "id"},
}

func fin014TableFingerprints(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, tbl := range fin014ReadOnlyTables {
		var count int
		var hash sql.NullString
		q := "SELECT COUNT(*), md5(COALESCE(string_agg(" + tbl.key + "::text, ',' ORDER BY " + tbl.key + "::text),'')) FROM " + tbl.table
		if err := db.QueryRow(q).Scan(&count, &hash); err != nil {
			t.Fatalf("fingerprint %s: %v", tbl.table, err)
		}
		out[tbl.table] = fmt.Sprintf("%d:%s", count, hash.String)
	}
	return out
}

func TestFIN014Report(t *testing.T) {
	db := fin014FixtureDB(t)
	loadFIN014Fixture(t, db, "f14r-", "2026-09")
	ctx := context.Background()

	before := fin014TableFingerprints(t, db)
	first, err := GetFIN014Report(ctx, db, FIN014ReportOptions{Limit: 100, IDPrefix: "f14r-"})
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := FIN014ReportJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	mid := fin014TableFingerprints(t, db)
	second, err := GetFIN014Report(ctx, db, FIN014ReportOptions{Limit: 100, IDPrefix: "f14r-"})
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := FIN014ReportJSON(second)
	if err != nil {
		t.Fatal(err)
	}
	after := fin014TableFingerprints(t, db)

	// Read-only: identical bytes, zero writes.
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("report is not repeatable: two runs differ:\n%s\n---\n%s", firstJSON, secondJSON)
	}
	for tbl, h := range before {
		if mid[tbl] != h || after[tbl] != h {
			t.Errorf("table %s changed across report runs: before=%s mid=%s after=%s", tbl, h, mid[tbl], after[tbl])
		}
	}
	assertFIN014ExpectedCounts(t, first)
	if first.SchemaVersion < 212 {
		t.Errorf("schema version=%d, want >= 212 (all migrations applied)", first.SchemaVersion)
	}

	// Cursor paging: limit 5 walks all 18 rows exactly once, totals stable.
	seen := map[string]int{}
	cursor := ""
	pages := 0
	for {
		page, err := GetFIN014Report(ctx, db, FIN014ReportOptions{Limit: 5, Cursor: cursor, IDPrefix: "f14r-"})
		if err != nil {
			t.Fatal(err)
		}
		if page.TotalRows != 18 || page.CertainCount != 10 {
			t.Fatalf("paged report totals drifted: total=%d certain=%d", page.TotalRows, page.CertainCount)
		}
		for _, r := range page.Rows {
			seen[r.Class+"/"+r.RecordID]++
		}
		pages++
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pages > 10 {
			t.Fatalf("paging did not terminate")
		}
	}
	if pages != 4 {
		t.Errorf("pages=%d, want 4 (18 rows at limit 5)", pages)
	}
	if len(seen) != 18 {
		t.Errorf("paged rows seen=%d distinct, want 18", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("row %s visited %d times, want 1", id, n)
		}
	}

	// Bad cursor fails honestly instead of silently returning a page.
	if _, err := GetFIN014Report(ctx, db, FIN014ReportOptions{Cursor: "!!!not-base64"}); err == nil {
		t.Errorf("invalid cursor accepted, want an error")
	}
}

// TestFIN014ReportSQLMirror proves scripts/fin014-report.sql agrees
// with the Go detectors on the same fixture: same rows (class,
// record, confidence, cash basis), same totals, and the same
// lot-id-scoped class-5 row. If the two ever disagree, the Go report
// wins and the SQL file must be updated in the same change.
func TestFIN014ReportSQLMirror(t *testing.T) {
	db := fin014FixtureDB(t)
	unresolvedLot, _ := loadFIN014Fixture(t, db, "f14s-", "2026-08")
	goReport := fin014ReportForFixture(t, db, "f14s-")
	assertFIN014ExpectedCounts(t, goReport)

	raw, err := os.ReadFile("../../../scripts/fin014-report.sql")
	if err != nil {
		t.Fatal(err)
	}
	runSQL := func(prefix string) map[string][3]string {
		t.Helper()
		var stmt strings.Builder
		inGuard := false
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, `\`) || strings.HasPrefix(trimmed, "--") {
				continue
			}
			if strings.HasPrefix(trimmed, "DO $$") {
				inGuard = true
				continue
			}
			if inGuard {
				if strings.HasPrefix(trimmed, "END $$;") {
					inGuard = false
				}
				continue
			}
			stmt.WriteString(line + "\n")
		}
		q := strings.ReplaceAll(stmt.String(), ":'prefix'", "'"+prefix+"'")
		rows, err := db.Query(q)
		if err != nil {
			t.Fatalf("fin014-report.sql (prefix %q): %v", prefix, err)
		}
		defer rows.Close()
		out := map[string][3]string{}
		for rows.Next() {
			var kind, class, recordID, recordKind string
			var storeID, orderID sql.NullString
			var amount sql.NullInt64
			var cashBasis, confidence, explanation string
			var evidence []string
			if err := rows.Scan(&kind, &class, &recordID, &recordKind, &storeID, &orderID, &amount, &cashBasis, &confidence, &explanation, pq.Array(&evidence)); err != nil {
				t.Fatalf("fin014-report.sql scan: %v", err)
			}
			if kind == "row" {
				out[class+"/"+recordID] = [3]string{confidence, cashBasis, recordKind}
			}
			if kind == "total" {
				out["total/"+class] = [3]string{recordID, "", ""}
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("fin014-report.sql rows: %v", err)
		}
		return out
	}
	sqlRows := runSQL("f14s-")
	if len(sqlRows) != len(goReport.Rows)+4 {
		t.Fatalf("SQL mirror rows=%d, want %d (18 detail + 4 totals)", len(sqlRows), len(goReport.Rows)+4)
	}
	for _, r := range goReport.Rows {
		got, ok := sqlRows[r.Class+"/"+r.RecordID]
		if !ok {
			t.Errorf("SQL mirror missing row %s/%s", r.Class, r.RecordID)
			continue
		}
		if got[0] != r.Confidence || got[1] != r.CashBasis || got[2] != r.RecordKind {
			t.Errorf("SQL mirror row %s/%s = %v, want confidence=%s cash=%s kind=%s", r.Class, r.RecordID, got, r.Confidence, r.CashBasis, r.RecordKind)
		}
	}
	for _, want := range [][2]string{
		{"TOTAL", "18"}, {"certain", "10"}, {"ambiguous", "4"}, {"unresolved", "4"},
	} {
		if got, ok := sqlRows["total/"+want[0]]; !ok || got[0] != want[1] {
			t.Errorf("SQL mirror total %s = %v, want %s", want[0], got, want[1])
		}
	}
	// Lot-id scope: exactly the unresolved lot row, nothing else.
	lotRows := runSQL(unresolvedLot)
	if len(lotRows) != 1+4 {
		t.Fatalf("SQL mirror lot scope rows=%d, want 5 (1 detail + 4 totals)", len(lotRows))
	}
	if got, ok := lotRows[FIN014ClassStorageHistoryGap+"/"+unresolvedLot]; !ok || got[0] != FIN014ConfidenceUnresolved {
		t.Errorf("SQL mirror lot row = %v, want storage_history_gap/unresolved", got)
	}
}
