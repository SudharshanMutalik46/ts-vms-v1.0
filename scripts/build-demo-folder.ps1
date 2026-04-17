param(
    [string]$Destination = "C:\Users\sudha\Desktop\TS-VMS-Demo"
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

    Ensure-Directory $Destination
    Copy-Item -Path (Join-Path $Source "*") -Destination $Destination -Recurse -Force
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

Copy-TreeContents -Source (Join-Path $repoRoot "desktop\TSVmsDesktop\bin\Release\net8.0-windows") -Destination $desktopRoot
Copy-TreeContents -Source (Join-Path $repoRoot "config") -Destination $configRoot
Copy-TreeContents -Source (Join-Path $repoRoot "db\migrations") -Destination $dbMigrationsRoot
Copy-TreeContents -Source (Join-Path $repoRoot "sfu\dist") -Destination (Join-Path $sfuRoot "dist")
Copy-TreeContents -Source (Join-Path $repoRoot "sfu\node_modules") -Destination (Join-Path $sfuRoot "node_modules")

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

if (-not (Test-Path -LiteralPath (Join-Path $binRoot "vms-media.exe"))) {
    throw "Missing Release media binary at media-plane\\build\\Release\\vms-media.exe. Build Release first."
}

Push-Location $repoRoot
go build -o (Join-Path $binRoot "migrator.exe") .\cmd\migrator
Pop-Location

Copy-Item -LiteralPath (Join-Path $repoRoot "scripts\demo\Start-TSVmsDemo.ps1") -Destination (Join-Path $scriptsRoot "Start-TSVmsDemo.ps1") -Force
Copy-Item -LiteralPath (Join-Path $repoRoot "scripts\demo\Start-TSVmsDemo.cmd") -Destination (Join-Path $scriptsRoot "Start-TSVmsDemo.cmd") -Force

Copy-TreeContents -Source "C:\Program Files\PostgreSQL\18\bin" -Destination (Join-Path $toolsRoot "postgres\bin")
Copy-TreeContents -Source "C:\Program Files\PostgreSQL\18\lib" -Destination (Join-Path $toolsRoot "postgres\lib")
Copy-TreeContents -Source "C:\Program Files\PostgreSQL\18\share" -Destination (Join-Path $toolsRoot "postgres\share")

Copy-TreeContents -Source "C:\Program Files\gstreamer\1.0\msvc_x86_64\bin" -Destination (Join-Path $toolsRoot "gstreamer\bin")
Copy-TreeContents -Source "C:\Program Files\gstreamer\1.0\msvc_x86_64\lib" -Destination (Join-Path $toolsRoot "gstreamer\lib")
if (Test-Path -LiteralPath "C:\Program Files\gstreamer\1.0\msvc_x86_64\libexec") {
    Copy-TreeContents -Source "C:\Program Files\gstreamer\1.0\msvc_x86_64\libexec" -Destination (Join-Path $toolsRoot "gstreamer\libexec")
}
if (Test-Path -LiteralPath "C:\Program Files\gstreamer\1.0\msvc_x86_64\share") {
    Copy-TreeContents -Source "C:\Program Files\gstreamer\1.0\msvc_x86_64\share" -Destination (Join-Path $toolsRoot "gstreamer\share")
}
if (Test-Path -LiteralPath "C:\Program Files\gstreamer\1.0\msvc_x86_64\etc") {
    Copy-TreeContents -Source "C:\Program Files\gstreamer\1.0\msvc_x86_64\etc" -Destination (Join-Path $toolsRoot "gstreamer\etc")
}

Ensure-Directory (Join-Path $toolsRoot "redis")
Copy-Item -LiteralPath "C:\Users\sudha\Downloads\Redis-x64-5.0.14.1\redis-server.exe" -Destination (Join-Path $toolsRoot "redis\redis-server.exe") -Force
if (Test-Path -LiteralPath "C:\Users\sudha\Downloads\Redis-x64-5.0.14.1\redis.windows.conf") {
    Copy-Item -LiteralPath "C:\Users\sudha\Downloads\Redis-x64-5.0.14.1\redis.windows.conf" -Destination (Join-Path $toolsRoot "redis\redis.windows.conf") -Force
}

Ensure-Directory (Join-Path $toolsRoot "nats")
Copy-Item -LiteralPath (Join-Path $repoRoot "src\vms-ai\nats-server.exe") -Destination (Join-Path $toolsRoot "nats\nats-server.exe") -Force

Ensure-Directory (Join-Path $toolsRoot "node")
Copy-Item -LiteralPath "C:\Program Files\nodejs\node.exe" -Destination (Join-Path $toolsRoot "node\node.exe") -Force

Ensure-Directory (Join-Path $toolsRoot "ffmpeg")
if (Test-Path -LiteralPath "C:\ffmpeg\bin\ffmpeg.exe") {
    Copy-Item -LiteralPath "C:\ffmpeg\bin\ffmpeg.exe" -Destination (Join-Path $toolsRoot "ffmpeg\ffmpeg.exe") -Force
}

$installerSource = Join-Path $repoRoot "packaging\TSVmsDemoInstaller\Program.cs"
$installerExe = Join-Path $Destination "Install-TS-VMS-Demo.exe"
$uninstallerSource = Join-Path $repoRoot "packaging\TSVmsDemoUninstaller\Program.cs"
$uninstallerExe = Join-Path $Destination "Uninstall-TS-VMS-Demo.exe"
$cscExe = "C:\Windows\Microsoft.NET\Framework64\v4.0.30319\csc.exe"
& $cscExe /nologo /target:exe /out:$installerExe $installerSource
if ($LASTEXITCODE -ne 0) {
    throw "Failed to compile installer exe."
}
& $cscExe /nologo /target:exe /out:$uninstallerExe $uninstallerSource
if ($LASTEXITCODE -ne 0) {
    throw "Failed to compile uninstaller exe."
}

$readme = @'
TS-VMS Demo

1. Double-click Install-TS-VMS-Demo.exe
2. It creates a Desktop shortcut: TS VMS Demo
3. The shortcut starts the full demo stack from this folder
4. Use Uninstall-TS-VMS-Demo.exe to remove the shortcut and demo folder

This folder bundles:
- Desktop client
- Control plane
- Media plane
- Recording service
- HLSD
- PostgreSQL tools and local data bootstrap
- Redis
- NATS
- Node.js
- GStreamer runtime
- FFmpeg runtime if present on the build machine
'@
Set-Content -LiteralPath (Join-Path $Destination "README.txt") -Value $readme

Write-Host "Demo folder created at $Destination" -ForegroundColor Green
