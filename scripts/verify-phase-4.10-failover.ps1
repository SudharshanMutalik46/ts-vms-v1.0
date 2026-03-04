$ErrorActionPreference = "Stop"

Write-Host "Building Phase 4.10 Failover Test Harness..." -ForegroundColor Cyan
go build -o vms-recovery-test.exe "$PSScriptRoot\..\cmd\vms-recovery-test\main.go"

function Run-Scenario ($Name, $ScenarioArgs, $ExpectedHttp, $ExpectedLog) {
    Write-Host "`n=== Running Scenario: $Name ===" -ForegroundColor Yellow
    
    if ([string]::IsNullOrWhiteSpace($ScenarioArgs)) {
        $proc = Start-Process -FilePath ".\vms-recovery-test.exe" -PassThru -NoNewWindow -RedirectStandardOutput "test_out.log"
    }
    else {
        $proc = Start-Process -FilePath ".\vms-recovery-test.exe" -ArgumentList $ScenarioArgs -PassThru -NoNewWindow -RedirectStandardOutput "test_out.log"
    }
    Start-Sleep -Seconds 3

    if ($ExpectedLog -match "FATAL") {
        # Fast fail expected
        if (-not $proc.HasExited) {
            Write-Error "Test Harness was expected to crash, but is still running!"
        }
        $log = Get-Content "test_out.log" -Raw
        if ($log -match "FATAL: Unhandled Exception") {
            Write-Host "[OK] Detected simulated crash log." -ForegroundColor Green
        }
        else {
            Write-Error "Failed to find crash string in logs."
        }
    }
    else {
        try {
            $resp = Invoke-WebRequest -Uri "http://localhost:8099/readyz" -UseBasicParsing
            if ($resp.StatusCode -eq 200 -and $ExpectedHttp -eq 200) {
                Write-Host "[OK] Readiness is 200 OK!" -ForegroundColor Green
            }
            elseif ($ExpectedHttp -ne 200) {
                Write-Error "Expected failure code $ExpectedHttp, got 200."
            }
        }
        catch {
            $ex = $_.Exception.Response
            $code = $ex.StatusCode.value__
            if ($code -eq $ExpectedHttp) {
                Write-Host "[OK] Received expected failure code: $code." -ForegroundColor Green
            }
            else {
                Write-Error "Unexpected failure code: $code (Expected $ExpectedHttp)"
            }
        }

        $log = Get-Content "test_out.log" -Raw
        if ($log -match $ExpectedLog) {
            Write-Host "[OK] Expected log found: $ExpectedLog" -ForegroundColor Green
        }
        else {
            Write-Error "Failed to find expected log: $ExpectedLog"
        }

        Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    }
}

# 1. Normal Recovery (Resumes correctly, 200 OK ready)
Run-Scenario "Normal Recovery & Readiness" "" 200 "recovery.scanner.complete"

# 2. Simulate Crash
Run-Scenario "Simulated Crash" "-simulate-crash" 0 "FATAL"

# 3. Degraded No-DB Readiness
Run-Scenario "Degraded DB Link" "-simulate-no-db" 503 "Test harness running"

# 4. Circuit Breaker Engaged
Run-Scenario "Circuit Breaker Disks Full" "-simulate-disk-full" 503 "recording.circuit_breaker.engaged"

Write-Host "`nAll Phase 4.10 Failover & Circuit Breaker tests passed!" -ForegroundColor Green
Remove-Item ".\vms-recovery-test.exe"
Remove-Item ".\test_out.log"
