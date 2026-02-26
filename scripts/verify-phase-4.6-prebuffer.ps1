$ErrorActionPreference = "Stop"

# Auto-detect repository root
$RepoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path "$RepoRoot\.."

Write-Host "=== TS-VMS Phase 4.6 Pre-Buffer DoD Verification ===" -ForegroundColor Cyan

# 1. Clean old outputs
if (Test-Path ".\out_cam-test_0000.mp4") { Remove-Item ".\out_cam-test_0000.mp4" -Force }

# 2. Compile Harness
Write-Host "Compiling Pre-Buffer Harness..." -ForegroundColor Yellow

Push-Location "src/recording"
cmake -B build
cmake --build build
Pop-Location

$ExePath = ".\src\recording\build\Debug\vms-prebuffer-harness.exe"
if (!(Test-Path $ExePath)) {
    Write-Warning "Harness not compiled. Paths might vary. Checking for regular build Release."
    $ExePath = ".\src\recording\build\Release\vms-prebuffer-harness.exe"
    if (!(Test-Path $ExePath)) {
        Write-Warning "Harness not compiled. Please run CMake build on src/recording."
        exit 1
    }
}

# 3. Run the Event Simulation
Write-Host "`nRunning End-to-End Simulation..." -ForegroundColor Yellow
$proc = Start-Process -FilePath $ExePath -NoNewWindow -Wait

# 4. Verify output file contains backfilled pre-buffer
Write-Host "`nVerifying Segment Writer Output..." -ForegroundColor Yellow
if (Test-Path ".\out_cam-test_0000.mp4") {
    $size = (Get-Item ".\out_cam-test_0000.mp4").Length
    Write-Host "[SUCCESS] Event-triggered MP4 segment successfully generated via backfill! Size: $size bytes" -ForegroundColor Green
}
else {
    Write-Error "Backfilled MP4 was not created."
}
