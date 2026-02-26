$ErrorActionPreference = "Stop"

$ServiceName = "ts-vms-recording"

Write-Host "Configuring Windows Service Recovery for $ServiceName..." -ForegroundColor Cyan

# Check if service exists
$service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if (-not $service) {
    Write-Warning "Service '$ServiceName' is not currently installed. Run the main installer first."
    exit 0
}

# Use sc.exe (Service Control) to set recovery actions
# Actions format: restart (delay in ms)
# 5000ms = 5 seconds
$scPath = "sc.exe"

Write-Host "Setting actions: First=Restart, Second=Restart, Subsequent=Restart..."
& $scPath failure $ServiceName reset= 86400 actions= restart/5000/restart/5000/restart/5000 2>&1 | Out-String | Write-Host

# Enable "Enable actions for stops with errors." (Required on newer Windows versions for crash handling)
& $scPath failureflag $ServiceName 1 2>&1 | Out-String | Write-Host

Write-Host "`n[SUCCESS] Crash recovery configured for $ServiceName!" -ForegroundColor Green
