# verify-sfu-signaling.ps1
# Tests signaling flow without needing a real browser.

$ErrorActionPreference = "Stop"

$CameraId = "00000000-0000-0000-0000-000000000001" # Mock ID
$Token = "mock-token" # In a real test we'd get a JWT

Write-Host "--- Verifying SFU Signaling Implementation ---" -ForegroundColor Cyan

# 1. Start SFU in Background
Write-Host "Starting SFU Service..."
$SfuJob = Start-Job -ScriptBlock {
    $env:SFU_SECRET = "sfu-internal-secret"
    $env:PORT = "8085"
    cd "c:\Users\sudha\Desktop\ts_vms_1.0\sfu"
    node dist/main.js
}
Start-Sleep -Seconds 5

try {
    # 2. Test Internal SFU API (Direct)
    Write-Host "Testing SFU Internal API..."
    $Capabilities = Invoke-RestMethod -Uri "http://localhost:8085/rooms/test-tenant:test-camera/rtp-capabilities" `
        -Method Get -Headers @{ "x-internal-auth" = "sfu-internal-secret" }
    
    if ($Capabilities.codecs.Count -gt 0) {
        Write-Host "  [OK] SFU Capabilities retrieved." -ForegroundColor Green
    }
    else {
        Write-Error "  [FAIL] Failed to retrieve SFU capabilities."
    }

    # 3. Test Ingest Preparation (PlainTransport)
    Write-Host "Testing SFU Ingest Allocation..."
    $Ingest = Invoke-RestMethod -Uri "http://localhost:8085/rooms/test-tenant:test-camera/ingest" `
        -Method Post -Headers @{ "x-internal-auth" = "sfu-internal-secret" }
    
    Write-Host "  [OK] Allocated port: $($Ingest.port), SSRC: $($Ingest.ssrc)" -ForegroundColor Green

    # 4. Test WebRTC Transport Creation
    Write-Host "Testing WebRTC Transport Creation..."
    $Transport = Invoke-RestMethod -Uri "http://localhost:8085/rooms/test-tenant:test-camera/transports/webrtc" `
        -Method Post -Headers @{ "x-internal-auth" = "sfu-internal-secret" }
    
    if ($null -ne $Transport.id) {
        Write-Host "  [OK] WebRTC Transport created: $($Transport.id)" -ForegroundColor Green
    }
    else {
        Write-Error "  [FAIL] Failed to create WebRTC transport."
    }

    Write-Host "Signaling Verification Successful!" -ForegroundColor Green
}
catch {
    Write-Host "Verification Failed: $_" -ForegroundColor Red
}
finally {
    Write-Host "Stopping SFU Service..."
    Stop-Job -Job $SfuJob
    Remove-Job -Job $SfuJob
}
