# run-all-phase4.ps1
$ErrorActionPreference = "Continue"
$Root = Split-Path $PSScriptRoot -Parent
$ReportPath = Join-Path $Root "docs\phase4\audit_report.md"

if (!(Test-Path (Split-Path $ReportPath))) { New-Item -ItemType Directory -Path (Split-Path $ReportPath) -Force }

$Header = @"
# TS-VMS Phase 4 Integration Audit Report
Generated: $(Get-Date -Format "yyyy-MM-dd HH:mm:ss")
---
"@
$Header | Out-File -FilePath $ReportPath -Encoding utf8

$Scripts = @(
    "vms-service-audit.ps1",
    "vms-storage-audit.ps1",
    "vms-api-health.ps1",
    "verify-phase-4.2.ps1",
    "verify-phase-4.2.5.ps1",
    "verify-phase-4.3.ps1",
    "verify-phase-4.4.ps1"
)

foreach ($Script in $Scripts) {
    Write-Host "Running $Script..." -ForegroundColor Cyan
    $ScriptPath = Join-Path $PSScriptRoot $Script
    if (Test-Path $ScriptPath) {
        & $ScriptPath | Out-File -FilePath $ReportPath -Append -Encoding utf8
    } else {
        "## Missed: $Script (File not found)" | Out-File -FilePath $ReportPath -Append -Encoding utf8
    }
}

Write-Host "All audits complete. Report at $ReportPath" -ForegroundColor Green
