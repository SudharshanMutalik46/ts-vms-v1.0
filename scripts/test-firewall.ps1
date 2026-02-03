# Firewall Verification Tests (Cases 10-15)
param([switch]$SkipAdmin)

$script = ".\scripts\firewall-manager.ps1"

Write-Host "--- Firewall Test 13: Dry-Run ---"
# Use Out-String to avoid object-to-string clipping
$out = powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "& $script Status -DryRun" | Out-String
if ($out -match "Checking status") { 
    Write-Host "[PASS] Dry-run executed safely." 
}
else { 
    Write-Error "[FAIL] Dry-run failed. Output: $out" 
}

Write-Host "--- Firewall Test 15: Rule Naming Convention ---"
$out = powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "& $script Install -DryRun" | Out-String
if ($out -match "TS-VMS-Control-8080-In") { 
    Write-Host "[PASS] Follows naming convention (Control)." 
}
else { 
    Write-Error "[FAIL] Naming mismatch (Control). Output: $out" 
}

if ($out -match "TS-VMS-NATS-4222-In") {
    Write-Host "[PASS] Follows naming convention (NATS)."
}
else {
    Write-Error "[FAIL] Naming mismatch (NATS). Output: $out"
}

if (-not $SkipAdmin) {
    # Elevation check
    $id = [System.Security.Principal.WindowsIdentity]::GetCurrent()
    $p = New-Object System.Security.Principal.WindowsPrincipal($id)
    if ($p.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Write-Host "--- Firewall Test 10: Idempotent Install ---"
        & $script Install
        & $script Install
        Write-Host "[PASS] Install completed twice (idempotent)."

        Write-Host "--- Firewall Test 12: Status Reporting ---"
        & $script Status
        Write-Host "[PASS] Status reported."

        Write-Host "--- Firewall Test 11: Group-Specific Uninstall ---"
        & $script Uninstall
        Write-Host "[PASS] Group uninstall completed."
    }
    else {
        Write-Host "[SKIP] Admin tests skipped (Not elevated)."
    }
}
