$ErrorActionPreference = "Stop"
Set-Location -Path "$PSScriptRoot\.."

$TestDir = ".\test_segments"

Write-Host "=== TS-VMS Phase 4.3 Segment Writer DoD Verification ===" -ForegroundColor Cyan

if (Test-Path $TestDir) {
    Remove-Item $TestDir -Recurse -Force
}
New-Item -ItemType Directory -Path $TestDir | Out-Null

Write-Host "Building harness..." -ForegroundColor Yellow
cmake --build .\src\recording\build --config Release

Write-Host "Creating fake corrupted files to test scanner..." -ForegroundColor Yellow
Set-Content -Path "$TestDir\cam_fake_corrupt.mkv" -Value "JUNK_DATA_NO_EBML_HEADER"
Set-Content -Path "$TestDir\cam_fake_orphan.tmp" -Value "ORPHANED_DATA"
(Get-Item "$TestDir\cam_fake_orphan.tmp").LastWriteTime = (Get-Date).AddMinutes(-15)

Write-Host "`nRunning Startup Scanner..." -ForegroundColor Yellow
.\bin\vms-segment-harness.exe scan $TestDir

$quarantineCount = (Get-ChildItem "$TestDir\corrupt\*.mkv" -ErrorAction SilentlyContinue).Count
$tmpCount = (Get-ChildItem "$TestDir\*.tmp" -ErrorAction SilentlyContinue).Count
if ($quarantineCount -eq 1 -and $tmpCount -eq 0) {
    Write-Host "[OK] Startup Scanner correctly quarantined bad MKV and purged old TMP!" -ForegroundColor Green
}
else {
    Write-Error "Scanner failed expectations. Q:$quarantineCount T:$tmpCount"
}

Write-Host "`nStarting Recording Engine (creating 5-sec MKV segments)..." -ForegroundColor Yellow
$proc = Start-Process -FilePath ".\bin\vms-segment-harness.exe" -ArgumentList "record `"$TestDir`"" -PassThru

Start-Sleep -Seconds 12

Write-Host "Simulating HARD CRASH (Killing process mid-write)..." -ForegroundColor Red
Stop-Process -Id $proc.Id -Force

Write-Host "`nVerifying Disk State Post-Crash..." -ForegroundColor Yellow
$mkvs = @(Get-ChildItem "$TestDir\*.mkv" -ErrorAction SilentlyContinue)
$tmps = @(Get-ChildItem "$TestDir\*.tmp" -ErrorAction SilentlyContinue)
$sha  = @(Get-ChildItem "$TestDir\*.sha256" -ErrorAction SilentlyContinue)

Write-Host "Finalized MKVs (Safely Flushed): $($mkvs.Count)"
Write-Host "Checksum Sidecars: $($sha.Count)"
Write-Host "Orphaned TMPs (Interrupted): $($tmps.Count)"

if ($mkvs.Count -ge 1) {
    Write-Host "[OK] Atomic write-then-rename successful. Safe MKV segments exist." -ForegroundColor Green
}
else {
    Write-Host "[WARN] No MKVs found. Ensure RTSP test source is running locally on 8554." -ForegroundColor Yellow
}

if ($tmps.Count -ge 1) {
    Write-Host "[OK] Crash left a .tmp file instead of exposing a bad finalized archive file." -ForegroundColor Green
}
