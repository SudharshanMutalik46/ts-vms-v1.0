<#
.SYNOPSIS
    Techno Support VMS Service Manager
    Phase 1.8 integration for Windows SCM.

.DESCRIPTION
    Manages the installation, lifecycle, and status of the TS-VMS service stack.
    Supports: Control, Media, SFU, Recorder, AI, and Web.
#>

param (
    [Parameter(Mandatory = $true)]
    [ValidateSet("Install", "Uninstall", "Start", "Stop", "Restart", "Status")]
    [string]$Action,

    [Parameter(Mandatory = $false)]
    [string]$Component = "All",

    [Parameter(Mandatory = $false)]
    [switch]$PurgeData
)

$ErrorActionPreference = "Stop"

# --- Constants & Configuration ---

$InstallRoot = "C:\Program Files\TechnoSupport\VMS"
$DataRoot = "C:\ProgramData\TechnoSupport\VMS"
$ConfigPath = "$DataRoot\config\default.yaml"

# Service Definitions
$Services = @(
    @{
        Name    = "TS-VMS-Control"
        Display = "Techno Support VMS Control"
        Desc    = "Central Control Plane for VMS"
        Bin     = "$InstallRoot\server.exe"
        Args    = "--config `"$ConfigPath`""
        Deps    = @()
        Type    = "Go-Native"
    },
    @{
        Name    = "TS-VMS-Web"
        Display = "Techno Support VMS Web"
        Desc    = "Web Interface for VMS"
        Bin     = "$InstallRoot\web-server.exe"
        Args    = "--config `"$ConfigPath`""
        Deps    = @("TS-VMS-Control")
        Type    = "Wrapper"
    },
    @{
        Name    = "TS-VMS-Media"
        Display = "Techno Support VMS Media"
        Desc    = "Media Processing Plane (C++)"
        Bin     = "$InstallRoot\vms-media.exe"
        Args    = "--config `"$ConfigPath`""
        Deps    = @("TS-VMS-Control")
        Type    = "C++-Native"
    },
    @{
        Name    = "TS-VMS-SFU"
        Display = "Techno Support VMS SFU"
        Desc    = "Selective Forwarding Unit (Node.js)"
        Bin     = "$InstallRoot\node.exe"
        Args    = "`"$InstallRoot\sfu\dist\main.js`""
        Deps    = @("TS-VMS-Control", "TS-VMS-Media")
        Type    = "Node-Native"
    },
    @{
        Name    = "TS-VMS-Recorder"
        Display = "Techno Support VMS Recorder"
        Desc    = "Video Recording Service (Rust)"
        Bin     = "$InstallRoot\vms-recorder.exe"
        Args    = "--config `"$ConfigPath`""
        Deps    = @("TS-VMS-Control")
        Type    = "Rust-Native"
    },
    @{
        Name    = "TS-VMS-AI"
        Display = "Techno Support VMS AI"
        Desc    = "AI Analytics Service (Python)"
        Bin     = "cmd.exe"
        Args    = "/c `"$InstallRoot\python\python.exe`" `"$InstallRoot\ai\main.py`" --config `"$ConfigPath`""
        Deps    = @("TS-VMS-Control")
        Type    = "Wrapper"
    }
)

# --- Helpers ---

function Check-Admin {
    $currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Write-Error "This script must be run as an Administrator."
    }
}

function Register-EventSource {
    param([string]$Source)
    $LogName = "Application"
    if (-not [System.Diagnostics.EventLog]::SourceExists($Source)) {
        Write-Host "Registering Event Log Source: $Source" -ForegroundColor Cyan
        New-EventLog -LogName $LogName -Source $Source
    }
}

function Get-ServiceStatus {
    param($SvcName)
    $s = Get-Service -Name $SvcName -ErrorAction SilentlyContinue
    if ($null -eq $s) { return "Not Installed" }
    return $s.Status.ToString()
}

# --- Actions ---

function Install-Services {
    Check-Admin
    if (-not (Test-Path $InstallRoot)) { New-Item -ItemType Directory -Path $InstallRoot -Force | Out-Null }
    if (-not (Test-Path $DataRoot)) { New-Item -ItemType Directory -Path $DataRoot -Force | Out-Null }

    foreach ($svc in $Services) {
        Write-Host "Installing $($svc.Name)..." -ForegroundColor Yellow
        
        # 1. Register Event Source
        Register-EventSource -Source $svc.Name

        # 2. Create Service
        # sc.exe is notoriously difficult to call from PowerShell with spaces in paths.
        # We use cmd.exe /c to ensure the command line is passed exactly as sc.exe expects.
        
        $escapedBin = $svc.Bin.Replace('"', '\"')
        $escapedArgs = $svc.Args.Replace('"', '\"')
        $fullBinPath = "\`"$escapedBin\`" $escapedArgs"
        
        $exists = Get-Service -Name $svc.Name -ErrorAction SilentlyContinue
        
        $scCmd = ""
        if ($exists) {
            Write-Host "  Service exists, updating configuration..."
            $scCmd = "sc.exe config `"$($svc.Name)`" binPath= `"$fullBinPath`" DisplayName= `"$($svc.Display)`" start= auto"
        }
        else {
            $scCmd = "sc.exe create `"$($svc.Name)`" binPath= `"$fullBinPath`" DisplayName= `"$($svc.Display)`" start= auto"
        }

        if ($svc.Deps.Count -gt 0) {
            $depString = [string]::Join("/", $svc.Deps)
            $scCmd += " depend= `"$depString`""
        }

        # Execute via cmd.exe to handle the picky sc.exe quoting logic
        cmd.exe /c $scCmd

        if ($LASTEXITCODE -ne 0) {
            Write-Error "Failed to install/configure service $($svc.Name). Exit code: $LASTEXITCODE"
        }
        
        cmd.exe /c "sc.exe description `"$($svc.Name)`" `"$($svc.Desc)`""

        # 3. Recovery Actions
        # reset= 86400 (1 day)
        # actions= restart/60000 (1m) / restart/60000 (1m) / ""/60000 (Fail)
        sc.exe failure $($svc.Name) reset= 86400 actions= restart/60000/restart/60000//60000 | Out-Null
    }

    # 4. Firewall Rules (Phase 3.4)
    Write-Host "Configuring Firewall Rules for WebRTC..." -ForegroundColor Cyan
    $rules = @(
        @{ Name = "TS-VMS-WebRTC-UDP"; Range = "40000-49999"; Proto = "UDP" },
        @{ Name = "TS-VMS-PlainTransport-UDP"; Range = "50000-51000"; Proto = "UDP" },
        @{ Name = "TS-VMS-SFU-API"; Range = "8085"; Proto = "TCP" }
    )

    foreach ($rule in $rules) {
        $existing = Get-NetFirewallRule -Name $rule.Name -ErrorAction SilentlyContinue
        if ($null -eq $existing) {
            New-NetFirewallRule -DisplayName $rule.Name -Name $rule.Name -Direction Inbound -Action Allow -Protocol $rule.Proto -LocalPort $rule.Range -Description "Allow traffic for $($rule.Name)"
        }
    }

    Write-Host "Installation Complete." -ForegroundColor Green
}

function Uninstall-Services {
    Check-Admin
    $reverseServices = $Services[($Services.Count - 1)..0]
    foreach ($svc in $reverseServices) {
        Write-Host "Uninstalling $($svc.Name)..." -ForegroundColor Yellow
        Stop-Service -Name $svc.Name -Force -ErrorAction SilentlyContinue
        sc.exe delete $($svc.Name) | Out-Null
    }

    if ($PurgeData) {
        Write-Host "Purging data at $DataRoot..." -ForegroundColor Red
        if (Test-Path $DataRoot) { Remove-Item -Path $DataRoot -Recurse -Force }
    }
    else {
        Write-Host "Skipping data purge (use -PurgeData to remove logs/config/DB files)."
    }
    Write-Host "Uninstallation Complete." -ForegroundColor Green
}

function Start-Services {
    Check-Admin
    foreach ($svc in $Services) {
        if ($Component -ne "All" -and $svc.Name -ne $Component) { continue }
        Write-Host "Starting $($svc.Name)..." -ForegroundColor Yellow
        Start-Service -Name $svc.Name -ErrorAction SilentlyContinue
    }
}

function Stop-Services {
    Check-Admin
    $reverseServices = $Services[($Services.Count - 1)..0]
    foreach ($svc in $reverseServices) {
        if ($Component -ne "All" -and $svc.Name -ne $Component) { continue }
        Write-Host "Stopping $($svc.Name)..." -ForegroundColor Yellow
        Stop-Service -Name $svc.Name -ErrorAction SilentlyContinue
    }
}

function Show-Status {
    $Results = foreach ($svc in $Services) {
        $state = Get-ServiceStatus -SvcName $svc.Name
        $binary = "N/A"
        $startType = "N/A"
        
        if ($state -ne "Not Installed") {
            $qc = sc.exe qc $($svc.Name) | Out-String
            if ($qc -match "BINARY_PATH_NAME\s+:\s+(.+)") { $binary = $Matches[1].Trim() }
            if ($qc -match "START_TYPE\s+:\s+\w+\s+(.+)") { $startType = $Matches[1].Trim() }
        }

        [PSCustomObject]@{
            ServiceName = $svc.Name
            Status      = $state
            StartType   = $startType
            BinaryPath  = $binary
        }
    }
    $Results | Format-Table -AutoSize
}

# --- Main Entry ---

switch ($Action) {
    "Install" { Install-Services }
    "Uninstall" { Uninstall-Services }
    "Start" { Start-Services }
    "Stop" { Stop-Services }
    "Restart" { Stop-Services; Start-Services }
    "Status" { Show-Status }
}
