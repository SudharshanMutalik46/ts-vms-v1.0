param(
    [string]$OutputRoot = "",
    [switch]$SkipDesktopPublish,
    [switch]$SkipSetupBuild
)

$ErrorActionPreference = "Stop"

function Ensure-Directory {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) {
        New-Item -ItemType Directory -Path $Path -Force | Out-Null
    }
}

function Copy-IfExists {
    param(
        [string]$Source,
        [string]$Destination
    )

    if (Test-Path -LiteralPath $Source) {
        $parent = Split-Path -Parent $Destination
        if ($parent) {
            Ensure-Directory $parent
        }
        Copy-Item -LiteralPath $Source -Destination $Destination -Force
        return $true
    }

    return $false
}

function Copy-TreeIfExists {
    param(
        [string]$Source,
        [string]$Destination
    )

    if (Test-Path -LiteralPath $Source) {
        Ensure-Directory $Destination
        Copy-Item -Path (Join-Path $Source "*") -Destination $Destination -Recurse -Force
        return $true
    }

    return $false
}

$repoRoot = Split-Path -Parent $PSScriptRoot
if (-not $OutputRoot) {
    $OutputRoot = Join-Path $repoRoot "dist\TS-VMS-Portable"
}

$stagingRoot = Join-Path $OutputRoot "staging"
$setupRoot = Join-Path $stagingRoot "setup"
$packageRoot = Join-Path $setupRoot "package"
$appRoot = Join-Path $packageRoot "app"
$desktopOut = Join-Path $appRoot "desktop"
$binOut = Join-Path $appRoot "bin"
$sfuOut = Join-Path $appRoot "sfu"
$scriptsOut = Join-Path $packageRoot "scripts"
$zipPath = Join-Path $OutputRoot "TS-VMS-Portable.zip"

if (Test-Path -LiteralPath $stagingRoot) {
    Remove-Item -LiteralPath $stagingRoot -Recurse -Force
}

Ensure-Directory $desktopOut
Ensure-Directory $binOut
Ensure-Directory $sfuOut
Ensure-Directory $scriptsOut

if (-not $SkipDesktopPublish) {
    Push-Location $repoRoot
    dotnet publish .\desktop\TSVmsDesktop\TSVmsDesktop.csproj -c Release -r win-x64 --self-contained true -o $desktopOut
    Pop-Location
}
else {
    $existingDesktop = Join-Path $repoRoot "desktop\TSVmsDesktop\bin\Release\net8.0-windows"
    if (-not (Copy-TreeIfExists -Source $existingDesktop -Destination $desktopOut)) {
        throw "Desktop output not found at $existingDesktop"
    }
}

$binCandidates = @(
    @{ Source = (Join-Path $repoRoot "server.exe"); Destination = (Join-Path $binOut "server.exe") },
    @{ Source = (Join-Path $repoRoot "vms-control.exe"); Destination = (Join-Path $binOut "vms-control.exe") },
    @{ Source = (Join-Path $repoRoot "vms-hlsd.exe"); Destination = (Join-Path $binOut "vms-hlsd.exe") },
    @{ Source = (Join-Path $repoRoot "hlsd.exe"); Destination = (Join-Path $binOut "hlsd.exe") },
    @{ Source = (Join-Path $repoRoot "bin\vms-recording-bin.exe"); Destination = (Join-Path $binOut "vms-recording-bin.exe") },
    @{ Source = (Join-Path $repoRoot "vms-recording.exe"); Destination = (Join-Path $binOut "vms-recording.exe") },
    @{ Source = (Join-Path $repoRoot "src\vms-ai\nats-server.exe"); Destination = (Join-Path $binOut "nats-server.exe") },
    @{ Source = "C:\Program Files\nodejs\node.exe"; Destination = (Join-Path $binOut "node.exe") },
    @{ Source = "C:\Users\sudha\Downloads\Redis-x64-5.0.14.1\redis-server.exe"; Destination = (Join-Path $binOut "redis-server.exe") }
)

foreach ($candidate in $binCandidates) {
    Copy-IfExists -Source $candidate.Source -Destination $candidate.Destination | Out-Null
}

Copy-TreeIfExists -Source (Join-Path $repoRoot "config") -Destination (Join-Path $appRoot "config") | Out-Null
Copy-TreeIfExists -Source (Join-Path $repoRoot "sfu\dist") -Destination (Join-Path $sfuOut "dist") | Out-Null
Copy-TreeIfExists -Source (Join-Path $repoRoot "sfu\node_modules") -Destination (Join-Path $sfuOut "node_modules") | Out-Null

Copy-IfExists -Source (Join-Path $repoRoot "scripts\portable\Start-TSVmsPortable.ps1") -Destination (Join-Path $scriptsOut "Start-TSVmsPortable.ps1") | Out-Null
Copy-IfExists -Source (Join-Path $repoRoot "scripts\portable\Start-TSVmsPortable.cmd") -Destination (Join-Path $scriptsOut "Start-TSVmsPortable.cmd") | Out-Null

$readme = @'
TS-VMS Portable Package

Files:
- TSVmsSetup.exe: installs the package to LocalAppData, creates a desktop shortcut, and starts the app.
- package\: runtime files copied by the installer.

Notes:
- This package tries to include local copies of node.exe, nats-server.exe, and redis-server.exe when available.
- PostgreSQL, Redis, GStreamer, camera/network access, and driver-level dependencies may still need to exist on the target machine.
- The desktop app expects GStreamer at C:\Program Files\gstreamer\1.0\msvc_x86_64\bin in the current codebase.
'@
Set-Content -LiteralPath (Join-Path $setupRoot "README.txt") -Value $readme

if (-not $SkipSetupBuild) {
    $setupPublishDir = Join-Path $stagingRoot "setup-publish"
    Ensure-Directory $setupPublishDir
    Push-Location $repoRoot
    dotnet publish .\packaging\TSVmsSetup\TSVmsSetup.csproj -c Release -r win-x64 --self-contained true -o $setupPublishDir
    Pop-Location
    Copy-IfExists -Source (Join-Path $setupPublishDir "TSVmsSetup.exe") -Destination (Join-Path $setupRoot "TSVmsSetup.exe") | Out-Null
}

if (Test-Path -LiteralPath $zipPath) {
    Remove-Item -LiteralPath $zipPath -Force
}

Compress-Archive -Path (Join-Path $setupRoot "*") -DestinationPath $zipPath

Write-Host "Portable package created:" -ForegroundColor Green
Write-Host $zipPath
