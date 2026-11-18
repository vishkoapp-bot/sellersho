"""OBS-01 deploy-fix dry-run harness: the merged-config assertion from
scripts/deploy_remote.py, executed standalone (no Docker, no prod).

The deploy script cannot be imported — module import runs the deploy — so
the assertion functions are extracted from its source by AST (the same
extract-by-content pattern as the OBS-01 F-4 dry run) and exec'd in an
isolated namespace. If the extraction fails, every test here fails loudly
instead of silently passing against a stale copy.

The merge fixture uses the REAL repo compose files
(docker-compose.prod.yml, docker-compose.ssl.yml,
docker-compose.metrics-ports.yml) and a small MODEL of `docker compose
config` merge semantics for the `ports` key only (!reset clears,
!override replaces, plain lists merge by target port with later entries
winning). The model is not compose itself: the real proof of the merge is
the server-side `docker compose config --format json` + `docker port`
check on the redeploy (gates G-F1/G-F8). The model exists so a regression
in file ORDER or in the !reset/!override pairing fails here first.

Run from the repo root:  python3 scripts/test_deploy_metrics_ports.py
"""
import json
import re
import shutil
import subprocess
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

import yaml

REPO = Path(__file__).resolve().parent.parent
DEPLOY_REMOTE = REPO / "scripts" / "deploy_remote.py"


def extract_assertion_namespace():
    """Exec the assertion functions + constants from deploy_remote.py
    source (AST-selected, never imported) and return their namespace."""
    import ast

    tree = ast.parse(DEPLOY_REMOTE.read_text())
    wanted_functions = {"_metrics_published_ports", "assert_metrics_ports"}
    wanted_assigns = {"METRICS_PORTS_EXPECTED", "METRICS_PORT_TARGETS"}
    nodes = []
    for node in tree.body:
        if isinstance(node, ast.FunctionDef) and node.name in wanted_functions:
            nodes.append(node)
        elif isinstance(node, ast.Assign) and any(
            isinstance(t, ast.Name) and t.id in wanted_assigns for t in node.targets
        ):
            nodes.append(node)
    if len(nodes) != 4:
        raise AssertionError(
            f"expected 4 assertion nodes in deploy_remote.py, extracted {len(nodes)}; "
            "the harness is stale — update it, do not delete this check"
        )
    module = ast.Module(body=nodes, type_ignores=[])
    # The functions use deploy_remote.py's module-level `import json`;
    # the harness supplies that same stdlib module (nothing else).
    namespace = {"json": json}
    exec(compile(module, str(DEPLOY_REMOTE), "exec"), namespace)  # noqa: S102 — test harness, repo-owned source
    return namespace


NS = extract_assertion_namespace()
assert_metrics_ports = NS["assert_metrics_ports"]
EXPECTED = NS["METRICS_PORTS_EXPECTED"]


def merged_config_json(services_ports):
    """Build a `docker compose config --format json` shaped payload from
    {service: [port entries]}. Dict-form entries are what compose emits;
    string entries are accepted too (short syntax passthrough)."""
    return json.dumps({"services": {s: {"ports": p} for s, p in services_ports.items()}})


def dict_port(host_ip, published, target):
    return {"mode": "ingress", "target": target, "published": published,
            "host_ip": host_ip, "protocol": "tcp"}


GOOD_MERGED = {
    "backend": [dict_port("127.0.0.1", 8080, 8080), dict_port("127.0.0.1", 9090, 9090)],
    "edge": [dict_port("", 80, 80), dict_port("", 443, 443),
             dict_port("127.0.0.1", 2019, 2019)],
    "frontend": [dict_port("127.0.0.1", 8081, 8081)],
}


# --- minimal `docker compose config` merge model for the `ports` key ---
class _Reset(list):
    pass


class _Override(list):
    pass


def _compose_loader():
    loader = yaml.SafeLoader
    loader.add_constructor("!reset", lambda l, n: _Reset(l.construct_sequence(n)))
    loader.add_constructor("!override", lambda l, n: _Override(l.construct_sequence(n)))
    return loader


def _port_key(entry):
    """Merge key for one short-syntax port entry: target (+ protocol)."""
    protocol = "tcp"
    if "/" in entry:
        entry, protocol = entry.rsplit("/", 1)
    parts = entry.split(":")
    return (parts[-1], protocol)


def model_merge_ports(*files):
    """Merge the `ports` lists of the given compose files in order, the
    way `docker compose config` does for this key: a plain list extends
    (a later entry for the same target replaces the earlier one), `!reset`
    clears the list, `!override` replaces it wholesale. Returns
    {service: [short-syntax port strings]}. Ports only — every other key
    (notably environment, which carries interpolated secrets) is ignored
    and never read."""
    merged = {}
    for path in files:
        data = yaml.load(Path(path).read_text(), Loader=_compose_loader())
        for service, body in (data.get("services") or {}).items():
            if "ports" not in (body or {}):
                continue
            ports = body["ports"]
            current = merged.setdefault(service, {})
            if isinstance(ports, _Reset):
                current.clear()
                for entry in ports:
                    current[_port_key(entry)] = entry
            elif isinstance(ports, _Override):
                current.clear()
                for entry in ports:
                    current[_port_key(entry)] = entry
            else:
                for entry in ports:
                    current[_port_key(entry)] = entry
    return {service: list(entries.values()) for service, entries in merged.items()}


def short_to_dict(entry):
    """Short-syntax "[[host_ip:]published:]target[/protocol]" -> the dict
    form `docker compose config --format json` emits."""
    protocol = "tcp"
    if "/" in entry:
        entry, protocol = entry.rsplit("/", 1)
    parts = entry.split(":")
    target = int(parts[-1])
    if len(parts) == 1:
        return {"mode": "ingress", "target": target, "published": target,
                "host_ip": "", "protocol": protocol}
    published = int(parts[-2])
    host_ip = ":".join(parts[:-2])
    return {"mode": "ingress", "target": target, "published": published,
            "host_ip": host_ip, "protocol": protocol}


class TestAssertMetricsPorts(unittest.TestCase):
    def test_all_three_present_dict_form(self):
        self.assertTrue(assert_metrics_ports(merged_config_json(GOOD_MERGED)))

    def test_all_three_present_string_form(self):
        payload = merged_config_json({
            "backend": ["127.0.0.1:8080:8080", "127.0.0.1:9090:9090"],
            "edge": ["80:80", "443:443", "127.0.0.1:2019:2019"],
            "frontend": ["127.0.0.1:8081:8081"],
        })
        self.assertTrue(assert_metrics_ports(payload))

    def test_missing_frontend_raises_and_names_service(self):
        merged = dict(GOOD_MERGED)
        merged["frontend"] = []
        with self.assertRaises(RuntimeError) as ctx:
            assert_metrics_ports(merged_config_json(merged))
        self.assertIn("frontend", str(ctx.exception))

    def test_missing_backend_raises(self):
        merged = dict(GOOD_MERGED)
        merged["backend"] = [dict_port("127.0.0.1", 8080, 8080)]
        with self.assertRaises(RuntimeError) as ctx:
            assert_metrics_ports(merged_config_json(merged))
        self.assertIn("backend", str(ctx.exception))

    def test_missing_edge_raises_when_required(self):
        merged = {k: v for k, v in GOOD_MERGED.items() if k != "edge"}
        with self.assertRaises(RuntimeError) as ctx:
            assert_metrics_ports(merged_config_json(merged), require_edge=True)
        self.assertIn("edge", str(ctx.exception))

    def test_missing_edge_ok_when_not_required(self):
        merged = {k: v for k, v in GOOD_MERGED.items() if k != "edge"}
        self.assertTrue(assert_metrics_ports(merged_config_json(merged), require_edge=False))

    def test_non_loopback_metrics_publish_refused(self):
        for bad in ("0.0.0.0:2019:2019", "2019:2019"):
            merged = dict(GOOD_MERGED)
            merged["edge"] = [dict_port("", 80, 80), dict_port("", 443, 443),
                              bad if isinstance(bad, str) else bad]
            with self.assertRaises(RuntimeError, msg=bad):
                assert_metrics_ports(merged_config_json(merged))

    def test_public_non_metrics_ports_allowed(self):
        # Edge 80/443 are legitimately public; only the metrics targets
        # (9090/2019/8081) must be loopback-only.
        self.assertTrue(assert_metrics_ports(merged_config_json(GOOD_MERGED)))

    def test_invalid_json_raises(self):
        with self.assertRaises(RuntimeError):
            assert_metrics_ports("{not json")

    def test_error_never_prints_env_values(self):
        # A fake env value rides in the payload; no failure message may
        # repeat it (the merged config carries interpolated secrets).
        fake_value = "FAKE-SECRET-VALUE-7f3a9c"
        payload = json.dumps({"services": {
            "backend": {"ports": [], "environment": {"SOME_KEY": fake_value}},
            "edge": {"ports": []}, "frontend": {"ports": []}}})
        with self.assertRaises(RuntimeError) as ctx:
            assert_metrics_ports(payload)
        self.assertNotIn(fake_value, str(ctx.exception))
        self.assertNotIn("SOME_KEY", str(ctx.exception))


class TestThreeListsOneConstant(unittest.TestCase):
    """Audit F-11: the metrics-ports file must be named in compose_files,
    saved_files, the ship copy loop and rollback_files through ONE
    constant. A hardcoded second copy of the filename is a failure."""

    def test_single_constant_feeds_all_four_uses(self):
        src = DEPLOY_REMOTE.read_text()
        self.assertEqual(src.count("'docker-compose.metrics-ports.yml'"), 1,
                         "filename must appear literally exactly once (the constant)")
        self.assertIn("METRICS_PORTS_FILE = 'docker-compose.metrics-ports.yml'", src)
        for use in ("compose_files.append(root / METRICS_PORTS_FILE)",
                    "METRICS_PORTS_FILE]",
                    "shutil.copy2(release / METRICS_PORTS_FILE, root / METRICS_PORTS_FILE)",
                    "rollback_files.insert(3, root / METRICS_PORTS_FILE)"):
            self.assertIn(use, src, f"missing use of the constant: {use}")

    def test_metrics_file_ordered_after_ssl(self):
        src = DEPLOY_REMOTE.read_text()
        ssl_at = src.index("compose_files.append(root / 'docker-compose.ssl.yml')")
        metrics_at = src.index("compose_files.append(root / METRICS_PORTS_FILE)")
        self.assertLess(ssl_at, metrics_at,
                        "metrics-ports file must come AFTER ssl.yml in compose_files")

    def test_rollback_order_mirrors_compose_order(self):
        src = DEPLOY_REMOTE.read_text()
        self.assertLess(src.index("rollback_files.insert(2, root / 'docker-compose.ssl.yml')"),
                        src.index("rollback_files.insert(3, root / METRICS_PORTS_FILE)"))

    def test_edge_up_uses_force_recreate(self):
        src = DEPLOY_REMOTE.read_text()
        self.assertIn("compose('up', '-d', '--no-deps', '--force-recreate', 'edge')", src)
        self.assertIn("run(*cmd, 'up', '-d', '--no-deps', '--force-recreate', 'edge')", src)


class TestMergeFixture(unittest.TestCase):
    """Fixture incl. a `!reset` file: the REAL repo compose files merged
    by the ports model, then checked by the REAL assertion."""

    def test_real_files_merge_to_all_three_publishes(self):
        merged = model_merge_ports(REPO / "docker-compose.prod.yml",
                                   REPO / "docker-compose.ssl.yml",
                                   REPO / "docker-compose.metrics-ports.yml")
        # G-F1 frontend condition: exactly the 8081 publish, no port 80.
        self.assertEqual(merged["frontend"], ["127.0.0.1:8081:8081"])
        self.assertIn("127.0.0.1:9090:9090", merged["backend"])
        self.assertIn("127.0.0.1:2019:2019", merged["edge"])
        payload = merged_config_json(
            {s: [short_to_dict(e) for e in entries] for s, entries in merged.items()})
        self.assertTrue(assert_metrics_ports(payload))

    def test_without_metrics_file_frontend_is_wiped(self):
        # The pre-fix shape: prod.yml + ssl.yml only. ssl.yml's !reset
        # wipes the frontend ports, so the assertion must refuse.
        merged = model_merge_ports(REPO / "docker-compose.prod.yml",
                                   REPO / "docker-compose.ssl.yml")
        self.assertEqual(merged["frontend"], [])
        payload = merged_config_json(
            {s: [short_to_dict(e) for e in entries] for s, entries in merged.items()})
        with self.assertRaises(RuntimeError) as ctx:
            assert_metrics_ports(payload)
        self.assertIn("frontend", str(ctx.exception))

    def test_reset_without_override_stays_wiped(self):
        # Explicit fixture (not the repo files): a !reset file with NO
        # later !override must leave the service portless.
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp) / "base.yml"
            reset = Path(tmp) / "reset.yml"
            base.write_text("services:\n  frontend:\n    ports:\n      - \"80:80\"\n")
            reset.write_text("services:\n  frontend:\n    ports: !reset []\n")
            merged = model_merge_ports(base, reset)
        self.assertEqual(merged["frontend"], [])


class TestComposeCaptureWiring(unittest.TestCase):
    """The assertion call site is `compose('config', '--format', 'json',
    capture=True)` — the one link the assertion tests above do not cover.
    Exec the REAL `run` + `compose` functions from deploy_remote.py
    against a stubbed subprocess and prove the capture kwarg threads
    through (default False: unchanged behavior for every other caller)."""

    def _namespace(self):
        import ast
        from types import SimpleNamespace

        tree = ast.parse(DEPLOY_REMOTE.read_text())
        nodes = [n for n in tree.body
                 if isinstance(n, ast.FunctionDef) and n.name in ("run", "compose")]
        self.assertEqual(len(nodes), 2, "could not extract run + compose")
        calls = []

        def fake_run(args, check=None, text=None, stdout=None, **kwargs):
            calls.append({"args": args, "stdout": stdout})
            return SimpleNamespace(stdout='{"services":{}}' if stdout == "PIPE" else None)

        namespace = {
            "subprocess": SimpleNamespace(PIPE="PIPE", run=fake_run),
            "root": Path("/tmp/fake-drup"),
            "compose_files": [Path("/tmp/fake-drup/docker-compose.yml"),
                              Path("/tmp/fake-drup/docker-compose.ssl.yml"),
                              Path("/tmp/fake-drup/docker-compose.metrics-ports.yml")],
        }
        module = ast.Module(body=nodes, type_ignores=[])
        exec(compile(module, str(DEPLOY_REMOTE), "exec"), namespace)  # noqa: S102
        return namespace, calls

    def test_config_json_capture_returns_stdout(self):
        namespace, calls = self._namespace()
        out = namespace["compose"]("config", "--format", "json", capture=True)
        self.assertEqual(out, '{"services":{}}')
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["stdout"], "PIPE")
        argv = calls[0]["args"]
        self.assertEqual(argv[0:2], ("docker", "compose"))
        files = [argv[i + 1] for i, a in enumerate(argv) if a == "-f"]
        self.assertEqual(files, ["/tmp/fake-drup/docker-compose.yml",
                                 "/tmp/fake-drup/docker-compose.ssl.yml",
                                 "/tmp/fake-drup/docker-compose.metrics-ports.yml"])

    def test_plain_compose_call_unchanged(self):
        namespace, calls = self._namespace()
        out = namespace["compose"]("up", "-d", "--no-deps", "edge")
        self.assertIsNone(out)
        self.assertIsNone(calls[0]["stdout"])


class TestMetricsFileShippedBeforeConfig(unittest.TestCase):
    """Critic B-1 regression: on the first redeploy carrying the fix,
    docker-compose.metrics-ports.yml does NOT exist on the server yet —
    it exists only in the release dir. compose_files already names it,
    so `docker compose config` fails (missing -f file) unless the deploy
    ships it BEFORE the first config invocation. The old ship step ran
    after the migrations, downstream of the failing config, so the
    deploy could never succeed. These tests run REAL `docker compose
    config` (client-side only, no daemon) against a fixture reproducing
    the redeploy-time server state."""

    # Synthetic base/prod/ssl files: same port structure as the real
    # ones, but no ${...} interpolation (the real prod/ssl files need
    # runtime env). The metrics file IS the real repo file, copied from
    # the release dir — the merge proof must cover the file that ships.
    BASE_YML = """services:
  backend:
    image: example/backend:latest
    ports:
      - "127.0.0.1:8080:8080"
  frontend:
    image: nginx:alpine
    ports:
      - "80:80"
"""
    PROD_YML = """services:
  backend:
    ports:
      - "127.0.0.1:9090:9090"
  frontend:
    ports:
      - "127.0.0.1:8081:8081"
"""
    SSL_YML = """services:
  frontend:
    ports: !reset []
  edge:
    image: caddy:2-alpine
    ports:
      - "80:80"
      - "443:443"
"""

    def _docker(self):
        """docker binary: PATH first, then the VM's static install
        (the critic's fixtures used ~/docker-static/docker)."""
        found = shutil.which("docker")
        if found:
            return found
        static = Path.home() / "docker-static" / "docker"
        if static.is_file():
            return str(static)
        return None

    def _fixture(self):
        """Redeploy-time server state: root has base/prod/ssl but NO
        metrics file; the release dir HAS the real metrics file."""
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name) / "root"
        release = root / "releases" / ("a" * 40)
        release.mkdir(parents=True)
        (root / "docker-compose.yml").write_text(self.BASE_YML)
        (root / "docker-compose.prod.yml").write_text(self.PROD_YML)
        (root / "docker-compose.ssl.yml").write_text(self.SSL_YML)
        shutil.copy2(REPO / "docker-compose.metrics-ports.yml",
                     release / "docker-compose.metrics-ports.yml")
        return root, release

    def _config(self, docker, root, extra_files=()):
        cmd = [docker, "compose", "--project-directory", str(root)]
        for name in ("docker-compose.yml", "docker-compose.prod.yml",
                     "docker-compose.ssl.yml") + tuple(extra_files):
            cmd += ["-f", str(root / name)]
        cmd += ["config", "--quiet"]
        return subprocess.run(cmd, capture_output=True, text=True)

    def test_config_fails_when_metrics_file_absent_from_root(self):
        # NEGATIVE CONTROL (pre-fix ordering): the file is in the
        # release only, compose_files names root / metrics file, config
        # is invoked before any ship. Real compose must refuse.
        docker = self._docker()
        if docker is None:
            self.skipTest("no docker binary available for the real-compose check")
        root, _release = self._fixture()
        result = self._config(docker, root,
                              extra_files=("docker-compose.metrics-ports.yml",))
        self.assertNotEqual(result.returncode, 0,
                            "compose config must fail when the -f file is absent")
        self.assertIn("no such file or directory", result.stderr)

    def test_ship_then_config_succeeds(self):
        # POSITIVE (fixed ordering): the exact ship operation the deploy
        # now performs first in its try block, then the same config.
        docker = self._docker()
        if docker is None:
            self.skipTest("no docker binary available for the real-compose check")
        root, release = self._fixture()
        shutil.copy2(release / "docker-compose.metrics-ports.yml",
                     root / "docker-compose.metrics-ports.yml")
        result = self._config(docker, root,
                              extra_files=("docker-compose.metrics-ports.yml",))
        self.assertEqual(result.returncode, 0, result.stderr)
        # And the merged model really carries the three publishes.
        cmd = [docker, "compose", "--project-directory", str(root)]
        for name in ("docker-compose.yml", "docker-compose.prod.yml",
                     "docker-compose.ssl.yml", "docker-compose.metrics-ports.yml"):
            cmd += ["-f", str(root / name)]
        cmd += ["config", "--format", "json"]
        merged = json.loads(subprocess.run(
            cmd, capture_output=True, text=True, check=True).stdout)
        ports = {s: NS["_metrics_published_ports"]((b or {}).get("ports"))
                 for s, b in merged["services"].items()}
        self.assertIn("127.0.0.1:9090:9090", ports["backend"])
        self.assertIn("127.0.0.1:2019:2019", ports["edge"])
        self.assertEqual(ports["frontend"], {"127.0.0.1:8081:8081"})

    def test_ship_step_precedes_config_in_source(self):
        # The ordering invariant itself, in deploy_remote.py source:
        # the ship must come AFTER the saved_files backup loop (so
        # `existed` already records the file's prior state for
        # rollback) and BEFORE the first `compose('config', ...)` call.
        src = DEPLOY_REMOTE.read_text()
        backup_at = src.index("existed[relative] = src.exists()")
        ship_at = src.index(
            "shutil.copy2(release / METRICS_PORTS_FILE, root / METRICS_PORTS_FILE)")
        # Indented statement form: the plain string also occurs in the
        # B-1 comment above the ship step, which must not count.
        config_at = src.index("    compose('config', '--quiet')")
        self.assertLess(backup_at, ship_at,
                        "backup loop must record the file before it is shipped")
        self.assertLess(ship_at, config_at,
                        "ship must precede the first compose config")

    def test_rollback_restores_prior_state(self):
        # The B-1 rollback-safety claim, behaviourally: the backup loop
        # records `existed` before the ship, and the except handler
        # restores the backed-up copy (or unlinks the file when it did
        # not exist) BEFORE rollback_files is built — so a failed deploy
        # leaves root exactly as it was. Both prior states checked.
        root, release = self._fixture()
        metrics = root / "docker-compose.metrics-ports.yml"
        for prior_content in (None, "# old metrics file, pre-fix deploy\n"):
            if prior_content is None:
                self.assertFalse(metrics.exists())
                existed = False
            else:
                metrics.write_text(prior_content)
                existed = True
            # Backup loop (as in deploy_remote.py) + ship.
            backup = root / "backup"
            backup.mkdir(exist_ok=True)
            if existed:
                shutil.copy2(metrics, backup / "docker-compose.metrics-ports.yml")
            shutil.copy2(release / "docker-compose.metrics-ports.yml", metrics)
            # Except-handler restore (same rule as deploy_remote.py).
            if existed:
                restored = metrics.with_name(metrics.name + ".rollback")
                shutil.copy2(backup / "docker-compose.metrics-ports.yml", restored)
                restored.replace(metrics)
            elif metrics.exists():
                metrics.unlink()
            if prior_content is None:
                self.assertFalse(metrics.exists(),
                                 "first deploy: rollback must unlink the shipped file")
            else:
                self.assertEqual(metrics.read_text(), prior_content,
                                 "later deploy: rollback must restore the old file")


def extract_query_namespace():
    """Exec prometheus_query_first_value from deploy_remote.py (AST
    selected, never imported) and return its namespace."""
    import ast

    tree = ast.parse(DEPLOY_REMOTE.read_text())
    nodes = [n for n in tree.body
             if isinstance(n, ast.FunctionDef) and n.name == "prometheus_query_first_value"]
    if len(nodes) != 1:
        raise AssertionError(
            "expected prometheus_query_first_value in deploy_remote.py; harness is stale")
    module = ast.Module(body=nodes, type_ignores=[])
    namespace = {"json": json, "urllib": __import__("urllib.request")}
    exec(compile(module, str(DEPLOY_REMOTE), "exec"), namespace)  # noqa: S102
    return namespace


class _StubPrometheusHandler(BaseHTTPRequestHandler):
    """Serves canned /api/v1/query payloads from server.responses
    {query: [sample values] or None for an empty result}."""

    def do_GET(self):
        query = parse_qs(urlparse(self.path).query).get("query", [""])[0]
        values = self.server.responses.get(query)
        if values is None:
            result = []
        else:
            result = [{"metric": {"__name__": query},
                       "value": [1700000000, str(v)]} for v in values]
        payload = json.dumps(
            {"status": "success", "data": {"resultType": "vector", "result": result}}
        ).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *args):
        pass


class TestPrometheusQueryFirstValue(unittest.TestCase):
    """Critic S-1: the new /api/v1/query helper, stub-server based —
    the same pattern the target/rule helpers are documented for."""

    def setUp(self):
        self.query_first_value = extract_query_namespace()["prometheus_query_first_value"]
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), _StubPrometheusHandler)
        self.server.responses = {}
        self.thread = threading.Thread(
            target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.server.shutdown)
        self.addCleanup(self.server.server_close)
        self.base_url = f"http://127.0.0.1:{self.server.server_address[1]}"

    def test_nginx_up_one_returns_one(self):
        self.server.responses = {"nginx_up": [1]}
        self.assertEqual(self.query_first_value(self.base_url, "nginx_up"), 1.0)

    def test_nginx_up_zero_returns_zero(self):
        # The hollow case: the helper must surface 0, not hide it —
        # deploy_observability raises on anything != 1.
        self.server.responses = {"nginx_up": [0]}
        self.assertEqual(self.query_first_value(self.base_url, "nginx_up"), 0.0)

    def test_sellersho_series_returns_data(self):
        self.server.responses = {"sellersho_http_active": [3]}
        self.assertEqual(
            self.query_first_value(self.base_url, "sellersho_http_active"), 3.0)

    def test_sellersho_series_zero_still_counts_as_data(self):
        # An idle backend legitimately reports 0 active requests; 0.0
        # is data, None is "no series" — the deploy must tell them apart.
        self.server.responses = {"sellersho_http_active": [0]}
        self.assertEqual(
            self.query_first_value(self.base_url, "sellersho_http_active"), 0.0)

    def test_empty_result_returns_none(self):
        self.server.responses = {}
        self.assertIsNone(self.query_first_value(self.base_url, "nginx_up"))

    def test_deploy_observability_wiring(self):
        # Source-level wiring: inside deploy_observability(), after the
        # rules check and BEFORE the status-file write, nginx_up must
        # equal 1 and a sellersho_ series must return data — both
        # raising on failure — and the values must land in the status.
        src = DEPLOY_REMOTE.read_text()
        body = src[src.index("def deploy_observability():"):]
        body = body[:body.index("(root / '.observability-status.json')")]
        self.assertIn("prometheus_query_first_value(OBSERVABILITY_PROMETHEUS_URL, 'nginx_up')", body)
        self.assertIn("if nginx_up != 1:", body)
        self.assertIn("prometheus_query_first_value(OBSERVABILITY_PROMETHEUS_URL, 'sellersho_http_active')", body)
        self.assertIn("if sellersho_value is None:", body)
        self.assertIn("raise RuntimeError", body)
        status = src[src.index("status = {'ok': True, 'targets_up': targets_up"):]
        self.assertIn("'nginx_up': nginx_up", status)
        self.assertIn("'sellersho_http_active': sellersho_value", status)


if __name__ == "__main__":
    unittest.main()
