param(
    [switch]$NoDesktop
)

$ErrorActionPreference = "Continue"

function Ensure-Directory {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) {
        New-Item -ItemType Directory -Path $Path -Force | Out-Null
    }
}

function Stop-ExistingProcesses {
    $names = @(
        "TSVmsDesktop",
        "server",
        "vms-control",
        "vms-hlsd",
        "vms-recording-bin",
        "nats-server",
        "redis-server",
        "node"
    )

    foreach ($name in $names) {
        Stop-Process -Name $name -Force -ErrorAction SilentlyContinue
    }
}

function Resolve-CommandPath {
    param(
        [string[]]$Candidates,
        [string]$CommandName
    )

    foreach ($candidate in $Candidates) {
        if ($candidate -and (Test-Path -LiteralPath $candidate)) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }

    if ($CommandName) {
        $cmd = Get-Command $CommandName -ErrorAction SilentlyContinue
        if ($cmd) {
            return $cmd.Source
        }
    }

    return $null
}

function Start-BackgroundProcess {
    param(
        [string]$FilePath,
        [string[]]$Arguments,
        [string]$WorkingDirectory,
        [string]$StdOutPath,
        [string]$StdErrPath
    )

    if (-not $FilePath) {
        return
    }

    $startParams = @{
        FilePath = $FilePath
        WorkingDirectory = $WorkingDirectory
        WindowStyle = "Hidden"
    }

    if ($Arguments -and $Arguments.Count -gt 0) {
        $startParams.ArgumentList = $Arguments
    }
    if ($StdOutPath) {
        $startParams.RedirectStandardOutput = $StdOutPath
    }
    if ($StdErrPath) {
        $startParams.RedirectStandardError = $StdErrPath
    }

    Start-Process @startParams | Out-Null
}

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$packageRoot = Split-Path -Parent $scriptRoot
$appRoot = Join-Path $packageRoot "app"
$desktopRoot = Join-Path $appRoot "desktop"
$binRoot = Join-Path $appRoot "bin"
$configRoot = Join-Path $appRoot "config"
$sfuRoot = Join-Path $appRoot "sfu"
$logsRoot = Join-Path $packageRoot "logs"
$dataRoot = Join-Path $packageRoot "data"

Ensure-Directory $logsRoot
Ensure-Directory $dataRoot

$env:VMS_INSTALL_ROOT = $appRoot
$env:VMS_DATA_ROOT = $dataRoot
$env:DB_HOST = "localhost"
$env:DB_PORT = "5432"
$env:DB_USER = "postgres"
$env:DB_PASSWORD = "ts1234"
$env:DB_NAME = "ts_vms"
$env:REDIS_ADDR = "127.0.0.1:6379"
$env:NATS_URL = "nats://localhost:4222"
$env:SFU_BASE_URL = "http://127.0.0.1:8085"
$env:MEDIA_PLANE_ADDR = "localhost:50051"
$env:MASTER_KEYS = '[{"kid":"dev-1","material":"MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE="}]'
$env:ACTIVE_MASTER_KID = "dev-1"
$env:AI_SERVICE_TOKEN = "dev_ai_secret"
$env:SFU_SECRET = "sfu-internal-secret"
$env:TS_VMS_SERVICE_KEY = "your_shared_service_key"
$env:TS_VMS_RECORDING_INTERNAL_URL = "http://127.0.0.1:8087"
$env:TS_VMS_DSN = "postgres://postgres:ts1234@localhost:5432/ts_vms?sslmode=disable"

Write-Host "Stopping existing TS-VMS processes..." -ForegroundColor Yellow
Stop-ExistingProcesses

$redisExe = Resolve-CommandPath -Candidates @(
    (Join-Path $binRoot "redis-server.exe")
) -CommandName "redis-server.exe"

if ($redisExe) {
    Write-Host "Starting Redis..." -ForegroundColor Cyan
    Start-BackgroundProcess -FilePath $redisExe -WorkingDirectory (Split-Path -Parent $redisExe) -StdOutPath (Join-Path $logsRoot "redis.log") -StdErrPath (Join-Path $logsRoot "redis_err.log")
}
else {
    Write-Warning "Redis executable not found in package or PATH. Start Redis manually before using the app."
}

$natsExe = Resolve-CommandPath -Candidates @(
    (Join-Path $binRoot "nats-server.exe")
) -CommandName "nats-server.exe"

if ($natsExe) {
    Write-Host "Starting NATS..." -ForegroundColor Cyan
    Start-BackgroundProcess -FilePath $natsExe -WorkingDirectory (Split-Path -Parent $natsExe) -StdOutPath (Join-Path $logsRoot "nats.log") -StdErrPath (Join-Path $logsRoot "nats_err.log")
}
else {
    Write-Warning "NATS executable not found in package or PATH. Start NATS manually before using the app."
}

$serverExe = Resolve-CommandPath -Candidates @(
    (Join-Path $binRoot "server.exe"),
    (Join-Path $binRoot "vms-control.exe")
) -CommandName $null

if ($serverExe) {
    Write-Host "Starting Control Plane..." -ForegroundColor Cyan
    Start-BackgroundProcess -FilePath $serverExe -WorkingDirectory $appRoot -StdOutPath (Join-Path $logsRoot "server.log") -StdErrPath (Join-Path $logsRoot "server_err.log")
}
else {
    Write-Warning "Control plane executable not found. Desktop login and APIs will fail."
}

$hlsdExe = Resolve-CommandPath -Candidates @(
    (Join-Path $binRoot "vms-hlsd.exe"),
    (Join-Path $binRoot "hlsd.exe")
) -CommandName $null

if ($hlsdExe) {
    Write-Host "Starting HLSD..." -ForegroundColor Cyan
    Start-BackgroundProcess -FilePath $hlsdExe -WorkingDirectory $appRoot -StdOutPath (Join-Path $logsRoot "hlsd.log") -StdErrPath (Join-Path $logsRoot "hlsd_err.log")
}

$recordingExe = Resolve-CommandPath -Candidates @(
    (Join-Path $binRoot "vms-recording-bin.exe"),
    (Join-Path $binRoot "vms-recording.exe")
) -CommandName $null

if ($recordingExe) {
    Write-Host "Starting Recording Engine..." -ForegroundColor Cyan
    Start-BackgroundProcess -FilePath $recordingExe -WorkingDirectory $appRoot -StdOutPath (Join-Path $logsRoot "recording.log") -StdErrPath (Join-Path $logsRoot "recording_err.log")
}

$nodeExe = Resolve-CommandPath -Candidates @(
    (Join-Path $binRoot "node.exe")
) -CommandName "node.exe"

$sfuMain = Join-Path $sfuRoot "dist\main.js"
if ($nodeExe -and (Test-Path -LiteralPath $sfuMain)) {
    Write-Host "Starting SFU..." -ForegroundColor Cyan
    Start-BackgroundProcess -FilePath $nodeExe -Arguments @($sfuMain) -WorkingDirectory $sfuRoot -StdOutPath (Join-Path $logsRoot "sfu.log") -StdErrPath (Join-Path $logsRoot "sfu_err.log")
}
elseif (Test-Path -LiteralPath $sfuMain) {
    Write-Warning "SFU files exist but node.exe was not found in package or PATH."
}

if (-not $NoDesktop) {
    $desktopExe = Join-Path $desktopRoot "TSVmsDesktop.exe"
    if (Test-Path -LiteralPath $desktopExe) {
        Write-Host "Starting Desktop Client..." -ForegroundColor Green
        Start-Process -FilePath $desktopExe -WorkingDirectory $desktopRoot | Out-Null
    }
    else {
        Write-Warning "Desktop executable not found at $desktopExe"
    }
}

Write-Host "TS-VMS portable startup complete." -ForegroundColor Green
