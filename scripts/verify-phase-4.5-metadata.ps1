$ErrorActionPreference = "Stop"

# Auto-detect repository root and change directory so go build works perfectly
Set-Location -Path "$PSScriptRoot\.."

Write-Host "=== TS-VMS Phase 4.5 Metadata DB Verification ===" -ForegroundColor Cyan

# 1. Create Fake Phase 4.1 Directory Structure
$FakeVol = ".\fake_vols_45\t1\site1\cam-99\2026-02-26\14"
if (Test-Path ".\fake_vols_45") { Remove-Item -Recurse -Force ".\fake_vols_45" }
New-Item -ItemType Directory -Path $FakeVol -Force | Out-Null

$mockFile = "$FakeVol\cam-99_20260226T140000Z_300_0001.mp4"
Set-Content -Path $mockFile -Value "MOCK_VIDEO_DATA"

# 2. Build Tools
Write-Host "Compiling Backfill Tool & API Harness..." -ForegroundColor Yellow
if (!(Test-Path "bin")) { New-Item -ItemType Directory -Path "bin" | Out-Null }
go build -o bin/recording_backfill.exe ./cmd/recording_backfill
go build -o bin/vms-api-test.exe ./cmd/vms-recording-api-test

# 3. Start API (Also applies DB schema silently)
Write-Host "Starting API & Migrations Database Server..." -ForegroundColor Cyan
$apiProc = Start-Process -FilePath ".\bin\vms-api-test.exe" -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3

try {
    # 4. Run Backfill Tool
    Write-Host "`nRunning Backfill Scanner on fake_vols_45..." -ForegroundColor Yellow
    .\bin\recording_backfill.exe -dir ".\fake_vols_45" -db "postgres://postgres:ts1234@localhost:5432/ts_vms?sslmode=disable"

    # 5. Test Timeline Query (Should be unprotected)
    Write-Host "`nQuerying Desktop Timeline API..." -ForegroundColor Cyan
    $headers = @{ "Authorization" = "Bearer debug-admin-token" }
    
    $segments = Invoke-RestMethod -Uri "http://localhost:8083/api/v1/recordings/segments?camera_id=cam-99&from=2026-01-01T00:00:00Z&to=2026-12-31T00:00:00Z" -Headers $headers
    $segments | ConvertTo-Json

    $targetSegId = $segments[0].id

    # 6. Simulate Event and Link (Protection)
    Write-Host "`nSimulating AI Event and linking segment to protect it..." -ForegroundColor Red
    $eventPayload = @{ tenant_id = "t1"; site_id = "site1"; camera_id = "cam-99"; event_type = "person_detected" } | ConvertTo-Json
    $event = Invoke-RestMethod -Uri "http://localhost:8083/api/v1/recordings/events" -Method POST -Body $eventPayload -Headers $headers -ContentType "application/json"

    Invoke-RestMethod -Uri "http://localhost:8083/api/v1/recordings/events/link?event_id=$($event.id)&segment_id=$targetSegId" -Method POST -Headers $headers

    # 7. Test Timeline Query Again (Should be PROTECTED = TRUE)
    Write-Host "`nQuerying Timeline API After Event Linkage..." -ForegroundColor Green
    $protectedSegments = Invoke-RestMethod -Uri "http://localhost:8083/api/v1/recordings/segments?camera_id=cam-99&from=2026-01-01T00:00:00Z&to=2026-12-31T00:00:00Z" -Headers $headers
    $protectedSegments | ConvertTo-Json

    if ($protectedSegments[0].is_protected -eq $true) {
        Write-Host "`n[SUCCESS] Metadata DB reliably ingested, queried, and derived event protection!" -ForegroundColor Green
    }
    else {
        Write-Error "Protection flag failed to propagate to API response."
    }

}
finally {
    Write-Host "`nCleaning up API Server..."
    Stop-Process -Id $apiProc.Id -Force
}
