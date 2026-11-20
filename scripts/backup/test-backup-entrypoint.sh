#!/bin/bash
# test-backup-entrypoint.sh — BAK-01 implementation test harness.
#
# Runs the consolidated entry point + restore verification against
# disposable local infrastructure (synthetic data only, no prod
# contact, no prod data, no OTP):
#   - a fake host root (temp dir) standing in for /opt/drup
#   - a disposable PostgreSQL database on 127.0.0.1:5433, built from
#     the REAL migration set + a small synthetic seed (2 stores,
#     3 products, 3 orders, 5 ledger rows, 3 inventory balances,
#     2 verified payment receipts, 1 custom domain — the same shape
#     as the BAK-01 audit rehearsal)
#   - a TEST-ONLY encryption key generated fresh per run (never
#     committed, never used for prod, deleted with the temp dir)
#
# Usage: scripts/backup/test-backup-entrypoint.sh [--keep] [--evidence-dir <dir>]
#   --keep         keep the temp dirs + disposable DBs for inspection
#   --evidence-dir copy the run logs + restore report there
#                  (default: a fresh mktemp dir, path printed at end)
#
# Env (all optional):
#   BAK01_PGHOST/BACK01_PGPORT/BACK01_PGUSER (defaults 127.0.0.1/5433/gap13)
#   BAK01_MIGRATE_BIN — prebuilt migrate binary; if unset, the
#     harness builds one from backend/cmd/migrate (needs go).
#
# Exit 0 only when every test passes. Each test prints PASS/FAIL.
set -u
umask 077

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENTRYPOINT="$REPO/scripts/backup/backup-entrypoint.sh"
PUBLISHER="$REPO/scripts/publish-backup-success.sh"
FAILURE_PUB="$REPO/scripts/backup/publish-backup-failure.sh"
HEALTH="$REPO/scripts/backup/check-backup-health.sh"
RESTORE_VERIFY="$REPO/scripts/backup/restore-verify.sh"
DEPLOY_BACKEND="$REPO/scripts/deploy/deploy-backend.sh"

PGHOST="${BAK01_PGHOST:-127.0.0.1}"
PGPORT="${BAK01_PGPORT:-5433}"
PGUSER="${BAK01_PGUSER:-gap13}"
SRC_DB="bak01_impl_src"
TSDIR="$(mktemp -d)"
EVIDENCE_DIR=""
KEEP=0
while [ $# -gt 0 ]; do
  case "$1" in
    --keep) KEEP=1; shift;;
    --evidence-dir) EVIDENCE_DIR="$2"; shift 2;;
    *) echo "usage: $0 [--keep] [--evidence-dir <dir>]" >&2; exit 2;;
  esac
done
[ -n "$EVIDENCE_DIR" ] || EVIDENCE_DIR="$(mktemp -d)"
mkdir -p "$EVIDENCE_DIR"
LOG="$EVIDENCE_DIR/test-backup-entrypoint.log"
exec > >(tee "$LOG") 2>&1

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); echo "PASS: $*"; }
bad()  { FAIL=$((FAIL+1)); echo "FAIL: $*"; }
check(){ if [ "$1" -eq 0 ]; then ok "$2"; else bad "$2"; fi; }

FAKE_ROOT="$TSDIR/fake-root"
FAKE_BIN="$TSDIR/fake-bin"
mkdir -p "$FAKE_ROOT/static/uploads/products" "$FAKE_BIN" "$FAKE_ROOT/backups"
# Fake host root: real (non-secret) edge files copied from the repo,
# fake .env / compose files with TEST-ONLY placeholder values.
cp "$REPO/nginx.conf" "$REPO/Caddyfile" "$FAKE_ROOT/"
printf 'TEST_ONLY_FAKE_ENV=1\nTEST_ONLY_PLACEHOLDER=yes\n' > "$FAKE_ROOT/.env"
for f in docker-compose.yml docker-compose.prod.yml docker-compose.ssl.yml docker-compose.observability.yml docker-compose.metrics-ports.yml; do
  printf '# TEST-ONLY placeholder compose file for the BAK-01 rehearsal\nservices: {}\n' > "$FAKE_ROOT/$f"
done
printf '0123456789abcdef0123456789abcdef01234567\n' > "$FAKE_ROOT/.deployed-commit"
printf 'synth-kettle-bytes' > "$FAKE_ROOT/static/uploads/products/synth-kettle.jpg"
printf 'synth-rug-bytes' > "$FAKE_ROOT/static/uploads/products/synth-rug.jpg"
printf 'synth-lamp-bytes' > "$FAKE_ROOT/static/uploads/products/synth-lamp.jpg"
# TEST-ONLY key, generated fresh per run, never committed, never prod.
openssl rand -base64 48 > "$FAKE_ROOT/.backup-encryption-key"
chmod 600 "$FAKE_ROOT/.backup-encryption-key"
# Fake docker: every invocation fails (no Caddy volumes on the
# rehearsal host) — exercises the recorded-missing path (G6).
printf '#!/bin/bash\necho "fake-docker: no docker here" >&2\nexit 1\n' > "$FAKE_BIN/docker"
chmod +x "$FAKE_BIN/docker"
# Failing pg_dump stub (injected failure, G1).
printf '#!/bin/bash\necho "pg_dump: error: connection to server failed (injected)" >&2\nexit 1\n' > "$FAKE_BIN/failing-pg-dump"
chmod +x "$FAKE_BIN/failing-pg-dump"

PSQL_SRC="psql -h $PGHOST -p $PGPORT -U $PGUSER -d $SRC_DB -v ON_ERROR_STOP=1 -At"
PSQL_ADMIN="psql -h $PGHOST -p $PGPORT -U $PGUSER -d postgres -v ON_ERROR_STOP=1 -At"
DUMP_SRC="pg_dump -h $PGHOST -p $PGPORT -U $PGUSER -d $SRC_DB"

export BAK_ROOT="$FAKE_ROOT"
export BAK_KEY_FILE="$FAKE_ROOT/.backup-encryption-key"
export BAK_PG_DUMP_CMD="$DUMP_SRC"
export BAK_PSQL_CMD="$PSQL_SRC"
export BAK_PG_RESTORE_LIST_CMD="pg_restore --list"
export BAK_DOCKER_BIN="$FAKE_BIN/docker"
export BAK_PUBLISHER="$PUBLISHER"
export BAK_FAILURE_PUBLISHER="$FAILURE_PUB"

cleanup() {
  if [ "$KEEP" -eq 0 ]; then
    $PSQL_ADMIN -c "DROP DATABASE IF EXISTS $SRC_DB" >/dev/null 2>&1 || true
    $PSQL_ADMIN -c "SELECT datname FROM pg_database WHERE datname LIKE 'bakverify_%'" 2>/dev/null | while read -r db; do
      [ -n "$db" ] && $PSQL_ADMIN -c "DROP DATABASE IF EXISTS \"$db\"" >/dev/null 2>&1 || true
    done
    rm -rf "$TSDIR"
  else
    echo "KEPT: temp dir $TSDIR, source DB $SRC_DB (drop manually)"
  fi
}
trap cleanup EXIT

echo "=== BAK-01 implementation tests ==="
echo "repo: $REPO"
echo "fake root: $FAKE_ROOT"
echo "evidence: $EVIDENCE_DIR"

# --- source DB: real migrations + synthetic seed ----------------------
echo "--- setup: disposable source DB from the real migration set ---"
MIGRATE_BIN="${BAK01_MIGRATE_BIN:-}"
if [ -z "$MIGRATE_BIN" ]; then
  if [ -x "$HOME/go/bin/go" ]; then GO="$HOME/go/bin/go"; else GO="$(command -v go || true)"; fi
  if [ -n "$GO" ]; then
    (cd "$REPO/backend" && "$GO" build -o "$TSDIR/migrate" ./cmd/migrate) && MIGRATE_BIN="$TSDIR/migrate"
  fi
fi
[ -z "$MIGRATE_BIN" ] && [ -x ~/workspace/bak01-audit/rehearsal/migrate ] && MIGRATE_BIN=~/workspace/bak01-audit/rehearsal/migrate
if [ -z "$MIGRATE_BIN" ] || [ ! -x "$MIGRATE_BIN" ]; then
  echo "FAIL: no migrate binary (set BAK01_MIGRATE_BIN or make go available)" >&2; exit 2
fi
$PSQL_ADMIN -c "DROP DATABASE IF EXISTS $SRC_DB" -c "CREATE DATABASE $SRC_DB" >/dev/null
DATABASE_URL="postgres://$PGUSER@$PGHOST:$PGPORT/$SRC_DB?sslmode=disable" "$MIGRATE_BIN" -dir "$REPO/backend/migrations" > "$EVIDENCE_DIR/migrate.log" 2>&1
MIG_RC=$?
MIG_COUNT="$($PSQL_SRC -c 'SELECT count(*) FROM schema_migrations' 2>/dev/null | tr -d '[:space:]')"
echo "migrate rc=$MIG_RC applied=$MIG_COUNT"
check "$([ "$MIG_RC" -eq 0 ] && [ "$MIG_COUNT" = "210" ]; echo $?)" "source DB has 210 migrations applied (rc=$MIG_RC count=$MIG_COUNT)"

# Synthetic seed (same shape/values as the audit rehearsal).
$PSQL_SRC <<'SQLEOF' >/dev/null
BEGIN;
INSERT INTO stores(id,name,slug,owner_type,owner_name,custom_domain,total_sales,status)
VALUES ('store-synth-1','Synthetic Store One','synth-one','dropshipper','Synthetic Seller','shop-synth.example.test',1250000,'active'),
       ('store-synth-2','Synthetic Store Two','synth-two','dropshipper','Synthetic Seller Two',NULL,0,'active');
INSERT INTO store_domains(domain,store_id,verification_token,status,verified_at) VALUES ('shop-synth.example.test','store-synth-1','synth-token-0001','active',now())
  ON CONFLICT DO NOTHING;
INSERT INTO suppliers(id,name) VALUES ('supplier-synth-1','Synthetic Supplier One'),('supplier-synth-2','Synthetic Supplier Two');
INSERT INTO products(id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,image_url)
VALUES ('prod-synth-1','Synthetic Kettle','synth-kettle','appliances','supplier-synth-1',1500000,1000000,150000,40,'/static/uploads/products/synth-kettle.jpg'),
       ('prod-synth-2','Synthetic Rug','synth-rug','appliances','supplier-synth-1',4200000,3000000,420000,12,'/static/uploads/products/synth-rug.jpg'),
       ('prod-synth-3','Synthetic Lamp','synth-lamp','appliances','supplier-synth-2',900000,600000,90000,0,'/static/uploads/products/synth-lamp.jpg');
INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,quantity,status,reconciled)
VALUES ('inv-synth-1','prod-synth-1','supplier-synth-1','supplier','warehouse','wh-tehran',40,'sellable',true),
       ('inv-synth-2','prod-synth-2','supplier-synth-1','supplier','warehouse','wh-tehran',12,'sellable',true),
       ('inv-synth-3','prod-synth-3','supplier-synth-2','supplier','warehouse','wh-qom',0,'sellable',true);
INSERT INTO orders(id,order_number,store_id,store_name,total_amount,discount_amount,payable_amount,payment_method,
  customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,status,tracking_code,shipping_cost,
  dropshipper_profit,platform_profit,referral_commission,profit_released)
VALUES ('ord-synth-1','SYN-1001','store-synth-1','Synthetic Store One',1500000,0,1500000,'cash','Synthetic Buyer','09110000001','Tehran','Tehran','1 Synthetic St','1000000001','processing','TRK-SYNTH-001',50000,150000,150000,0,false),
       ('ord-synth-2','SYN-1002','store-synth-1','Synthetic Store One',4200000,200000,4000000,'cash','Synthetic Buyer Two','09000000002','Qom','Qom','2 Synthetic St','2000000002','shipped','TRK-SYNTH-002',80000,420000,420000,8400,false),
       ('ord-synth-3','SYN-1003','store-synth-2','Synthetic Store Two',900000,0,900000,'cash','Synthetic Buyer Three','09000000003','Isfahan','Isfahan','3 Synthetic St','3000000003','pending','TRK-SYNTH-003',40000,90000,90000,0,false);
INSERT INTO order_items(order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price,product_title_snapshot)
VALUES ('ord-synth-1','prod-synth-1',1,1000000,150000,150000,1500000,'Synthetic Kettle'),
       ('ord-synth-2','prod-synth-2',1,3000000,420000,420000,4000000,'Synthetic Rug'),
       ('ord-synth-3','prod-synth-3',1,600000,90000,90000,900000,'Synthetic Lamp');
INSERT INTO ledger_entries(id,target_account,target_type,type,amount,reason,operator,related_order_or_dispute)
VALUES ('le-synth-1','platform_profit','platform','credit',150000,'order ord-synth-1 platform profit','bak01-rehearsal','ord-synth-1'),
       ('le-synth-2','store-synth-1','seller','credit',150000,'order ord-synth-1 dropshipper profit','bak01-rehearsal','ord-synth-1'),
       ('le-synth-3','platform_profit','platform','credit',420000,'order ord-synth-2 platform profit','bak01-rehearsal','ord-synth-2'),
       ('le-synth-4','store-synth-1','seller','credit',420000,'order ord-synth-2 dropshipper profit','bak01-rehearsal','ord-synth-2'),
       ('le-synth-5','platform_profit','platform','credit',90000,'order ord-synth-3 platform profit','bak01-rehearsal','ord-synth-3');
INSERT INTO users(id,mobile,full_name,role) VALUES ('11111111-1111-1111-1111-111111111111','09110000001','Synthetic Buyer','customer');
INSERT INTO users(id,mobile,full_name,role) VALUES ('22222222-2222-2222-2222-222222222222','09110000002','Synthetic Admin','admin');
UPDATE orders SET customer_user_id='11111111-1111-1111-1111-111111111111' WHERE id IN ('ord-synth-1','ord-synth-2');
INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES
 ('pay-synth-1','ord-synth-1','SYN-1001','gw-synth-1',1500000,'paid'),
 ('pay-synth-2','ord-synth-2','SYN-1002','gw-synth-2',4000000,'paid');
INSERT INTO order_payment_verifications(id,payment_id,order_id,customer_id,amount_toman,currency,funding_kind) VALUES
 ('ver-synth-1','pay-synth-1','ord-synth-1','11111111-1111-1111-1111-111111111111',1500000,'TOMAN','gateway'),
 ('ver-synth-2','pay-synth-2','ord-synth-2','11111111-1111-1111-1111-111111111111',4000000,'TOMAN','gateway');
INSERT INTO order_payment_receipts(id,verification_id,funding_kind,payment_id,order_id,customer_id,amount_toman,currency,provider_reference,verified_at)
VALUES ('rcpt-synth-1','ver-synth-1','gateway','pay-synth-1','ord-synth-1','11111111-1111-1111-1111-111111111111',1500000,'TOMAN','SYNTH-REF-0001',now()),
       ('rcpt-synth-2','ver-synth-2','gateway','pay-synth-2','ord-synth-2','11111111-1111-1111-1111-111111111111',4000000,'TOMAN','SYNTH-REF-0002',now());
COMMIT;
SQLEOF
SEED_RC=$?
check "$SEED_RC" "synthetic seed applied"

# --- T1: happy-path nightly run ---------------------------------------
echo "--- T1: happy-path nightly run ---"
T1_START="$(date +%s)"
"$ENTRYPOINT" run nightly > "$EVIDENCE_DIR/t1.out" 2> "$EVIDENCE_DIR/t1.err"; T1_RC=$?
T1_END="$(date +%s)"
T1_DIR="$FAKE_ROOT/backups/auto/$(date +%F)"
check "$T1_RC" "T1 nightly run exit 0 (rc=$T1_RC)"
grep -q "BACKUP_OK nightly" "$EVIDENCE_DIR/t1.out" && ok "T1 stdout carries BACKUP_OK" || bad "T1 stdout carries BACKUP_OK"
[ -d "$T1_DIR" ] && ok "T1 backup dir exists: $T1_DIR" || bad "T1 backup dir exists"
for f in db.dump.enc db.sql.gz.enc uploads.tar.gz.enc config.tar.gz.enc MANIFEST.sha256 manifest.json MANIFEST.enc.sha256; do
  [ -f "$T1_DIR/$f" ] && ok "T1 artifact present: $f" || bad "T1 artifact present: $f"
done
if [ -f "$T1_DIR/db.dump" ] || [ -f "$T1_DIR/db.sql.gz" ]; then bad "T1 no plaintext artifacts remain"; else ok "T1 no plaintext artifacts remain (encrypted only)"; fi
MANIFEST_MIG="$(python3 -c "import json;print(json.load(open('$T1_DIR/manifest.json'))['schema_migrations_applied'])")"
[ "$MANIFEST_MIG" = "210" ] && ok "T1 manifest records 210 migrations" || bad "T1 manifest records 210 migrations (got $MANIFEST_MIG)"
python3 -c "import json;m=json.load(open('$T1_DIR/manifest.json'));assert 'caddy_volume:caddy_data' in m['missing'] and 'caddy_volume:caddy_config' in m['missing']" \
  && ok "T1 manifest records missing Caddy volumes" || bad "T1 manifest records missing Caddy volumes"
SIG="$($PSQL_SRC -c "SELECT count(*) FROM backup_signals WHERE kind='nightly'" | tr -d '[:space:]')"
[ "$SIG" = "1" ] && ok "T1 verified-success signal published (backup_signals nightly=1)" || bad "T1 verified-success signal published (got $SIG)"
# umask 077: files 0600, dirs 0700.
MODE_OK=1
[ "$(stat -c %a "$T1_DIR/db.dump.enc")" = "600" ] || MODE_OK=0
[ "$(stat -c %a "$T1_DIR")" = "700" ] || MODE_OK=0
[ "$(stat -c %a "$FAKE_ROOT/backups")" = "700" ] || MODE_OK=0
check "$([ "$MODE_OK" -eq 1 ]; echo $?)" "T1 umask 077 (files 0600, dirs 0700)"
echo "T1 backup wall time: $((T1_END - T1_START))s"

# --- T2: wrappers ------------------------------------------------------
echo "--- T2: backup-daily.sh / backup-auto.sh wrappers ---"
"$REPO/backup-daily.sh" --dest "$FAKE_ROOT/backups/wrapper-daily" > "$EVIDENCE_DIR/t2.out" 2>&1; T2_RC=$?
check "$T2_RC" "T2 backup-daily.sh wrapper exit 0 (rc=$T2_RC)"
[ -f "$FAKE_ROOT/backups/wrapper-daily/db.dump.enc" ] && ok "T2 wrapper produced an encrypted backup" || bad "T2 wrapper produced an encrypted backup"

# --- T3: injected pg_dump failure -------------------------------------
echo "--- T3: injected pg_dump failure ---"
NOTICES_BEFORE="$($PSQL_SRC -c "SELECT count(*) FROM notification_outbox WHERE kind='operations_alert'" | tr -d '[:space:]')"
BAK_PG_DUMP_CMD="$FAKE_BIN/failing-pg-dump" "$ENTRYPOINT" run nightly --dest "$FAKE_ROOT/backups/should-not-exist" > "$EVIDENCE_DIR/t3.out" 2> "$EVIDENCE_DIR/t3.err"; T3_RC=$?
[ "$T3_RC" -ne 0 ] && ok "T3 injected pg_dump failure -> nonzero exit ($T3_RC)" || bad "T3 injected pg_dump failure -> nonzero exit"
grep -q "BACKUP_OK" "$EVIDENCE_DIR/t3.out" && bad "T3 no BACKUP_OK on failure" || ok "T3 no BACKUP_OK on failure"
[ -d "$FAKE_ROOT/backups/should-not-exist" ] && bad "T3 no backup dir on failure" || ok "T3 no backup dir on failure"
ls "$FAKE_ROOT/backups"/.backup-failure-nightly-*.marker >/dev/null 2>&1 && ok "T3 failure marker file written" || bad "T3 failure marker file written"
NOTICES_AFTER="$($PSQL_SRC -c "SELECT count(*) FROM notification_outbox WHERE kind='operations_alert'" | tr -d '[:space:]')"
[ "$NOTICES_AFTER" -gt "$NOTICES_BEFORE" ] && ok "T3 immediate failure notice in admin inbox ($NOTICES_BEFORE -> $NOTICES_AFTER)" || bad "T3 immediate failure notice in admin inbox ($NOTICES_BEFORE -> $NOTICES_AFTER)"

# --- T4: corruption / missing artifacts --------------------------------
echo "--- T4: corruption and missing artifacts ---"
C1="$TSDIR/corrupt1"; cp -r "$T1_DIR" "$C1"
python3 -c "
d=open('$C1/db.dump.enc','rb').read(); b=bytearray(d); b[len(d)//2]^=0xFF; open('$C1/db.dump.enc','wb').write(bytes(b))"
"$ENTRYPOINT" verify "$C1" >/dev/null 2>&1; [ $? -ne 0 ] && ok "T4 single-byte flip in db.dump.enc detected" || bad "T4 single-byte flip in db.dump.enc detected"
C2="$TSDIR/corrupt2"; cp -r "$T1_DIR" "$C2"
python3 -c "
d=open('$C2/db.sql.gz.enc','rb').read(); b=bytearray(d); b[100]^=0x01; open('$C2/db.sql.gz.enc','wb').write(bytes(b))"
"$ENTRYPOINT" verify "$C2" >/dev/null 2>&1; [ $? -ne 0 ] && ok "T4 single-byte flip in db.sql.gz.enc detected" || bad "T4 single-byte flip in db.sql.gz.enc detected"
C3="$TSDIR/corrupt3"; cp -r "$T1_DIR" "$C3"; rm "$C3/config.tar.gz.enc"
"$ENTRYPOINT" verify "$C3" >/dev/null 2>&1; [ $? -ne 0 ] && ok "T4 missing artifact detected" || bad "T4 missing artifact detected"
C4="$TSDIR/corrupt4"; cp -r "$T1_DIR" "$C4"
mkdir -p "$TSDIR/c4tmp"
openssl enc -d -aes-256-cbc -pbkdf2 -md sha256 -salt -pass "file:$BAK_KEY_FILE" -in "$C4/db.dump.enc" -out "$TSDIR/c4tmp/db.dump" 2>/dev/null
python3 -c "
d=open('$TSDIR/c4tmp/db.dump','rb').read(); b=bytearray(d); b[len(d)//2]^=0xFF; open('$TSDIR/c4tmp/db.dump','wb').write(bytes(b))"
openssl enc -aes-256-cbc -pbkdf2 -md sha256 -salt -pass "file:$BAK_KEY_FILE" -in "$TSDIR/c4tmp/db.dump" -out "$C4/db.dump.enc" 2>/dev/null
"$ENTRYPOINT" verify "$C4" >/dev/null 2>&1; [ $? -ne 0 ] && ok "T4 single-byte flip in PLAINTEXT (re-encrypted) detected by manifest" || bad "T4 single-byte flip in PLAINTEXT (re-encrypted) detected by manifest"
C5="$TSDIR/corrupt5"; cp -r "$T1_DIR" "$C5"; truncate -s -100 "$C5/db.sql.gz.enc"
"$ENTRYPOINT" verify "$C5" >/dev/null 2>&1; [ $? -ne 0 ] && ok "T4 truncated artifact detected" || bad "T4 truncated artifact detected"

# --- T5: missing key fails loudly --------------------------------------
echo "--- T5: missing encryption key ---"
BAK_KEY_FILE=/nonexistent-test-key "$ENTRYPOINT" run nightly --dest "$FAKE_ROOT/backups/no-key" > "$EVIDENCE_DIR/t5.out" 2>&1; T5_RC=$?
[ "$T5_RC" -ne 0 ] && ok "T5 missing key -> nonzero exit ($T5_RC)" || bad "T5 missing key -> nonzero exit"
[ -d "$FAKE_ROOT/backups/no-key" ] && bad "T5 no backup dir without a key" || ok "T5 no backup dir without a key"

# --- T6: encryption round-trip -----------------------------------------
echo "--- T6: encryption round-trip ---"
"$ENTRYPOINT" verify "$T1_DIR" >/dev/null 2>&1; check "$?" "T6 verify (decrypt + manifest + gzip + --list) passes on the good backup"
mkdir -p "$TSDIR/rt"
openssl enc -d -aes-256-cbc -pbkdf2 -md sha256 -salt -pass "file:$BAK_KEY_FILE" -in "$T1_DIR/db.dump.enc" -out "$TSDIR/rt/db.dump" 2>/dev/null
RT_SHA="$(sha256sum "$TSDIR/rt/db.dump" | cut -d' ' -f1)"
MAN_SHA="$(python3 -c "import json;print(json.load(open('$T1_DIR/manifest.json'))['artifact_sha256']['db.dump'])")"
[ "$RT_SHA" = "$MAN_SHA" ] && ok "T6 decrypted db.dump sha256 matches the manifest" || bad "T6 decrypted db.dump sha256 matches the manifest"

# --- T7: retention ------------------------------------------------------
echo "--- T7: retention ---"
for i in 01 02 03 04 05 06 07; do
  d="$FAKE_ROOT/backups/deploy-aaaaaaa$i-2026010${i}T000000Z"; mkdir -p "$d"; touch -d "2026-01-0${i} 00:00" "$d"
done
"$ENTRYPOINT" run deploy --commit 1111111111111111111111111111111111111111 >/dev/null 2>&1
"$ENTRYPOINT" run deploy --commit 2222222222222222222222222222222222222222 >/dev/null 2>&1
DEPLOY_COUNT="$(ls -d "$FAKE_ROOT/backups"/deploy-* 2>/dev/null | wc -l)"
[ "$DEPLOY_COUNT" -eq 5 ] && ok "T7 deploy backups pruned to newest 5 (count=$DEPLOY_COUNT)" || bad "T7 deploy backups pruned to newest 5 (count=$DEPLOY_COUNT)"
mkdir -p "$FAKE_ROOT/backups/auto/2026-01-01"; touch -d "2026-01-01 00:00" "$FAKE_ROOT/backups/auto/2026-01-01"
"$ENTRYPOINT" run nightly --dest "$FAKE_ROOT/backups/nightly-retention-probe" >/dev/null 2>&1
[ -d "$FAKE_ROOT/backups/auto/2026-01-01" ] && bad "T7 nightly dirs older than 7d pruned" || ok "T7 nightly dirs older than 7d pruned"

# --- T8: deploy mode with --include ------------------------------------
echo "--- T8: deploy mode + --include ---"
echo "release-content-test" > "$TSDIR/extra-release.json"
"$ENTRYPOINT" run deploy --commit 3333333333333333333333333333333333333333 \
  --include "$TSDIR/extra-release.json" --include "$FAKE_ROOT/nginx.conf" \
  --dest "$FAKE_ROOT/backups/deploy-include-test" > "$EVIDENCE_DIR/t8.out" 2>&1; T8_RC=$?
check "$T8_RC" "T8 deploy run with --include exit 0 (rc=$T8_RC)"
python3 -c "import json;m=json.load(open('$FAKE_ROOT/backups/deploy-include-test/manifest.json'));assert 'extra/extra-release.json' in m['artifacts'] and 'nginx.conf' in m['artifacts']" \
  && ok "T8 includes recorded in the manifest (extra/ + relative path)" || bad "T8 includes recorded in the manifest"
"$ENTRYPOINT" verify "$FAKE_ROOT/backups/deploy-include-test" >/dev/null 2>&1; check "$?" "T8 deploy backup verifies"
SIG_DEPLOY="$($PSQL_SRC -c "SELECT count(*) FROM backup_signals WHERE kind='deploy'" | tr -d '[:space:]')"
[ "$SIG_DEPLOY" -ge 1 ] && ok "T8 deploy success signal published (kind='deploy')" || bad "T8 deploy success signal published"

# --- T9: full restore rehearsal ---------------------------------------
echo "--- T9: full restore rehearsal (restore-verify.sh) ---"
T9_START_S="$(date +%s)"
BAK_ADMIN_PSQL="$PSQL_ADMIN" BAK_PG_RESTORE_CMD="pg_restore -h $PGHOST -p $PGPORT -U $PGUSER" \
  BAK_ENTRYPOINT="$ENTRYPOINT" BAK_BACKUP_BASE="$FAKE_ROOT/backups" \
  "$RESTORE_VERIFY" "$T1_DIR" --report "$EVIDENCE_DIR/restore-report.json" \
  --keep-db --target-db bakverify_t9check \
  > "$EVIDENCE_DIR/t9.out" 2> "$EVIDENCE_DIR/t9.err"; T9_RC=$?
T9_END_S="$(date +%s)"
check "$T9_RC" "T9 restore-verify exit 0 (rc=$T9_RC)"
# The restore report carries GLOBAL counts; the migration set itself
# seeds fixture rows (6 stores / 6 products / 1 order / 2 ledger
# rows — audit §4.2), so the synthetic subset is asserted here
# against the kept DB before it is dropped.
T9Q="psql -h $PGHOST -p $PGPORT -U $PGUSER -d bakverify_t9check -v ON_ERROR_STOP=1 -At"
SYN_STORES="$($T9Q -c "SELECT count(*) FROM stores WHERE id LIKE 'store-synth%'" 2>/dev/null | tr -d '[:space:]')"
SYN_PRODUCTS="$($T9Q -c "SELECT count(*) FROM products WHERE id LIKE 'prod-synth%'" 2>/dev/null | tr -d '[:space:]')"
SYN_ORDERS="$($T9Q -c "SELECT count(*) FROM orders WHERE id LIKE 'ord-synth%'" 2>/dev/null | tr -d '[:space:]')"
SYN_ITEMS="$($T9Q -c "SELECT count(*) FROM order_items WHERE order_id LIKE 'ord-synth%'" 2>/dev/null | tr -d '[:space:]')"
SYN_LEDGER_ROWS="$($T9Q -c "SELECT count(*) FROM ledger_entries WHERE related_order_or_dispute LIKE 'ord-synth%'" 2>/dev/null | tr -d '[:space:]')"
SYN_LEDGER_SUM="$($T9Q -c "SELECT COALESCE(sum(amount),0) FROM ledger_entries WHERE related_order_or_dispute LIKE 'ord-synth%'" 2>/dev/null | tr -d '[:space:]')"
SYN_INV="$($T9Q -c "SELECT COALESCE(sum(quantity),0) FROM inventory_balances WHERE product_id LIKE 'prod-synth%'" 2>/dev/null | tr -d '[:space:]')"
SYN_RECEIPTS="$($T9Q -c "SELECT count(*) FROM order_payment_receipts WHERE id LIKE 'rcpt-synth%'" 2>/dev/null | tr -d '[:space:]')"
SYN_DOMAINS="$($T9Q -c "SELECT count(*) FROM store_domains WHERE domain='shop-synth.example.test'" 2>/dev/null | tr -d '[:space:]')"
SYN_STATUS="$($T9Q -c "SELECT string_agg(status,',' ORDER BY status) FROM orders WHERE id LIKE 'ord-synth%'" 2>/dev/null | tr -d '[:space:]')"
$PSQL_ADMIN -c "DROP DATABASE IF EXISTS bakverify_t9check" >/dev/null 2>&1
echo "T9 synthetic subset: stores=$SYN_STORES products=$SYN_PRODUCTS orders=$SYN_ORDERS items=$SYN_ITEMS ledger=$SYN_LEDGER_ROWS/$SYN_LEDGER_SUM inv=$SYN_INV receipts=$SYN_RECEIPTS domains=$SYN_DOMAINS statuses=$SYN_STATUS"
[ "$SYN_STORES" = "2" ] && [ "$SYN_PRODUCTS" = "3" ] && [ "$SYN_ORDERS" = "3" ] && [ "$SYN_ITEMS" = "3" ] \
  && [ "$SYN_LEDGER_ROWS" = "5" ] && [ "$SYN_LEDGER_SUM" = "1230000" ] && [ "$SYN_INV" = "52" ] \
  && [ "$SYN_RECEIPTS" = "2" ] && [ "$SYN_DOMAINS" = "1" ] && [ "$SYN_STATUS" = "pending,processing,shipped" ] \
  && ok "T9 synthetic subset restored exactly (2/3/3/3, ledger 5/1230000, inv 52, receipts 2, domains 1)" \
  || bad "T9 synthetic subset restored exactly"
if [ -f "$EVIDENCE_DIR/restore-report.json" ]; then
  python3 - "$EVIDENCE_DIR/restore-report.json" <<'PYEOF'
import json, sys
r = json.load(open(sys.argv[1]))
v = r["validation"]
print("restore-report result:", r["result"])
print("restore_ms:", r["restore_ms"], "recovery_seconds:", r["recovery_seconds"],
      "data_age_seconds:", r["data_age_seconds"], "targets:", r["targets"])
print("validation (global counts, incl. migration fixtures):", v)
print("failures:", r["failures"])
sys.exit(0 if r["result"] == "PASS" and v["schema_migrations"] == 210 and v["media_missing"] == 0 else 1)
PYEOF
  check "$?" "T9 restore report PASS with 210 migrations and 0 missing media refs"
else
  bad "T9 restore report written"
fi
echo "T9 wall time (job start -> report): $((T9_END_S - T9_START_S))s"
LEFTOVER="$($PSQL_ADMIN -c "SELECT count(*) FROM pg_database WHERE datname LIKE 'bakverify_%'" | tr -d '[:space:]')"
[ "$LEFTOVER" = "0" ] && ok "T9 disposable DB dropped afterwards" || bad "T9 disposable DB dropped afterwards ($LEFTOVER left)"

# --- T10: publisher failure -> exit 3, backup intact -------------------
echo "--- T10: publisher failure is exit 3 (distinct state) ---"
BAK_PUBLISHER=/bin/false "$ENTRYPOINT" run nightly --dest "$FAKE_ROOT/backups/pubfail" > "$EVIDENCE_DIR/t10.out" 2>&1; T10_RC=$?
[ "$T10_RC" -eq 3 ] && ok "T10 publisher failure -> exit 3 (rc=$T10_RC)" || bad "T10 publisher failure -> exit 3 (rc=$T10_RC)"
[ -f "$FAKE_ROOT/backups/pubfail/db.dump.enc" ] && [ -f "$FAKE_ROOT/backups/pubfail/.signal-failed" ] \
  && ok "T10 backup files intact + .signal-failed marker present" || bad "T10 backup files intact + .signal-failed marker"

# --- T11: off-site copy to a local stand-in ---------------------------
echo "--- T11: off-site copy (local-directory stand-in) ---"
BAK_OFFSITE_DEST="$FAKE_ROOT/offsite-standin" "$ENTRYPOINT" run nightly --dest "$FAKE_ROOT/backups/offsite-probe" > "$EVIDENCE_DIR/t11.out" 2>&1; T11_RC=$?
check "$T11_RC" "T11 off-site copy run exit 0 (rc=$T11_RC)"
OFF_DIR="$FAKE_ROOT/offsite-standin/offsite-probe"
[ -f "$OFF_DIR/db.dump.enc" ] && (cd "$OFF_DIR" && sha256sum -c MANIFEST.enc.sha256 >/dev/null 2>&1) \
  && ok "T11 off-site stand-in copy present + hashes verify" || bad "T11 off-site stand-in copy present + hashes verify"

# --- T12: fallback health check retries pending markers ---------------
echo "--- T12: check-backup-health retries markers + staleness ---"
BAK_PSQL_CMD="$PSQL_SRC" "$HEALTH" --retry-markers > "$EVIDENCE_DIR/t12.out" 2>&1; T12_RC=$?
check "$T12_RC" "T12 health check exit 0 after retry (rc=$T12_RC)"
ls "$FAKE_ROOT/backups"/.backup-failure-*.marker.acked >/dev/null 2>&1 && ok "T12 failure marker acked after retry" || bad "T12 failure marker acked after retry"
grep -q "BACKUP_HEALTH_OK" "$EVIDENCE_DIR/t12.out" && ok "T12 BACKUP_HEALTH_OK printed" || bad "T12 BACKUP_HEALTH_OK printed"

# --- T13: deploy-backend.sh rehearsal path ----------------------------
echo "--- T13: deploy-backend.sh rehearsal (no Docker) ---"
T13_ROOT="$TSDIR/fake-deploy-root"
T13_REL="$TSDIR/fake-release"
mkdir -p "$T13_ROOT/backend/bin" "$T13_ROOT/releases" "$T13_REL/backend/bin" "$T13_REL/scripts/release"
COMMIT13="4444444444444444444444444444444444444444"
printf 'old-binary-bytes' > "$T13_ROOT/backend/bin/server-linux"
printf '{"commit": "%s"}' "$COMMIT13" > "$T13_REL/release.json"
printf 'new-binary-bytes' > "$T13_REL/backend/bin/server-linux"
printf 'migrate-bytes' > "$T13_REL/backend/bin/migrate-linux"
# Fake python3: validate_release.py -> exit 0; everything else -> real python3.
cat > "$FAKE_BIN/python3" <<'PYEOF'
#!/bin/bash
for a in "$@"; do case "$a" in *validate_release.py*) exit 0;; esac; done
exec /usr/bin/python3 "$@"
PYEOF
chmod +x "$FAKE_BIN/python3"
PATH="$FAKE_BIN:$PATH" "$DEPLOY_BACKEND" "$T13_REL" --local-root "$T13_ROOT" --rehearse --skip-health > "$EVIDENCE_DIR/t13.out" 2>&1; T13_RC=$?
check "$T13_RC" "T13 deploy-backend rehearsal exit 0 (rc=$T13_RC)"
grep -q "entry-point backup SKIPPED" "$EVIDENCE_DIR/t13.out" && ok "T13 rehearsal loudly skips the entry-point backup (no Docker)" || bad "T13 rehearsal loudly skips the entry-point backup"
grep -q "BACKEND DEPLOYED: $COMMIT13" "$EVIDENCE_DIR/t13.out" && ok "T13 rehearsal completes the swap" || bad "T13 rehearsal completes the swap"
[ "$(cat "$T13_ROOT/backend/bin/server-linux")" = "new-binary-bytes" ] && ok "T13 binary swapped in rehearsal" || bad "T13 binary swapped in rehearsal"
ls "$T13_ROOT/backups"/backend-"$COMMIT13"-*-server-linux >/dev/null 2>&1 && ok "T13 old binary kept as BACKUP-server-linux" || bad "T13 old binary kept as BACKUP-server-linux"

# --- T14: deploy_remote.py + metrics test ------------------------------
echo "--- T14: deploy_remote.py syntax + metrics-ports test ---"
python3 -c "import ast; ast.parse(open('$REPO/scripts/deploy_remote.py').read())" && ok "T14 deploy_remote.py parses" || bad "T14 deploy_remote.py parses"
# The metrics test is a standalone script (run from the repo root),
# not a pytest module.
if (cd "$REPO" && python3 scripts/test_deploy_metrics_ports.py > "$EVIDENCE_DIR/t14.out" 2>&1); then
  ok "T14 test_deploy_metrics_ports.py passes"
else
  bad "T14 test_deploy_metrics_ports.py passes (see $EVIDENCE_DIR/t14.out)"
fi

echo "=== RESULT: $PASS passed, $FAIL failed ==="
echo "evidence dir: $EVIDENCE_DIR"
echo "log: $LOG"
[ "$FAIL" -eq 0 ]
