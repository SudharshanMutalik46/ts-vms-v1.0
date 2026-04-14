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

function Trace-Step {
    param(
        [string]$LogsRoot,
        [string]$Message
    )
    Ensure-Directory $LogsRoot
    $line = "[{0}] {1}" -f (Get-Date -Format "yyyy-MM-dd HH:mm:ss"), $Message
    Add-Content -LiteralPath (Join-Path $LogsRoot "bootstrap_trace.log") -Value $line -Force
}

function Resolve-ExistingPath {
    param([string[]]$Candidates)
    foreach ($candidate in $Candidates) {
        if ($candidate -and (Test-Path -LiteralPath $candidate)) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }
    return $null
}

function Convert-ToYamlPath {
    param([string]$Path)
    return ($Path -replace '\\', '\\')
}

function Write-DemoConfigs {
    param(
        [string]$ConfigRoot,
        [string]$DataRoot,
        [string]$ToolsRoot,
        [string]$FfmpegExe
    )

    Ensure-Directory $ConfigRoot

    $licenseRoot = Join-Path $DataRoot "license"
    $auditRoot = Join-Path $DataRoot "audit_spool"
    $recordingsRoot = Join-Path $DataRoot "recordings"
    $exportsRoot = Join-Path $DataRoot "exports"
    $cacheRoot = Join-Path $DataRoot "cache"

    Ensure-Directory $licenseRoot
    Ensure-Directory $auditRoot
    Ensure-Directory $recordingsRoot
    Ensure-Directory $exportsRoot
    Ensure-Directory $cacheRoot
    Ensure-Directory (Join-Path $recordingsRoot "hot")

    $gstLaunchPath = Join-Path $ToolsRoot "gstreamer\bin\gst-launch-1.0.exe"

    $defaultYaml = @"
rate_limit:
  global_ip:
    rate: 100
    window: 1s
  user:
    rate: 1000
    window: 1h
  login:
    rate: 5
    window: 15m
  endpoints:
    "/api/v1/auth/refresh":
      rate: 10
      window: 1m
    "/api/v1/auth/logout":
      rate: 20
      window: 1m

license:
  path: "$(Convert-ToYamlPath (Join-Path $licenseRoot "license.lic"))"
  public_key_path: "$(Convert-ToYamlPath (Join-Path $licenseRoot "license_pub.pem"))"
  check_interval: "1h"

audit:
  spool_dir: "$(Convert-ToYamlPath $auditRoot)"
  retention_years: 7
  max_spool_size_mb: 1024

events:
  nvr:
    enabled: false
    poll_interval_ms: 5000
    max_inflight_nvrs: 50
    max_events_per_poll: 200
    time_budget_ms: 3000
    backoff_ms: 5000
    publish_retry_max: 3
    dedup_ttl_seconds: 300
    dedup_max_keys: 50000
    nats_subject: "events.nvr"
    snapshot_mode: "vendor_ref"
"@
    Set-Content -LiteralPath (Join-Path $ConfigRoot "default.yaml") -Value $defaultYaml -Force

    $recordingYaml = @"
global:
  segment_duration_sec: 60
  health_port: 8082
  api_base_url: "http://localhost:8082"
  sfu_base_url: "http://localhost:8085"
  dev_rbac_bypass: true
  storage_root: "$(Convert-ToYamlPath $recordingsRoot)"
  export_root: "$(Convert-ToYamlPath $exportsRoot)"
  ffmpeg_path: "$(Convert-ToYamlPath $FfmpegExe)"
  gst_launch_path: "$(Convert-ToYamlPath $gstLaunchPath)"
  default_tenant_id: "tenant_sys"
  default_site_id: "site_hq"

cameras:

schedules: []

health_monitoring:
  enabled: true
  sample_interval_sec: 5
  frame_drop:
    enabled: true
    warn_drop_rate_pct: 1.0
    crit_drop_rate_pct: 5.0
    window_sec: 60
    method: "discont|pts_gap"
  disk_write_rate:
    enabled: true
    window_sec: 30
    warn_min_mbps: 2.0
    crit_min_mbps: 0.5
  alerts:
    cooldown_sec: 30
    sustained_windows_for_critical: 3

failover_recovery:
  enabled: true
  restart_backoff_sec: 5
  db_required_for_ready: false
  orphan_reconcile_mode: "log_only"

circuit_breaker:
  enabled: true
  warn_free_gb: 20
  crit_free_gb: 10
  warn_usage_percent: 80
  crit_usage_percent: 95
  check_interval_sec: 5
  cooldown_sec: 30

performance:
  pipeline:
    queue_max_time_ns: 2000000000
    fragment_duration_ms: 1000
    faststart: false
    rtspsrc_latency_ms: 200
  io:
    segment_writer_batch_bytes: 4194304
    preallocate_files: true

recording:
  prebuffer_seconds: 15
  segment_duration_seconds: 60
  max_restarts_per_minute: 5
  force_main_stream: true
  storage_tiers:
    - name: "hot"
      path: "$(Convert-ToYamlPath (Join-Path $recordingsRoot "hot"))"
      max_size_gb: 1000
      pressure_threshold_pct: 85
  retention:
    default_days: 30
    protect_events: true
  health:
    bind_addr: "localhost:8091"
"@
    Set-Content -LiteralPath (Join-Path $ConfigRoot "recording.yaml") -Value $recordingYaml -Force

    $prebufferYaml = @"
prebuffer:
  global:
    prebuffer_seconds_default: 30
    prebuffer_max_total_mb: 2048
    prebuffer_persist_enabled: false
    prebuffer_persist_path: "$(Convert-ToYamlPath (Join-Path $cacheRoot "prebuffer.dat"))"
  cameras:
    - camera_id: "cam-01"
      prebuffer_enabled: true
      prebuffer_seconds: 60
      prebuffer_max_mb: 200
"@
    Set-Content -LiteralPath (Join-Path $ConfigRoot "recording_prebuffer.yaml") -Value $prebufferYaml -Force
}

function Start-LoggedProcess {
    param(
        [string]$FilePath,
        [string[]]$Arguments,
        [string]$WorkingDirectory,
        [string]$StdOutPath,
        [string]$StdErrPath,
        [switch]$Wait
    )

    $params = @{
        FilePath = $FilePath
        WorkingDirectory = $WorkingDirectory
        WindowStyle = "Hidden"
    }

    if ($Arguments) {
        $params.ArgumentList = $Arguments
    }
    if ($StdOutPath) {
        $params.RedirectStandardOutput = $StdOutPath
    }
    if ($StdErrPath) {
        if ($StdOutPath -and ($StdErrPath -eq $StdOutPath)) {
            $StdErrPath = "$StdErrPath.stderr"
        }
        $params.RedirectStandardError = $StdErrPath
    }
    if ($Wait) {
        $params.Wait = $true
        $params.PassThru = $true
    }

    return Start-Process @params
}

function Wait-ForTcpPort {
    param(
        [string]$Address,
        [int]$Port,
        [int]$TimeoutSeconds = 30
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $client = New-Object System.Net.Sockets.TcpClient
            $async = $client.BeginConnect($Address, $Port, $null, $null)
            if ($async.AsyncWaitHandle.WaitOne(1000, $false)) {
                $client.EndConnect($async)
                $client.Close()
                return $true
            }
            $client.Close()
        }
        catch {
        }
        Start-Sleep -Milliseconds 500
    }
    return $false
}

function Ensure-PostgresCluster {
    param(
        [string]$InitDbExe,
        [string]$DataDir,
        [string]$PasswordFile,
        [string]$LogFile
    )

    if (Test-Path -LiteralPath (Join-Path $DataDir "PG_VERSION")) {
        return
    }

    Ensure-Directory $DataDir
    Set-Content -LiteralPath $PasswordFile -Value "ts1234"
    $args = @(
        "-D", $DataDir,
        "-U", "postgres",
        "-A", "password",
        "--pwfile", $PasswordFile,
        "-E", "UTF8"
    )
    $proc = Start-LoggedProcess -FilePath $InitDbExe -Arguments $args -WorkingDirectory (Split-Path -Parent $InitDbExe) -StdOutPath $LogFile -StdErrPath $LogFile -Wait
    if ($proc.ExitCode -ne 0) {
        throw "initdb failed with exit code $($proc.ExitCode). See $LogFile"
    }
}

function Ensure-Database {
    param(
        [string]$PsqlExe,
        [string]$CreatedbExe,
        [int]$Port
    )

    & $CreatedbExe -h 127.0.0.1 -p $Port -U postgres ts_vms 2>$null
    if ($LASTEXITCODE -ne 0) {
        $checkArgs = @(
            "-w",
            "-h", "127.0.0.1",
            "-p", "$Port",
            "-U", "postgres",
            "-d", "postgres",
            "-tAc",
            "SELECT 1 FROM pg_database WHERE datname='ts_vms';"
        )
        $result = & $PsqlExe @checkArgs 2>$null
        if (($result | Out-String).Trim() -ne "1") {
            throw "Failed to ensure ts_vms database on port $Port"
        }
    }
}

function Stop-DemoProcessesByRoot {
    param(
        [string]$DemoRoot,
        [string[]]$Names
    )

    foreach ($name in $Names) {
        Get-Process -Name $name -ErrorAction SilentlyContinue | ForEach-Object {
            $procPath = $null
            try { $procPath = $_.Path } catch {}
            if ($procPath -and $procPath.StartsWith($DemoRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
                Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
            }
        }
    }
}

function Stop-DemoServices {
    param(
        [string]$DemoRoot,
        [string]$PgCtlExe,
        [string]$DbRoot,
        [string]$LogsRoot
    )

    if ((Test-Path -LiteralPath $PgCtlExe) -and (Test-Path -LiteralPath (Join-Path $DbRoot "PG_VERSION"))) {
        & $PgCtlExe -D $DbRoot stop -m fast | Out-Null
    }

    $names = @(
        "redis-server",
        "nats-server",
        "server",
        "vms-control",
        "vms-media",
        "vms-mosaic",
        "vms-hlsd",
        "hlsd",
        "vms-recording-bin",
        "vms-recording",
        "node",
        "TSVmsDesktop"
    )
    Stop-DemoProcessesByRoot -DemoRoot $DemoRoot -Names $names
    Trace-Step -LogsRoot $LogsRoot -Message "Demo services stopped"
}

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$demoRoot = Split-Path -Parent $scriptRoot
$appRoot = Join-Path $demoRoot "app"
$toolsRoot = Join-Path $demoRoot "tools"
$dataRoot = Join-Path $demoRoot "data"
$logsRoot = Join-Path $demoRoot "logs"
$dbRoot = Join-Path $dataRoot "postgres"
$tmpRoot = Join-Path $dataRoot "tmp"
$configRoot = Join-Path $appRoot "config"
$binRoot = Join-Path $appRoot "bin"
$desktopRoot = Join-Path $appRoot "desktop"
$sfuRoot = Join-Path $appRoot "sfu"

Ensure-Directory $dataRoot
Ensure-Directory $logsRoot
Ensure-Directory $tmpRoot

$postgresBin = Join-Path $toolsRoot "postgres\bin"
$postgresShare = Join-Path $toolsRoot "postgres\share"
$gstreamerBin = Join-Path $toolsRoot "gstreamer\bin"
$gstreamerLib = Join-Path $toolsRoot "gstreamer\lib\gstreamer-1.0"
$gstreamerLibexec = Join-Path $toolsRoot "gstreamer\libexec\gstreamer-1.0"
$ffmpegExe = Resolve-ExistingPath @(
    (Join-Path $toolsRoot "ffmpeg\ffmpeg.exe"),
    (Join-Path $toolsRoot "ffmpeg\bin\ffmpeg.exe")
)
$redisExe = Resolve-ExistingPath @(
    (Join-Path $toolsRoot "redis\redis-server.exe"),
    (Join-Path $binRoot "redis-server.exe")
)
$natsExe = Resolve-ExistingPath @(
    (Join-Path $toolsRoot "nats\nats-server.exe"),
    (Join-Path $binRoot "nats-server.exe")
)
$nodeExe = Resolve-ExistingPath @(
    (Join-Path $toolsRoot "node\node.exe"),
    (Join-Path $binRoot "node.exe")
)
$pgCtlExe = Join-Path $postgresBin "pg_ctl.exe"
$postgresExe = Join-Path $postgresBin "postgres.exe"
$initDbExe = Join-Path $postgresBin "initdb.exe"
$psqlExe = Join-Path $postgresBin "psql.exe"
$createdbExe = Join-Path $postgresBin "createdb.exe"
$migratorExe = Join-Path $binRoot "migrator.exe"
$demoDbPort = 55432

$env:PGPASSWORD = "ts1234"
$env:PGUSER = "postgres"
$env:PGHOST = "127.0.0.1"
$env:PGPORT = "$demoDbPort"
$env:PGDATABASE = "ts_vms"
$env:PGSHAREDIR = $postgresShare
$env:VMS_INSTALL_ROOT = $appRoot
$env:VMS_DATA_ROOT = $dataRoot
$env:TS_VMS_DATA_ROOT = $dataRoot
$env:DB_HOST = "127.0.0.1"
$env:DB_PORT = "$demoDbPort"
$env:DB_USER = "postgres"
$env:DB_PASSWORD = "ts1234"
$env:DB_NAME = "ts_vms"
$env:DB_SSLMODE = "disable"
$env:REDIS_ADDR = "127.0.0.1:6379"
$env:NATS_URL = "nats://127.0.0.1:4222"
$env:SFU_BASE_URL = "http://127.0.0.1:8085"
$env:MEDIA_PLANE_ADDR = "127.0.0.1:50051"
$env:METRICS_PER_CAMERA = "true"
$env:MASTER_KEYS = '[{"kid":"demo-1","material":"MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE="}]'
$env:ACTIVE_MASTER_KID = "demo-1"
$env:AI_SERVICE_TOKEN = "demo_ai_secret"
$env:SFU_SECRET = "demo_sfu_secret"
$env:TS_VMS_SERVICE_KEY = "demo_service_key"
$env:TS_VMS_RECORDING_INTERNAL_URL = "http://127.0.0.1:8087"
$env:TS_VMS_DSN = "postgres://postgres:ts1234@127.0.0.1:$demoDbPort/ts_vms?sslmode=disable"
$env:TS_VMS_GSTREAMER_ROOT = (Join-Path $toolsRoot "gstreamer")
$env:TS_VMS_GST_DISCOVERER = (Join-Path $gstreamerBin "gst-discoverer-1.0.exe")
$env:TS_VMS_GST_LAUNCH_PATH = (Join-Path $gstreamerBin "gst-launch-1.0.exe")
$env:GSTREAMER_1_0_ROOT_X86_64 = (Join-Path $toolsRoot "gstreamer")
$env:GST_PLUGIN_SYSTEM_PATH_1_0 = $gstreamerLib
$env:GST_PLUGIN_PATH_1_0 = $gstreamerLib
$env:GST_PLUGIN_SCANNER_1_0 = (Join-Path $gstreamerLibexec "gst-plugin-scanner.exe")
$env:GST_PLUGIN_SCANNER = (Join-Path $gstreamerLibexec "gst-plugin-scanner.exe")
if ($ffmpegExe) {
    $env:TS_VMS_FFMPEG_PATH = $ffmpegExe
}

$pathParts = @($postgresBin, $gstreamerBin, $desktopRoot, $binRoot, $env:PATH)
$env:PATH = ($pathParts | Where-Object { $_ -and $_.Trim() -ne "" }) -join ";"

Write-DemoConfigs -ConfigRoot $configRoot -DataRoot $dataRoot -ToolsRoot $toolsRoot -FfmpegExe $ffmpegExe

Write-Host "Stopping existing demo processes..." -ForegroundColor Yellow
$startupNames = @(
    "redis-server",
    "nats-server",
    "server",
    "vms-control",
    "vms-media",
    "vms-mosaic",
    "vms-hlsd",
    "hlsd",
    "vms-recording-bin",
    "vms-recording",
    "node",
    "TSVmsDesktop"
)
Stop-DemoProcessesByRoot -DemoRoot $demoRoot -Names $startupNames
Trace-Step -LogsRoot $logsRoot -Message "Stopped existing demo processes"

if ((Test-Path -LiteralPath $pgCtlExe) -and (Test-Path -LiteralPath (Join-Path $dbRoot "PG_VERSION"))) {
    Trace-Step -LogsRoot $logsRoot -Message "Stopping existing demo postgres cluster"
    & $pgCtlExe -D $dbRoot stop -m fast | Out-Null
    Trace-Step -LogsRoot $logsRoot -Message "Stopped existing demo postgres cluster"
}

if ((Test-Path -LiteralPath $initDbExe) -and (Test-Path -LiteralPath $pgCtlExe) -and (Test-Path -LiteralPath $psqlExe)) {
    Write-Host "Preparing PostgreSQL cluster..." -ForegroundColor Cyan
    Trace-Step -LogsRoot $logsRoot -Message "Ensure-PostgresCluster start"
    Ensure-PostgresCluster -InitDbExe $initDbExe -DataDir $dbRoot -PasswordFile (Join-Path $tmpRoot "pgpass.txt") -LogFile (Join-Path $logsRoot "initdb.log")
    Trace-Step -LogsRoot $logsRoot -Message "Ensure-PostgresCluster complete"

    $postgresLog = Join-Path $logsRoot "postgres.log"
    Trace-Step -LogsRoot $logsRoot -Message "postgres.exe start begin"
    Start-LoggedProcess -FilePath $postgresExe -Arguments @("-D", $dbRoot, "-p", "$demoDbPort") -WorkingDirectory $postgresBin -StdOutPath $postgresLog -StdErrPath (Join-Path $logsRoot "postgres_err.log") | Out-Null
    Trace-Step -LogsRoot $logsRoot -Message "postgres.exe start complete"
    Trace-Step -LogsRoot $logsRoot -Message "Wait-ForTcpPort begin"
    if (-not (Wait-ForTcpPort -Address "127.0.0.1" -Port $demoDbPort -TimeoutSeconds 30)) {
        throw "PostgreSQL did not start on port $demoDbPort."
    }
    Trace-Step -LogsRoot $logsRoot -Message "Wait-ForTcpPort complete"

    Write-Host "Ensuring database exists..." -ForegroundColor Cyan
    Trace-Step -LogsRoot $logsRoot -Message "Ensure-Database begin"
    Ensure-Database -PsqlExe $psqlExe -CreatedbExe $createdbExe -Port $demoDbPort
    Trace-Step -LogsRoot $logsRoot -Message "Ensure-Database complete"

    if (Test-Path -LiteralPath $migratorExe) {
        Write-Host "Running migrations..." -ForegroundColor Cyan
        Trace-Step -LogsRoot $logsRoot -Message "Migrator begin"
        $proc = Start-LoggedProcess -FilePath $migratorExe -Arguments @("-up") -WorkingDirectory $appRoot -StdOutPath (Join-Path $logsRoot "migrator.log") -StdErrPath (Join-Path $logsRoot "migrator_err.log") -Wait
        if ($proc.ExitCode -ne 0) {
            Write-Warning "Migration exited with code $($proc.ExitCode). Check logs."
        }
        Trace-Step -LogsRoot $logsRoot -Message "Migrator complete"
    }
}
else {
    Write-Warning "PostgreSQL tools are not fully bundled. Database startup skipped."
    Trace-Step -LogsRoot $logsRoot -Message "PostgreSQL tools not bundled"
}

if ($redisExe) {
    Write-Host "Starting Redis..." -ForegroundColor Cyan
    Start-LoggedProcess -FilePath $redisExe -WorkingDirectory (Split-Path -Parent $redisExe) -StdOutPath (Join-Path $logsRoot "redis.log") -StdErrPath (Join-Path $logsRoot "redis_err.log") | Out-Null
    Trace-Step -LogsRoot $logsRoot -Message "Redis started"
}

if ($natsExe) {
    Write-Host "Starting NATS..." -ForegroundColor Cyan
    Start-LoggedProcess -FilePath $natsExe -WorkingDirectory (Split-Path -Parent $natsExe) -StdOutPath (Join-Path $logsRoot "nats.log") -StdErrPath (Join-Path $logsRoot "nats_err.log") | Out-Null
    Trace-Step -LogsRoot $logsRoot -Message "NATS started"
}

$serverExe = Resolve-ExistingPath @(
    (Join-Path $binRoot "server.exe"),
    (Join-Path $binRoot "vms-control.exe")
)
if ($serverExe) {
    Write-Host "Starting Control Plane..." -ForegroundColor Cyan
    Start-LoggedProcess -FilePath $serverExe -WorkingDirectory $appRoot -StdOutPath (Join-Path $logsRoot "server.log") -StdErrPath (Join-Path $logsRoot "server_err.log") | Out-Null
    Trace-Step -LogsRoot $logsRoot -Message "Control plane started"
}

$mediaExe = Resolve-ExistingPath @(
    (Join-Path $binRoot "vms-media.exe")
)
if ($mediaExe) {
    $defaultConfig = Join-Path $configRoot "default.yaml"
    if (Test-Path -LiteralPath $defaultConfig) {
        Write-Host "Starting Media Plane..." -ForegroundColor Cyan
        Start-LoggedProcess -FilePath $mediaExe -Arguments @("--config", $defaultConfig) -WorkingDirectory (Split-Path -Parent $mediaExe) -StdOutPath (Join-Path $logsRoot "media.log") -StdErrPath (Join-Path $logsRoot "media_err.log") | Out-Null
        Trace-Step -LogsRoot $logsRoot -Message "Media plane start attempted"
    }
}

$mosaicExe = Resolve-ExistingPath @(
    (Join-Path $binRoot "vms-mosaic.exe")
)
if ($mosaicExe) {
    $mosaicConfig = Join-Path $configRoot "mosaic_8x8.yaml"
    if (Test-Path -LiteralPath $mosaicConfig) {
        Write-Host "Starting Mosaic..." -ForegroundColor Cyan
        Start-LoggedProcess -FilePath $mosaicExe -Arguments @($mosaicConfig) -WorkingDirectory (Split-Path -Parent $mosaicExe) -StdOutPath (Join-Path $logsRoot "mosaic.log") -StdErrPath (Join-Path $logsRoot "mosaic_err.log") | Out-Null
        Trace-Step -LogsRoot $logsRoot -Message "Mosaic started"
    }
}

$hlsdExe = Resolve-ExistingPath @(
    (Join-Path $binRoot "vms-hlsd.exe"),
    (Join-Path $binRoot "hlsd.exe")
)
if ($hlsdExe) {
    Write-Host "Starting HLSD..." -ForegroundColor Cyan
    Start-LoggedProcess -FilePath $hlsdExe -WorkingDirectory $appRoot -StdOutPath (Join-Path $logsRoot "hlsd.log") -StdErrPath (Join-Path $logsRoot "hlsd_err.log") | Out-Null
    Trace-Step -LogsRoot $logsRoot -Message "HLSD started"
}

$recordingExe = Resolve-ExistingPath @(
    (Join-Path $binRoot "vms-recording-bin.exe"),
    (Join-Path $binRoot "vms-recording.exe")
)
if ($recordingExe) {
    Write-Host "Starting Recording..." -ForegroundColor Cyan
    Start-LoggedProcess -FilePath $recordingExe -WorkingDirectory $appRoot -StdOutPath (Join-Path $logsRoot "recording.log") -StdErrPath (Join-Path $logsRoot "recording_err.log") | Out-Null
    Trace-Step -LogsRoot $logsRoot -Message "Recording started"
}

$sfuMain = Join-Path $sfuRoot "dist\main.js"
if ($nodeExe -and (Test-Path -LiteralPath $sfuMain)) {
    Write-Host "Starting SFU..." -ForegroundColor Cyan
    Start-LoggedProcess -FilePath $nodeExe -Arguments @($sfuMain) -WorkingDirectory $sfuRoot -StdOutPath (Join-Path $logsRoot "sfu.log") -StdErrPath (Join-Path $logsRoot "sfu_err.log") | Out-Null
    Trace-Step -LogsRoot $logsRoot -Message "SFU started"
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

Write-Host "TS-VMS demo startup complete." -ForegroundColor Green
Trace-Step -LogsRoot $logsRoot -Message "Startup complete"
