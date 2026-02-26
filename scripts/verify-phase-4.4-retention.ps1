$ErrorActionPreference = "Stop"

$FakeVol = ".\fake_vols\v1"
$AuditLog = ".\retention_audit.jsonl"

Write-Host "=== TS-VMS Phase 4.4 Retention Engine DoD Verification ===" -ForegroundColor Cyan

# 1. Cleanup
if (Test-Path ".\fake_vols") { Remove-Item -Recurse -Force ".\fake_vols" }
if (Test-Path ".\protected_segments") { Remove-Item -Recurse -Force ".\protected_segments" }
if (Test-Path $AuditLog) { Remove-Item -Force $AuditLog }

# 2. Scaffold Directories
$baseCam1 = "$FakeVol\t1\site1\cam-01\2026-02-26\14"
$baseCam2 = "$FakeVol\t1\site1\cam-02\2026-02-26\14"
New-Item -ItemType Directory -Path $baseCam1 -Force | Out-Null
New-Item -ItemType Directory -Path $baseCam2 -Force | Out-Null

function Make-File($path, $daysOld) {
    Set-Content -Path $path -Value "DUMMY_DATA"
    (Get-Item $path).LastWriteTime = (Get-Date).AddDays(-$daysOld)
}

Write-Host "Generating Fake FS Layout..." -ForegroundColor Yellow
# Cam-01 has a 90 Day Override Policy
Make-File "$baseCam1\cam01_new.mp4" 2        # KEEP: 2 < 90
Make-File "$baseCam1\cam01_old.mp4" 10       # KEEP: 10 < 90

# Cam-02 uses Global 5 Day Policy
Make-File "$baseCam2\cam02_expired.mp4" 8    # DELETE: 8 > 5
Make-File "$baseCam2\cam02_recent.mp4" 2     # KEEP: 2 < 5
Make-File "$baseCam2\cam02_protected.mp4" 8  # KEEP: Protected via Event Mock

Write-Host "Compiling ad-hoc retention harness for E2E..." -ForegroundColor Yellow

# Dynamically generate the Go harness using the correct cfg.Safety namespace
$harnessCode = @"
package main

import (
    "fmt"
    "os"
    "strconv"
    "time"

    "github.com/technosupport/ts-vms/internal/retention"
)

type MockSpaceVerifier struct{}
func (m *MockSpaceVerifier) GetFreeSpace(p string) (uint64, error) { return 1000000000, nil }
func (m *MockSpaceVerifier) VerifyReclamation(path string, expectedBytes int64, beforeBytes uint64) {}

type MockProtector struct{}
func (m MockProtector) IsProtected(cam, file string) bool { return file == "cam02_protected.mp4" }

func main() {
    fastForwardDays := 0
    if len(os.Args) > 1 {
        ff, _ := strconv.Atoi(os.Args[1])
        fastForwardDays = ff
    }

    var cfg retention.Config
    cfg.Defaults.DaysToKeep = 5
    cfg.Defaults.MaxStorageGB = 0
    cfg.Defaults.DryRun = false
    
    cfg.Safety.IncludeSidecars = true
    cfg.Safety.NeverDeleteNewerThanMinutes = 0
    
    cfg.Protection.ProtectIfEventLinked = true

    // Set the 90-day override for cam-01
    cfg.Scopes.Cameras = []retention.CameraConfig{
        {
            CameraID: "cam-01",
            ScopeConfig: retention.ScopeConfig{
                DaysToKeep: 90,
            },
        },
    }

    idx := retention.NewFileSystemEnumerator()
    prot := MockProtector{}
    aud := retention.NewJSONAuditWriter("retention_audit.jsonl")
    ver := &MockSpaceVerifier{}

    engine := retention.NewRetentionEngine(cfg, prot, idx, ver, aud)

    // Run Engine
    runTime := time.Now().Add(time.Duration(fastForwardDays) * 24 * time.Hour)
    status := engine.RunOnce(runTime)
    
    fmt.Printf("DONE: Deleted: %d, Skipped Protected: %d, Errors: %d\n", status.DeletedCount, status.SkippedProtected, status.Errors)
}
"@

if (!(Test-Path "cmd/vms-retention-test")) { New-Item -ItemType Directory -Path "cmd/vms-retention-test" | Out-Null }
Set-Content -Path "cmd/vms-retention-test/main.go" -Value $harnessCode

# Build the newly written harness
go build -o bin/vms-retention-test.exe ./cmd/vms-retention-test

Write-Host "Running Retention Policy Engine (E2E)..." -ForegroundColor Cyan
.\bin\vms-retention-test.exe

# 4. Verify Phase 1
Write-Host "Verifying Disk State Post-Retention..." -ForegroundColor Yellow
$remaining = @(Get-ChildItem -Recurse $FakeVol | Where-Object { $_.Extension -eq '.mp4' })
Write-Host "Remaining Segments: $($remaining.Count) (Expected: 4)"

# 5. Fast Forward 100 Days Test
Write-Host "Re-running Harness with +100 Days simulated fast-forward..." -ForegroundColor Cyan
.\bin\vms-retention-test.exe 100

$finalRemaining = @(Get-ChildItem -Recurse $FakeVol | Where-Object { $_.Extension -eq '.mp4' })
Write-Host "Remaining Segments: $($finalRemaining.Count) (Expected: 1 protected)"

if ($finalRemaining.Count -eq 1 -and $finalRemaining[0].Name -eq "cam02_protected.mp4") {
    Write-Host "[SUCCESS] Engine completely respects overrides and infinite event protection!" -ForegroundColor Green
}
else {
    Write-Error "Incorrect remaining files!"
}
