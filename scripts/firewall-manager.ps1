param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet("Install", "Uninstall", "Status")]
    [string]$Action,

    [string]$ConfigPath = "C:\ProgramData\TechnoSupport\VMS\config\default.yaml",

    [switch]$DryRun
)

$GroupName = "TechnoSupport VMS"
$RulePrefix = "TS-VMS"

# Default Ports (Fallback)
$Ports = @(
    @{ Service = "Control"; Port = 8080; Protocol = "TCP" },
    @{ Service = "Web"; Port = 8081; Protocol = "TCP" },
    @{ Service = "NATS"; Port = 4222; Protocol = "TCP" }
)

# Attempt to parse ports from config if it exists
if (Test-Path $ConfigPath) {
    Write-Host "[INFO] Reading ports from config: $ConfigPath"
    $content = Get-Content $ConfigPath -Raw
    # Simple regex for top-level or section ports
    # e.g., "port: 8080"
    if ($content -match 'port:\s*(\d+)') {
        $foundPort = $matches[1]
        Write-Host "[INFO] Detected server port: $foundPort"
        # Update Control port if found
        $Ports[0].Port = [int]$foundPort
    }
}

function Get-Elevation {
    $id = [System.Security.Principal.WindowsIdentity]::GetCurrent()
    $p = New-Object System.Security.Principal.WindowsPrincipal($id)
    return $p.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)
}

if (-not (Get-Elevation) -and -not $DryRun) {
    Write-Error "This script requires Administrator privileges to manage firewall rules."
    exit 1
}

function Install-Rules {
    Write-Host "[INFO] Installing firewall rules for group '$GroupName'..."
    foreach ($p in $Ports) {
        $name = "$RulePrefix-$($p.Service)-$($p.Port)-In"
        Write-Host "[INFO] Processing rule: $name"
        
        if ($DryRun) {
            Write-Host "[DRY-RUN] New-NetFirewallRule -Name '$name' -DisplayName '$name' -Group '$GroupName' -LocalPort $($p.Port) -Protocol $($p.Protocol) -Direction Inbound -Action Allow -Profile Private -RemoteAddress LocalSubnet"
            continue
        }

        # Check if exists
        $existing = Get-NetFirewallRule -Name $name -ErrorAction SilentlyContinue
        if ($existing) {
            Write-Host "[INFO] Rule already exists, skipping."
            continue
        }

        New-NetFirewallRule -Name $name `
            -DisplayName $name `
            -Description "TechnoSupport VMS $($p.Service) Port" `
            -Group $GroupName `
            -LocalPort $p.Port `
            -Protocol $p.Protocol `
            -Direction Inbound `
            -Action Allow `
            -Profile Private `
            -RemoteAddress LocalSubnet | Out-Null
        Write-Host "[SUCCESS] Rule '$name' created."
    }
}

function Uninstall-Rules {
    Write-Host "[INFO] Uninstalling firewall rules for group '$GroupName'..."
    if ($DryRun) {
        Write-Host "[DRY-RUN] Remove-NetFirewallRule -Group '$GroupName'"
        return
    }

    $rules = Get-NetFirewallRule -Group $GroupName -ErrorAction SilentlyContinue
    if ($rules) {
        Remove-NetFirewallRule -Group $GroupName
        Write-Host "[SUCCESS] Removed $($rules.Count) rules."
    }
    else {
        Write-Host "[INFO] No rules found for group '$GroupName'."
    }
}

function Get-Status {
    Write-Host "[INFO] Checking status for group '$GroupName'..."
    $rules = Get-NetFirewallRule -Group $GroupName -ErrorAction SilentlyContinue
    if ($rules) {
        Write-Host "[INFO] Found $($rules.Count) rules:"
        foreach ($r in $rules) {
            $filter = Get-NetFirewallPortFilter -All | Where-Object { $_.InstanceID -eq $r.Name }
            Write-Host " - $($r.DisplayName) (Port: $($filter.LocalPort), Enabled: $($r.Enabled))"
        }
    }
    else {
        Write-Host "[INFO] No rules present for group '$GroupName'."
    }
}

switch ($Action) {
    "Install" { Install-Rules }
    "Uninstall" { Uninstall-Rules }
    "Status" { Get-Status }
}
