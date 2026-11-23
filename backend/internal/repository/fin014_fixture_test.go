package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

// FIN-014 W2 — the realistic legacy fixture (audit §4.2, §2 column (c)).
//
// The loader reads backend/internal/repository/testdata/
// fin014-legacy-fixture.sql, splits it on the "-- FIN014-PART2 --"
// marker, and executes each part in its own transaction (the FIN-005
// deferred consistency triggers read the CURRENT order/dispute state
// at commit time — see the SQL file header).
//
// Every test loads its OWN copy under a unique prefix (default
// "fin014-"), so tests are independent and order-proof. The loader
// rewrites every fin014-/FIN014/Fin014 spelling consistently, and
// takes a distinct settlement cycle period per copy (the cycle period
// is a primary key; the detectors do not depend on it).
//
// Expected report for one copy (asserted by TestFIN014Fixture):
//   fin014- prefix scope: certain 10 / ambiguous 4 / unresolved 4 — 18 rows.
//   class1 payout_oversubscription:     certain 2 (payout-1, settle-1)
//   class2 paid_cancellation:           certain 1 (missing), unresolved 1 (unknown)
//   class3 duplicate_parcel_charge:     certain 3 (parcel-1/2/3)
//   class4 resolved_but_held_dispute:   certain 1 (d4a), ambiguous 1 (d4b)
//   class5 storage_history_gap:         (lot-id scope, not fin014- — see below)
//   class6 coupon_oversubscription:     certain 2 (coupon-1/2)
//   class7 statement_mismatch:          certain 1 (d7), ambiguous 2 (d7b, store-a opening)
//   class8 uncertain_psp_outcome:       ambiguous 1 (simulated), unresolved 3
//
// Class 5 lives outside the fin014- prefix: lot ids are deterministic
// UUIDv5 ('WSP-' + sha1('inbound_qc:<disposition>:<movement>')), so the
// test asserts it through a lot-id-prefixed report call — exactly one
// unresolved row for the unverified-measurement lot, zero rows for the
// verified lot.
//
// Negative controls (must NOT flag): payout-2 (exactly at
// availability), payout-3 (rejected), settle-2 (net == released
// profit), parcel-4 (one debit + matching ledger + partial reversal),
// coupon-3 (used == reservations), c2c (exactly one obligation),
// d4c (resolved + released), c8e (gateway receipt + verification),
// the verified storage lot (history_status 'verified').
//
// Shapes the current schema FORBIDS (documented, not faked): a
// duplicate cancellation obligation (UNIQUE on
// order_refund_obligations), a refund-verdict dispute with no
// obligation (FIN-005 decision_consistency), and duplicate /
// non-positive storage charges (UNIQUE + CHECK) — plus any receipt
// lot or fee charge seeded by plain SQL (the lot insert guard needs a
// full inbound-QC chain; the charge guard needs a VERIFIED lot). The
// detectors still cover those branches for pre-fence legacy data; the
// fixture notes them as unseedable rather than inventing them. The
// lots themselves ARE seeded — by the Go helper
// seedFIN014StorageLots, which replays the real QC chain.
//
// Full-package ordering note: the FIN-013 oracle's coupon check (6)
// is GLOBAL, so a loaded fixture copy makes it count the seeded
// coupon-2 drift. Test files run alphabetically and every fin013_*
// file precedes every fin014_* file, so the oracle test runs before
// any fixture load in a full-package run. The FIN-014 tests themselves
// always scope by prefix.

func fin014FixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := testdb.Open(t, os.Getenv("OPERATIONS_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// loadFIN014Fixture loads one fixture copy. prefix must end with "-"
// (e.g. "f14f-"); cyclePeriod is the settlement cycle period for
// this copy (e.g. "2026-10"). It returns the two storage-lot ids
// built by seedFIN014StorageLots (unresolved, verified).
func loadFIN014Fixture(t *testing.T, db *sql.DB, prefix, cyclePeriod string) (string, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/fin014-legacy-fixture.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(raw), "-- == FIN014 PART 2 ==")
	if len(parts) != 2 {
		t.Fatalf("fin014 fixture: expected exactly 2 parts, got %d", len(parts))
	}
	upper := strings.ToUpper(strings.TrimSuffix(prefix, "-"))
	title := strings.ToUpper(prefix[:1]) + prefix[1:len(prefix)-1]
	rewrite := func(s string) string {
		s = strings.ReplaceAll(s, "fin014-", prefix)
		s = strings.ReplaceAll(s, "FIN014", upper)
		s = strings.ReplaceAll(s, "Fin014", title)
		s = strings.ReplaceAll(s, "'2026-10'", "'"+cyclePeriod+"'")
		return s
	}
	for i, part := range parts {
		if _, err := db.Exec(rewrite(part)); err != nil {
			t.Fatalf("fin014 fixture part %d (prefix %s): %v", i+1, prefix, err)
		}
	}
	// The d7 order carries one order item (merchandise value 400000
	// = its payable): the seller-profit-reversal writer derives the
	// reversal base from order_items (no payable fallback), so an
	// item-less d7 order would make the writer silently no-op. The id
	// is random per load so two fixture copies never collide.
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1, $2, NULL, 1, 0, 0, 400000, 400000)`, uuid.NewString(), prefix+"order-d7"); err != nil {
		t.Fatalf("fin014 fixture order item (prefix %s): %v", prefix, err)
	}
	seedFIN014CouponDrift(t, db, prefix)
	return seedFIN014StorageLots(t, db, prefix)
}

// seedFIN014CouponDrift seeds the coupon-2 drift: used_count (2) above
// its occupying reservations (1). Reservations are immutable (the
// FIN-007 fence forbids DELETE), so the drift is a used_count overcount
// with the deferred count-consistency trigger disabled around the one
// UPDATE and re-enabled immediately after — the same fixture-setup
// precedent as fin006Fixture's policy trigger. It runs as separate
// Execs (not inside the part-2 multi-statement transaction) because
// pending deferred trigger events block ALTER TABLE. The trigger
// still guards every runtime write; the seeded drift is exactly the
// legacy defect under test.
func seedFIN014CouponDrift(t *testing.T, db *sql.DB, prefix string) {
	t.Helper()
	// One transaction: the writer flag is transaction-local (same
	// pattern as the SQL fixture parts), and the trigger disable must
	// precede the UPDATE so no deferred event is pending at ALTER.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`SELECT set_config('sellersho.fin007_writer','v1',true)`, nil},
		{`ALTER TABLE seller_discount_codes DISABLE TRIGGER fin007_count_consistency`, nil},
		{`UPDATE seller_discount_codes SET used_count=2 WHERE id=$1`, []any{prefix + "coupon-2"}},
		{`ALTER TABLE seller_discount_codes ENABLE TRIGGER fin007_count_consistency`, nil},
	} {
		if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("fin014 coupon drift %q: %v", q.sql, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("fin014 coupon drift commit: %v", err)
	}
}

// seedFIN014StorageLots builds the class-5 fixture shapes through the
// real inbound-QC chain (the lot insert guard forbids plain-SQL
// seeding). It mirrors fin006KnownReceipt: shipment -> inventory line
// -> quarantine movement -> QC disposition -> putaway (qc_accepted
// balance) -> acceptance movement -> creditStorageReceiptLotTx.
//
// Chain 1 runs while the fixture product has NO verified measurement,
// so its lot lands with history_status 'unresolved_measurement' — the
// class-5 row. Chain 2 runs after RecordProductMeasurement verifies
// the product, so its lot is 'verified' — the negative control. Lot
// ids are deterministic UUIDv5 (see insertStorageReceiptLotTx), so the
// caller can scope a report call at each lot id exactly.
func seedFIN014StorageLots(t *testing.T, db *sql.DB, prefix string) (unresolvedLot, verifiedLot string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Other tests drop/recreate the measurements view; restore it
	// idempotently before the lot writer reads it.
	createEffectiveMeasurementsView(t, db)
	if _, err := db.ExecContext(ctx, `UPDATE platform_settings SET storage_fee_enabled=TRUE,storage_fee_free_days=30,storage_fee_per_kg_day=2000,storage_fee_per_m3_day=8000 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// The lot guard requires disposition.created_at >= the history
	// policy's effective_at; the policy defaults to the migration
	// time, which postdates the fixture's 2026-10 receipts. Model a
	// known history-policy installation in the past (mirror
	// fin006Fixture): disable the policy-identity trigger around the
	// one UPDATE and re-enable it immediately after.
	if _, err := db.ExecContext(ctx, `ALTER TABLE warehouse_storage_history_policy DISABLE TRIGGER storage_history_policy_identity`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE warehouse_storage_history_policy SET effective_at=$1 WHERE id=1`, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE warehouse_storage_history_policy ENABLE TRIGGER storage_history_policy_identity`); err != nil {
		t.Fatal(err)
	}
	supplier := prefix + "supplier-1"
	product := prefix + "product-1"
	warehouse := prefix + "warehouse-1"
	actor := prefix + "fixture"

	chain := func(suffix string, billing time.Time) string {
		inbound := prefix + "inbound-" + suffix
		line := prefix + "line-" + suffix
		disposition := prefix + "qc-" + suffix
		movTransfer := prefix + "mov-t-" + suffix
		movAccept := prefix + "mov-a-" + suffix
		physical := billing.Add(-time.Hour)
		qty := 10
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err = LockTransactionRowsTx(ctx, tx, &TransactionLockSet{},
			TransactionRowLock{Table: "suppliers", ID: supplier},
			TransactionRowLock{Table: "products", ID: product}); err != nil {
			t.Fatal(err)
		}
		statements := []struct {
			q    string
			args []any
		}{
			{`INSERT INTO inbound_shipments(id,supplier_id,supplier_name,warehouse_id,items_count,cartons_count,status,created_at,received_at) VALUES($1,$2,'fin014 fixture',$3,$4,1,'received',$5,$5)`, []any{inbound, supplier, warehouse, qty, physical}},
			{`INSERT INTO inbound_inventory_lines(id,shipment_id,supplier_id,warehouse_id,product_id,source_kind,external_reference,reason,quantity,dispatched_quantity,received_quantity) VALUES($1,$2,$3,$4,$5,'external_replenishment',$1,'fin014 fixture receipt',$6,$6,$6)`, []any{line, inbound, supplier, warehouse, product, qty}},
			{`INSERT INTO inventory_movements(id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,created_at) VALUES($1,$2,$3,$4,'external','quarantine','external',$5,'inbound_transfer',$6,$1,$7,$8)`, []any{movTransfer, product, supplier, qty, "inbound-line:" + line, line, actor, physical}},
			{`INSERT INTO inbound_qc_dispositions(id,receipt_line_id,cumulative_disposed,received_quantity,quantity_passed,quantity_failed,quantity_shortage,passed_delta,failed_delta,reason,actor_user_id,created_at) VALUES($1,$2,$3,$3,$3,0,0,$3,0,'fin014 fixture acceptance',$4,$5)`, []any{disposition, line, qty, actor, billing}},
			{`INSERT INTO qc_results(id,product_id,inbound_id,inspector_user_id,status,quantity_passed,quantity_failed,created_at) VALUES($1,$2,$3,$4,'completed',$5,0,$6)`, []any{disposition, product, inbound, actor, qty, billing}},
		}
		for _, stmt := range statements {
			if _, err = tx.ExecContext(ctx, stmt.q, stmt.args...); err != nil {
				t.Fatalf("fin014 storage chain %s: %v", suffix, err)
			}
		}
		if err = createInboundPutawayTx(ctx, tx, inboundPersistedLine{ID: line, Shipment: inbound, Supplier: supplier, Warehouse: warehouse, Product: product, Kind: "external_replenishment", Quantity: qty, Dispatched: qty, Received: qty}, disposition, qty, qty); err != nil {
			t.Fatalf("fin014 storage putaway %s: %v", suffix, err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,created_at) VALUES($1,$2,$3,$4,'quarantine','qc_accepted',$5,$6,'inbound_qc_disposition',$7,$1,$8,$9)`, movAccept, product, supplier, qty, "inbound-line:"+line, "qc-accepted:"+disposition, disposition, actor, billing); err != nil {
			t.Fatalf("fin014 storage accept movement %s: %v", suffix, err)
		}
		if err = creditStorageReceiptLotTx(ctx, tx, line, product, supplier, "", warehouse, qty, physical, billing); err != nil {
			t.Fatalf("fin014 storage lot %s: %v", suffix, err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatalf("fin014 storage chain %s commit: %v", suffix, err)
		}
		// Deterministic lot id (insertStorageReceiptLotTx).
		return "WSP-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte("inbound_qc:"+disposition+":"+movTransfer)).String()
	}

	// Chain 1: no verified measurement yet -> unresolved lot.
	unresolvedLot = chain("1", time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC))
	// Chain 2: verify the measurement first -> verified lot (control).
	// The lot guard requires the revision to exist at or before the
	// lot's billing time, so chain 2 bills now, after the measurement.
	if err := NewSupplierRepository(db).RecordProductMeasurement(ctx, product, actor, 100, 100, 100, 500, "fin014 fixture verified measurement"); err != nil {
		t.Fatalf("fin014 storage measurement: %v", err)
	}
	verifiedLot = chain("2", time.Now().UTC())
	return unresolvedLot, verifiedLot
}

// fin014ExpectedCounts is the per-class expected report for one
// fixture copy in the fin014- prefix scope: class ->
// [certain, ambiguous, unresolved]. Class 5 is asserted separately
// by lot-id scope (its lot ids are UUIDv5, not fin014- prefixed).
var fin014ExpectedCounts = map[string][3]int{
	FIN014ClassPayoutOversubscription: {2, 0, 0},
	FIN014ClassPaidCancellation:       {1, 0, 1},
	FIN014ClassDuplicateParcelCharge:  {3, 0, 0},
	FIN014ClassResolvedButHeldDispute: {1, 1, 0},
	FIN014ClassStorageHistoryGap:      {0, 0, 0},
	FIN014ClassCouponOversubscription: {2, 0, 0},
	FIN014ClassStatementMismatch:      {1, 2, 0},
	FIN014ClassUncertainPSPOutcome:    {0, 1, 3},
}

func fin014ReportForFixture(t *testing.T, db *sql.DB, prefix string) *FIN014Report {
	t.Helper()
	report, err := GetFIN014Report(context.Background(), db, FIN014ReportOptions{Limit: 100, IDPrefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// assertFIN014ExpectedCounts checks a report against the fixture's
// expected per-class [certain, ambiguous, unresolved] counts.
func assertFIN014ExpectedCounts(t *testing.T, report *FIN014Report) {
	t.Helper()
	if report.TotalRows != 18 {
		t.Errorf("total rows=%d, want 18", report.TotalRows)
	}
	if report.CertainCount != 10 || report.AmbiguousCount != 4 || report.UnresolvedCount != 4 {
		t.Errorf("certain/ambiguous/unresolved=%d/%d/%d, want 10/4/4", report.CertainCount, report.AmbiguousCount, report.UnresolvedCount)
	}
	got := map[string][3]int{}
	for _, r := range report.Rows {
		c := got[r.Class]
		switch r.Confidence {
		case FIN014ConfidenceCertain:
			c[0]++
		case FIN014ConfidenceAmbiguous:
			c[1]++
		case FIN014ConfidenceUnresolved:
			c[2]++
		default:
			t.Errorf("row %s/%s: unknown confidence %q", r.Class, r.RecordID, r.Confidence)
		}
		got[r.Class] = c
	}
	for class, want := range fin014ExpectedCounts {
		if got[class] != want {
			t.Errorf("class %s: got %v, want %v", class, got[class], want)
		}
	}
	// The FIN-011 asymmetry stays pinned in every report.
	if !report.FIN011Pinned {
		t.Errorf("report must carry the pinned FIN-011 asymmetry (check 7 expected-fail)")
	}
}

func TestFIN014Fixture(t *testing.T) {
	db := fin014FixtureDB(t)
	unresolvedLot, verifiedLot := loadFIN014Fixture(t, db, "f14f-", "2026-10")
	report := fin014ReportForFixture(t, db, "f14f-")
	assertFIN014ExpectedCounts(t, report)

	// Class 5, lot-id scope: the unresolved-measurement lot flags
	// exactly once (unresolved, cash unknown); the verified lot
	// (negative control) flags zero times.
	unresolvedReport := fin014ReportForFixture(t, db, unresolvedLot)
	if unresolvedReport.TotalRows != 1 {
		t.Errorf("unresolved lot report rows=%d, want 1", unresolvedReport.TotalRows)
	} else {
		row := unresolvedReport.Rows[0]
		if row.Class != FIN014ClassStorageHistoryGap || row.Confidence != FIN014ConfidenceUnresolved || row.CashBasis != FIN014CashUnknown {
			t.Errorf("unresolved lot row=%+v, want storage_history_gap/unresolved/unknown", row)
		}
		if row.RecordID != unresolvedLot {
			t.Errorf("unresolved lot record=%q, want %q", row.RecordID, unresolvedLot)
		}
	}
	if verifiedReport := fin014ReportForFixture(t, db, verifiedLot); verifiedReport.TotalRows != 0 {
		t.Errorf("verified lot report rows=%d, want 0", verifiedReport.TotalRows)
	}

	// Spot-check: every expected record ID is present, exactly once.
	wantIDs := map[string]bool{
		"f14f-payout-1": true, "f14f-settle-1": true,
		"f14f-order-c2m": true, "f14f-order-c2u": true,
		"f14f-parcel-1": true, "f14f-parcel-2": true, "f14f-parcel-3": true,
		"f14f-dispute-d4a": true, "f14f-dispute-d4b": true,
		"f14f-coupon-1": true, "f14f-coupon-2": true,
		"f14f-order-d7": true, "f14f-order-d7b": true, "f14f-store-a:2026-10": true,
		"f14f-order-c8a": true, "f14f-order-c8b": true, "f14f-order-c8c": true, "f14f-order-c8d": true,
	}
	seen := map[string]int{}
	for _, r := range report.Rows {
		seen[r.RecordID]++
		if r.CashBasis == "" || r.Explanation == "" {
			t.Errorf("row %s/%s: missing cash_basis or explanation", r.Class, r.RecordID)
		}
		if r.EvidenceTotal == 0 || len(r.Evidence) == 0 {
			t.Errorf("row %s/%s: missing evidence", r.Class, r.RecordID)
		}
	}
	for id := range wantIDs {
		if seen[id] != 1 {
			t.Errorf("expected record %s present %d times, want 1", id, seen[id])
		}
	}
	// Negative controls must NOT flag.
	for _, id := range []string{
		"f14f-payout-2", "f14f-payout-3", "f14f-settle-2",
		"f14f-parcel-4", "f14f-coupon-3", "f14f-order-c2c",
		"f14f-dispute-d4c", "f14f-order-c8e",
	} {
		if seen[id] != 0 {
			t.Errorf("negative control %s flagged %d times, want 0", id, seen[id])
		}
	}
	// Cash-basis honesty: the simulated receipt is never real cash,
	// and no row claims verified cash without a gateway receipt.
	for _, r := range report.Rows {
		if r.RecordID == "f14f-order-c8d" && r.CashBasis != FIN014CashSimulated {
			t.Errorf("simulated order cash_basis=%q, want simulated", r.CashBasis)
		}
		if r.CashBasis == FIN014CashVerifiedGateway {
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM order_payment_receipts WHERE order_id=$1 AND funding_kind='gateway'`, r.OrderID).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if r.OrderID != "" && n == 0 {
				t.Errorf("row %s/%s claims verified_gateway with no gateway receipt", r.Class, r.RecordID)
			}
		}
	}
}
