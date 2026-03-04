$ErrorActionPreference = "Stop"

Write-Host "Building Test Harness..." -ForegroundColor Cyan
go build -o vms-recorder-health.exe "$PSScriptRoot\..\cmd\vms-recorder-health\main.go"

function Run-TestScenario ($Name, $ScenarioArgs, $ExpectedLogs) {
    Write-Host "`n=== Running Scenario: $Name ===" -ForegroundColor Yellow
    
    if ([string]::IsNullOrWhiteSpace($ScenarioArgs)) {
        $proc = Start-Process -FilePath ".\vms-recorder-health.exe" -PassThru -NoNewWindow -RedirectStandardOutput "test_out.log"
    }
    else {
        $proc = Start-Process -FilePath ".\vms-recorder-health.exe" -ArgumentList $ScenarioArgs -PassThru -NoNewWindow -RedirectStandardOutput "test_out.log"
    }
    Start-Sleep -Seconds 2

    Write-Host "Waiting 12 seconds for rolling windows to fill and alerts to trigger..."
    for ($i = 1; $i -le 3; $i++) {
        $status = Invoke-RestMethod -Uri "http://localhost:8089/status"
        $cam = $status.cameras."cam-test-01"
        Write-Host "  Tick $i -> MB/s: $([math]::Round($cam.write_mbps_avg, 2)), Drop%: $([math]::Round($cam.frame_drop_rate_pct_window, 2))"
        Start-Sleep -Seconds 4
    }

    Stop-Process -Id $proc.Id -Force
    
    $logs = Get-Content "test_out.log" -Raw
    if ($logs -match $ExpectedLogs) {
        Write-Host "[OK] Expected alert found." -ForegroundColor Green
    }
    else {
        Write-Error "Failed to find expected alert: $ExpectedLogs"
    }
}

# 1. Normal Operation
Run-TestScenario "Normal Operation" "" '"level":"INFO"'

# 2. Slow Disk Operation
Run-TestScenario "Slow Disk Alert" "-simulate-slow-disk" 'recording.disk.low_write_rate.crit'

# 3. Frame Drop Operation
Run-TestScenario "Frame Drop Alert" "-simulate-drops" 'recording.frame_drop.crit'

Write-Host "`nAll Phase 4.9 Health Monitoring tests passed!" -ForegroundColor Green
Remove-Item ".\vms-recorder-health.exe"
Remove-Item ".\test_out.log"
