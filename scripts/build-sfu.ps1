# build-sfu.ps1
# Build script for SFU service

$ErrorActionPreference = "Stop"

$SfuDir = Join-Path $PSScriptRoot "..\sfu"
$OutDir = Join-Path $PSScriptRoot "..\sfu\dist"

Write-Host "--- Building SFU Service ---" -ForegroundColor Cyan

Push-Location $SfuDir

try {
    Write-Host "Installing dependencies (npm ci)..."
    npm ci

    Write-Host "Compiling TypeScript..."
    npm run build

    Write-Host "SFU Build Complete." -ForegroundColor Green
    Write-Host "Output located in: $OutDir"
}
finally {
    Pop-Location
}
