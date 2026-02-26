$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path "$RepoRoot\.."

Write-Host "=== TS-VMS Phase 4.7 Recording API Verification ===" -ForegroundColor Cyan

# 1. Compile and Run the Dual-Server Harness
Write-Host "Starting API Gateways (Test Ports 18080 & 18082)..." -ForegroundColor Yellow
if (!(Test-Path "bin")) { New-Item -ItemType Directory -Path "bin" | Out-Null }
go build -o bin/vms-phase47.exe ./cmd/vms-phase47-test
$apiProc = Start-Process -FilePath ".\bin\vms-phase47.exe" -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3

try {
    # Check if process is still running (if it crashed due to ports, this catches it)
    if ($apiProc.HasExited) {
        Write-Error "Test harness crashed immediately. Check ports 18080/18082."
    }

    $headers = @{ "Authorization" = "Bearer debug-token" }

    # 2. Schedule Management (CRUD + Reload Trigger)
    Write-Host "`nTesting Schedule API (Creates DB row & reloads worker)..." -ForegroundColor Cyan
    $schedRes = Invoke-RestMethod -Uri "http://localhost:18080/api/v1/recording/schedules" -Method POST -Headers $headers -ContentType "application/json" -Body '{}'
    if ($schedRes.status -eq "saved_and_reloaded") { Write-Host "[OK] Schedule Saved & Reloaded." }

    # 3. Start Camera (Manual Override)
    Write-Host "`nTesting Camera START (Manual Override)..." -ForegroundColor Cyan
    Invoke-RestMethod -Uri "http://localhost:18080/api/v1/recording/cameras/cam-01/start" -Method POST -Headers $headers | Out-Null
    
    $status1 = Invoke-RestMethod -Uri "http://localhost:18080/api/v1/recording/status" -Headers $headers
    if ($status1."cam-01" -eq "RECORDING") { Write-Host "[OK] cam-01 is RECORDING." -ForegroundColor Green }

    # 4. Test Pause
    Write-Host "`nTesting Camera PAUSE..." -ForegroundColor Cyan
    Invoke-RestMethod -Uri "http://localhost:18080/api/v1/recording/cameras/cam-01/pause" -Method POST -Headers $headers | Out-Null
    
    $status2 = Invoke-RestMethod -Uri "http://localhost:18080/api/v1/recording/status" -Headers $headers
    if ($status2."cam-01" -eq "PAUSED") { Write-Host "[OK] cam-01 is PAUSED." -ForegroundColor Green }

    # 5. Test License Quota on Bulk Start
    Write-Host "`nTesting Bulk START-ALL (With License Quota = 1)..." -ForegroundColor Cyan
    Invoke-RestMethod -Uri "http://localhost:18080/api/v1/recording/start-all" -Method POST -Headers $headers | Out-Null
    
    $status3 = Invoke-RestMethod -Uri "http://localhost:18080/api/v1/recording/status" -Headers $headers
    $status3 | ConvertTo-Json
    if ($status3."cam-02" -eq "THROTTLED_BY_LICENSE") { 
        Write-Host "[OK] Quota successfully throttled cam-02 during bulk start!" -ForegroundColor Green 
    }
    else {
        Write-Error "Quota failed."
    }

    # 6. Test Export API
    Write-Host "`nTesting Export Generation..." -ForegroundColor Cyan
    $export = Invoke-RestMethod -Uri "http://localhost:18080/api/v1/recording/exports" -Method POST -Headers $headers -ContentType "application/json" -Body '{}'
    if ($export.state -eq "PROCESSING" -and $export.download_url -ne $null) {
        Write-Host "[OK] Export Job Created: $($export.export_id)" -ForegroundColor Green
    }

    Write-Host "`n[SUCCESS] Phase 4.7 API integration is complete!" -ForegroundColor Cyan

}
finally {
    Write-Host "`nCleaning up background services..."
    Stop-Process -Id $apiProc.Id -Force
}
