#!/usr/bin/env bash
# REL-01 — build, stage and verify an immutable release from the exact
# committed source (canonical scripted release path, coordinator D2).
#
#   scripts/release/make_release.sh [--commit <sha>] [--payment-mode mock|real]
#       [--repo <path>] [--out-dir <path>] [--production]
#       [--allow-dirty-worktree] [--node-modules auto|npm-ci|symlink]
#
# What it does, in order:
#   1. Resolves the exact commit (default HEAD) in the git checkout.
#   2. G10: fails if docs/fa or backend/internal/networkreward is
#      tracked at that commit.
#   3. G1: extracts ONLY `git archive <commit>` into releases/<commit>/
#      — the dirty workspace can never change the output, because the
#      workspace is never read. (Tracked-tree cleanliness is still
#      enforced by default; --allow-dirty-worktree is a rehearsal-only
#      escape hatch that prints a loud warning and changes nothing
#      about the archive input.)
#   4. G2: fails loudly if ANY VITE_* variable is set in the caller's
#      environment, then builds the frontend inside the stage with a
#      scrubbed environment (env -i + explicit PATH/HOME only, no
#      VITE_*). The allow-list is EMPTY (D4).
#   5. Builds the backend from the staged source with the release
#      commit + contract hash injected via -ldflags (G9).
#   6. G3: writes the extended release.json via make_manifest.py.
#   7. Runs validate_release.py (manifest, checksums, VITE, contract,
#      exclusions, payment mode, migration set). A failure here stops
#      the release.
#   8. Prunes staged releases/<commit>/ dirs to the newest 5 (D3).
#
# It never touches production, never sends OTP, never deploys. The
# deploy scripts (~/workspace/deploy-frontend.sh,
# ~/workspace/deploy-backend.sh) consume the staged release this
# produces. Exit non-zero on any failure; on success it prints the
# stage path, manifest path and checksums.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMIT=""
PAYMENT_MODE="${PAYMENT_GATEWAY_MODE:-}"
REPO=""
OUT_DIR=""
PRODUCTION=0
ALLOW_DIRTY=0
NODE_MODULES_MODE="auto"

while [ $# -gt 0 ]; do
  case "$1" in
    --commit) COMMIT="$2"; shift 2;;
    --payment-mode) PAYMENT_MODE="$2"; shift 2;;
    --repo) REPO="$2"; shift 2;;
    --out-dir) OUT_DIR="$2"; shift 2;;
    --production) PRODUCTION=1; shift;;
    --allow-dirty-worktree) ALLOW_DIRTY=1; shift;;
    --node-modules) NODE_MODULES_MODE="$2"; shift 2;;
    *) echo "unknown argument: $1" >&2; exit 2;;
  esac
done

# --- resolve repo -------------------------------------------------------
if [ -z "$REPO" ]; then
  if git -C "$PWD" rev-parse --show-toplevel >/dev/null 2>&1; then
    REPO="$(git -C "$PWD" rev-parse --show-toplevel)"
  elif [ -d "$SCRIPT_DIR/../../.git" ]; then
    REPO="$(cd "$SCRIPT_DIR/../.." && pwd)"
  else
    echo "FAIL: cannot locate the git checkout (no .git here). Run from ~/workspace/sellersho-git or pass --repo." >&2
    exit 2
  fi
fi
cd "$REPO"
if [ ! -d .git ]; then
  echo "FAIL: $REPO is not a git checkout (this tree is a manifest pull, not the repo)." >&2
  exit 2
fi

if [ -z "$COMMIT" ]; then COMMIT="$(git rev-parse HEAD)"; fi
COMMIT="$(git rev-parse "$COMMIT")" || { echo "FAIL: unknown commit $COMMIT" >&2; exit 2; }
BRANCH="$(git branch --show-current 2>/dev/null || echo detached)"
if [ -z "$BRANCH" ]; then BRANCH="detached"; fi

if [ -z "$PAYMENT_MODE" ]; then
  echo "FAIL: payment mode must be set explicitly: pass --payment-mode mock|real or set PAYMENT_GATEWAY_MODE (D5)." >&2
  exit 2
fi
case "$PAYMENT_MODE" in
  mock|real) ;;
  *) echo "FAIL: --payment-mode must be mock or real, got '$PAYMENT_MODE'." >&2; exit 2;;
esac

# --- G2 (caller env): any VITE_* set fails the release loudly ---------
LEAKED_VITE="$(env | grep '^VITE_' || true)"
if [ -n "$LEAKED_VITE" ]; then
  echo "FAIL: VITE_* variables are set in the operator environment (allow-list is EMPTY, D4):" >&2
  echo "$LEAKED_VITE" | sed 's/^/  /' >&2
  echo "Unset them and re-run; a release build must never inherit ambient VITE_* values." >&2
  exit 1
fi

# --- tracked-tree guard (default) ---------------------------------------
if [ "$ALLOW_DIRTY" -eq 0 ]; then
  if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
    echo "FAIL: tracked tree is dirty — commit before building a release (or pass --allow-dirty-worktree for a rehearsal)." >&2
    git status --porcelain --untracked-files=no | head -20 >&2
    exit 1
  fi
else
  echo "WARNING: --allow-dirty-worktree: building from git archive of $COMMIT; the dirty worktree cannot affect the output." >&2
fi

# --- G10: exclusions must not be tracked at this commit -----------------
if [ -n "$(git ls-files docs/fa backend/internal/networkreward)" ]; then
  echo "FAIL: excluded paths are tracked at $COMMIT:" >&2
  git ls-files docs/fa backend/internal/networkreward >&2
  exit 1
fi

# --- toolchain ----------------------------------------------------------
GO_BIN="${GO_BIN:-$(command -v go || echo "$HOME/go/bin/go")}"
NODE_BIN="${NODE_BIN:-$(command -v node || echo "")}"
NPM_BIN="${NPM_BIN:-$(command -v npm || echo "")}"
[ -x "$GO_BIN" ] || { echo "FAIL: go binary not found ($GO_BIN)" >&2; exit 2; }
[ -n "$NODE_BIN" ] && [ -x "$NODE_BIN" ] || { echo "FAIL: node not found on PATH" >&2; exit 2; }
[ -n "$NPM_BIN" ] && [ -x "$NPM_BIN" ] || { echo "FAIL: npm not found on PATH" >&2; exit 2; }
GO_VERSION="$("$GO_BIN" version)"
NODE_VERSION="$("$NODE_BIN" --version)"
NPM_VERSION="$("$NPM_BIN" --version)"
PNPM_VERSION="$(command -v pnpm >/dev/null 2>&1 && pnpm --version || echo "")"
echo "toolchain: $GO_VERSION / node $NODE_VERSION / npm $NPM_VERSION"

# --- G1: stage from git archive only ------------------------------------
if [ -z "$OUT_DIR" ]; then OUT_DIR="$REPO/releases/$COMMIT"; fi
if [ -e "$OUT_DIR" ]; then
  echo "FAIL: release already staged: $OUT_DIR (remove it to rebuild)." >&2
  exit 1
fi
mkdir -p "$OUT_DIR"
echo "staging git archive $COMMIT -> $OUT_DIR"
git archive --format=tar "$COMMIT" | tar -xf - -C "$OUT_DIR"
if [ -e "$OUT_DIR/docs/fa" ] || [ -e "$OUT_DIR/backend/internal/networkreward" ]; then
  echo "FAIL: excluded path present in staged archive." >&2
  exit 1
fi
if [ -n "$(find "$OUT_DIR" -maxdepth 2 -name '.env*' -not -name '.env.example' 2>/dev/null)" ]; then
  echo "FAIL: .env* file present in staged archive:" >&2
  find "$OUT_DIR" -maxdepth 2 -name '.env*' >&2
  exit 1
fi

# --- G9: contract hash from the staged source ---------------------------
ROUTES_HASH="$(python3 "$SCRIPT_DIR/contract_routes.py" "$OUT_DIR/backend/cmd/server/main.go")"
echo "contract routesHash: $ROUTES_HASH"

# --- frontend build (scrubbed env, inside the stage) --------------------
NODE_MODULES_DESC=""
setup_node_modules() {
  if [ "$NODE_MODULES_MODE" = "symlink" ]; then
    ln -s "$REPO/node_modules" "$OUT_DIR/node_modules"
    NODE_MODULES_DESC="symlink:$REPO/node_modules"
    return 0
  fi
  # Canonical: npm ci from the staged package-lock.json.
  if (cd "$OUT_DIR" && env -i HOME="$HOME" PATH="$PATH" TMPDIR="${TMPDIR:-/tmp}" "$NPM_BIN" ci --no-audit --no-fund); then
    NODE_MODULES_DESC="npm ci"
    return 0
  fi
  if [ "$NODE_MODULES_MODE" = "npm-ci" ]; then
    echo "FAIL: npm ci failed and --node-modules=npm-ci forbids the symlink fallback." >&2
    return 1
  fi
  echo "WARNING: npm ci failed; falling back to read-only symlink of $REPO/node_modules (recorded in manifest)." >&2
  if [ ! -d "$REPO/node_modules" ]; then
    echo "FAIL: no $REPO/node_modules to symlink and npm ci failed." >&2
    return 1
  fi
  ln -s "$REPO/node_modules" "$OUT_DIR/node_modules"
  NODE_MODULES_DESC="symlink:$REPO/node_modules (npm ci failed)"
  return 0
}
setup_node_modules
echo "node_modules: $NODE_MODULES_DESC"

echo "building frontend in stage (scrubbed env: no VITE_*, no inherited shell)..."
(cd "$OUT_DIR" && env -i HOME="$HOME" PATH="$PATH" TMPDIR="${TMPDIR:-/tmp}" PUPPETEER_SKIP_DOWNLOAD=true "$NPM_BIN" run build)
[ -f "$OUT_DIR/dist/index.html" ] || { echo "FAIL: frontend build produced no dist/index.html" >&2; exit 1; }
echo "frontend build OK: $(find "$OUT_DIR/dist" -type f | wc -l) dist files"

# --- backend build (staged source, ldflags inject commit + contract) ----
mkdir -p "$OUT_DIR/backend/bin"
echo "building backend from staged source..."
(cd "$OUT_DIR/backend" && env -i HOME="$HOME" PATH="$PATH" TMPDIR="${TMPDIR:-/tmp}" \
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOMAXPROCS=1 \
  "$GO_BIN" build -buildvcs=false -trimpath \
  -ldflags "-s -w -X main.ReleaseCommit=$COMMIT -X main.ReleaseContract=$ROUTES_HASH" \
  -o bin/server-linux ./cmd/server)
(cd "$OUT_DIR/backend" && env -i HOME="$HOME" PATH="$PATH" TMPDIR="${TMPDIR:-/tmp}" \
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOMAXPROCS=1 \
  "$GO_BIN" build -buildvcs=false -trimpath -ldflags "-s -w" \
  -o bin/migrate-linux ./cmd/migrate)
echo "backend build OK: $(sha256sum "$OUT_DIR/backend/bin/server-linux" | cut -c1-16)..."

# --- G3: manifest --------------------------------------------------------
PSP_FLAGS=()
if [ -n "${PSP_TERMINAL_ID:-}" ] && [ -n "${PSP_API_KEY:-}" ]; then PSP_FLAGS+=(--psp-configured); fi
if [ "${PSP_VERIFIED:-}" = "1" ]; then PSP_FLAGS+=(--psp-verified); fi
python3 "$SCRIPT_DIR/make_manifest.py" \
  --stage "$OUT_DIR" --commit "$COMMIT" --branch "$BRANCH" \
  --payment-mode "$PAYMENT_MODE" "${PSP_FLAGS[@]}" \
  --node-modules "$NODE_MODULES_DESC" \
  --go-version "$GO_VERSION" --node-version "$NODE_VERSION" \
  --npm-version "$NPM_VERSION" --pnpm-version "$PNPM_VERSION"

# --- validate ------------------------------------------------------------
VALIDATE_ARGS=(--stage "$OUT_DIR" --commit "$COMMIT")
[ "$PRODUCTION" -eq 1 ] && VALIDATE_ARGS+=(--production)
python3 "$SCRIPT_DIR/validate_release.py" "${VALIDATE_ARGS[@]}"

# --- D3 retention: newest 5 staged releases ------------------------------
python3 - "$REPO/releases" <<'EOF'
import sys
from pathlib import Path
root = Path(sys.argv[1])
dirs = sorted((d for d in root.iterdir() if d.is_dir() and len(d.name) == 40 and all(c in "0123456789abcdef" for c in d.name)),
              key=lambda d: d.stat().st_mtime)
for old in dirs[:-5]:
    import shutil
    shutil.rmtree(old)
    print(f"pruned old staged release: {old.name}")
EOF

echo "RELEASE OK: $OUT_DIR"
echo "  manifest: $OUT_DIR/release.json"
echo "  commit:   $COMMIT"
echo "  contract: $ROUTES_HASH"
