$ErrorActionPreference = "Continue"

Write-Host "Stopping All VMS Services..." -ForegroundColor Yellow

# List of processes to stop
$processes = @(
    "vms-control", 
    "server", 
    "vms-media", 
    "vms-hlsd", 
    "node", 
    "vms-ai", 
    "nats-server",
    "redis-server"
)

foreach ($proc in $processes) {
    Write-Host "Stopping $proc..." -NoNewline
    Stop-Process -Name $proc -Force -ErrorAction SilentlyContinue
    Write-Host " Done." -ForegroundColor Green
}

Write-Host "All VMS Services Stopped." -ForegroundColor Green
