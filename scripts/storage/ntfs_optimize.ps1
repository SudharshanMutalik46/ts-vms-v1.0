param (
    [switch]$Apply
)

Write-Host "TS-VMS Phase 4.1 Storage NTFS Optimizer" -ForegroundColor Cyan
Write-Host "This script optimizes active storage volumes for high-throughput 4MB contiguous block writing.`n"

# 1. Disable Last Access Time Update
Write-Host "Testing Last Access Time setting..."
$behavior = fsutil behavior query disablelastaccess

if ($Apply) {
    Write-Host "Command: fsutil behavior set disablelastaccess 1" -ForegroundColor Yellow
    fsutil behavior set disablelastaccess 1
    Write-Host "[x] Successfully disabled NTFS Last Access Time." -ForegroundColor Green
}
else {
    Write-Host "[Audit Mode] Run script with -Apply to set disablelastaccess to 1."
    if ($behavior -match "0") {
        Write-Host "RECOMMENDATION: Disable last access time for better I/O performance on video blocks." -ForegroundColor Yellow
    }
    else {
        Write-Host "STATUS: Last access time is already disabled." -ForegroundColor Green
    }
}

Write-Host "`nWARNING: 64KB Allocation Unit Size Requirements" -ForegroundColor DarkGray
Write-Host "For optimal 4MB block video recording, dedicated video storage volumes must be formatted with a 64KB Cluster Size." -ForegroundColor DarkGray
Write-Host "DO NOT automate disk formatting. Use Windows Disk Management manually for target evidence drives to prevent data loss." -ForegroundColor DarkGray
