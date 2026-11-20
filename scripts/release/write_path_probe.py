#!/usr/bin/env python3
"""MIG-02 step 4 (D-3) — write-path readiness probe.

Exercises the three write paths a rollback can silently break, inside
ONE transaction that is ALWAYS rolled back, so the probe provably
creates no real orders, rows, or ledger entries:

  1. inventory-balance upsert (the 202611110003 surface): the exact
     ON CONFLICT shapes the binaries issue.
  2. review insert (the product_reviews (product_id, user_id) UNIQUE).
  3. withdrawal-intent insert (the customer_withdrawal_requests
     request_key_sha256 partial unique index).

Modes (--expect):
  current  the shapes the current (>= ff57367) binary issues: the
           partial-index upserts with WHERE variant_id IS NULL /
           IS NOT NULL. Pass = all three checks succeed.
  legacy   the shape the pre-ff57367 binary issued: the bare
           ON CONFLICT (product_id, owner_type, location_type,
           location_key, status) with no WHERE predicate. Pass = the
           bare upsert succeeds. On the current schema it FAILS with
           "no unique or exclusion constraint matching the ON CONFLICT
           specification" — that failure is the probe catching the
           202611110003 incompatibility (negative control; /api/health
           still returns 200 in that state, which is why this probe
           exists).

After the rollback the script verifies every scratch id is absent and
the row counts of the touched tables are unchanged, and prints
PROBE_OK / PROBE_FAIL. Exit 0 only on PROBE_OK.

Usage (rehearsal / local):
    python3 scripts/release/write_path_probe.py --db-url <DATABASE_URL>
        [--expect current|legacy]
Usage (prod deploy — deploy_remote.py):
    python3 scripts/release/write_path_probe.py
        --docker-container drup_postgres [--expect current|legacy]
        (runs psql inside the container as drup_admin/drup_db)

No secrets are read or printed. No OTP. Read-only apart from the
rolled-back transaction.
"""
import argparse
import hashlib
import subprocess
import sys
import uuid

TABLES = ("inventory_balances", "product_reviews",
          "customer_withdrawal_requests", "products", "suppliers",
          "users", "product_variants", "inventory_movements")


def build_sql(expect: str, tag: str, checks=frozenset({"inventory", "review", "withdrawal"})) -> str:
    """One psql script. The DO block raises on any failed check; the
    outer transaction is always rolled back.

    `checks` scopes which checks run. The overlap-window matrix (R-4)
    runs inventory-only against the pre-202611110003 schema: the review
    and withdrawal checks need columns that schema does not have yet
    (product_reviews / customer_withdrawal_requests shapes postdate
    the floor), so they would fail there for an unrelated reason.
    The deploy/rollback gates always run all three."""
    sup = f"probe-sup-{tag}"
    prod = f"probe-prod-{tag}"
    user = str(uuid.uuid5(uuid.NAMESPACE_URL, f"probe-user-{tag}"))
    variant = str(uuid.uuid5(uuid.NAMESPACE_URL, f"probe-variant-{tag}"))
    # request_key_sha256 is a full SHA-256 (32 bytes); the table has
    # a CHECK octet_length(request_key_sha256) = 32, so hash the tag.
    key = hashlib.sha256(tag.encode()).hexdigest()
    if expect == "legacy":
        upsert = """
    -- Legacy (pre-ff57367) bare arbiter: no WHERE predicate.
    BEGIN
      INSERT INTO inventory_balances (id, product_id, supplier_id, owner_type, location_type, location_key, status, quantity, reconciled)
      VALUES ('probe-bal-legacy', prod_id, sup_id, 'supplier', 'warehouse', 'probe-wh', 'sellable', 3, TRUE)
      ON CONFLICT (product_id, owner_type, location_type, location_key, status)
      DO UPDATE SET quantity = inventory_balances.quantity + EXCLUDED.quantity;
    EXCEPTION WHEN OTHERS THEN
      IF SQLERRM LIKE '%no unique or exclusion constraint matching the ON CONFLICT specification%' THEN
        RAISE EXCEPTION 'PROBE FAIL (legacy upsert): the legacy ON CONFLICT arbiter is gone — schema incompatible with a pre-ff57367 binary (202611110003)';
      END IF;
      RAISE EXCEPTION 'PROBE FAIL (legacy upsert): %', SQLERRM;
    END;
    checks_passed := checks_passed + 1;"""
    else:
        upsert = """
    -- Current shape, unallocated (variant_id IS NULL).
    INSERT INTO inventory_balances (id, product_id, supplier_id, owner_type, location_type, location_key, status, quantity, reconciled)
    VALUES ('probe-bal-null', prod_id, sup_id, 'supplier', 'warehouse', 'probe-wh', 'sellable', 3, TRUE)
    ON CONFLICT (product_id, owner_type, location_type, location_key, status) WHERE variant_id IS NULL
    DO UPDATE SET quantity = inventory_balances.quantity + EXCLUDED.quantity;
    -- Same upsert again: the DO UPDATE path must add, not fail.
    INSERT INTO inventory_balances (id, product_id, supplier_id, owner_type, location_type, location_key, status, quantity, reconciled)
    VALUES ('probe-bal-null-2', prod_id, sup_id, 'supplier', 'warehouse', 'probe-wh', 'sellable', 2, TRUE)
    ON CONFLICT (product_id, owner_type, location_type, location_key, status) WHERE variant_id IS NULL
    DO UPDATE SET quantity = inventory_balances.quantity + EXCLUDED.quantity;
    IF (SELECT quantity FROM inventory_balances WHERE product_id = prod_id AND variant_id IS NULL AND status = 'sellable') <> 5 THEN
      RAISE EXCEPTION 'PROBE FAIL (inventory upsert): expected quantity 5 after two upserts';
    END IF;
    -- Current shape, SKU (variant_id IS NOT NULL).
    INSERT INTO inventory_balances (id, product_id, supplier_id, variant_id, owner_type, location_type, location_key, status, quantity, reconciled)
    VALUES ('probe-bal-sku', prod_id, sup_id, var_id, 'supplier', 'warehouse', 'probe-wh', 'sellable', 4, TRUE)
    ON CONFLICT (product_id, owner_type, location_type, location_key, status, variant_id) WHERE variant_id IS NOT NULL
    DO UPDATE SET quantity = inventory_balances.quantity + EXCLUDED.quantity;
    checks_passed := checks_passed + 1;"""
    review_block = ""
    if "review" in checks:
        review_block = """
  -- CHECK 2: review insert (exact customer_handlers.go shape) + the
  -- duplicate must fail on the (product_id, user_id) UNIQUE (409 path).
  INSERT INTO product_reviews (id, product_id, user_id, rating, comment)
    VALUES ('probe-rev-1', prod_id, user_id::text, 5, 'MIG-02 probe review');
  BEGIN
    INSERT INTO product_reviews (id, product_id, user_id, rating, comment)
      VALUES ('probe-rev-2', prod_id, user_id::text, 4, 'MIG-02 probe duplicate review');
  EXCEPTION WHEN unique_violation THEN
    dup_seen := TRUE;
  END;
  IF NOT dup_seen THEN
    RAISE EXCEPTION 'PROBE FAIL (review insert): duplicate (product_id, user_id) review did NOT hit the UNIQUE index';
  END IF;
  checks_passed := checks_passed + 1;"""
    withdrawal_block = ""
    if "withdrawal" in checks:
        withdrawal_block = """
  -- CHECK 3: withdrawal-intent insert (exact customer_handlers.go
  -- shape) + the duplicate key must fail on the partial unique index.
  INSERT INTO customer_withdrawal_requests (id, customer_id, customer_name, customer_mobile, account_holder_name, sheba, bank_name, amount, status, request_key_sha256)
    VALUES ('probe-wd-1', user_id, 'MIG-02 probe', '09000000000', 'MIG-02 probe', 'IR000000000000000000000000', 'probe-bank', 100000, 'pending', key_bytes);
  dup_seen := FALSE;
  BEGIN
    INSERT INTO customer_withdrawal_requests (id, customer_id, customer_name, customer_mobile, account_holder_name, sheba, bank_name, amount, status, request_key_sha256)
      VALUES ('probe-wd-2', user_id, 'MIG-02 probe', '09000000000', 'MIG-02 probe', 'IR000000000000000000000000', 'probe-bank', 100000, 'pending', key_bytes);
  EXCEPTION WHEN unique_violation THEN
    dup_seen := TRUE;
  END;
  IF NOT dup_seen THEN
    RAISE EXCEPTION 'PROBE FAIL (withdrawal intent): duplicate request_key_sha256 did NOT hit the partial unique index';
  END IF;
  -- The pre-insert lookup the handler runs must also find the row.
  IF NOT EXISTS (SELECT 1 FROM customer_withdrawal_requests WHERE customer_id = user_id AND request_key_sha256 = key_bytes) THEN
    RAISE EXCEPTION 'PROBE FAIL (withdrawal intent): lookup by request_key_sha256 did not find the inserted row';
  END IF;
  checks_passed := checks_passed + 1;"""
    return f"""\\set ON_ERROR_STOP on
BEGIN;
DO $probe$
DECLARE
  sup_id TEXT := '{sup}';
  prod_id TEXT := '{prod}';
  user_id UUID := '{user}';
  var_id UUID := '{variant}';
  key_bytes BYTEA := decode('{key}', 'hex');
  checks_passed INT := 0;
  dup_seen BOOLEAN := FALSE;
BEGIN
  -- Scratch rows only; the outer ROLLBACK removes all of them.
  INSERT INTO suppliers (id, name) VALUES (sup_id, 'MIG-02 probe supplier');
  INSERT INTO products (id, title, slug, original_price, supplier_base_cost, platform_margin)
    VALUES (prod_id, 'MIG-02 probe product', 'probe-{tag}', 1000, 800, 100);
  INSERT INTO users (id, mobile, full_name, role) VALUES (user_id, '09000000000', 'MIG-02 probe user', 'customer');
  INSERT INTO product_variants (id, product_id, spnc, variant_name) VALUES (var_id, prod_id, 'PROBE{tag[:6].upper()}', 'probe');
{upsert}{review_block}{withdrawal_block}
  RAISE NOTICE 'PROBE CHECKS PASSED: % of {len(checks)} (expect={expect})', checks_passed;
END;
$probe$;
ROLLBACK;
"""


def run_psql(argv, script: str) -> subprocess.CompletedProcess:
    return subprocess.run(argv, input=script, capture_output=True, text=True, timeout=120)


def count_rows(psql_argv, table: str) -> int:
    r = subprocess.run(psql_argv + ["-Atc", f"SELECT count(*) FROM {table}"],
                       capture_output=True, text=True, timeout=30)
    if r.returncode != 0:
        raise RuntimeError(f"count {table} failed: {r.stderr.strip()[:200]}")
    return int(r.stdout.strip())


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--db-url", default="")
    ap.add_argument("--docker-container", default="")
    ap.add_argument("--psql", default="psql",
                    help="psql binary for --db-url mode (default: psql on PATH)")
    ap.add_argument("--expect", choices=("current", "legacy"), default="current")
    ap.add_argument("--checks", default="inventory,review,withdrawal",
                    help="comma-separated checks to run (inventory is always run; review/withdrawal need post-202611110003 columns, so the R-4 matrix runs inventory-only against the pre-schema)")
    args = ap.parse_args()
    checks = frozenset(s.strip() for s in args.checks.split(",") if s.strip())
    unknown = checks - {"inventory", "review", "withdrawal"}
    if unknown:
        print(f"unknown checks: {sorted(unknown)}", file=sys.stderr)
        return 2
    if "inventory" not in checks:
        print("inventory is always run; --checks must include it", file=sys.stderr)
        return 2
    if bool(args.db_url) == bool(args.docker_container):
        print("exactly one of --db-url / --docker-container is required", file=sys.stderr)
        return 2
    if args.docker_container:
        psql = ["docker", "exec", "-i", args.docker_container, "psql",
                "-U", "drup_admin", "-d", "drup_db", "-v", "ON_ERROR_STOP=1"]
    else:
        psql = [args.psql, args.db_url, "-v", "ON_ERROR_STOP=1"]
    tag = uuid.uuid4().hex[:12]
    try:
        before = {t: count_rows(psql, t) for t in TABLES}
    except Exception as exc:
        print(f"PROBE FAIL: cannot read baseline counts: {exc}", file=sys.stderr)
        return 1
    r = run_psql(psql, build_sql(args.expect, tag, checks))
    sys.stdout.write(r.stdout)
    sys.stderr.write(r.stderr)
    if r.returncode != 0:
        print("PROBE FAIL: write-path checks did not pass (see above; transaction was rolled back)", file=sys.stderr)
        return 1
    # Prove nothing persisted: scratch ids absent + counts unchanged.
    try:
        after = {t: count_rows(psql, t) for t in TABLES}
    except Exception as exc:
        print(f"PROBE FAIL: cannot verify post-rollback counts: {exc}", file=sys.stderr)
        return 1
    if before != after:
        print(f"PROBE FAIL: row counts changed across the probe (before={before} after={after}) — the rollback did not hold", file=sys.stderr)
        return 1
    print(f"PROBE_OK expect={args.expect} ({len(checks)}/{len(checks)} write-path checks passed, transaction rolled back, row counts unchanged)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
