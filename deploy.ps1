$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$configPath = Join-Path $root '.env.deploy.local'
if (-not (Test-Path $configPath)) {
    throw "Missing $configPath"
}

$config = @{}
Get-Content $configPath | ForEach-Object {
    if ($_ -match '^([^#=]+)=(.*)$') {
        $config[$Matches[1].Trim()] = $Matches[2].Trim()
    }
}

foreach ($name in @('DEPLOY_HOST', 'DEPLOY_PORT', 'DEPLOY_USER', 'DEPLOY_PASSWORD', 'DEPLOY_HOSTKEY', 'DEPLOY_TARGET')) {
    if (-not $config[$name]) { throw "Missing $name in $configPath" }
}

$plink = (Get-Command plink.exe -ErrorAction SilentlyContinue).Source
$pscp = (Get-Command pscp.exe -ErrorAction SilentlyContinue).Source
if (-not $plink -or -not $pscp) {
    throw 'PuTTY plink.exe and pscp.exe are required.'
}

function Invoke-Remote([string]$command) {
    & $plink -batch -ssh -P $config.DEPLOY_PORT -pw $config.DEPLOY_PASSWORD -hostkey $config.DEPLOY_HOSTKEY "$($config.DEPLOY_USER)@$($config.DEPLOY_HOST)" $command
    if ($LASTEXITCODE -ne 0) { throw "Remote command failed ($LASTEXITCODE)" }
}

$dist = Join-Path $root 'dist'
if (Test-Path $dist) { Remove-Item $dist -Recurse -Force }
Push-Location $root
try {
    npm run build
    if ($LASTEXITCODE -ne 0) { throw 'Build failed.' }
} finally {
    Pop-Location
}

$target = $config.DEPLOY_TARGET.TrimEnd('/')
Invoke-Remote "mkdir -p '$target/dist'"
& $pscp -batch -P $config.DEPLOY_PORT -pw $config.DEPLOY_PASSWORD -hostkey $config.DEPLOY_HOSTKEY -r "$dist\*" "$($config.DEPLOY_USER)@$($config.DEPLOY_HOST):$target/dist/"
if ($LASTEXITCODE -ne 0) { throw "Upload failed ($LASTEXITCODE)" }

if ($config.DEPLOY_MODE -eq 'docker') {
    Invoke-Remote "cd '$target' && docker compose up -d --build frontend"
} elseif ($config.DEPLOY_MODE -eq 'nginx') {
    Invoke-Remote "nginx -t && systemctl reload nginx"
} else {
    throw "Unsupported DEPLOY_MODE: $($config.DEPLOY_MODE)"
}

Write-Host 'Deploy completed.'
