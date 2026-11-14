$ErrorActionPreference = 'Stop'
& python (Join-Path $PSScriptRoot 'load_test_isolated.py') @args
exit $LASTEXITCODE
