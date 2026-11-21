#!/usr/bin/env bash
# FIN-014 snapshot producer — scrub script (Q1).
#
# Procedure (runbook §8.6): backup -> restore to a disposable database
# -> scrub via the named column transforms below -> verify row counts
# per table, no PII patterns remain, and the oracle runs as expected
# -> hash + manifest.
#
# The agent that built this script CANNOT produce the snapshot itself
# (no production data is read). The producer — whoever holds the
# backup — runs this on the production host under separate
# authorization (see MEMORY: FIN-014 Q1), and confirms the column
# list covers every PII-bearing column before running it on a real
# backup.
#
# Usage:
#   scripts/fin014-snapshot-scrub.sh --db-url <postgres-url> --dump <file>
#       [--dry-run] [--skip-restore] [--id-prefix <p>]
#       [--manifest <path>] [--psql <path>]
#
#   --db-url     required: the target database URL. The database name
#                MUST be sellersho_test_* (disposable guard) — the
#                script refuses anything else.
#   --dump       the pg_dump file to restore (plain SQL or custom
#                format). Not needed with --skip-restore.
#   --dry-run    report what would be scrubbed (per-transform row
#                counts + the columns the transforms touch) WITHOUT
#                restoring or updating anything. Run this on synthetic
#                data first.
#   --skip-restore the target database is already populated (e.g. a
#                rehearsal on synthetic data); scrub + verify only.
#   --id-prefix  optional scope for rehearsals: only rows whose id
#                starts with this prefix are scrubbed / verified.
#                The production run omits it and scrubs everything.
#   --manifest   optional: path of the JSON manifest (dump SHA-256,
#                row counts, transforms applied, verification result).
#                Written only when verification passes.
#   --psql       psql binary (default: psql on PATH).
#
# Scrubbed replacements are deliberately non-matching placeholders:
# phones become 00000000000, IBANs become SCRUBBED-IBAN, names become
# 'Scrubbed ...' — none of them matches the PII patterns the
# verification scan looks for, so a surviving pattern is a real leak.
# Supplier names become a stable pseudonym (supplier-<md5>) so joins
# and distinctness survive while the identity does not. Credential
# columns (any column named like %token% / %secret% / %password% /
# %api_key%) are nulled where they hold platform credentials
# (suppliers.tapin_token, platform_settings.tapin_platform_token);
# any other credential-named column is reported, not touched — see
# the sweep section below for why (e.g.
# payment_transactions.gateway_token is immutable captured payment
# identity).
#
# Exit code: 0 when the scrub + verification pass (manifest written,
# if requested); 1 otherwise (no manifest).
set -euo pipefail

DB_URL=""
DUMP=""
DRY_RUN=0
SKIP_RESTORE=0
ID_PREFIX=""
MANIFEST=""
PSQL="psql"

while [ $# -gt 0 ]; do
  case "$1" in
    --db-url) DB_URL="$2"; shift 2 ;;
    --dump) DUMP="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --skip-restore) SKIP_RESTORE=1; shift ;;
    --id-prefix) ID_PREFIX="$2"; shift 2 ;;
    --manifest) MANIFEST="$2"; shift 2 ;;
    --psql) PSQL="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,45p' "$0"
      exit 0 ;;
    *) echo "fin014-snapshot-scrub: unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$DB_URL" ]; then
  echo "fin014-snapshot-scrub: --db-url is required" >&2; exit 2
fi
if [ "$SKIP_RESTORE" -eq 0 ] && [ -z "$DUMP" ]; then
  echo "fin014-snapshot-scrub: --dump is required (or pass --skip-restore)" >&2; exit 2
fi
if [ "$SKIP_RESTORE" -eq 0 ] && [ ! -f "$DUMP" ]; then
  echo "fin014-snapshot-scrub: dump not found: $DUMP" >&2; exit 2
fi

# Disposable guard: refuse anything that is not a marked
# sellersho_test_* database. The name comes from the URL path.
DB_NAME="${DB_URL##*/}"; DB_NAME="${DB_NAME%%\?*}"
case "$DB_NAME" in
  sellersho_test_*) ;;
  *) echo "fin014-snapshot-scrub: refusing target: database name must be sellersho_test_*, got: $DB_NAME" >&2; exit 2 ;;
esac
if ! "$PSQL" "$DB_URL" -tAc "SELECT 1 FROM public.sellersho_test_guard WHERE singleton AND database_name = current_database()" | grep -q 1; then
  echo "fin014-snapshot-scrub: refusing target: disposable guard marker missing for $DB_NAME" >&2; exit 2
fi

# Named column transforms: table|column|replacement-SQL-expression.
# Each runs only when the column exists (checked first); a missing
# column is reported, not fatal — the verification scan is the
# backstop that catches PII surviving under an unlisted column.
TRANSFORMS=$(cat <<'EOF'
users|mobile|'00000000000'
users|full_name|'Scrubbed User'
orders|customer_full_name|'Scrubbed Customer'
orders|customer_phone|'00000000000'
orders|customer_address|'Scrubbed Address'
orders|customer_postal_code|'0000000000'
disputes|customer_name|'Scrubbed Customer'
stores|owner_name|'Scrubbed Owner'
payout_requests|recipient_name|'Scrubbed Recipient'
payout_requests|shaba|'SCRUBBED-IBAN'
suppliers|name|'supplier-' || substr(md5(id), 1, 8)
EOF
)

scope_where() {
  # $1 = table name. Returns a WHERE fragment limiting the
  # transform/verification to the rehearsal prefix. id is cast to
  # text so the scope works for UUID ids as well as VARCHAR ids.
  local tbl="$1"
  if [ -z "$ID_PREFIX" ]; then
    echo "TRUE"
  else
    echo "id::text LIKE '${ID_PREFIX}%'"
  fi
}

if [ "$DRY_RUN" -eq 1 ]; then
  echo "fin014-snapshot-scrub: DRY RUN on $DB_NAME (nothing will be restored or updated)"
  echo "$TRANSFORMS" | while IFS='|' read -r tbl col repl; do
    [ -z "$tbl" ] && continue
    exists=$("$PSQL" "$DB_URL" -tAc "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='$tbl' AND column_name='$col'")
    if [ "$exists" = "0" ]; then
      echo "  SKIP (column missing): $tbl.$col"
      continue
    fi
    n=$("$PSQL" "$DB_URL" -tAc "SELECT COUNT(*) FROM $tbl WHERE $(scope_where "$tbl")")
    echo "  WOULD SCRUB: $tbl.$col ($n rows in scope)"
  done
  creds=$("$PSQL" "$DB_URL" -tAc "SELECT table_name||'.'||column_name FROM information_schema.columns WHERE table_schema='public' AND (column_name ILIKE '%token%' OR column_name ILIKE '%secret%' OR column_name ILIKE '%password%' OR column_name ILIKE '%api_key%') AND NOT (table_name='suppliers' AND column_name='tapin_token') AND NOT (table_name='platform_settings' AND column_name='tapin_platform_token') ORDER BY 1")
  echo "  WOULD NULL credential columns: suppliers.tapin_token, platform_settings.tapin_platform_token (when present)"
  if [ -n "$creds" ]; then
    echo "  WOULD REPORT (kept, for producer review) credential-named columns:"
    echo "$creds" | sed 's/^/    /'
  else
    echo "  No other credential-named columns found."
  fi
  echo "fin014-snapshot-scrub: dry run complete (no changes made)"
  exit 0
fi

# Counts snapshot lives in a real (non-temp) table: each psql call
# below is its own session, so a TEMP table would not survive.
"$PSQL" "$DB_URL" -v ON_ERROR_STOP=1 -c "DROP TABLE IF EXISTS public.fin014_scrub_counts_before" >/dev/null

if [ "$SKIP_RESTORE" -eq 0 ]; then
  echo "fin014-snapshot-scrub: restoring $DUMP into $DB_NAME ..."
  if head -c 5 "$DUMP" | grep -q PGDMP; then
    command -v pg_restore >/dev/null || { echo "fin014-snapshot-scrub: pg_restore not found (custom-format dump)" >&2; exit 2; }
    pg_restore -d "$DB_URL" --no-owner --no-privileges "$DUMP"
  else
    "$PSQL" "$DB_URL" -v ON_ERROR_STOP=1 -f "$DUMP" >/dev/null
  fi
fi

# Row counts per table, before the scrub (exact counts, captured
# into a snapshot table — pg_stat_user_tables can lag).
"$PSQL" "$DB_URL" -v ON_ERROR_STOP=1 -c "
CREATE TABLE public.fin014_scrub_counts_before (tbl text PRIMARY KEY, rows bigint NOT NULL);
" >/dev/null
"$PSQL" "$DB_URL" -v ON_ERROR_STOP=1 -c "
DO \$\$
DECLARE r record; q text; n bigint;
BEGIN
  FOR r IN SELECT table_name FROM information_schema.tables
           WHERE table_schema='public' AND table_type='BASE TABLE'
           AND table_name <> 'fin014_scrub_counts_before' -- the snapshot must not count itself
  LOOP
    q := format('SELECT count(*) FROM %I', r.table_name);
    EXECUTE q INTO n;
    INSERT INTO public.fin014_scrub_counts_before VALUES ('public.' || r.table_name, n);
  END LOOP;
END \$\$;
" >/dev/null

echo "fin014-snapshot-scrub: scrubbing ..."
# The scrub rewrites PII columns only — but the deferred FIN-007
# order-consistency trigger re-validates coupon consistency at commit
# for ANY order update, and legacy rows can fail re-validation for
# reasons unrelated to the scrub (e.g. a drifted legacy coupon's
# used_count — itself a FIN-014 defect, not the scrub's job to fix).
# So the script disables exactly that one trigger around the scrub
# (same precedent as the W2 fixture's coupon-drift seeding, which
# disables fin007_count_consistency) and re-enables it immediately
# after — a trap re-enables it even if the script dies mid-scrub.
# Every other trigger keeps validating: if another one fails, the
# script fails loudly (fail-closed) and the producer investigates.
# The scrub is re-runnable: a failed run leaves partial scrubbing
# (constant replacements), and a re-run finishes it.
TRIGGER_DISABLED=0
re_enable_trigger() {
  if [ "$TRIGGER_DISABLED" = "1" ]; then
    "$PSQL" "$DB_URL" -v ON_ERROR_STOP=1 -c "ALTER TABLE orders ENABLE TRIGGER fin007_order_consistency" >/dev/null 2>&1 || true
    TRIGGER_DISABLED=0
  fi
}
trap re_enable_trigger EXIT
if [ "$("$PSQL" "$DB_URL" -tAc "SELECT COUNT(*) FROM pg_trigger WHERE tgname='fin007_order_consistency'")" = "1" ]; then
  "$PSQL" "$DB_URL" -v ON_ERROR_STOP=1 -c "ALTER TABLE orders DISABLE TRIGGER fin007_order_consistency" >/dev/null
  TRIGGER_DISABLED=1
  echo "  (disabled fin007_order_consistency for the scrub; re-enabled afterwards)"
fi
echo "$TRANSFORMS" | while IFS='|' read -r tbl col repl; do
  [ -z "$tbl" ] && continue
  exists=$("$PSQL" "$DB_URL" -tAc "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='$tbl' AND column_name='$col'")
  if [ "$exists" = "0" ]; then
    echo "  SKIP (column missing): $tbl.$col"
    continue
  fi
  "$PSQL" "$DB_URL" -v ON_ERROR_STOP=1 -c "UPDATE $tbl SET $col = $repl WHERE $(scope_where "$tbl")" >/dev/null
  echo "  scrubbed: $tbl.$col"
done
# Credential sweep: NULL the KNOWN platform-credential columns
# (Tapin tokens are stored plaintext — SEC-01 — and must never
# reach a scrubbed snapshot). Any OTHER credential-named column is
# REPORTED, not touched: payment_transactions.gateway_token, for
# example, is captured payment identity — immutable by design
# (guard payment_refund_source_identity) and a gateway-issued
# reference, not a platform credential; nulling it would also break
# the payment↔receipt linkage the reconciliation reads. The
# producer reviews the reported list before releasing the snapshot.
CRED_COLUMNS=$(cat <<'EOF'
suppliers|tapin_token
platform_settings|tapin_platform_token
EOF
)
echo "$CRED_COLUMNS" | while IFS='|' read -r tbl col; do
  [ -z "$tbl" ] && continue
  exists=$("$PSQL" "$DB_URL" -tAc "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='$tbl' AND column_name='$col'")
  if [ "$exists" = "0" ]; then
    echo "  SKIP (column missing): $tbl.$col"
    continue
  fi
  "$PSQL" "$DB_URL" -v ON_ERROR_STOP=1 -c "UPDATE $tbl SET $col = NULL WHERE $(scope_where "$tbl") AND $col IS NOT NULL" >/dev/null
  echo "  nulled credential column: $tbl.$col"
done
reported=$("$PSQL" "$DB_URL" -tA -c "
SELECT table_name || '.' || column_name FROM information_schema.columns
WHERE table_schema='public'
  AND (column_name ILIKE '%token%' OR column_name ILIKE '%secret%'
       OR column_name ILIKE '%password%' OR column_name ILIKE '%api_key%')
  AND NOT (table_name='suppliers' AND column_name='tapin_token')
  AND NOT (table_name='platform_settings' AND column_name='tapin_platform_token')
ORDER BY 1")
if [ -n "$reported" ]; then
  echo "  credential-named columns KEPT (review before releasing the snapshot):"
  echo "$reported" | sed 's/^/    /'
else
  echo "  no other credential-named columns found."
fi
re_enable_trigger
trap - EXIT

# Verification (fail-closed): row counts unchanged, and no PII
# pattern survives in any character column of a user-data table
# (scoped by prefix when given). Patterns: Iranian mobile 09 + 9
# digits, email, IBAN IR + 24 digits.
#
# Excluded from the scan, by design: views (e.g. pg_stat_statements
# holds query telemetry, not snapshot data); schema_migrations
# (checksums are hashes — a hex string can coincidentally contain a
# digit run matching the mobile pattern); site_content (public CMS
# content — a contact address in it is there by editorial choice,
# reviewed separately, not scrubbed automatically).
python3 - "$DB_URL" "$PSQL" "$ID_PREFIX" "$MANIFEST" "$DUMP" "$SKIP_RESTORE" <<'PYEOF'
import json, subprocess, sys, hashlib, os

db_url, psql, prefix, manifest, dump, skip_restore = sys.argv[1:7]
skip_restore = skip_restore == "1"

def psql_out(sql):
    r = subprocess.run([psql, db_url, "-tA", "-F", "|", "-c", sql],
                       capture_output=True, text=True)
    if r.returncode != 0:
        print(f"fin014-snapshot-scrub: psql failed: {r.stderr.strip()}", file=sys.stderr)
        sys.exit(1)
    return [l for l in r.stdout.splitlines() if l.strip()]

# Row counts after the scrub; compare with the before snapshot.
# pg_stat counts are exact after ANALYZE (one call, not one per
# table).
psql_out("ANALYZE")
before = dict((row.split("|")[0], int(row.split("|")[1]))
              for row in psql_out("SELECT tbl, rows FROM public.fin014_scrub_counts_before"))
after = dict((row.split("|")[0], int(row.split("|")[1]))
             for row in psql_out(
                 "SELECT schemaname || '.' || relname, n_live_tup FROM pg_stat_user_tables "
                 "WHERE schemaname='public' AND relname <> 'fin014_scrub_counts_before'"))
mismatch = {t: (before.get(t), after.get(t)) for t in set(before) | set(after)
            if before.get(t) != after.get(t)}
if mismatch:
    print(f"fin014-snapshot-scrub: FAIL: row counts changed: {mismatch}", file=sys.stderr)
    sys.exit(1)
print(f"fin014-snapshot-scrub: row counts unchanged across {len(after)} tables")

# PII pattern scan over every character column of every user-data
# table, in a single UNION ALL query (one psql call, not one per
# column).
patterns = [r"09[0-9]{9}", r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}", r"IR[0-9]{24}"]
cols = psql_out(
    "SELECT c.table_name || '|' || c.column_name FROM information_schema.columns c "
    "JOIN information_schema.tables t ON t.table_schema=c.table_schema AND t.table_name=c.table_name "
    "WHERE c.table_schema='public' AND t.table_type='BASE TABLE' "
    "AND c.data_type IN ('text', 'character varying', 'character') "
    "AND c.table_name NOT IN ('schema_migrations', 'site_content') "
    "ORDER BY 1")
id_tables = set(psql_out(
    "SELECT table_name FROM information_schema.columns "
    "WHERE table_schema='public' AND column_name='id'"))
parts = []
for entry in cols:
    tbl, col = entry.split("|")
    conds = " OR ".join(f"{col} ~ '{p}'" for p in patterns)
    scope = f" AND id::text LIKE '{prefix}%'" if (prefix and tbl in id_tables) else ""
    parts.append(f"SELECT '{tbl}.{col}' AS col, COUNT(*) AS n FROM {tbl} WHERE ({conds}){scope}")
# One psql call per chunk: the whole UNION ALL at once exceeds
# ARG_MAX (hundreds of columns).
leaks = []
CHUNK = 40
for i in range(0, len(parts), CHUNK):
    for row in psql_out(" UNION ALL ".join(parts[i:i + CHUNK])):
        col, n = row.rsplit("|", 1)
        if int(n):
            leaks.append(f"{col} ({n} rows)")
if leaks:
    print("fin014-snapshot-scrub: FAIL: PII patterns survive in: " + "; ".join(leaks), file=sys.stderr)
    sys.exit(1)
print("fin014-snapshot-scrub: no PII patterns remain in any character column")

if manifest:
    digest = None
    if dump and os.path.exists(dump):
        h = hashlib.sha256()
        with open(dump, 'rb') as f:
            for chunk in iter(lambda: f.read(1 << 20), b''):
                h.update(chunk)
        digest = h.hexdigest()
    with open(manifest, 'w') as f:
        json.dump({
            "script": "fin014-snapshot-scrub",
            "result": "pass",
            "database": db_url.split('/')[-1].split('?')[0],
            "dumpSha256": digest,
            "restored": not skip_restore,
            "idPrefix": prefix or None,
            "tableCount": len(after),
            "rowCounts": after,
            "transforms": "named column transforms + credential sweep (see script)",
            "verification": "row counts unchanged; no PII patterns (mobile/email/IBAN) in any character column of a user-data table (views, schema_migrations, site_content excluded by design — see script)",
        }, f, indent=2)
        f.write("\n")
    print(f"fin014-snapshot-scrub: PASS: manifest written to {manifest}")
else:
    print("fin014-snapshot-scrub: PASS")
PYEOF
"$PSQL" "$DB_URL" -v ON_ERROR_STOP=1 -c "DROP TABLE IF EXISTS public.fin014_scrub_counts_before" >/dev/null
