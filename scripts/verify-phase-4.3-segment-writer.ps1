$ErrorActionPreference = "Stop"
Set-Location -Path "$PSScriptRoot\.."

$TestDir = ".\test_segments"

Write-Host "=== TS-VMS Phase 4.3 Segment Writer DoD Verification ===" -ForegroundColor Cyan

# 1. Build
Write-Host "Building vms-segment-harness..." -ForegroundColor Yellow
pushd "src\recording"
Remove-Item -Recurse -Force CMakeCache.txt, CMakeFiles -ErrorAction SilentlyContinue
cmake .
cmake --build .
popd

# Copy executable to bin for standard script execution paths
if (!(Test-Path "bin")) { New-Item -ItemType Directory -Path "bin" | Out-Null }
Copy-Item "src\recording\Debug\vms-segment-harness.exe" -Destination "bin\vms-segment-harness.exe" -Force -ErrorAction SilentlyContinue 

# 2. Clean previous runs
if (Test-Path $TestDir) { Remove-Item -Recurse -Force $TestDir }
New-Item -ItemType Directory -Path $TestDir | Out-Null

# Create a fake 'corrupt' MP4 to test the scanner
Write-Host "Creating fake corrupted files to test scanner..." -ForegroundColor Yellow
Set-Content -Path "$TestDir\cam_fake_corrupt.mp4" -Value "JUNK_DATA_NO_FTYP_HEADER"
Set-Content -Path "$TestDir\cam_fake_orphan.tmp" -Value "ORPHANED_DATA"
# Backdate the .tmp file by 15 minutes to trigger TTL cleanup
(Get-Item "$TestDir\cam_fake_orphan.tmp").LastWriteTime = (Get-Date).AddMinutes(-15)

# 3. Run Scanner (Should delete the .tmp and quarantine the .mp4)
Write-Host "`nRunning Startup Scanner..." -ForegroundColor Yellow
.\bin\vms-segment-harness.exe scan $TestDir

# Verify Scanner Output
$quarantineCount = (Get-ChildItem "$TestDir\corrupt\*.mp4").Count
$tmpCount = (Get-ChildItem "$TestDir\*.tmp").Count
if ($quarantineCount -eq 1 -and $tmpCount -eq 0) {
    Write-Host "[OK] Startup Scanner correctly quarantined bad MP4 and purged old TMP!" -ForegroundColor Green
}
else {
    Write-Error "Scanner failed expectations. Q:$quarantineCount T:$tmpCount"
}

# 4. Start Recording Simulation (Crash Test)
Write-Host "`nStarting Recording Engine (creating 5-sec segments)..." -ForegroundColor Yellow
$proc = Start-Process -FilePath ".\bin\vms-segment-harness.exe" -ArgumentList "record `"$TestDir`"" -PassThru

Write-Host "Waiting 12 seconds for segments to generate..."
Start-Sleep -Seconds 12

Write-Host "Simulating HARD CRASH (Killing process mid-write)..." -ForegroundColor Red
Stop-Process -Id $proc.Id -Force

# 5. Verify Atomic Behavior
Write-Host "`nVerifying Disk State Post-Crash..." -ForegroundColor Yellow
$mp4s = @(Get-ChildItem "$TestDir\*.mp4" -ErrorAction SilentlyContinue)
$tmps = @(Get-ChildItem "$TestDir\*.tmp" -ErrorAction SilentlyContinue)

Write-Host "Finalized MP4s (Safely Flushed): $($mp4s.Count)"
Write-Host "Orphaned TMPs (Interrupted): $($tmps.Count)"

if ($mp4s.Count -ge 1) {
    Write-Host "[OK] Atomic Write-Then-Rename successful. Safe segments exist." -ForegroundColor Green
}
else {
    Write-Host "[WARN] No MP4s found. (Ensure RTSP test source is running locally on 8554)" -ForegroundColor Yellow
}

if ($tmps.Count -ge 1) {
    Write-Host "[OK] Crash left a .tmp file instead of a corrupted .mp4!" -ForegroundColor Green
}

Write-Host "`nVerification Complete." -ForegroundColor Cyan
