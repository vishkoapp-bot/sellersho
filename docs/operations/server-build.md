# Production builds on the server

Use `./deploy.ps1 -BuildOnServer` after validating, committing and pushing the
release. This mode avoids uploading a local release archive: the server fetches
the branch, verifies the exact pushed commit and builds its Git archive in a new
release directory. The existing migration-history preflight and checks excluding
`docs/fa/` and `backend/internal/networkreward/` still apply.

The frontend builder uses `node:24-alpine`, matching the validated local Node 24
major. Locked Puppeteer 25.16.0 requires Node >=22.12.0. Only the builder container
receives `PUPPETEER_SKIP_DOWNLOAD=true`; the static production build needs no
downloaded browser. `npm ci` continues to run other package install scripts.
Neither project nor global npm/Puppeteer configuration is changed.

The backend builder remains `golang:1.23`, with `CGO_ENABLED=0` and
`GOMAXPROCS=1` to limit CPU pressure. Both binaries are written to the release's
backend bind mount. The Go compiler cache stays inside the `--rm` container and
is discarded with it. The named Go module cache (`sellersho-go-mod-cache`) and
npm cache (`sellersho-npm-build-cache`) remain persistent.

Before release, verify disk headroom for the Node image pull, production
`node_modules`, and temporary Go compiler cache while retaining at least 128 MiB
free. The deployment script does not prune images, volumes or application data;
any separate cleanup requires verified ownership and absence of active use.
Build failures stop before the existing release runner is invoked.

After both builds, the same `scripts/deploy_remote.py` runner preserves runtime
credentials, backs up production, runs migrations and verifies health. Local
build mode and existing SSH arguments are unchanged. Validate command generation
without a server using `python scripts/test_deploy_server_build.py -v` on Windows
with PowerShell, Git Bash at `D:/Git/bin/bash.exe`, and installed Node dependencies.
The test captures remote bodies without executing them; it does not validate
server capacity, image availability or a complete production release.
