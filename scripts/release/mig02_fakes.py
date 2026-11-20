#!/usr/bin/env python3
"""MIG-02 rehearsal fakes (step 6): a fake `docker` CLI, a fake `curl`,
and a stub Prometheus/Grafana server, so the REAL
scripts/deploy_remote.py can run end-to-end against a fake /opt/drup
root with disposable PostgreSQL + scratch Redis. No prod contact,
no real Docker, no SSH.

Usage (invoked by scripts/release/rehearse_mig02.py via PATH):
    mig02_fakes.py docker <docker argv...>
    mig02_fakes.py curl <curl argv...>
    mig02_fakes.py obs-stub   (serves Prometheus on 127.0.0.1:9091
                               and Grafana /api/health on 127.0.0.1:3000)

Environment (fake docker):
    MIG02_FAKE_ROOT        fake install root (default /opt/drup)
    MIG02_CONTAINER_ROOT   dir standing in for the container fs
    MIG02_STATE_DIR        PIDs, logs, fail-once flag
    MIG02_PG_URL           postgres URL for drup_postgres psql
                           (e.g. postgres://gap13@127.0.0.1:5433/mig02_r1?sslmode=disable)
    MIG02_PG_DB / MIG02_PG_USER / MIG02_PGHOST / MIG02_PGPORT
    MIG02_PSQL_BIN         psql binary (default: psql on PATH)
    MIG02_BACKEND_ENV_JSON path to a JSON list of "K=V" strings for
                           the backend container's Config.Env
    MIG02_VOL_DIR          dir holding drup_caddy_data/ + drup_caddy_config/
                           mountpoint dirs for `docker volume inspect`
    MIG02_FAIL_BACKEND_ONCE path to a flag file: when present, the next
                           `compose up backend` starts the server with
                           a dead REDIS_URL (R-3 health failure) and
                           removes the flag.
"""
import json
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

FAKE_ROOT = Path(os.environ.get("MIG02_FAKE_ROOT", "/opt/drup"))
CONTAINER_ROOT = Path(os.environ.get("MIG02_CONTAINER_ROOT", "/tmp/mig02-container"))
STATE_DIR = Path(os.environ.get("MIG02_STATE_DIR", "/tmp/mig02-state"))
PG_URL = os.environ.get("MIG02_PG_URL", "")
PG_DB = os.environ.get("MIG02_PG_DB", "")
PG_USER = os.environ.get("MIG02_PG_USER", "gap13")
PG_HOST = os.environ.get("MIG02_PGHOST", "127.0.0.1")
PG_PORT = os.environ.get("MIG02_PGPORT", "5433")
PSQL_BIN = os.environ.get("MIG02_PSQL_BIN", "psql")
BACKEND_ENV_JSON = os.environ.get("MIG02_BACKEND_ENV_JSON", "")
VOL_DIR = Path(os.environ.get("MIG02_VOL_DIR", "/tmp/mig02-vol"))
FAIL_ONCE = os.environ.get("MIG02_FAIL_BACKEND_ONCE", "")


def log(msg):
    sys.stderr.write(f"[fake-docker] {msg}\n")


def backend_env_list():
    if BACKEND_ENV_JSON and Path(BACKEND_ENV_JSON).is_file():
        return json.loads(Path(BACKEND_ENV_JSON).read_text())
    return []


def backend_env_dict():
    env = {}
    for item in backend_env_list():
        key, _, value = item.partition("=")
        env[key] = value
    return env


def container_path(arg):
    """Translate a container-absolute path to the container root."""
    if arg.startswith("/"):
        return str(CONTAINER_ROOT / arg.lstrip("/"))
    return arg


def translate_args(args):
    out = []
    for arg in args:
        if arg.startswith("/") and (arg.startswith("/tmp/sellersho-migrate-")
                                    or (CONTAINER_ROOT / arg.lstrip("/")).exists()):
            out.append(container_path(arg))
        else:
            out.append(arg)
    return out


def docker_ps(argv):
    # No containers are ever listed (no edge in rehearsal, ssl=0).
    return 0


def docker_inspect(argv):
    name = argv[-1]
    if name == "drup_backend":
        obj = {"Config": {"Env": backend_env_list()},
               "Mounts": [{"Destination": "/app/server",
                           "Source": str(FAKE_ROOT / "backend/bin/server-linux")}]}
    elif name == "drup_frontend":
        obj = {"Config": {"Env": []},
               "Mounts": [{"Destination": "/usr/share/nginx/html",
                           "Source": str(FAKE_ROOT / "dist")}]}
    else:
        print(f"fake-docker: inspect: no such container {name}", file=sys.stderr)
        return 1
    print(json.dumps([obj]))
    return 0


def docker_exec(argv):
    # argv: [-i] <container> <cmd...>
    args = list(argv)
    stdin_data = None
    if args and args[0] == "-i":
        args = args[1:]
        # The probe pipes a SQL script on stdin for the main check, but
        # count_rows uses -Atc with no stdin input: the inherited stdin
        # never reaches EOF, so a blocking read() would hang until the
        # caller's timeout. Read only what is already available.
        import select
        try:
            ready, _, _ = select.select([sys.stdin], [], [], 2.0)
            if ready:
                stdin_data = sys.stdin.read()
        except Exception:
            stdin_data = None
    if not args:
        return 1
    container, cmd = args[0], args[1:]
    if container == "drup_postgres":
        if not cmd or cmd[0] != "psql":
            print(f"fake-docker: unsupported postgres exec: {cmd}", file=sys.stderr)
            return 1
        rest = cmd[1:]
        # Drop the prod credentials; keep the query flags.
        filtered = []
        skip = False
        for item in rest:
            if skip:
                skip = False
                continue
            if item in ("-U", "-d"):
                skip = True
                continue
            filtered.append(item)
        psql_argv = [PSQL_BIN, "-h", PG_HOST, "-p", PG_PORT, "-U", PG_USER, "-d", PG_DB] + filtered
        result = subprocess.run(psql_argv, input=stdin_data, capture_output=False, text=True)
        return result.returncode
    if container == "drup_backend":
        if not cmd:
            return 1
        env = dict(os.environ)
        env["DATABASE_URL"] = PG_URL
        translated = translate_args(cmd)
        result = subprocess.run(translated, cwd=str(CONTAINER_ROOT),
                                input=stdin_data, text=True, env=env)
        return result.returncode
    if container == "drup_frontend":
        # Only `nginx -t` is ever run against the frontend.
        return 0
    print(f"fake-docker: exec: no such container {container}", file=sys.stderr)
    return 1


def docker_cp(argv):
    # Only host -> container is used by deploy_remote.py.
    if len(argv) != 2:
        return 1
    src, dst = argv
    if ":" not in dst:
        print(f"fake-docker: cp: expected <container>:<path>, got {dst}", file=sys.stderr)
        return 1
    _, _, cpath = dst.partition(":")
    target = CONTAINER_ROOT / cpath.lstrip("/")
    src_path = Path(src)
    if src_path.is_dir():
        target.mkdir(parents=True, exist_ok=True)
        subprocess.run(["cp", "-a", str(src_path) + "/.", str(target)], check=True)
    else:
        target.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(["cp", "-a", str(src_path), str(target)], check=True)
    return 0


def server_pid_file():
    return STATE_DIR / "backend.pid"


def stop_server():
    pid_file = server_pid_file()
    if not pid_file.is_file():
        return
    try:
        pid = int(pid_file.read_text().strip())
    except ValueError:
        pid_file.unlink(missing_ok=True)
        return
    try:
        os.kill(pid, signal.SIGTERM)
    except ProcessLookupError:
        pid_file.unlink(missing_ok=True)
        return
    for _ in range(50):
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            break
        time.sleep(0.1)
    else:
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    pid_file.unlink(missing_ok=True)


def start_server(runtime_env):
    """Start the real server binary with the runtime environment."""
    stop_server()
    env = dict(os.environ)
    env.update(runtime_env)
    env.setdefault("MIGRATIONS_DIR", str(FAKE_ROOT / "backend/migrations"))
    if FAIL_ONCE and Path(FAIL_ONCE).is_file():
        # R-3: the first boot after the migration gets a dead Redis,
        # so /api/health reports redis down and health() fails.
        env["REDIS_URL"] = "redis://127.0.0.1:1/0"
        Path(FAIL_ONCE).unlink()
        log("FAIL_ONCE: starting backend with a dead REDIS_URL")
    server_bin = FAKE_ROOT / "backend/bin/server-linux"
    log_path = STATE_DIR / "backend.log"
    STATE_DIR.mkdir(parents=True, exist_ok=True)
    with open(log_path, "ab") as log_handle:
        proc = subprocess.Popen([str(server_bin)], env=env,
                                stdout=log_handle, stderr=subprocess.STDOUT,
                                start_new_session=True)
    server_pid_file().write_text(str(proc.pid))
    log(f"backend started pid={proc.pid}")
    return 0


def runtime_env_from_compose_files(files):
    """Read services.backend.environment from the runtime JSON file
    among the -f files (the last -f arg)."""
    for path in reversed(files):
        if path.endswith(".env.deploy.runtime.json"):
            try:
                doc = json.loads(Path(path).read_text())
                return dict(doc.get("services", {}).get("backend", {}).get("environment", {}))
            except Exception:
                return {}
    return backend_env_dict()


def docker_compose(argv):
    # argv: [--project-directory <dir>] [-f <file> ...] <sub> <args...>
    files = []
    rest = list(argv)
    while rest:
        if rest[0] == "--project-directory":
            rest = rest[2:]
        elif rest[0] == "-f":
            files.append(rest[1])
            rest = rest[2:]
        else:
            break
    if not rest:
        return 1
    sub, args = rest[0], rest[1:]
    if sub == "config":
        if "--format" in args and "json" in args:
            print(json.dumps({"services": {}}))
        return 0
    if sub == "up":
        services = [a for a in args if not a.startswith("-")]
        if "backend" in services:
            return start_server(runtime_env_from_compose_files(files))
        return 0
    if sub == "stop":
        services = [a for a in args if not a.startswith("-")]
        if "backend" in services:
            stop_server()
        return 0
    print(f"fake-docker: compose: unsupported subcommand {sub}", file=sys.stderr)
    return 1


def docker_volume(argv):
    # volume inspect -f <fmt> <vol>
    if len(argv) >= 2 and argv[0] == "inspect":
        vol = argv[-1]
        if vol in ("drup_caddy_data", "drup_caddy_config"):
            print(str(VOL_DIR / vol))
            return 0
        return 1
    print(f"fake-docker: volume: unsupported {argv}", file=sys.stderr)
    return 1


def fake_docker(argv):
    if not argv:
        return 1
    cmd, rest = argv[0], argv[1:]
    if cmd == "ps":
        return docker_ps(rest)
    if cmd == "inspect":
        return docker_inspect(rest)
    if cmd == "exec":
        return docker_exec(rest)
    if cmd == "cp":
        return docker_cp(rest)
    if cmd == "compose":
        return docker_compose(rest)
    if cmd == "pull" or cmd == "run":
        return 0
    if cmd == "volume":
        return docker_volume(rest)
    print(f"fake-docker: unsupported command {cmd}", file=sys.stderr)
    return 1


def fake_curl(argv):
    log(f"curl {' '.join(argv)} -> 0 (stubbed site check)")
    return 0


def obs_stub():
    """Stub Prometheus (9091) + Grafana (3000) for deploy_observability."""
    from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
    from urllib.parse import urlparse, parse_qs

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def _json(self, code, payload):
            body = json.dumps(payload).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            parsed = urlparse(self.path)
            port = self.server.server_address[1]
            if port == 9091 and parsed.path == "/-/healthy":
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b"OK")
                return
            if port == 9091 and parsed.path == "/api/v1/targets":
                jobs = ["sellersho-backend", "caddy", "nginx", "node"]
                self._json(200, {"status": "success", "data": {"activeTargets": [
                    {"labels": {"job": j}, "health": "up"} for j in jobs]}})
                return
            if port == 9091 and parsed.path == "/api/v1/rules":
                self._json(200, {"status": "success", "data": {"groups": [{"rules": [{}]}]}})
                return
            if port == 9091 and parsed.path == "/api/v1/query":
                query = parse_qs(parsed.query).get("query", [""])[0]
                if query in ("nginx_up", "sellersho_http_active"):
                    self._json(200, {"status": "success", "data": {"result": [
                        {"value": [0, "1"]}]}})
                else:
                    self._json(200, {"status": "success", "data": {"result": []}})
                return
            if port == 3000 and parsed.path == "/api/health":
                self._json(200, {"database": "ok"})
                return
            self.send_response(404)
            self.end_headers()

    prom = ThreadingHTTPServer(("127.0.0.1", 9091), Handler)
    graf = ThreadingHTTPServer(("127.0.0.1", 3000), Handler)
    import threading
    for server in (prom, graf):
        threading.Thread(target=server.serve_forever, daemon=True).start()
    print("obs-stub up: prometheus 127.0.0.1:9091, grafana 127.0.0.1:3000", flush=True)
    try:
        while True:
            time.sleep(3600)
    except KeyboardInterrupt:
        pass
    return 0


def main():
    if len(sys.argv) < 2:
        print("usage: mig02_fakes.py docker|curl|obs-stub [args...]", file=sys.stderr)
        return 2
    mode, rest = sys.argv[1], sys.argv[2:]
    if mode == "docker":
        return fake_docker(rest)
    if mode == "curl":
        return fake_curl(rest)
    if mode == "obs-stub":
        return obs_stub()
    print(f"unknown mode {mode}", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
