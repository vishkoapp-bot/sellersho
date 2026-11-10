param(
    [string]$SshHost = 'parspack-sellersho',
    [string]$Target = '/opt/drup',
    [switch]$EnableSSL,
    [switch]$MoveXray443
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -LiteralPath $root
if ($Target -ne '/opt/drup') { throw 'This deployment supports the verified /opt/drup installation only.' }
if ($MoveXray443 -and -not $EnableSSL) { throw 'MoveXray443 requires EnableSSL.' }
function Run-Git([string[]]$GitArgs) {
    $result = & git -c "safe.directory=$($root.Replace('\','/'))" @GitArgs
    if ($LASTEXITCODE -ne 0) { throw "Git failed: $($GitArgs[0])" }
    return $result
}
function Remote([string]$Script) {
    ($Script.Replace("`r", '') + "`n") | & ssh -o BatchMode=yes -o ConnectTimeout=20 $SshHost bash -se
    if ($LASTEXITCODE -ne 0) { throw 'Remote deployment failed; review the rollback result above.' }
}
$commit = (Run-Git -GitArgs @('rev-parse','HEAD')).Trim()
$branch = (Run-Git -GitArgs @('branch','--show-current')).Trim()
if ((Run-Git -GitArgs @('status','--porcelain','--untracked-files=no'))) { throw 'Commit tracked changes before deployment.' }
if ((Run-Git -GitArgs @('rev-parse',"refs/remotes/origin/$branch")).Trim() -ne $commit) { throw 'Push the current commit before deployment.' }
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
    & go build -trimpath -ldflags '-s -w' -o bin/server-linux ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw 'Backend build failed.' }
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
