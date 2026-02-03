# verify_nvr_health.ps1
# Script to verify Phase 2.9 NVR Health Monitoring API

$ErrorActionPreference = "Stop"

# Config
$Global:BaseUrl = "http://localhost:8080/api/v1"
$Global:AdminEmail = "admin@technosupport.com"
$Global:AdminPassword = "password123"
# Dev Tenant ID 000...001
$Global:TenantID = "00000000-0000-0000-0000-000000000001" 
$Global:Token = ""

# Colors
function Print-Green ($msg) { Write-Host $msg -ForegroundColor Green }
function Print-Red ($msg) { Write-Host $msg -ForegroundColor Red }
function Print-Yellow ($msg) { Write-Host $msg -ForegroundColor Yellow }

# --- Auth Helper ---
function Get-AuthToken {
    $body = @{ email = $Global:AdminEmail; password = $Global:AdminPassword; tenant_id = $Global:TenantID } | ConvertTo-Json
    try {
        $response = Invoke-RestMethod -Uri "$Global:BaseUrl/auth/login" -Method Post -Body $body -ContentType "application/json"
        $Global:Token = $response.access_token
        Print-Green "[OK] Authenticated"
    }
    catch {
        Print-Red "[FAIL] Login failed: $_"
        exit 1
    }
}

function Invoke-API {
    param($Uri, $Method = "Get", $Body = $null)
    $headers = @{ "Authorization" = "Bearer $Global:Token" }
    try {
        if ($Body) {
            $json = $Body | ConvertTo-Json -Depth 10
            return Invoke-RestMethod -Uri $Uri -Method $Method -Headers $headers -Body $json -ContentType "application/json"
        }
        else {
            return Invoke-RestMethod -Uri $Uri -Method $Method -Headers $headers
        }
    }
    catch {
        $err = $_.Exception.Response
        # Print-Red "[API Error] $Method $Uri -> $($err.StatusCode)"
        throw $_
    }
}

# --- Main ---

Get-AuthToken

Print-Yellow "--- 1. Checking NVR Health Summary ---"
try {
    $summary = Invoke-API "$Global:BaseUrl/health/nvrs/summary"
    Print-Green "[OK] Summary Fetched"
    $summary | Format-List
    
    if ($summary.total_nvrs -ge 0) {
        Print-Green "[PASS] total_nvrs field exists"
    }
}
catch {
    Print-Red "[FAIL] fetching summary"
    Write-Error $_
}

Print-Yellow "--- 2. Checking NVR Channel Health (First NVR) ---"
try {
    # Get List of NVRs
    $nvrs = Invoke-API "$Global:BaseUrl/nvrs"
    $list = $nvrs.nvrs
    
    # Handle if null or count 0
    if ($list -ne $null -and $list.Count -gt 0) {
        $nvrID = $list[0].id
        Print-Yellow "Checking Channels for NVR: $nvrID"
        
        $chHealth = Invoke-API "$Global:BaseUrl/health/nvrs/$nvrID/channels"
        Print-Green "[OK] Channel Health Fetched"
        if ($chHealth.Count -gt 0) {
            $chHealth[0] | Format-List
            Print-Green "[PASS] Found channel health entries"
        }
        else {
            Print-Yellow "[WARN] No channels found for this NVR"
        }
    }
    else {
        Print-Yellow "[WARN] No NVRs to check channels"
    }
}
catch {
    Print-Red "[FAIL] fetching channel health"
    Write-Error $_
}

Print-Green "Done."
