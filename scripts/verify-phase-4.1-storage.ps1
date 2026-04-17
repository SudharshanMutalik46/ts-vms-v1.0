Write-Host "TS-VMS Phase 4.1 Storage Architecture Verification" -ForegroundColor Cyan

# 1. Load config
$schema = "$PSScriptRoot\..\config\storage.yaml"
if (!(Test-Path $schema)) {
    Write-Error "storage.yaml not found at $schema!"
    exit 1
}
Write-Host "[x] Config $schema found." -ForegroundColor Green

# 2. Simulate Planner logic
Write-Host "`n[x] Simulating Planner Paths:" -ForegroundColor Green
$camId = "cam-01"
$tenantId = "tenant-alpha"
$siteId = "site-hq"
$ts = Get-Date -Date "2026-02-26 14:00:00"
$dateStr = $ts.ToString("yyyy-MM-dd")
$hourStr = $ts.ToString("HH")
$volPath = "C:\ts_vms_storage\hot"

$path = Join-Path $volPath "$tenantId\$siteId\$camId\$dateStr\$hourStr"
Write-Host "    Expected Path: $path"

# 3. Simulate Spillover & Monitor
Write-Host "`n[x] Simulating Monitor Loop & Spillover Alerts:" -ForegroundColor Green
$volumes = @(
    @{ Id = "vol-hot-1"; Total = 1000; Used = 700; MaxUsage = 80; Warn = 75; Crit = 85 },
    @{ Id = "vol-hot-1"; Total = 1000; Used = 750; MaxUsage = 80; Warn = 75; Crit = 85 },
    @{ Id = "vol-hot-1"; Total = 1000; Used = 850; MaxUsage = 80; Warn = 75; Crit = 85 }
)

foreach ($v in $volumes) {
    $pct = ($v.Used / $v.Total) * 100
    Write-Host "EVENT storage.volume.status vol=$($v.Id) total=$($v.Total)GB used=$($v.Used)GB usage=$pct%"
    
    if ($pct -ge $v.Crit) {
        Write-Host "CRITICAL storage.volume.low_space_critical vol=$($v.Id) usage=$pct% limit=$($v.Crit)%" -ForegroundColor Red
        Write-Host "--> Spillover triggered! Target volume is breached. Next eligible volume will be selected." -ForegroundColor DarkYellow
    }
    elseif ($pct -ge $v.Warn) {
        Write-Host "WARNING storage.volume.low_space_warning vol=$($v.Id) usage=$pct% limit=$($v.Warn)%" -ForegroundColor Yellow
    }
    Start-Sleep -Seconds 1
}

# 4. Run Go unit tests
Write-Host "`n[x] Running Go Planner Unit Tests:" -ForegroundColor Green
pushd "$PSScriptRoot\..\internal\storage"
$testOutput = go test -v
Write-Host ($testOutput | Out-String)
if ($LASTEXITCODE -ne 0) {
    Write-Error "Unit tests failed!"
    exit 1
}
popd

Write-Host "All Phase 4.1 Storage Verification Checks complete." -ForegroundColor Green
