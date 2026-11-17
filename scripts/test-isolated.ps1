# Arguments after -- are passed to a child command with disposable fixture URLs.
$ErrorActionPreference = 'Stop'
& python (Join-Path $PSScriptRoot 'test-isolated.py') @args
exit $LASTEXITCODE
