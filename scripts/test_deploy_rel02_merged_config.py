"""REL-02 dry-run harness: the merged-config assertion from
scripts/deploy_remote.py (S-5), executed standalone (no Docker, no prod).

The deploy script cannot be imported — module import runs the deploy — so
the assertion function + constants are extracted from its source by AST
(the same extract-by-content pattern as the OBS-01 F-4 dry run) and exec'd
in an isolated namespace. If the extraction fails, every test here fails
loudly instead of silently passing against a stale copy.

Two layers:
  1. Unit tests on assert_merged_config against hand-built
     `docker compose config --format json` shaped payloads (good config
     passes; each single defect fails naming only the service/key).
  2. Real-file tests: the repo compose files themselves must carry the
     REL-02 rows (digest pins, budgets, healthchecks, log rotation,
     grace periods, health-gated depends_on), and deploy_remote.py must
     ship docker-compose.yml + docker-compose.prod.yml from the release
     (early ship before `compose config`, saved_files backup cover,
     forward-repair re-ship).

Run from the repo root:  python3 scripts/test_deploy_rel02_merged_config.py
"""
import ast
import copy
import json
import re
import unittest
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parent.parent
DEPLOY_REMOTE = REPO / "scripts" / "deploy_remote.py"
SOURCE = DEPLOY_REMOTE.read_text()


def extract_assertion_namespace():
    """Exec the assertion function + constants from deploy_remote.py
    source (AST-selected, never imported) and return their namespace."""
    tree = ast.parse(SOURCE)
    wanted_functions = {"_canonical_memory", "_merged_memory_limit",
                        "_merged_memory_reservation", "_merged_cpus_limit",
                        "_image_is_digest_pinned", "_merged_pids_limit",
                        "assert_merged_config"}
    wanted_assigns = {"REL02_MERGED_EXPECTED", "REL02_APP_PORTS",
                      "REL02_LOG_MAX_SIZE", "REL02_LOG_MAX_FILE", "REL02_COLLECTORS"}
    nodes = []
    for node in tree.body:
        if isinstance(node, ast.FunctionDef) and node.name in wanted_functions:
            nodes.append(node)
        elif isinstance(node, ast.Assign) and any(
            isinstance(t, ast.Name) and t.id in wanted_assigns for t in node.targets
        ):
            nodes.append(node)
    if len(nodes) != 12:
        raise AssertionError(
            f"expected 12 assertion nodes in deploy_remote.py, extracted {len(nodes)}; "
            "the harness is stale — update it, do not delete this check"
        )
    module = ast.Module(body=nodes, type_ignores=[])
    namespace = {"json": json}
    exec(compile(module, str(DEPLOY_REMOTE), "exec"), namespace)  # noqa: S102 — test harness, repo-owned source
    return namespace


NS = extract_assertion_namespace()
assert_merged_config = NS["assert_merged_config"]
EXPECTED = NS["REL02_MERGED_EXPECTED"]


def _service(restart, memory, health=True, pids=256, ports=(), cpus="0.25",
             reservation="64M", grace=None,
             image="example/app@sha256:" + "a" * 64):
    svc = {
        "restart": restart,
        "image": image,
        # NOTE: pids lives inside deploy.resources.limits — a top-level
        # `pids_limit` key alongside ANY deploy.resources block makes
        # `docker compose config` fail (Compose v5.6). Never use it.
        "deploy": {"resources": {
            "limits": {"memory": memory, "pids": pids, "cpus": cpus},
            "reservations": {"memory": reservation}}},
        "logging": {"driver": "json-file",
                    "options": {"max-size": "10m", "max-file": "3"}},
        "ports": [{"host_ip": "127.0.0.1", "published": p, "target": p} for p in ports],
    }
    if health:
        svc["healthcheck"] = {"test": ["CMD", "wget", "-q", "http://127.0.0.1/"]}
    if grace is not None:
        svc["stop_grace_period"] = grace
    return svc


def good_merged():
    """A merged config matching the REL-02 rows exactly (all 9 services)."""
    services = {
        "postgres": _service("always", "1024M", ports=[5432], cpus="1.0",
                             reservation="256M"),
        "redis": _service("always", "640M", ports=[6379], cpus="0.5",
                          reservation="64M"),
        "backend": _service("always", "512M", pids=512, ports=[8080, 9090],
                            cpus="2.0", reservation="128M", grace="30s"),
        "frontend": _service("always", "128M", ports=[8081], cpus="0.5",
                             reservation="32M", grace="30s"),
        "edge": _service("unless-stopped", "256M", ports=[2019], cpus="0.5",
                         reservation="64M", grace="30s"),
        "prometheus": _service("unless-stopped", "256M", ports=[9091],
                               cpus="0.25", reservation="64M"),
        "grafana": _service("unless-stopped", "256M", ports=[3000],
                            cpus="0.25", reservation="64M"),
        "node-exporter": _service("unless-stopped", "64M", ports=[9100],
                                  cpus="0.25", reservation="16M"),
        # nginx-exporter carries NO healthcheck by design (scratch image).
        "nginx-exporter": _service("unless-stopped", "64M", health=False,
                                   ports=[9113], cpus="0.25",
                                   reservation="16M"),
    }
    # The edge also publishes the public 80/443 — allowed (not app ports).
    services["edge"]["ports"] += [
        {"host_ip": "", "published": 80, "target": 80},
        {"host_ip": "", "published": 443, "target": 443},
    ]
    return json.dumps({"services": services})


class TestAssertMergedConfig(unittest.TestCase):
    def test_good_config_passes(self):
        self.assertTrue(assert_merged_config(good_merged(), require_edge=True))

    def test_good_config_passes_without_edge_when_not_required(self):
        cfg = json.loads(good_merged())
        del cfg["services"]["edge"]
        self.assertTrue(assert_merged_config(json.dumps(cfg), require_edge=False))

    def test_missing_edge_raises_when_required(self):
        cfg = json.loads(good_merged())
        del cfg["services"]["edge"]
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg), require_edge=True)
        self.assertIn("edge", str(ctx.exception))

    def test_app_merge_shape_passes_without_collectors(self):
        # The app deploy merges base+prod+ssl+metrics — no observability
        # file — so its assertion runs with require_collectors=False.
        cfg = json.loads(good_merged())
        for svc in ["prometheus", "grafana", "node-exporter", "nginx-exporter"]:
            del cfg["services"][svc]
        self.assertTrue(assert_merged_config(json.dumps(cfg), require_collectors=False))
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg), require_collectors=True)
        self.assertIn("prometheus", str(ctx.exception))

    def test_bytes_string_memory_normalizes(self):
        # `docker compose config --format json` emits memory as a BYTES
        # string ('536870912'); the assertion must accept it as 512M.
        cfg = json.loads(good_merged())
        cfg["services"]["backend"]["deploy"]["resources"]["limits"]["memory"] = "536870912"
        self.assertTrue(assert_merged_config(json.dumps(cfg)))
        cfg["services"]["backend"]["deploy"]["resources"]["limits"]["memory"] = "1"
        with self.assertRaises(RuntimeError):
            assert_merged_config(json.dumps(cfg))

    def test_wrong_restart_raises_and_names_service(self):
        cfg = json.loads(good_merged())
        cfg["services"]["backend"]["restart"] = "unless-stopped"
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("backend", str(ctx.exception))
        self.assertIn("restart", str(ctx.exception))

    def test_missing_memory_limit_raises(self):
        cfg = json.loads(good_merged())
        del cfg["services"]["postgres"]["deploy"]["resources"]["limits"]["memory"]
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("postgres", str(ctx.exception))

    def test_wrong_memory_limit_raises(self):
        cfg = json.loads(good_merged())
        cfg["services"]["redis"]["deploy"]["resources"]["limits"]["memory"] = "512M"
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("redis", str(ctx.exception))

    # --- M-1: image digests, cpus, reservations, stop_grace_period ---

    def test_unpinned_image_tag_raises_and_names_service(self):
        # A future edit that drops the digest (floating tag) must fail
        # the deploy gate, not sail through it.
        cfg = json.loads(good_merged())
        cfg["services"]["backend"]["image"] = "alpine:3.19"
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("backend", str(ctx.exception))
        self.assertIn("image", str(ctx.exception))
        # The offending value is never echoed (errors name keys only).
        self.assertNotIn("alpine:3.19", str(ctx.exception))

    def test_malformed_digest_raises(self):
        # A truncated / non-hex digest is not a pin.
        cfg = json.loads(good_merged())
        cfg["services"]["edge"]["image"] = "caddy:2-alpine@sha256:003a9780"
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("edge", str(ctx.exception))
        self.assertIn("image", str(ctx.exception))

    def test_missing_image_raises(self):
        cfg = json.loads(good_merged())
        del cfg["services"]["redis"]["image"]
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("redis", str(ctx.exception))
        self.assertIn("image", str(ctx.exception))

    def test_changed_cpus_limit_raises(self):
        cfg = json.loads(good_merged())
        cfg["services"]["backend"]["deploy"]["resources"]["limits"]["cpus"] = "1.0"
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("backend", str(ctx.exception))
        self.assertIn("cpus", str(ctx.exception))

    def test_missing_cpus_limit_raises(self):
        cfg = json.loads(good_merged())
        del cfg["services"]["postgres"]["deploy"]["resources"]["limits"]["cpus"]
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("postgres", str(ctx.exception))
        self.assertIn("cpus", str(ctx.exception))

    def test_numeric_cpus_normalizes(self):
        # `docker compose config --format json` emits cpus as a JSON
        # number (2, 0.5); the repo files write quoted strings ('2.0').
        # Both forms must be accepted as the same limit.
        cfg = json.loads(good_merged())
        cfg["services"]["backend"]["deploy"]["resources"]["limits"]["cpus"] = 2
        cfg["services"]["redis"]["deploy"]["resources"]["limits"]["cpus"] = 0.5
        self.assertTrue(assert_merged_config(json.dumps(cfg)))
        cfg["services"]["backend"]["deploy"]["resources"]["limits"]["cpus"] = 1
        with self.assertRaises(RuntimeError):
            assert_merged_config(json.dumps(cfg))

    def test_wrong_memory_reservation_raises(self):
        cfg = json.loads(good_merged())
        cfg["services"]["frontend"]["deploy"]["resources"]["reservations"]["memory"] = "64M"
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("frontend", str(ctx.exception))
        self.assertIn("reservation", str(ctx.exception))

    def test_missing_memory_reservation_raises(self):
        cfg = json.loads(good_merged())
        del cfg["services"]["grafana"]["deploy"]["resources"]["reservations"]["memory"]
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("grafana", str(ctx.exception))
        self.assertIn("reservation", str(ctx.exception))

    def test_bytes_string_reservation_normalizes(self):
        # Merged JSON emits reservations.memory as a bytes string too.
        cfg = json.loads(good_merged())
        cfg["services"]["backend"]["deploy"]["resources"]["reservations"]["memory"] = "134217728"
        self.assertTrue(assert_merged_config(json.dumps(cfg)))
        cfg["services"]["backend"]["deploy"]["resources"]["reservations"]["memory"] = "1"
        with self.assertRaises(RuntimeError):
            assert_merged_config(json.dumps(cfg))

    def test_dropped_stop_grace_period_raises(self):
        cfg = json.loads(good_merged())
        del cfg["services"]["backend"]["stop_grace_period"]
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("backend", str(ctx.exception))
        self.assertIn("stop_grace_period", str(ctx.exception))

    def test_wrong_stop_grace_period_raises(self):
        cfg = json.loads(good_merged())
        cfg["services"]["frontend"]["stop_grace_period"] = "10s"
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("frontend", str(ctx.exception))
        self.assertIn("stop_grace_period", str(ctx.exception))

    def test_service_without_declared_grace_needs_none(self):
        # Only backend/frontend/edge declare a grace period; the other
        # services must pass with no stop_grace_period at all (the good
        # fixture already omits it for them).
        self.assertTrue(assert_merged_config(good_merged()))

    def test_missing_healthcheck_raises(self):
        cfg = json.loads(good_merged())
        del cfg["services"]["frontend"]["healthcheck"]
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("frontend", str(ctx.exception))

    def test_nginx_exporter_needs_no_healthcheck(self):
        # By design (scratch image): the good fixture already omits it.
        self.assertTrue(assert_merged_config(good_merged()))

    def test_missing_pids_limit_raises(self):
        cfg = json.loads(good_merged())
        del cfg["services"]["grafana"]["deploy"]["resources"]["limits"]["pids"]
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("grafana", str(ctx.exception))
        self.assertIn("pids", str(ctx.exception))

    def test_missing_log_rotation_raises(self):
        cfg = json.loads(good_merged())
        cfg["services"]["backend"]["logging"]["options"]["max-size"] = "0"
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("backend", str(ctx.exception))
        self.assertIn("max-size", str(ctx.exception))

    def test_non_loopback_app_port_refused(self):
        cfg = json.loads(good_merged())
        cfg["services"]["backend"]["ports"][0]["host_ip"] = "0.0.0.0"
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("backend", str(ctx.exception))

    def test_public_edge_ports_allowed(self):
        # 80/443 on the edge are the public edge by design.
        self.assertTrue(assert_merged_config(good_merged()))

    def test_missing_service_raises(self):
        cfg = json.loads(good_merged())
        del cfg["services"]["prometheus"]
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertIn("prometheus", str(ctx.exception))

    def test_invalid_json_raises(self):
        with self.assertRaises(RuntimeError):
            assert_merged_config("not json{")

    def test_error_never_prints_env_values(self):
        # The assertion never reads environment: a fixture carrying a
        # planted marker value must never see it in the error text.
        cfg = json.loads(good_merged())
        cfg["services"]["backend"]["environment"] = {"JWT_SECRET": "MARKER-SECRET-VALUE"}
        cfg["services"]["backend"]["restart"] = "no"
        with self.assertRaises(RuntimeError) as ctx:
            assert_merged_config(json.dumps(cfg))
        self.assertNotIn("MARKER-SECRET-VALUE", str(ctx.exception))
        self.assertIn("backend", str(ctx.exception))


# --- real repo compose files must carry the REL-02 rows ---

class _Reset(list):
    pass


class _Override(list):
    pass


def _compose_loader():
    loader = yaml.SafeLoader
    loader.add_constructor("!reset", lambda l, n: _Reset(l.construct_sequence(n)))
    loader.add_constructor("!override", lambda l, n: _Override(l.construct_sequence(n)))
    return loader


def load_compose(name):
    return yaml.load((REPO / name).read_text(), Loader=_compose_loader())


def _logging_ok(svc):
    log = (svc or {}).get("logging") or {}
    opts = log.get("options") or {}
    return (log.get("driver") == "json-file" and str(opts.get("max-size")) == "10m"
            and str(opts.get("max-file")) == "3")


class TestRealComposeFiles(unittest.TestCase):
    def test_images_are_digest_pinned(self):
        pinned = {
            "docker-compose.yml": {
                "postgres": "postgres:16-alpine@sha256:",
                "redis": "redis:7-alpine@sha256:",
                "frontend": "nginx:alpine@sha256:",
            },
            "docker-compose.prod.yml": {
                "postgres": "postgres:16-alpine@sha256:",
                "redis": "redis:7-alpine@sha256:",
                "backend": "alpine:3.19@sha256:",
                "frontend": "nginx:alpine@sha256:",
            },
            "docker-compose.ssl.yml": {
                "edge": "caddy:2-alpine@sha256:",
            },
            "docker-compose.observability.yml": {
                "prometheus": "prom/prometheus:v3.5.0@sha256:",
                "grafana": "grafana/grafana:11.6.2@sha256:",
                "node-exporter": "prom/node-exporter:v1.9.1@sha256:",
                "nginx-exporter": "nginx/nginx-prometheus-exporter:1.5.3@sha256:",
            },
        }
        for fname, services in pinned.items():
            data = load_compose(fname)
            for service, prefix in services.items():
                image = (data["services"][service] or {}).get("image", "")
                self.assertTrue(image.startswith(prefix),
                                f"{fname}:{service} image is not digest-pinned: {image}")
                self.assertRegex(image, r"^[^@]+@sha256:[0-9a-f]{64}$",
                                 f"{fname}:{service} digest malformed")
        # The drifted nginx:1.27-alpine tag must not reappear as an image.
        for fname in pinned:
            data = load_compose(fname)
            for service, body in (data.get("services") or {}).items():
                image = (body or {}).get("image", "")
                self.assertNotIn("1.27-alpine", image,
                                 f"{fname}:{service} still uses the drifted tag")

    def test_no_mutable_app_image_without_digest(self):
        for fname in ["docker-compose.yml", "docker-compose.prod.yml",
                      "docker-compose.ssl.yml", "docker-compose.observability.yml"]:
            data = load_compose(fname)
            for service, body in (data.get("services") or {}).items():
                image = (body or {}).get("image")
                if image:
                    self.assertIn("@sha256:", image, f"{fname}:{service} image unpinned: {image}")

    def test_budgets_and_logging_in_prod_and_observability(self):
        prod = load_compose("docker-compose.prod.yml")
        for service, memory in [("postgres", "1024M"), ("redis", "640M"),
                                ("backend", "512M"), ("frontend", "128M")]:
            body = prod["services"][service]
            limits = body["deploy"]["resources"]["limits"]
            self.assertEqual(str(limits["memory"]), memory, f"prod:{service} memory")
            self.assertIn("cpus", limits, f"prod:{service} cpus")
            # pids lives inside limits (top-level pids_limit + a deploy
            # block is rejected by `docker compose config`); backend 512,
            # everything else 256.
            self.assertEqual(limits.get("pids"), 512 if service == "backend" else 256,
                             f"prod:{service} pids")
            self.assertNotIn("pids_limit", body, f"prod:{service} must not use pids_limit")
            self.assertIn("memory", body["deploy"]["resources"]["reservations"],
                          f"prod:{service} reservation")
            self.assertTrue(_logging_ok(body), f"prod:{service} logging")
        obs = load_compose("docker-compose.observability.yml")
        for service, memory in [("prometheus", "256M"), ("grafana", "256M"),
                                ("node-exporter", "64M"), ("nginx-exporter", "64M")]:
            body = obs["services"][service]
            self.assertEqual(str(body["deploy"]["resources"]["limits"]["memory"]),
                             memory, f"obs:{service} memory")
            self.assertEqual(body["deploy"]["resources"]["limits"].get("pids"), 256,
                             f"obs:{service} pids")
            self.assertNotIn("pids_limit", body, f"obs:{service} must not use pids_limit")
            self.assertTrue(_logging_ok(body), f"obs:{service} logging")

    def test_repo_files_match_assertion_table(self):
        # M-1: the deploy-time table (REL02_MERGED_EXPECTED) must say
        # exactly what the repo compose files declare — otherwise the
        # gate would pass a config the files never declared, or fail a
        # correct one. This pins cpus / reservations / grace in the
        # files against the table, not just the table against itself.
        prod = load_compose("docker-compose.prod.yml")
        ssl = load_compose("docker-compose.ssl.yml")
        obs = load_compose("docker-compose.observability.yml")
        declared = {svc: prod["services"][svc]
                    for svc in ["postgres", "redis", "backend", "frontend"]}
        declared["edge"] = ssl["services"]["edge"]
        for svc in ["prometheus", "grafana", "node-exporter", "nginx-exporter"]:
            declared[svc] = obs["services"][svc]
        for service, (restart, memory, pids, _health, cpus, reservation,
                      grace) in EXPECTED.items():
            body = declared[service]
            limits = body["deploy"]["resources"]["limits"]
            self.assertEqual(str(float(str(limits["cpus"]))), cpus,
                             f"{service} cpus")
            self.assertEqual(
                str(body["deploy"]["resources"]["reservations"]["memory"]),
                reservation, f"{service} reservation")
            self.assertEqual(body.get("stop_grace_period"), grace,
                             f"{service} grace")

    def test_healthchecks_present_where_possible(self):
        prod = load_compose("docker-compose.prod.yml")
        self.assertIn("/api/live", str(prod["services"]["backend"]["healthcheck"]),
                      "backend must healthcheck /api/live, never /api/ready")
        self.assertNotIn("/api/ready", str(prod["services"]["backend"]["healthcheck"]))
        self.assertIn("healthcheck", prod["services"]["frontend"])
        ssl = load_compose("docker-compose.ssl.yml")
        self.assertIn("2019", str(ssl["services"]["edge"]["healthcheck"]))
        obs = load_compose("docker-compose.observability.yml")
        for service in ["prometheus", "grafana", "node-exporter"]:
            self.assertIn("healthcheck", obs["services"][service], service)
        # nginx-exporter: scratch image, no probe binary — none by design.
        self.assertNotIn("healthcheck", obs["services"]["nginx-exporter"])

    def test_grace_periods_and_health_gated_depends(self):
        prod = load_compose("docker-compose.prod.yml")
        self.assertEqual(prod["services"]["backend"].get("stop_grace_period"), "30s")
        self.assertEqual(prod["services"]["frontend"].get("stop_grace_period"), "30s")
        self.assertEqual(
            prod["services"]["frontend"]["depends_on"]["backend"]["condition"],
            "service_healthy")
        ssl = load_compose("docker-compose.ssl.yml")
        self.assertEqual(ssl["services"]["edge"].get("stop_grace_period"), "30s")
        self.assertEqual(
            ssl["services"]["edge"]["depends_on"]["frontend"]["condition"],
            "service_healthy")
        self.assertEqual(
            ssl["services"]["edge"]["depends_on"]["backend"]["condition"],
            "service_healthy")

    def test_app_ports_stay_loopback_in_repo_files(self):
        for fname in ["docker-compose.yml", "docker-compose.prod.yml"]:
            data = load_compose(fname)
            for service, body in (data.get("services") or {}).items():
                for entry in (body or {}).get("ports") or []:
                    if isinstance(entry, str) and ":" in entry:
                        host = entry.rsplit(":", 2)[0] if entry.count(":") >= 2 else ""
                        if host not in ("", "127.0.0.1"):
                            self.fail(f"{fname}:{service} publishes {entry} non-loopback")
        ssl = load_compose("docker-compose.ssl.yml")
        for entry in ssl["services"]["edge"].get("ports") or []:
            if isinstance(entry, str) and entry.startswith("127.0.0.1:2019"):
                break
        else:
            self.fail("ssl: edge admin port 2019 is not loopback-only")


class TestComposeShipWiring(unittest.TestCase):
    """deploy_remote.py must ship the base + prod compose files (S-1)."""

    def test_saved_files_covers_compose_files(self):
        m = re.search(r"saved_files = \[(.*?)\]\n", SOURCE, re.S)
        self.assertIsNotNone(m, "saved_files list not found")
        self.assertIn("'docker-compose.yml'", m.group(1))
        self.assertIn("'docker-compose.prod.yml'", m.group(1))

    def test_early_ship_before_compose_config(self):
        ship = SOURCE.index("for name in ['docker-compose.yml', 'docker-compose.prod.yml']:")
        # The real `compose('config', '--quiet')` CALL (own line), not the
        # comment mentions of it in the B-1 prose above the ship.
        config = SOURCE.index("\n    compose('config', '--quiet')\n")
        secret_check = SOURCE.index("run_secret_checks_nodocker.py")
        self.assertLess(secret_check, ship, "secret check must run before the ship")
        self.assertLess(ship, config, "compose ship must run before compose config")

    def test_ssl_shipped_early_before_compose_config(self):
        # REL-02 deploy fix: the b464d03 deploy failed fail-closed at
        # assert_merged_config because docker-compose.ssl.yml shipped
        # only in the late loop (after migrations) — the assert
        # validated NEW base/prod + the OLD root ssl file, whose edge
        # has no digest pin, budgets, healthcheck or logging. The ssl
        # file must ship before the first `compose config`, like
        # base+prod. (First occurrence: the early ship; the late loop
        # re-copy is the no-op safety net, pinned by
        # test_forward_repair_reships_compose_files.)
        ship = SOURCE.index(
            "shutil.copy2(release / 'docker-compose.ssl.yml', root / 'docker-compose.ssl.yml')")
        config = SOURCE.index("\n    compose('config', '--quiet')\n")
        merged_assert = SOURCE.index("assert_merged_config(compose('config'")
        secret_check = SOURCE.index("run_secret_checks_nodocker.py")
        self.assertLess(secret_check, ship, "secret check must run before the ssl ship")
        self.assertLess(ship, config, "ssl ship must run before compose config")
        self.assertLess(ship, merged_assert, "ssl ship must run before the merged-config assert")

    def test_ssl_early_ship_is_ssl_gated(self):
        # The early ssl ship mirrors the compose_files entry: only an
        # SSL deploy has an edge service. An unconditional ship would
        # plant an edge file on a non-SSL root.
        self.assertIn(
            "if ssl:\n        shutil.copy2(release / 'docker-compose.ssl.yml', "
            "root / 'docker-compose.ssl.yml')",
            SOURCE)

    def test_observability_shipped_early_before_compose_config(self):
        # Same mixed-config hazard, one refactor away from the
        # collectors: docker-compose.observability.yml carries the
        # REL-02 pins/budgets/healthchecks the observability merge
        # asserts, so it must also ship before any `compose config`
        # (its own assert runs later, inside deploy_observability(),
        # but the file must already be the release's by then).
        ship = SOURCE.index(
            "shutil.copy2(release / 'docker-compose.observability.yml', "
            "root / 'docker-compose.observability.yml')")
        config = SOURCE.index("\n    compose('config', '--quiet')\n")
        self.assertLess(ship, config,
                        "observability ship must run before compose config")

    def test_saved_files_covers_ssl_and_observability(self):
        # The early-shipped files must be in the deploy backup or a
        # rollback after a failed assert cannot restore the previous
        # root copies.
        m = re.search(r"saved_files = \[(.*?)\]\n", SOURCE, re.S)
        self.assertIsNotNone(m, "saved_files list not found")
        self.assertIn("'docker-compose.ssl.yml'", m.group(1))
        self.assertIn("'docker-compose.observability.yml'", m.group(1))

    def test_ssl_flag_includes_previous_edge(self):
        # require_edge=ssl is exact only because ssl is already OR'd
        # with bool(previous_edge): a live edge container makes the
        # deploy an SSL deploy, so the assert must require the edge —
        # and the early ship must have put the release's ssl file in
        # place for it to read. Dropping the `or` would let the assert
        # skip the edge on a host that runs one.
        self.assertIn("ssl = ssl or bool(previous_edge)", SOURCE)
        self.assertIn(
            "assert_merged_config(compose('config', '--format', 'json', capture=True),\n"
            "                         require_edge=ssl, require_collectors=False)",
            SOURCE)


class TestComposeObservabilityCapture(unittest.TestCase):
    """deploy_observability() calls compose_observability(..., capture=True).

    REL-02 added that call, but compose_observability() took *args only
    and rejected the keyword — a TypeError that hid behind the b464d03
    ssl-assert failure (the deploy never got that far). The fix drill
    exposed it at deploy_observability() after the binary swap, so the
    retry would have failed there instead. Pin the signature + the
    pass-through to run() with a stubbed run (no Docker)."""

    def _load(self):
        tree = ast.parse(SOURCE)
        nodes = [n for n in tree.body
                 if isinstance(n, ast.FunctionDef) and n.name == "compose_observability"]
        self.assertEqual(len(nodes), 1, "compose_observability not found")
        calls = {}

        def stub_run(*args, capture=False, **kwargs):
            calls["args"] = args
            calls["capture"] = capture
            return '{"services": {}}'

        namespace = {"run": stub_run, "root": Path("/opt/drup"),
                     "compose_files": [Path("/opt/drup/docker-compose.yml")]}
        exec(compile(ast.Module(body=nodes, type_ignores=[]),
                     str(DEPLOY_REMOTE), "exec"), namespace)  # noqa: S102 — test harness, repo-owned source
        return namespace["compose_observability"], calls

    def test_capture_kwarg_accepted_and_forwarded(self):
        fn, calls = self._load()
        self.assertEqual(fn("config", "--format", "json", capture=True),
                         '{"services": {}}')
        self.assertTrue(calls["capture"], "capture=True must reach run()")
        self.assertIn("config", calls["args"])
        self.assertIn("--format", calls["args"])

    def test_observability_file_appended_after_app_files(self):
        fn, calls = self._load()
        fn("up", "-d", capture=False)
        argv = list(calls["args"])
        obs = str(Path("/opt/drup/docker-compose.observability.yml"))
        self.assertIn(obs, argv)
        self.assertGreater(argv.index(obs),
                           argv.index(str(Path("/opt/drup/docker-compose.yml"))))
        self.assertFalse(calls["capture"])

    def test_forward_repair_reships_compose_files(self):
        self.assertIn(
            "for name in ['Caddyfile', 'docker-compose.ssl.yml', "
            "'docker-compose.observability.yml', 'docker-compose.yml', "
            "'docker-compose.prod.yml']:",
            SOURCE)

    def test_merged_assertion_called_before_migrations(self):
        call = SOURCE.index("assert_merged_config(compose('config'")
        self.assertLess(call, SOURCE.index("applied_before = query_applied()"))
        obs = SOURCE.index(
            "assert_merged_config(compose_observability('config'")
        self.assertLess(obs, SOURCE.index(
            "compose_observability('up', '-d', '--no-deps'"))

    def test_tapin_key_interpolation_from_live_container(self):
        # TAPIN_TOKEN_KEY is ABSENT from /opt/drup/.env but present in
        # the live backend env; the deploy must export it into the
        # interpolation environment or `compose config` fails on the
        # ${...:?} in docker-compose.prod.yml. (SEC-01 forbids weakening
        # the `:?` to `:-` — that form is flagged as a literal.)
        self.assertIn("os.environ['TAPIN_TOKEN_KEY'] = runtime_env['TAPIN_TOKEN_KEY']",
                      SOURCE)

    def test_caddy_validate_uses_pinned_digest(self):
        # The pre-deploy `caddy validate` must run the same pinned image
        # the edge service runs — never the floating tag.
        self.assertIn("caddy:2-alpine@sha256:003a97808e781aa9f7e75cc2f43d34d461099031faf5e20faefb9f61126836f0",
                      SOURCE)
        self.assertNotRegex(
            SOURCE, r"run\('docker', '(pull|run)',[^)]*'caddy:2-alpine'[,)]",
            "caddy pull/validate must not use the floating tag")


if __name__ == "__main__":
    unittest.main()
