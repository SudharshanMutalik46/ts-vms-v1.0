$ErrorActionPreference = "Continue"

$ReportPath = "C:\Users\sudha\Desktop\ts_vms_1.0\docs\phase4\audit_report.md"

$Scripts = @(
    "verify-phase-4.1-storage.ps1",
    "verify-phase-4.2-recording.ps1",
    "verify-phase-4.3-segment-writer.ps1",
    "verify-phase-4.4-retention.ps1",
    "verify-phase-4.5-metadata.ps1",
    "verify-phase-4.6-prebuffer.ps1",
    "verify-phase-4.7-recording-apis.ps1",
    "verify-phase-4.8-diskio.ps1",
    "verify-phase-4.9-health.ps1",
    "verify-phase-4.10-failover.ps1",
    "run-phase-4.11-perf.ps1"
)

$DateStr = Get-Date -Format "yyyy-MM-dd HH:mm:ss"

$ReportContent = @"
# TS-VMS Phase 4 Audit Report
**Date:** $DateStr

This document contains the automated verification results for all Phase 4 modules.

| Phase | Script Name | Status |
|---|---|---|
"@

$PassCount = 0
$FailCount = 0

foreach ($Script in $Scripts) {
    Write-Host "`n========================================" -ForegroundColor Cyan
    Write-Host "Running $Script..." -ForegroundColor Yellow
    Write-Host "========================================" -ForegroundColor Cyan
    
    $ScriptPath = "C:\Users\sudha\Desktop\ts_vms_1.0\scripts\$Script"
    
    # Run script and capture both output and error
    $Output = & powershell.exe -ExecutionPolicy Bypass -File $ScriptPath *>&1
    
    $ExitCode = $LASTEXITCODE

    if ($ExitCode -eq 0) {
        Write-Host "[PASS] $Script" -ForegroundColor Green
        $ReportContent += "`n| $($Script.Split('-')[2]) | $Script | ✅ PASS |"
        $PassCount++
    }
    else {
        Write-Host "[FAIL] $Script" -ForegroundColor Red
        Write-Host "Output:"
        $Output | Out-String | Write-Host
        $ReportContent += "`n| $($Script.Split('-')[2]) | $Script | ❌ FAIL |"
        $FailCount++
    }
}

$TotalCount = $PassCount + $FailCount

$ReportContent += @"

## Summary
* **Total Tests Executed:** $TotalCount
* **Passed:** $PassCount
* **Failed:** $FailCount

"@

if ($FailCount -eq 0) {
    $ReportContent += "**Status:** All Phase 4 requirements successfully verified."
}
else {
    $ReportContent += "**Status:** Some tests failed. Check logs for details."
}

Set-Content -Path $ReportPath -Value $ReportContent
Write-Host "`nAudit Report generated at: $ReportPath" -ForegroundColor Green
