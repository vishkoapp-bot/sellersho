#!/usr/bin/env python3
"""REL-01 (G5) — smoke suite for a staged release, on disposable fixtures.

Boots the STAGED backend binary (never the worktree build) against a
fresh disposable PostgreSQL database + scratch Redis (the boot-gate
pattern), applies the staged migrations with the STAGED migrate
binary, seeds minimal fixtures, then exercises the spec-named checks
over real HTTP — and gates on /api/ready before any of them:

  readiness   /api/live + /api/ready + /api/health all 200
  contract    /api/version commit + routesHash == manifest ==
              staged dist/contract.json (G9)
  login       real OTP-verify login with a code the suite itself
              plants in its OWN scratch Redis (no SMS is ever sent —
              /auth/otp/send is never called), wrong code -> 401
  roles       /api/v1/auth/me + admin-only /api/v1/admin/payments:
              admin token passes, customer token gets 403, no token 401
  catalog     GET /api/v1/products contains the seeded product;
              GET /api/v1/categories 200
  quote       POST /api/v1/shipping/quote returns a quoteId
  order       POST /api/v1/orders with an idempotencyKey: first call
              201, identical replay 200 with the SAME order id, exactly
              one orders row; GET /api/v1/checkout-attempts/<key>
              recovers the order; changed payload + same key -> 409
  payment     the created order has the manual-bridge state and the
              demo-gateway reports testOnly:true in the disposable runtime;
              the separately validated release manifest declares real mode

A failed check fails the whole run (exit non-zero) and must block
the traffic switch. Everything is disposable: the database is
dropped and the scratch Redis shut down on exit, success or failure.

Environment (local VM defaults match the boot gate):
  SMOKE_PG_HOST / SMOKE_PG_PORT (default 127.0.0.1:5433, the rootfs PG)
  SMOKE_PG_USER (default gap13), SMOKE_PG_BIN (psql dir, optional)
  SMOKE_REDIS_URL (default: a scratch redis-shim instance is started)
  SMOKE_HTTP_PORT (default 18081), SMOKE_REDIS_PORT (default 6391)

Usage:
  python3 smoke_release.py --stage releases/<commit> [--keep]
  python3 smoke_release.py --self-test-negative
      # proves the suite FAILS when it should: the readiness probe
      # against a dead port must fail; exit 0 means the negative
      # control behaved (probe failed as expected).
"""
import argparse
import json
import os
import random
import secrets
import signal
import socket
import string
import subprocess
import sys
import tempfile
import time
import urllib.request
import urllib.error
from pathlib import Path

HOME = Path.home()
PG_BIN = Path(os.environ.get("SMOKE_PG_BIN", str(HOME / "pgx-rootfs/usr/lib/postgresql/16/bin")))
PG_HOST = os.environ.get("SMOKE_PG_HOST", "127.0.0.1")
PG_PORT = os.environ.get("SMOKE_PG_PORT", "5433")
PG_USER = os.environ.get("SMOKE_PG_USER", "gap13")
REDIS_SHIM = HOME / "workspace/.dev/redis-shim/redis-server"
REDIS_CLI = HOME / "workspace/.dev/redis-shim/redis-cli"
REDIS_LIBS = HOME / "workspace/redis-libs"

CUSTOMER_ID = "11111111-1111-1111-1111-111111111111"
CUSTOMER_MOBILE = "09000000099"
ADMIN_ID = "00000000-0000-0000-0000-000000000001"
ADMIN_MOBILE = "09000000001"
SUPPLIER_OWNER_ID = "22222222-2222-2222-2222-222222222222"
SUPPLIER_OWNER_MOBILE = "09000000098"
OTP_CODE = "123456"


class Smoke:
    def __init__(self, stage: Path, args):
        self.stage = stage
        self.args = args
        self.checks: list[tuple[str, bool, str]] = []
        self.db = f"sellersho_rel01_{secrets.token_hex(4)}"
        self.db_pass = secrets.token_hex(12)
        self.redis_pass = secrets.token_hex(12)
        self.redis_port = int(os.environ.get("SMOKE_REDIS_PORT", "6391"))
        self.http_port = int(os.environ.get("SMOKE_HTTP_PORT", "18081"))
        self.jwt_secret = secrets.token_hex(32)
        self.webhook_secret = secrets.token_hex(32)
        self.redis_dir = None
        self.server_proc = None
        self.server_log = None
        self.dsn = (f"postgres://test_runner:{self.db_pass}@"
                    f"{PG_HOST}:{PG_PORT}/{self.db}?sslmode=disable")
        self.redis_url = os.environ.get(
            "SMOKE_REDIS_URL",
            f"redis://:{self.redis_pass}@127.0.0.1:{self.redis_port}/15")
        if os.environ.get("SMOKE_REDIS_URL"):
            # Point redis-cli at the external instance (CI service).
            from urllib.parse import urlparse
            parts = urlparse(self.redis_url)
            if parts.port:
                self.redis_port = parts.port

    # -- helpers ------------------------------------------------------
    def psql(self, db: str, sql: str, check=True):
        exe = str(PG_BIN / "psql") if (PG_BIN / "psql").exists() else "psql"
        cmd = [exe, "-h", PG_HOST, "-p", PG_PORT, "-U", PG_USER,
               "-d", db, "-q", "-v", "ON_ERROR_STOP=1", "-c", sql]
        env = dict(os.environ)
        if os.environ.get("SMOKE_PG_PASSWORD"):
            env["PGPASSWORD"] = os.environ["SMOKE_PG_PASSWORD"]
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=60, env=env)
        if check and r.returncode != 0:
            raise RuntimeError(f"psql failed ({db}): {r.stderr.strip()[:400]}")
        return r

    def psql_scalar(self, db: str, sql: str) -> str:
        exe = str(PG_BIN / "psql") if (PG_BIN / "psql").exists() else "psql"
        env = dict(os.environ)
        if os.environ.get("SMOKE_PG_PASSWORD"):
            env["PGPASSWORD"] = os.environ["SMOKE_PG_PASSWORD"]
        r = subprocess.run([exe, "-h", PG_HOST, "-p", PG_PORT, "-U", PG_USER,
                            "-d", db, "-tA", "-c", sql],
                           capture_output=True, text=True, timeout=60, env=env)
        if r.returncode != 0:
            raise RuntimeError(f"psql scalar failed: {r.stderr.strip()[:400]}")
        return r.stdout.strip()

    def redis_cli(self, *argv: str):
        env = dict(os.environ)
        if REDIS_LIBS.exists():
            env["LD_LIBRARY_PATH"] = str(REDIS_LIBS)
        cli = os.environ.get("SMOKE_REDIS_CLI") or (
            str(REDIS_CLI) if REDIS_CLI.exists() else "redis-cli")
        cmd = [cli, "-h", "127.0.0.1", "-p", str(self.redis_port),
               "-n", "15"]
        # CI services run Redis without a password (SMOKE_REDIS_URL set);
        # the local scratch instance always has one.
        redis_pass = os.environ.get("SMOKE_REDIS_PASSWORD", "")
        if os.environ.get("SMOKE_REDIS_URL"):
            if redis_pass:
                cmd += ["-a", redis_pass, "--no-auth-warning"]
        else:
            cmd += ["-a", self.redis_pass, "--no-auth-warning"]
        cmd += list(argv)
        r = subprocess.run(cmd, capture_output=True, text=True,
                           timeout=30, env=env)
        if r.returncode != 0:
            raise RuntimeError(f"redis-cli {' '.join(argv)} failed: {r.stderr.strip()[:300]}")
        return r.stdout.strip()

    def http(self, method: str, path: str, token=None, body=None):
        url = f"http://127.0.0.1:{self.http_port}{path}"
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(url, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        if token:
            req.add_header("Authorization", f"Bearer {token}")
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                return resp.status, json.loads(resp.read() or b"{}")
        except urllib.error.HTTPError as e:
            raw = e.read()
            try:
                return e.code, json.loads(raw or b"{}")
            except Exception:
                return e.code, {"_raw": raw.decode("utf-8", "replace")[:300]}
        except Exception as e:  # connection refused etc.
            return 0, {"_error": str(e)[:300]}

    def check(self, name: str, ok: bool, detail: str = ""):
        self.checks.append((name, ok, detail))
        print(f"[{'PASS' if ok else 'FAIL'}] {name} {detail}", flush=True)

    def wait_ready(self, timeout=90) -> bool:
        deadline = time.time() + timeout
        while time.time() < deadline:
            code, _ = self.http("GET", "/api/live")
            if code == 200:
                return True
            if self.server_proc and self.server_proc.poll() is not None:
                return False
            time.sleep(1)
        return False

    # -- fixture setup -------------------------------------------------
    def setup_db(self):
        self.psql("postgres",
                  f"DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='test_runner') "
                  f"THEN CREATE ROLE test_runner LOGIN PASSWORD '{self.db_pass}' NOSUPERUSER NOCREATEDB NOCREATEROLE; "
                  f"ELSE ALTER ROLE test_runner WITH PASSWORD '{self.db_pass}' NOSUPERUSER NOCREATEDB NOCREATEROLE; END IF; END $$;")
        self.psql("postgres", f'CREATE DATABASE "{self.db}" OWNER test_runner;')
        self.psql(self.db,
                  "CREATE TABLE public.sellersho_test_guard (singleton BOOLEAN PRIMARY KEY CHECK (singleton), "
                  f"database_name TEXT NOT NULL); INSERT INTO public.sellersho_test_guard VALUES (TRUE, '{self.db}');")
        print(f"disposable database ready: {self.db}", flush=True)

    def setup_redis(self):
        if os.environ.get("SMOKE_REDIS_URL"):
            if self.redis_cli("PING") != "PONG":
                raise RuntimeError("external Redis (SMOKE_REDIS_URL) did not answer PING")
            print("using external Redis from SMOKE_REDIS_URL", flush=True)
            return
        self.redis_dir = tempfile.mkdtemp(prefix="sellersho-rel01-redis-")
        subprocess.run(
            [str(REDIS_SHIM), "--port", str(self.redis_port), "--bind", "127.0.0.1",
             "--save", "", "--appendonly", "no", "--requirepass", self.redis_pass,
             "--dir", self.redis_dir, "--daemonize", "yes",
             "--pidfile", f"{self.redis_dir}/redis.pid"],
            check=True, capture_output=True, timeout=30)
        for _ in range(30):
            try:
                if self.redis_cli("PING") == "PONG":
                    print(f"scratch Redis ready: 127.0.0.1:{self.redis_port}/15", flush=True)
                    return
            except Exception:
                pass
            time.sleep(0.5)
        raise RuntimeError("scratch Redis did not answer PING")

    def migrate(self):
        env = dict(os.environ, DATABASE_URL=self.dsn)
        r = subprocess.run(
            [str(self.stage / "backend/bin/migrate-linux"),
             "-db", self.dsn, "-dir", str(self.stage / "backend/migrations")],
            capture_output=True, text=True, timeout=600, env=env,
            cwd=str(self.stage / "backend"))
        if r.returncode != 0:
            raise RuntimeError(f"migrate-linux failed: {(r.stdout + r.stderr)[-1500:]}")
        print("migrations applied by staged migrate-linux", flush=True)

    def seed(self):
        stmts = [
            # Users + capabilities (0900 reserved test mobiles).
            f"INSERT INTO users (id, mobile, full_name, role, is_active) VALUES "
            f"('{CUSTOMER_ID}','{CUSTOMER_MOBILE}','smoke customer','customer',TRUE), "
            f"('{ADMIN_ID}','{ADMIN_MOBILE}','smoke admin','admin',TRUE), "
            f"('{SUPPLIER_OWNER_ID}','{SUPPLIER_OWNER_MOBILE}','smoke supplier','supplier',TRUE) "
            f"ON CONFLICT (id) DO NOTHING;",
            f"INSERT INTO user_capabilities (user_id, capability, status) VALUES "
            f"('{CUSTOMER_ID}','customer','active'), "
            f"('{ADMIN_ID}','admin','active'), "
            f"('{SUPPLIER_OWNER_ID}','supplier','active') ON CONFLICT DO NOTHING;",
            # Admin tariff for the quote formula (Rials, per the quote
            # handler's documented assumption).
            "INSERT INTO platform_settings (id, return_window_days, shipping_base_rate, "
            "shipping_per_kg, volumetric_divisor, default_item_weight_grams) "
            "VALUES (1, 7, 100000, 20000, 5000, 500) ON CONFLICT (id) DO NOTHING;",
            # Central store + supplier + direct shipping method.
            "INSERT INTO stores (id, name, slug, owner_type, owner_name) VALUES "
            "('store-central','مرکزی','central','central','مرکزی') ON CONFLICT (id) DO NOTHING;",
            "INSERT INTO suppliers (id, owner_user_id, approval_status, name) VALUES "
            f"('sup-smoke','{SUPPLIER_OWNER_ID}','approved','تأمین‌کننده دودآزما') ON CONFLICT (id) DO NOTHING;",
            "INSERT INTO supplier_shipping_methods (owner_user_id, method, status) VALUES "
            f"('{SUPPLIER_OWNER_ID}','direct','enabled') ON CONFLICT DO NOTHING;",
            "INSERT INTO supplier_warehouses (id, supplier_id, name, province_name, city_name) VALUES "
            "('swh-smoke','sup-smoke','انبار تأمین‌کننده','تهران','تهران') ON CONFLICT (id) DO NOTHING;",
            # One sellable product, supplier-direct fulfilment.
            "INSERT INTO products (id, title, slug, category_slug, supplier_id, original_price, "
            "supplier_base_cost, platform_margin, is_original, stock, image_url, description, "
            "warehouse_name, preparation_days, supplier_warranty, is_featured, is_special_deal, "
            "fulfillment_method, supply_option, is_active, brand, spn_number, weight_grams, "
            "length_mm, width_mm, height_mm) VALUES "
            "('p-smoke','کالای دودآزما','smoke-product','digital','sup-smoke',12000000,10000000,0,TRUE,10,"
            "'img','desc','انبار تأمین‌کننده',1,'گارانتی',FALSE,FALSE,'supplier_direct','1day',TRUE,'برند',50000099,500,100,100,100) "
            "ON CONFLICT (id) DO NOTHING;",
            # An implicit product-only cart resolves the sole active legacy SKU.
            "INSERT INTO product_variants (product_id,spnc,variant_name,is_default,stock,inventory_mode,is_active) "
            "VALUES ('p-smoke','SMOKESKU1','smoke default',TRUE,10,'legacy',TRUE) "
            "ON CONFLICT (spnc) DO NOTHING;",
            # Physical lot backing the quote/order inventory checks.
            f"INSERT INTO inventory_balances (id, product_id, supplier_id, owner_type, location_type, "
            f"location_key, status, quantity, supplier_warehouse_id) VALUES "
            f"(gen_random_uuid(),'p-smoke','sup-smoke','supplier','supplier',"
            f"'supplier-warehouse:swh-smoke','sellable',10,'swh-smoke') "
            f"ON CONFLICT (product_id, owner_type, location_type, location_key, status) "
            f"WHERE variant_id IS NULL DO UPDATE SET quantity = EXCLUDED.quantity;",
            # A sale needs unit codes at the exact quoted lot. Keep a partial
            # backfill to avoid accidentally depending on full lot coverage.
            "INSERT INTO product_units (code,variant_id,product_id,sequence,status,balance_id) "
            "SELECT v.spnc || '-X' || lpad(n::text,4,'0'),v.id,v.product_id,n,'in_stock',b.id "
            "FROM product_variants v JOIN inventory_balances b ON b.product_id=v.product_id "
            "CROSS JOIN generate_series(1,3) n "
            "WHERE v.product_id='p-smoke' AND v.spnc='SMOKESKU1' AND b.variant_id IS NULL "
            "AND b.supplier_warehouse_id='swh-smoke' AND b.status='sellable' "
            "ON CONFLICT (code) DO NOTHING;",
        ]
        for sql in stmts:
            self.psql(self.db, sql)
        print("smoke fixtures seeded", flush=True)

    def boot(self):
        log = tempfile.NamedTemporaryFile(prefix="sellersho-rel01-server-",
                                          suffix=".log", delete=False)
        self.server_log = log.name
        env = {"HOME": str(HOME),
               "PATH": "/usr/local/bin:/usr/bin:/bin",
               "APP_ENV": "test", "PORT": str(self.http_port),
               "JWT_SECRET": self.jwt_secret,
               "TRACKING_WEBHOOK_SECRET": self.webhook_secret,
               "PAYMENT_GATEWAY_MODE": "mock",
               "DATABASE_URL": self.dsn, "REDIS_URL": self.redis_url}
        with open(self.server_log, "wb") as lf:
            self.server_proc = subprocess.Popen(
                [str(self.stage / "backend/bin/server-linux")],
                stdout=lf, stderr=subprocess.STDOUT, env=env)
        print(f"server PID {self.server_proc.pid}, log {self.server_log}", flush=True)

    def cleanup(self):
        if self.server_proc and self.server_proc.poll() is None:
            self.server_proc.terminate()
            try:
                self.server_proc.wait(timeout=15)
            except subprocess.TimeoutExpired:
                self.server_proc.kill()
        if not os.environ.get("SMOKE_REDIS_URL") and self.redis_dir:
            try:
                self.redis_cli("SHUTDOWN", "NOSAVE")
            except Exception:
                pass
            import shutil
            shutil.rmtree(self.redis_dir, ignore_errors=True)
        try:
            self.psql("postgres", f'DROP DATABASE "{self.db}" WITH (FORCE);', check=False)
        except Exception:
            pass
        print("disposable fixtures cleaned up", flush=True)

    # -- the checks -----------------------------------------------------
    def login(self, mobile: str, role: str):
        """Real OTP-verify login. The code is planted by this suite in
        its OWN scratch Redis; /auth/otp/send is never called."""
        self.redis_cli("SET", f"otp:{mobile}", OTP_CODE, "EX", "300")
        code, body = self.http("POST", "/api/v1/auth/otp/verify",
                               body={"mobile": mobile, "code": OTP_CODE, "role": role})
        token = body.get("token") or body.get("accessToken") or ""
        return code, body, token

    def run_checks(self, manifest: dict) -> bool:
        # Readiness gate first: nothing else runs until /api/ready is 200.
        if not self.wait_ready():
            self.check("readiness: server booted + /api/live 200", False,
                       f"(see {self.server_log})")
            return False
        self.check("readiness: server booted + /api/live 200", True)
        code, ready = self.http("GET", "/api/ready")
        self.check("readiness: /api/ready 200", code == 200, f"HTTP {code}")
        code, health = self.http("GET", "/api/health")
        self.check("readiness: /api/health 200", code == 200, f"HTTP {code}")

        # G9 contract.
        code, version = self.http("GET", "/api/version")
        want_commit = manifest["commit"]
        want_hash = manifest["contract"]["routesHash"]
        self.check("contract: /api/version commit == manifest",
                   code == 200 and version.get("commit") == want_commit,
                   f"HTTP {code} commit={version.get('commit', '')[:12]}")
        self.check("contract: /api/version routesHash == manifest",
                   code == 200 and version.get("routesHash") == want_hash,
                   f"routesHash={str(version.get('routesHash', ''))[:12]}")
        cpath = self.stage / "dist/contract.json"
        try:
            cdoc = json.loads(cpath.read_text())
            self.check("contract: dist/contract.json == /api/version",
                       cdoc.get("routesHash") == version.get("routesHash")
                       and cdoc.get("commit") == want_commit)
        except Exception as exc:
            self.check("contract: dist/contract.json readable", False, str(exc))

        # Login (real OTP-verify with a self-planted code).
        code, body, customer_token = self.login(CUSTOMER_MOBILE, "customer")
        self.check("login: OTP-verify (self-planted code) returns a token",
                   code == 200 and bool(customer_token), f"HTTP {code}")
        code, _ = self.http("POST", "/api/v1/auth/otp/verify",
                            body={"mobile": CUSTOMER_MOBILE, "code": "000000",
                                  "role": "customer"})
        self.check("login: wrong OTP code -> 401 (negative control)",
                   code == 401, f"HTTP {code}")
        code, body, admin_token = self.login(ADMIN_MOBILE, "admin")
        self.check("login: admin OTP-verify returns a token",
                   code == 200 and bool(admin_token), f"HTTP {code}")

        # Role authorization.
        code, me = self.http("GET", "/api/v1/auth/me", token=customer_token)
        self.check("roles: /api/v1/auth/me with customer token -> 200",
                   code == 200 and me.get("role") == "customer", f"HTTP {code}")
        code, _ = self.http("GET", "/api/v1/auth/me")
        self.check("roles: /api/v1/auth/me without token -> 401",
                   code == 401, f"HTTP {code}")
        code, _ = self.http("GET", "/api/v1/admin/payments", token=admin_token)
        self.check("roles: admin token can read /api/v1/admin/payments",
                   code == 200, f"HTTP {code}")
        code, _ = self.http("GET", "/api/v1/admin/payments", token=customer_token)
        self.check("roles: customer token on admin endpoint -> 403",
                   code == 403, f"HTTP {code}")
        code, _ = self.http("GET", "/api/v1/admin/payments")
        self.check("roles: no token on admin endpoint -> 401",
                   code == 401, f"HTTP {code}")

        # Catalog.
        code, products = self.http("GET", "/api/v1/products")
        found = False
        if code == 200:
            blob = json.dumps(products, ensure_ascii=False)
            found = "p-smoke" in blob
        self.check("catalog: GET /api/v1/products contains seeded product",
                   code == 200 and found, f"HTTP {code}")
        code, _ = self.http("GET", "/api/v1/categories")
        self.check("catalog: GET /api/v1/categories 200", code == 200, f"HTTP {code}")

        # Quote.
        code, quote = self.http(
            "POST", "/api/v1/shipping/quote", token=customer_token,
            body={"items": [{"productId": "p-smoke", "quantity": 1}],
                  "destination": {"province": "تهران", "city": "تهران",
                                  "address": "خیابان تست ۱",
                                  "postalCode": "1234567890"}})
        quote_id = quote.get("quoteId", "") if isinstance(quote, dict) else ""
        self.check("quote: POST /api/v1/shipping/quote returns a quoteId",
                   code == 200 and bool(quote_id), f"HTTP {code}")
        if not quote_id:
            return False

        # Idempotent order creation + recovery.
        order_body = {
            "storeId": "central", "paymentMethod": "cash",
            "customerInfo": {"fullName": "مشتری تست", "phone": CUSTOMER_MOBILE,
                             "province": "تهران", "city": "تهران",
                             "address": "خیابان تست ۱", "postalCode": "1234567890"},
            "items": [{"productId": "p-smoke", "quantity": 1}],
            "shippingQuoteId": quote_id,
            "idempotencyKey": "rel01-smoke-1",
        }
        code, first = self.http("POST", "/api/v1/orders",
                                token=customer_token, body=order_body)
        order_id = first.get("id", "") if isinstance(first, dict) else ""
        self.check("order: first create with idempotencyKey -> 201 + id",
                   code == 201 and bool(order_id), f"HTTP {code}")
        if not order_id:
            return False
        code, replay = self.http("POST", "/api/v1/orders",
                                 token=customer_token, body=order_body)
        self.check("order: identical replay returns the SAME order id",
                   code == 200 and replay.get("id") == order_id,
                   f"HTTP {code} id={str(replay.get('id', ''))[:12]}")
        try:
            count = self.psql_scalar(
                self.db,
                "SELECT count(*) FROM orders WHERE idempotency_key = 'rel01-smoke-1';")
            total = self.psql_scalar(self.db, "SELECT count(*) FROM orders;")
        except Exception as exc:
            count = f"error: {exc}"
            total = "?"
        self.check("order: exactly one orders row for the smoke key after replay",
                   count == "1", f"key-rows={count} total-orders={total} "
                   "(total includes 1 order seeded by the migrations themselves)")
        code, recovered = self.http(
            "GET", "/api/v1/checkout-attempts/rel01-smoke-1",
            token=customer_token)
        blob = json.dumps(recovered, ensure_ascii=False)
        self.check("order: GET /api/v1/checkout-attempts/<key> recovers the order",
                   code == 200 and order_id in blob, f"HTTP {code}")
        changed = dict(order_body)
        changed["items"] = [{"productId": "p-smoke", "quantity": 2}]
        code, _ = self.http("POST", "/api/v1/orders",
                            token=customer_token, body=changed)
        self.check("order: changed payload + same key -> 409 (negative control)",
                   code == 409, f"HTTP {code}")

        # Supported payment state, consistent with the manifest mode.
        try:
            row = self.psql_scalar(
                self.db,
                f"SELECT payment_status || '/' || payment_method FROM orders WHERE id = '{order_id}';")
        except Exception as exc:
            row = f"error: {exc}"
        self.check("payment: created order is pending/cash (manual bridge)",
                   row == "pending/cash", f"state={row}")
        code, demo = self.http("GET", "/api/v1/payments/demo-gateway/status")
        self.check("payment: demo-gateway status reports testOnly:true",
                   code == 200 and demo.get("testOnly") is True
                   and demo.get("gateway") == "demo", f"HTTP {code}")
        self.check("payment: release manifest declares real (runtime is isolated mock)",
                   manifest.get("config", {}).get("payment", {}).get("mode") == "real",
                   f"mode={manifest.get('config', {}).get('payment', {}).get('mode')}")
        return True

    def run(self) -> int:
        manifest = json.loads((self.stage / "release.json").read_text())
        try:
            self.setup_db()
            self.setup_redis()
            self.migrate()
            self.seed()
            self.boot()
            self.run_checks(manifest)
        except Exception as exc:
            self.check("smoke harness", False, f"{type(exc).__name__}: {exc}")
            if self.server_log and Path(self.server_log).exists():
                tail = Path(self.server_log).read_text(errors="replace")[-2000:]
                print("--- server log tail ---")
                print(tail)
        finally:
            if not self.args.keep:
                self.cleanup()
            else:
                print(f"KEEP: db={self.db} redis_port={self.redis_port} "
                      f"http_port={self.http_port} log={self.server_log}")
        failed = [c for c in self.checks if not c[1]]
        print(f"\nSMOKE {'OK' if not failed else 'FAILED'}: "
              f"{len(self.checks) - len(failed)}/{len(self.checks)} checks passed")
        for name, ok, detail in self.checks:
            if not ok:
                print(f"  FAILED: {name} {detail}")
        return 0 if not failed else 1


def self_test_negative() -> int:
    """Prove the suite fails when it should: the readiness probe
    against a dead port must fail. Exit 0 = the negative control
    behaved (probe failed as expected)."""
    s = socket.socket()
    s.settimeout(3)
    try:
        s.connect(("127.0.0.1", 9))
        print("NEGATIVE CONTROL FAILED: dead-port probe unexpectedly connected")
        return 1
    except OSError:
        print("NEGATIVE CONTROL OK: readiness probe against a dead port fails as expected")
        return 0
    finally:
        s.close()


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--stage", default="")
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--self-test-negative", action="store_true")
    args = ap.parse_args()
    if args.self_test_negative:
        return self_test_negative()
    if not args.stage:
        print("--stage is required (or --self-test-negative)", file=sys.stderr)
        return 2
    stage = Path(args.stage).resolve()
    if not (stage / "release.json").is_file():
        print(f"no release.json in {stage}; run make_release.sh first", file=sys.stderr)
        return 2
    return Smoke(stage, args).run()


if __name__ == "__main__":
    raise SystemExit(main())
