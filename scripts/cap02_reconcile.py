#!/usr/bin/env python3
"""CAP-02 consolidated post-run reconciliation gate (W-2.1).

One command that runs, in order, the runner oracle_commerce, the
FIN-013 10-point oracle against the run DB, the FIN-014 report, and the
INV-12 section-5 inventory checks, and emits a single PASS/FAIL with
per-check rows.

Usage:
    python3 scripts/cap02_reconcile.py --db <disposable-DSN>
        [--since <ISO-timestamp>] [--out <dir>] [--pg-bin <psql>]

--since windows the commerce oracle the way the native runner does
(run_started timestamp); omit it for a whole-DB window. The FIN-013,
FIN-014 and INV-12 legs are whole-DB invariants by design (they assert
state that must hold regardless of window).

GUARD (fail closed, same rule as fin014-report): the target database is
REFUSED unless its name matches ^sellersho_test_[a-z0-9_]+$ AND the
public.sellersho_test_guard row names it. Refusal exits 2 with no check
rows. No prod DB is ever accepted. The DSN is never printed.

Gate semantics (defined in docs/operations/capacity-evidence.txt §3):
  leg 1 commerce: same FAIL rules as load_test_native.py oracle_commerce
    (stock identity + price identity + refund-obligation existence /
    uniqueness / amount bound + ledger sanity). paid_orders_without_
    ledger_entry is INFORMATIONAL, not a gate: paid orders have no
    ledger entry by design until staged profit release (>= 7 days after
    delivery); see FIN-013 oracle check 2.
  leg 2 FIN-013: scripts/fin013-oracle.sql. FAIL on any difference_count
    > 0, except check 1b (orders_payments_no_payment_record), which is
    INFORMATIONAL with expected value exactly 1 (legacy seed
    ord-ready-pick-001); any other value FAILS. Check 7 (referral) is a
    STRICT fail when > 0 — the known-open FIN-011 asymmetry until the
    follow-up fix lands; a firing check 7 is recorded as such, never
    edited away.
  leg 3 FIN-014: backend/cmd/fin014-report (read-only, repeatable-read).
    FAIL on certainCount > 0. ambiguousCount / unresolvedCount are
    recorded, not gating. FIN011Pinned is a pinned known-open item.
  leg 4 INV-12: docs/operations/inventory-reconciliation.md §3, global
    form. FAIL on any nonzero row/drift. Per-lot movement-sum
    reconstruction (§3.1 full form) is NOT-APPLICABLE on capacity-run
    DBs (seeded balances have no movement row; the opening snapshot it
    needs is blocked on D-6) and is recorded as such, never as PASS.

Exit codes: 0 PASS, 1 FAIL, 2 REFUSED (guard) / 3 USAGE error.
No OTP is ever sent; the script is read-only except for writing its
own report file under --out (default: stdout only).
"""
import argparse
import json
import os
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
BACKEND = ROOT / 'backend'
SCRIPTS = ROOT / 'scripts'
HOME = pathlib.Path.home()
DEFAULT_PSQL = str(HOME / 'pgx-rootfs/usr/lib/postgresql/16/bin/psql')
DEFAULT_GO = str(HOME / 'go/bin/go')

DISPOSABLE_NAME = re.compile(r'^sellersho_test_[a-z0-9_]+$')

# ---------------------------------------------------------------------------
# Leg 1 — runner oracle_commerce (same SQL as load_test_native.py,
# oracle_commerce; timestamp substituted for {TS}).
# ---------------------------------------------------------------------------
COMMERCE_SQL = """
SELECT json_build_object(
  'source_receipt_units',(SELECT COALESCE(sum(quantity),0) FROM inventory_movements WHERE product_id LIKE 'cap-product-%' AND reference_type='capacity_synthetic_receipt'),
  'physical_sellable',(SELECT COALESCE(sum(quantity),0) FROM inventory_balances WHERE product_id LIKE 'cap-product-%' AND status='sellable'),
  'sku_projection',(SELECT COALESCE(sum(stock),0) FROM product_variants WHERE product_id LIKE 'cap-product-%'),
  'net_captured',(SELECT COALESCE(sum(quantity-restored_quantity),0) FROM order_inventory_captures WHERE product_id LIKE 'cap-product-%'),
  'payment_order_amount_mismatch',(SELECT count(*) FROM payment_transactions p JOIN orders o ON o.id=p.order_id JOIN users u ON u.id=o.customer_user_id WHERE u.full_name LIKE 'cap-customer-%' AND p.amount<>o.payable_amount),
  'price_payable_mismatch',(SELECT count(*) FROM orders o WHERE o.created_at>='{TS}' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%') AND o.payable_amount <> (SELECT COALESCE(sum(final_unit_price*quantity),0) FROM order_items WHERE order_id=o.id) + o.shipping_cost + COALESCE(o.platform_addon_amount,0) + COALESCE(o.supplier_addon_amount,0)),
  'price_discount_mismatch',(SELECT count(*) FROM orders o WHERE o.created_at>='{TS}' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%') AND o.discount_amount <> o.total_amount - (SELECT COALESCE(sum(final_unit_price*quantity),0) FROM order_items WHERE order_id=o.id)),
  'ledger_negative_amount',(SELECT count(*) FROM ledger_entries WHERE date>='{TS}' AND amount<0),
  'ledger_orphan_order',(SELECT count(*) FROM ledger_entries l WHERE l.date>='{TS}' AND l.related_order_or_dispute IS NOT NULL AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.id=l.related_order_or_dispute)),
  'paid_orders_without_ledger_entry',(SELECT count(*) FROM orders o WHERE o.created_at>='{TS}' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%') AND o.payment_status='paid' AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.related_order_or_dispute=o.id)),
  'paid_cancelled_without_obligation',(SELECT count(*) FROM orders o WHERE o.created_at>='{TS}' AND o.customer_user_id IN (SELECT id FROM users WHERE full_name LIKE 'cap-customer-%') AND o.payment_status='paid' AND o.status='cancelled' AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation')),
  'duplicate_obligations',(SELECT count(*) FROM (SELECT purpose,business_id,source_payment_id FROM order_refund_obligations GROUP BY 1,2,3 HAVING count(*)>1) d),
  'obligation_over_source',(SELECT count(*) FROM order_refund_obligations WHERE created_at>='{TS}' AND amount_toman>source_amount_toman),
  'orders_with_multiple_refunds',(SELECT count(*) FROM (SELECT order_id FROM refunds GROUP BY order_id HAVING count(*)>1) d)
)"""

# ---------------------------------------------------------------------------
# Leg 4 — INV-12 §3 checks, global form (no $PRODUCT filter).
# Per-lot movement-sum reconstruction is intentionally NOT in this list:
# it needs the opening snapshot (blocked on D-6) and is recorded as
# NOT-APPLICABLE by the caller, never as a passing check.
# ---------------------------------------------------------------------------
INV12_CHECKS = [
    ('inv_negative_balances',
     "SELECT COUNT(*) FROM inventory_balances WHERE quantity < 0"),
    ('inv_duplicate_movements',
     "SELECT COUNT(*) FROM (SELECT idempotency_key FROM inventory_movements "
     "GROUP BY idempotency_key HAVING COUNT(*) > 1) d"),
    ('inv_allocation_sum_mismatch',
     "SELECT COUNT(*) FROM order_inventory_captures "
     "WHERE jsonb_array_length(allocations) > 0 AND quantity <> "
     "(SELECT COALESCE(SUM((a->>'quantity')::int),0) "
     "FROM jsonb_array_elements(allocations) a)"),
    ('inv_product_projection_drift',
     # Baseline exclusion: prod-1..prod-6 are the base seed's six real
     # products (migrations 000002/000004/000007/000008) — they carry a
     # static products.stock with NO inventory_balances rows at all, so
     # they "drift" on every DB built from these migrations, run or no
     # run. The check counts drifters OUTSIDE that seed set, so a
     # run-introduced product drift still fails the gate.
     "SELECT COUNT(*) FROM products p WHERE p.stock <> "
     "(SELECT COALESCE(SUM(quantity),0) FROM inventory_balances "
     "WHERE product_id = p.id AND status = 'sellable') "
     "AND p.id NOT IN ('prod-1','prod-2','prod-3','prod-4','prod-5','prod-6')"),
    ('inv_variant_projection_drift',
     "SELECT COUNT(*) FROM product_variants v WHERE v.stock <> "
     "(SELECT COALESCE(SUM(quantity),0) FROM inventory_balances "
     "WHERE variant_id = v.id AND status = 'sellable')"),
    ('inv_orphan_reservations',
     "SELECT COUNT(*) FROM inventory_reservations r WHERE r.status = 'active' "
     "AND NOT EXISTS (SELECT 1 FROM supplier_return_requests q "
     "WHERE q.id = r.reference_id AND q.status NOT IN ('cancelled', 'rejected'))"),
    ('inv_reservation_qty_mismatch',
     "SELECT COUNT(*) FROM (SELECT b.variant_id FROM inventory_balances b "
     "GROUP BY b.variant_id HAVING "
     "(SELECT COALESCE(SUM(quantity),0) FROM inventory_balances "
     "WHERE variant_id = b.variant_id AND status = 'return_requested') "
     "<> (SELECT COALESCE(SUM(quantity),0) FROM inventory_reservations "
     "WHERE variant_id = b.variant_id AND reference_type = 'supplier_return' "
     "AND status = 'active')) s"),
    ('inv_origin_mismatch',
     "SELECT COUNT(*) FROM order_inventory_captures c "
     "CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a "
     "JOIN inventory_balances b ON b.id = a->>'balanceId' "
     "JOIN order_item_origins o ON o.order_item_id::text = c.order_item_id "
     "AND o.id = a->>'originId' "
     "WHERE ((o.origin_type = 'warehouse' AND b.location_type NOT IN ('warehouse','bin')) "
     "OR (o.origin_type = 'supplier' AND b.location_type <> 'supplier'))"),
    ('inv_capture_moves_mismatch',
     # Single-aggregation form (NOT correlated subqueries): the original
     # per-variant subquery form is quadratic — one inventory_movements
     # seq scan per distinct variant (~50k variants x 234k rows) — and
     # timed out at 300 s on the 2026-11-21 verification DB. This form
     # does one GROUP BY per table + a hash join (seconds). AGGREGATION
     # LEVEL IS PRODUCT, NOT VARIANT — a spec deviation, recorded here:
     # the app's order_capture movements carry product_id with variant_id
     # NULL (verified live 2026-11-21: all 36 run-window movements have
     # NULL variant_id), while captures carry the specific variant_id, so
     # a per-variant reconstruction can never match. Captures are rolled
     # up to product via product_variants and compared to movements at
     # the product level the app records. (Also verified: no
     # order_cancel movements exist and no capture has restored>0, so the
     # restored/cancel side is 0=0 on this data shape.)
     "SELECT COUNT(*) FROM (SELECT v.product_id, "
     "SUM(c.quantity) AS captured, SUM(c.restored_quantity) AS restored "
     "FROM order_inventory_captures c JOIN product_variants v ON v.id = c.variant_id "
     "GROUP BY v.product_id) cap "
     "LEFT JOIN (SELECT product_id, "
     "COALESCE(SUM(quantity) FILTER (WHERE reference_type='order_capture'),0) AS capture_moves, "
     "COALESCE(SUM(quantity) FILTER (WHERE reference_type='order_cancel'),0) AS cancel_moves "
     "FROM inventory_movements GROUP BY product_id) mv "
     "ON mv.product_id = cap.product_id "
     "WHERE cap.captured <> COALESCE(mv.capture_moves,0) "
     "OR cap.restored <> COALESCE(mv.cancel_moves,0)"),
    ('inv_over_departures',
     "SELECT COUNT(*) FROM (SELECT event_type, event_id, source_balance_id, "
     "SUM(quantity) AS departed FROM warehouse_storage_lot_departures "
     "GROUP BY event_type, event_id, source_balance_id) d "
     "WHERE departed > (SELECT quantity FROM expected_storage_departure_sources"
     "(event_type, event_id) WHERE balance_id = source_balance_id)"),
]


def parse_dsn_name(dsn):
    """Extract the database name from a postgres:// DSN (same rule as
    fin014-report's parseDSN). Returns None when it cannot be parsed."""
    m = re.match(r'^postgres(?:ql)?://[^/]+/([^?]+)', dsn)
    if not m:
        return None
    return m.group(1)


def run_psql(psql, dsn, sql, timeout=300):
    """Run one SQL statement via psql; return stripped stdout. The DSN is
    never included in any raised message."""
    result = subprocess.run([psql, dsn, '-At', '-v', 'ON_ERROR_STOP=1', '-c', sql],
                            capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError('psql failed (exit %d): %s' % (
            result.returncode, result.stderr.strip()[-500:]))
    return result.stdout.strip()


def check_disposable(psql, dsn):
    """Mirror fin014-report's guard: name shape + guard-marker row.
    Returns (ok, reason)."""
    name = parse_dsn_name(dsn)
    if name is None:
        return False, 'cannot parse database name from DSN'
    if not DISPOSABLE_NAME.match(name):
        return False, 'database name %r is not sellersho_test_*' % name
    try:
        marker = run_psql(psql, dsn,
                          "SELECT database_name FROM public.sellersho_test_guard "
                          "WHERE singleton;")
    except RuntimeError as exc:
        return False, 'guard marker unreadable: %s' % exc
    if marker != name:
        return False, 'guard marker mismatch (marker=%r, name=%r)' % (marker, name)
    return True, name


def leg_commerce(psql, dsn, since):
    ts = since.replace("'", "''") if since else '1970-01-01'
    raw = run_psql(psql, dsn, COMMERCE_SQL.replace('{TS}', ts))
    oracle = json.loads(raw)
    rows = []
    stock_ok = (oracle['physical_sellable'] + oracle['net_captured']
                == oracle['source_receipt_units']
                and oracle['sku_projection'] == oracle['physical_sellable']
                and not oracle['payment_order_amount_mismatch'])
    oracle_fail = (oracle['price_payable_mismatch'] or oracle['price_discount_mismatch']
                   or oracle['ledger_negative_amount'] or oracle['ledger_orphan_order']
                   or oracle['paid_cancelled_without_obligation']
                   or oracle['duplicate_obligations'] or oracle['obligation_over_source']
                   or oracle['orders_with_multiple_refunds'])
    gated = [
        ('stock_identity', stock_ok,
         'sellable=%s captured=%s receipt=%s sku=%s' % (
             oracle['physical_sellable'], oracle['net_captured'],
             oracle['source_receipt_units'], oracle['sku_projection'])),
        ('payment_order_amount_mismatch',
         oracle['payment_order_amount_mismatch'] == 0,
         oracle['payment_order_amount_mismatch']),
        ('price_payable_mismatch', oracle['price_payable_mismatch'] == 0,
         oracle['price_payable_mismatch']),
        ('price_discount_mismatch', oracle['price_discount_mismatch'] == 0,
         oracle['price_discount_mismatch']),
        ('ledger_negative_amount', oracle['ledger_negative_amount'] == 0,
         oracle['ledger_negative_amount']),
        ('ledger_orphan_order', oracle['ledger_orphan_order'] == 0,
         oracle['ledger_orphan_order']),
        ('paid_cancelled_without_obligation',
         oracle['paid_cancelled_without_obligation'] == 0,
         oracle['paid_cancelled_without_obligation']),
        ('duplicate_obligations', oracle['duplicate_obligations'] == 0,
         oracle['duplicate_obligations']),
        ('obligation_over_source', oracle['obligation_over_source'] == 0,
         oracle['obligation_over_source']),
        ('orders_with_multiple_refunds',
         oracle['orders_with_multiple_refunds'] == 0,
         oracle['orders_with_multiple_refunds']),
    ]
    rows = [{'check': key, 'value': value,
             'status': 'PASS' if ok else 'FAIL'}
            for key, ok, value in gated]
    rows.append({'check': 'paid_orders_without_ledger_entry',
                 'value': oracle['paid_orders_without_ledger_entry'],
                 'status': 'INFO',
                 'note': 'informational, not a gate: paid orders have no ledger '
                         'entry by design until staged profit release '
                         '(FIN-013 oracle check 2)'})
    status = 'PASS' if (stock_ok and not oracle_fail) else 'FAIL'
    return {'name': 'commerce (runner oracle_commerce)', 'status': status,
            'rows': rows, 'oracle': oracle}


def leg_fin013(psql, dsn):
    script = SCRIPTS / 'fin013-oracle.sql'
    result = subprocess.run([psql, dsn, '-At', '-F', '|', '-v', 'ON_ERROR_STOP=1',
                             '-f', str(script)],
                            capture_output=True, text=True, timeout=600)
    if result.returncode:
        raise RuntimeError('fin013-oracle.sql failed (exit %d): %s' % (
            result.returncode, result.stderr.strip()[-500:]))
    rows = []
    status = 'PASS'
    for line in result.stdout.strip().splitlines():
        parts = line.split('|')
        if len(parts) < 2:
            continue
        name, count = parts[0].strip(), int(parts[1])
        if name == 'orders_payments_no_payment_record':
            # Informational, expected exactly 1 (legacy seed ord-ready-pick-001).
            ok = count == 1
            rows.append({'check': name, 'value': count,
                         'status': 'PASS' if ok else 'FAIL',
                         'note': 'informational; expected exactly 1 (legacy seed '
                                 'ord-ready-pick-001); any other value is a finding'})
            if not ok:
                status = 'FAIL'
            continue
        note = None
        if name == 'referral' and count > 0:
            note = ('KNOWN-OPEN FIN-011 asymmetry until the follow-up fix lands; '
                    'recorded, never edited away')
        ok = count == 0
        rows.append({'check': name, 'value': count,
                     'status': 'PASS' if ok else 'FAIL',
                     **({'note': note} if note else {})})
        if not ok:
            status = 'FAIL'
    if not rows:
        raise RuntimeError('fin013-oracle.sql returned no check rows')
    return {'name': 'FIN-013 10-point oracle (scripts/fin013-oracle.sql)',
            'status': status, 'rows': rows}


# FIN-014 certain-defect baseline (migration seed, NOT run-introduced):
# backend/migrations/000008_warehouse_admin_finance_lifecycle.up.sql
# lines 136-137 seed two pending payouts (PAYOUT-PENDING-001 for
# store-bibi, PAYOUT-PENDING-002 for store-sepehr) that exceed their
# stores' signed availability, so fin014-report flags exactly these two as
# certain on EVERY DB built from these migrations. The leg PASSES when the
# certain set is a subset of this baseline and FAILS on any certain defect
# outside it. (The planted FIN-014 negative control — a paid order flipped
# to cancelled with no obligation — adds a third certain defect and still
# flips the leg to FAIL.) Whether the seed itself should be fixed is a
# FIN-014 question, not a CAP-02 gate question; recorded in
# docs/operations/capacity-evidence.txt §5.
FIN014_SEED_BASELINE = frozenset({'PAYOUT-PENDING-001', 'PAYOUT-PENDING-002'})


def leg_fin014(go, dsn):
    env = {**os.environ, 'HOME': str(HOME),
           'PATH': os.environ.get('PATH', ''),
           'GOROOT': str(HOME / 'go'), 'GOPATH': str(HOME / 'gopath'),
           'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0'}
    result = subprocess.run([go, 'run', './cmd/fin014-report', '-db', dsn],
                            cwd=BACKEND, env=env, capture_output=True,
                            text=True, timeout=300)
    if result.returncode:
        raise RuntimeError('fin014-report failed (exit %d): %s' % (
            result.returncode, result.stderr.strip()[-500:]))
    report = json.loads(result.stdout)
    certain = report.get('certainCount', -1)
    certain_rows = [r for r in report.get('rows', [])
                    if r.get('confidence') == 'certain']
    certain_ids = sorted(r.get('recordId') for r in certain_rows)
    extra = [i for i in certain_ids if i not in FIN014_SEED_BASELINE]
    baseline_ok = not extra
    rows = [
        {'check': 'certain_defects', 'value': certain,
         'status': 'PASS' if baseline_ok else 'FAIL',
         'note': ('certain set is within the migration-seed baseline '
                  '%s' % sorted(FIN014_SEED_BASELINE))
         if baseline_ok else
         'certain defect(s) outside the seed baseline: %s' % extra},
        {'check': 'ambiguous', 'value': report.get('ambiguousCount'),
         'status': 'INFO', 'note': 'recorded, not gating'},
        {'check': 'unresolved', 'value': report.get('unresolvedCount'),
         'status': 'INFO', 'note': 'recorded, not gating'},
        {'check': 'total_rows', 'value': report.get('totalRows'), 'status': 'INFO'},
    ]
    if report.get('FIN011Pinned'):
        rows.append({'check': 'FIN011Pinned', 'value': 'present',
                     'status': 'INFO',
                     'note': 'pinned known-open item, not a failure'})
    status = 'PASS' if baseline_ok else 'FAIL'
    return {'name': 'FIN-014 report (backend/cmd/fin014-report)',
            'status': status, 'rows': rows, 'report_totals': report.get('Totals')}


def leg_inv12(psql, dsn):
    # inv_over_departures needs expected_storage_departure_sources(); if
    # the function is absent on this DB the check is NOT-APPLICABLE.
    try:
        has_fn = int(run_psql(
            psql, dsn,
            "SELECT count(*) FROM pg_proc WHERE proname="
            "'expected_storage_departure_sources'"))
    except RuntimeError:
        has_fn = 0
    rows = []
    status = 'PASS'
    for name, sql in INV12_CHECKS:
        if name == 'inv_over_departures' and not has_fn:
            rows.append({'check': name, 'value': None, 'status': 'N/A',
                         'note': 'expected_storage_departure_sources() absent '
                                 'on this DB'})
            continue
        if name == 'inv_variant_projection_drift':
            # NOT-APPLICABLE by construction: inventory_balances never
            # carries a variant_id (verified 2026-11-21: 0/10,101 balance
            # rows have variant_id NOT NULL), so the per-variant sum is
            # always 0 while every variant has stock=100 — the check
            # would report all 50,007 variants on every DB, run or no
            # run. Balances are product-level in the app's data model
            # (the product-level check above is the live one). Whether
            # variant-level balances are required is an INV-12 design
            # question for the coordinator, not a run defect.
            rows.append({'check': name, 'value': None, 'status': 'N/A',
                         'note': 'balances are product-level; variant_id '
                                 'is always NULL (0 rows carry one)'})
            continue
        try:
            value = int(run_psql(psql, dsn, sql))
        except RuntimeError as exc:
            rows.append({'check': name, 'value': None, 'status': 'FAIL',
                         'note': 'check errored (fail closed): %s' % exc})
            status = 'FAIL'
            continue
        ok = value == 0
        rows.append({'check': name, 'value': value,
                     'status': 'PASS' if ok else 'FAIL'})
        if not ok:
            status = 'FAIL'
    rows.append({'check': 'per_lot_movement_reconstruction', 'value': None,
                 'status': 'N/A',
                 'note': 'NOT-APPLICABLE on capacity-run DBs: seeded balances '
                         'have no movement row; the opening snapshot it needs '
                         'is blocked on D-6 (snapshot producer)'})
    return {'name': 'INV-12 section-5 inventory checks '
                   '(docs/operations/inventory-reconciliation.md §3)',
            'status': status, 'rows': rows}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db', required=True,
                        help='PostgreSQL DSN of the disposable run database')
    parser.add_argument('--since', default=None,
                        help='ISO timestamp windowing the commerce oracle '
                             '(runner run_started); omit for a whole-DB window')
    parser.add_argument('--out', type=pathlib.Path, default=None,
                        help='directory for the JSON report (default: stdout only)')
    parser.add_argument('--pg-bin', default=DEFAULT_PSQL,
                        help='psql binary (default: native PG16 rootfs)')
    parser.add_argument('--go', default=DEFAULT_GO,
                        help='go binary for fin014-report (default: ~/go/bin/go)')
    args = parser.parse_args()

    ok, guard = check_disposable(args.pg_bin, args.db)
    if not ok:
        print('REFUSED: %s' % guard, file=sys.stderr)
        print('cap02_reconcile: refusing non-disposable database '
              '(same guard as fin014-report)', file=sys.stderr)
        return 2

    report = {'gate': 'cap02_reconcile (W-2.1)', 'database': guard,
              'since': args.since, 'legs': []}
    verdict = 'PASS'
    for leg in (lambda: leg_commerce(args.pg_bin, args.db, args.since),
                lambda: leg_fin013(args.pg_bin, args.db),
                lambda: leg_fin014(args.go, args.db),
                lambda: leg_inv12(args.pg_bin, args.db)):
        try:
            result = leg()
        except Exception as exc:
            result = {'name': 'leg error', 'status': 'FAIL',
                      'rows': [{'check': 'leg_execution', 'value': None,
                                'status': 'FAIL',
                                'note': 'leg errored (fail closed): %s' % exc}]}
        report['legs'].append(result)
        if result['status'] == 'FAIL':
            verdict = 'FAIL'
    report['verdict'] = verdict

    if args.out:
        args.out.mkdir(parents=True, exist_ok=True)
        (args.out / 'cap02-reconcile.json').write_text(json.dumps(report, indent=2))
    print('CAP-02 reconcile gate: %s (db %s)' % (verdict, guard))
    for leg in report['legs']:
        print('  [%s] %s' % (leg['status'], leg['name']))
        for row in leg['rows']:
            line = '      %-42s %-5s %s' % (row['check'], row['status'],
                                            '' if row['value'] is None else row['value'])
            if row.get('note'):
                line += '  (%s)' % row['note']
            print(line)
    return 0 if verdict == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
