param(
    [string]$SshHost = 'parspack-sellersho',
    [string]$Target = '/opt/drup',
    [switch]$EnableSSL,
    [switch]$MoveXray443,
    [switch]$BuildOnServer
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -LiteralPath $root
if ($Target -ne '/opt/drup') { throw 'This deployment supports the verified /opt/drup installation only.' }
if ($MoveXray443 -and -not $EnableSSL) { throw 'MoveXray443 requires EnableSSL.' }
function Run-Git([string[]]$GitArgs) {
    $result = & git -c "safe.directory=$($root.Replace('\','/'))" -c core.autocrlf=false @GitArgs
    if ($LASTEXITCODE -ne 0) { throw "Git failed: $($GitArgs[0])" }
    return $result
}
function Remote([string]$Script) {
    # Windows PowerShell adds CRLF after pipeline input. Exit before that
    # trailing record reaches the Linux shell; internal script lines use LF.
    ($Script.Replace("`r", '') + "`nexit 0`n") | & ssh -o BatchMode=yes -o ConnectTimeout=20 $SshHost bash -se
    if ($LASTEXITCODE -ne 0) { throw 'Remote deployment failed; review the rollback result above.' }
}
$commit = (Run-Git -GitArgs @('rev-parse','HEAD')).Trim()
$branch = (Run-Git -GitArgs @('branch','--show-current')).Trim()
if ((Run-Git -GitArgs @('status','--porcelain','--untracked-files=no'))) { throw 'Commit tracked changes before deployment.' }
if ((Run-Git -GitArgs @('rev-parse',"refs/remotes/origin/$branch")).Trim() -ne $commit) { throw 'Push the current commit before deployment.' }
# SEC-01 (supervisor F-03, gate G-15): fail fast on a reintroduced
# secret literal before any build or upload. Only the non-Docker
# checks run here; the 2 Docker-dependent checks are Docker-host-only
# (see scripts/run_secret_checks_nodocker.py).
$pythonExe = $null
$pythonCmd = Get-Command python3 -ErrorAction SilentlyContinue
if ($pythonCmd) { $pythonExe = $pythonCmd.Source } else {
    $pythonCmd = Get-Command python -ErrorAction SilentlyContinue
    if ($pythonCmd) { $pythonExe = $pythonCmd.Source }
}
if (-not $pythonExe) { throw 'SEC-01 secret checks need python3 (or python) on PATH; refusing to deploy.' }
& $pythonExe scripts/run_secret_checks_nodocker.py $root
if ($LASTEXITCODE -ne 0) { throw 'SEC-01 production-secret checks failed; refusing to deploy.' }
# The release runner performs reviewed legacy adoption only after its verified
# database backup. This early check is deliberately read-only.
Remote @'
set -eu
ready=$(docker exec drup_postgres psql -U drup_admin -d drup_db -Atc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='schema_migrations'")
if [ "$ready" != "1" ]; then
  echo 'DEPLOYMENT BLOCKED: production migration history is unavailable.' >&2
  exit 1
fi
'@
# Slow/unreliable client uploads can use the server's authenticated Git origin.
# Both applications still build from the exact pushed commit in an isolated release.
if ($BuildOnServer) {
    if ($branch -notmatch '^[A-Za-z0-9_./-]+$') { throw 'Unsupported deployment branch name.' }
    $sslArg = if ($EnableSSL) { '1' } else { '0' }
    $xrayArg = if ($MoveXray443) { '1' } else { '0' }
    Remote @"
set -eu
GIT_TERMINAL_PROMPT=0 git -C '$Target' fetch origin '$branch'
test "`$(git -C '$Target' rev-parse FETCH_HEAD)" = '$commit'
release='$Target/releases/$commit'
test ! -e "`$release"
mkdir -p "`$release"
git -C '$Target' archive '$commit' | tar -xf - -C "`$release"
test ! -e "`$release/docs/fa"
test ! -e "`$release/backend/internal/networkreward"
docker run --rm -e PUPPETEER_SKIP_DOWNLOAD=true -v "`$release:/app" -v sellersho-npm-build-cache:/root/.npm -w /app node:24-alpine sh -c 'npm ci --no-audit --no-fund && npm run build && test -f /app/dist/index.html && test ! -L /app/node_modules && rm -rf -- /app/node_modules'
docker run --rm -e CGO_ENABLED=0 -e GOMAXPROCS=1 -v "`$release/backend:/app" -v sellersho-go-mod-cache:/go/pkg/mod -w /app golang:1.23 sh -c 'mkdir -p bin && go build -buildvcs=false -trimpath -ldflags "-s -w" -o bin/server-linux ./cmd/server && go build -buildvcs=false -trimpath -ldflags "-s -w" -o bin/migrate-linux ./cmd/migrate'
python3 -c 'import json,datetime,pathlib,sys; pathlib.Path(sys.argv[1]).write_text(json.dumps(dict(commit="$commit",branch="$branch",builtAt=datetime.datetime.now(datetime.timezone.utc).isoformat())))' "`$release/release.json"
python3 "`$release/scripts/deploy_remote.py" '$Target' '$commit' '$sslArg' '$xrayArg'
"@
    Write-Host "Deployed commit $commit (built on server)"
    return
}
# Only committed source enters a release; local documents, credentials and
# the paused network-reward files are excluded by git archive.
$stage = Join-Path $root "releases\$commit"
if (Test-Path -LiteralPath $stage) { throw "Release already staged: $stage" }
New-Item -ItemType Directory -Path $stage -Force | Out-Null
$sourceTar = Join-Path $root "releases\source-$commit.tar"
Run-Git -GitArgs @('archive','--format=tar',"--output=$sourceTar",'HEAD') | Out-Null
& tar -xf $sourceTar -C $stage
if ($LASTEXITCODE -ne 0) { throw 'Source extraction failed.' }
& npm run build
if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
Copy-Item -LiteralPath (Join-Path $root 'dist') -Destination (Join-Path $stage 'dist') -Recurse
New-Item -ItemType Directory -Path (Join-Path $stage 'backend\bin') -Force | Out-Null
$previousGoOS = $env:GOOS; $previousGoArch = $env:GOARCH; $previousCGO = $env:CGO_ENABLED
Push-Location (Join-Path $stage 'backend')
try {
    $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    # The archive has no .git directory; release.json records its exact commit.
    & go build -buildvcs=false -trimpath -ldflags '-s -w' -o bin/server-linux ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw 'Backend build failed.' }
    & go build -buildvcs=false -trimpath -ldflags '-s -w' -o bin/migrate-linux ./cmd/migrate
    if ($LASTEXITCODE -ne 0) { throw 'Migration runner build failed.' }
} finally {
    Pop-Location
    $env:GOOS = $previousGoOS; $env:GOARCH = $previousGoArch; $env:CGO_ENABLED = $previousCGO
}
$manifest = @{commit=$commit; branch=$branch; builtAt=[DateTime]::UtcNow.ToString('o')}
[IO.File]::WriteAllText((Join-Path $stage 'release.json'), ($manifest | ConvertTo-Json), (New-Object Text.UTF8Encoding($false)))
$artifact = Join-Path $root "releases\release-$commit.tar.gz"
& tar -czf $artifact -C $stage .
if ($LASTEXITCODE -ne 0) { throw 'Release packaging failed.' }
$digest = (Get-FileHash -LiteralPath $artifact -Algorithm SHA256).Hash.ToLowerInvariant()
Remote "mkdir -p '$Target/incoming' '$Target/releases'"
& scp -o BatchMode=yes -o ConnectTimeout=20 $artifact "${SshHost}:$Target/incoming/$commit.tar.gz"
if ($LASTEXITCODE -ne 0) { throw 'Release upload failed.' }
$sslArg = if ($EnableSSL) { '1' } else { '0' }
$xrayArg = if ($MoveXray443) { '1' } else { '0' }
Remote @"
set -eu
echo '$digest  $Target/incoming/$commit.tar.gz' | sha256sum -c -
mkdir -p '$Target/releases/$commit'
tar -xzf '$Target/incoming/$commit.tar.gz' -C '$Target/releases/$commit'
python3 '$Target/releases/$commit/scripts/deploy_remote.py' '$Target' '$commit' '$sslArg' '$xrayArg'
"@
Write-Host "Deployed commit $commit"
