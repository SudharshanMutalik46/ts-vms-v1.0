param(
    [string]$Destination = (Join-Path $env:USERPROFILE "Desktop\TS-VMS-Demo")
)

$ErrorActionPreference = "Stop"

function Ensure-Directory {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) {
        New-Item -ItemType Directory -Path $Path -Force | Out-Null
    }
}

function Copy-TreeContents {
    param(
        [string]$Source,
        [string]$Destination
    )

    if (Test-Path -LiteralPath $Source) {
        Ensure-Directory $Destination
        Copy-Item -Path (Join-Path $Source "*") -Destination $Destination -Recurse -Force
    } else {
        Write-Warning "Source directory not found: $Source"
    }
}

$repoRoot = Split-Path -Parent $PSScriptRoot

Ensure-Directory $Destination
foreach ($refreshPath in @("app", "tools", "scripts")) {
    $target = Join-Path $Destination $refreshPath
    if (Test-Path -LiteralPath $target) {
        Remove-Item -LiteralPath $target -Recurse -Force -ErrorAction SilentlyContinue
    }
}
foreach ($rootFile in @("Install-TS-VMS-Demo.exe", "Uninstall-TS-VMS-Demo.exe", "README.txt")) {
    $target = Join-Path $Destination $rootFile
    if (Test-Path -LiteralPath $target) {
        Remove-Item -LiteralPath $target -Force -ErrorAction SilentlyContinue
    }
}

$appRoot = Join-Path $Destination "app"
$binRoot = Join-Path $appRoot "bin"
$desktopRoot = Join-Path $appRoot "desktop"
$configRoot = Join-Path $appRoot "config"
$dbMigrationsRoot = Join-Path $appRoot "db\migrations"
$sfuRoot = Join-Path $appRoot "sfu"
$toolsRoot = Join-Path $Destination "tools"
$scriptsRoot = Join-Path $Destination "scripts"
$dataRoot = Join-Path $Destination "data"

Ensure-Directory $binRoot
Ensure-Directory $desktopRoot
Ensure-Directory $configRoot
Ensure-Directory $dbMigrationsRoot
Ensure-Directory $sfuRoot
Ensure-Directory $toolsRoot
Ensure-Directory $scriptsRoot
Ensure-Directory $dataRoot

# 1. Desktop Client
$desktopBin = Join-Path $repoRoot "desktop\TSVmsDesktop\bin\Release\net8.0-windows"
if (Test-Path $desktopBin) {
    Copy-TreeContents -Source $desktopBin -Destination $desktopRoot
} else {
    Write-Warning "Desktop Release binaries not found. Build Desktop in Release mode first."
}

# 2. Config & Migrations
Copy-TreeContents -Source (Join-Path $repoRoot "config") -Destination $configRoot
Copy-TreeContents -Source (Join-Path $repoRoot "db\migrations") -Destination $dbMigrationsRoot

# 3. SFU
if (Test-Path (Join-Path $repoRoot "sfu\dist")) {
    Copy-TreeContents -Source (Join-Path $repoRoot "sfu\dist") -Destination (Join-Path $sfuRoot "dist")
    Copy-TreeContents -Source (Join-Path $repoRoot "sfu\node_modules") -Destination (Join-Path $sfuRoot "node_modules")
}

# 4. Binaries
$binMap = @{
    "server.exe" = (Join-Path $repoRoot "server.exe")
    "vms-control.exe" = (Join-Path $repoRoot "vms-control.exe")
    "vms-hlsd.exe" = (Join-Path $repoRoot "vms-hlsd.exe")
    "hlsd.exe" = (Join-Path $repoRoot "hlsd.exe")
    "vms-recording-bin.exe" = (Join-Path $repoRoot "vms-recording-bin.exe")
    "vms-recording.exe" = (Join-Path $repoRoot "vms-recording.exe")
    "vms-media.exe" = (Join-Path $repoRoot "media-plane\build\Release\vms-media.exe")
    "vms-mosaic.exe" = (Join-Path $repoRoot "media-plane\src\mosaic\Release\vms-mosaic.exe")
}

foreach ($name in $binMap.Keys) {
    $source = $binMap[$name]
    if (Test-Path -LiteralPath $source) {
        Copy-Item -LiteralPath $source -Destination (Join-Path $binRoot $name) -Force
    }
}

$mediaRuntimeDir = Join-Path $repoRoot "media-plane\build\Release"
if (Test-Path -LiteralPath $mediaRuntimeDir) {
    Get-ChildItem -LiteralPath $mediaRuntimeDir -Filter *.dll -File | ForEach-Object {
        Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $binRoot $_.Name) -Force
    }
}

# 5. Build Migrator
Push-Location $repoRoot
go build -o (Join-Path $binRoot "migrator.exe") .\cmd\migrator
Pop-Location

# 6. Scripts
Copy-Item -LiteralPath (Join-Path $repoRoot "scripts\demo\Start-TSVmsDemo.ps1") -Destination (Join-Path $scriptsRoot "Start-TSVmsDemo.ps1") -Force
Copy-Item -LiteralPath (Join-Path $repoRoot "scripts\demo\Start-TSVmsDemo.cmd") -Destination (Join-Path $scriptsRoot "Start-TSVmsDemo.cmd") -Force

# 7. PostgreSQL Tools
$PgPath = $env:POSTGRESQL_ROOT
if (!$PgPath) {
    $PossiblePgPaths = @(
        "$env:ProgramFiles\PostgreSQL\18"
        "$env:ProgramFiles\PostgreSQL\17"
        "$env:ProgramFiles\PostgreSQL\16"
        "$env:ProgramFiles\PostgreSQL\15"
        "$env:ProgramFiles\PostgreSQL\14"
    )
    foreach ($p in $PossiblePgPaths) {
        if (Test-Path $p) { $PgPath = $p; break }
    }
}
if ($PgPath) {
    Copy-TreeContents -Source (Join-Path $PgPath "bin") -Destination (Join-Path $toolsRoot "postgres\bin")
    Copy-TreeContents -Source (Join-Path $PgPath "lib") -Destination (Join-Path $toolsRoot "postgres\lib")
    Copy-TreeContents -Source (Join-Path $PgPath "share") -Destination (Join-Path $toolsRoot "postgres\share")
}

# 8. GStreamer Tools
$GstPath = $env:GSTREAMER_1_0_ROOT_MSVC_X86_64
if (!$GstPath -or !(Test-Path $GstPath)) {
    $GstPath = "$env:ProgramFiles\gstreamer\1.0\msvc_x86_64"
}
if (Test-Path $GstPath) {
    Copy-TreeContents -Source (Join-Path $GstPath "bin") -Destination (Join-Path $toolsRoot "gstreamer\bin")
    Copy-TreeContents -Source (Join-Path $GstPath "lib") -Destination (Join-Path $toolsRoot "gstreamer\lib")
    foreach($sub in @("libexec", "share", "etc")) {
        if (Test-Path (Join-Path $GstPath $sub)) {
            Copy-TreeContents -Source (Join-Path $GstPath $sub) -Destination (Join-Path $toolsRoot "gstreamer\$sub")
        }
    }
}

# 9. Redis
$RedisExe = Get-Command redis-server -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source
if (!$RedisExe) {
    $PossibleRedis = @(
        "$env:USERPROFILE\Downloads\Redis-x64-5.0.14.1\redis-server.exe"
        "$env:ProgramFiles\Redis\redis-server.exe"
    )
    foreach ($p in $PossibleRedis) { if (Test-Path $p) { $RedisExe = $p; break } }
}
if ($RedisExe) {
    Ensure-Directory (Join-Path $toolsRoot "redis")
    Copy-Item -LiteralPath $RedisExe -Destination (Join-Path $toolsRoot "redis\redis-server.exe") -Force
    $RedisDir = Split-Path $RedisExe -Parent
    if (Test-Path (Join-Path $RedisDir "redis.windows.conf")) {
        Copy-Item -LiteralPath (Join-Path $RedisDir "redis.windows.conf") -Destination (Join-Path $toolsRoot "redis\redis.windows.conf") -Force
    }
}

# 10. NATS
Ensure-Directory (Join-Path $toolsRoot "nats")
if (Test-Path (Join-Path $repoRoot "src\vms-ai\nats-server.exe")) {
    Copy-Item -LiteralPath (Join-Path $repoRoot "src\vms-ai\nats-server.exe") -Destination (Join-Path $toolsRoot "nats\nats-server.exe") -Force
}

# 11. Node.js
$NodeExe = Get-Command node -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source
if ($NodeExe) {
    Ensure-Directory (Join-Path $toolsRoot "node")
    Copy-Item -LiteralPath $NodeExe -Destination (Join-Path $toolsRoot "node\node.exe") -Force
}

# 12. FFmpeg
$FfmpegExe = Get-Command ffmpeg -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source
if ($FfmpegExe) {
    Ensure-Directory (Join-Path $toolsRoot "ffmpeg")
    Copy-Item -LiteralPath $FfmpegExe -Destination (Join-Path $toolsRoot "ffmpeg\ffmpeg.exe") -Force
}

# 13. Installer Compilation
$installerSource = Join-Path $repoRoot "packaging\TSVmsDemoInstaller\Program.cs"
$installerExe = Join-Path $Destination "Install-TS-VMS-Demo.exe"
$uninstallerSource = Join-Path $repoRoot "packaging\TSVmsDemoUninstaller\Program.cs"
$uninstallerExe = Join-Path $Destination "Uninstall-TS-VMS-Demo.exe"

$cscExe = Join-Path $env:SystemRoot "Microsoft.NET\Framework64\v4.0.30319\csc.exe"
if (!(Test-Path $cscExe)) {
    # Try alternate location or PATH
    $cscExe = Join-Path $env:SystemRoot "Microsoft.NET\Framework\v4.0.30319\csc.exe"
}
if (!(Test-Path $cscExe)) {
    # Try current MSBuild/Roslyn location
    $cscExe = Get-Command csc -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source
}

if ($cscExe -and (Test-Path $installerSource)) {
    & $cscExe /nologo /target:exe /out:$installerExe $installerSource
    if ($LASTEXITCODE -eq 0) {
        & $cscExe /nologo /target:exe /out:$uninstallerExe $uninstallerSource
    }
}

$readme = @'
TS-VMS Demo

1. Double-click Install-TS-VMS-Demo.exe
2. It creates a Desktop shortcut: TS VMS Demo
3. The shortcut starts the full demo stack from this folder
4. Use Uninstall-TS-VMS-Demo.exe to remove the shortcut and demo folder
'@
Set-Content -LiteralPath (Join-Path $Destination "README.txt") -Value $readme

Write-Host "Demo folder created at $Destination" -ForegroundColor Green

