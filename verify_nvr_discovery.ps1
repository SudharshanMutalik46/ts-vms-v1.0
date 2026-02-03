# NVR Discovery Verification Script

$ErrorActionPreference = "Stop"

$BaseUrl = "http://localhost:8080/api/v1"
$AdminEmail = "admin@example.com"
$AdminPassword = "admin_password"

function Invoke-Api {
    param(
        [string]$Method,
        [string]$Uri,
        [hashtable]$Body = $null,
        [string]$Token
    )
    
    $Headers = @{ "Content-Type" = "application/json" }
    if ($Token) { $Headers["Authorization"] = "Bearer $Token" }

    $Params = @{
        Method  = $Method
        Uri     = $BaseUrl + $Uri
        Headers = $Headers
    }
    if ($Body) { $Params.Body = ($Body | ConvertTo-Json -Depth 4) }

    try {
        $Response = Invoke-RestMethod @Params
        return $Response
    }
    catch {
        Write-Host "Error calling $Uri" -ForegroundColor Red
        Write-Host $_.Exception.Message -ForegroundColor Red
        if ($_.Exception.Response) {
            $Stream = $_.Exception.Response.GetResponseStream()
            $Reader = [System.IO.StreamReader]::New($Stream)
            Write-Host $Reader.ReadToEnd() -ForegroundColor Yellow
        }
        throw
    }
}

Write-Host "1. Logging in..." -ForegroundColor Cyan
$Login = Invoke-Api -Method POST -Uri "/auth/login" -Body @{ email = $AdminEmail; password = $AdminPassword }
$Token = $Login.token
Write-Host "Logged in." -ForegroundColor Green

Write-Host "`n2. Creating Test NVR..." -ForegroundColor Cyan
# Ensure we have a mock NVR or use a real IP if available. For test, we use localhost or dummy.
# We'll use the 'simulated' adapter if I wrote one, or 'hikvision' pointing to a mock server?
# I'll create an NVR pointing to 192.168.1.100 (dummy) with adapter 'hikvision'.
# TestConnection might fail if no device, but we want to exercise the API.
$NvrBody = @{
    name       = "Test NVR Discovery"
    ip_address = "192.168.1.100"
    port       = 80
    vendor     = "hikvision"
    username   = "admin"
    password   = "password123"
}
try {
    $NVR = Invoke-Api -Method POST -Uri "/nvrs" -Body $NvrBody -Token $Token
    $NvrId = $NVR.id
    Write-Host "Created NVR: $NvrId" -ForegroundColor Green
}
catch {
    # Maybe already exists? List and pick one.
    $List = Invoke-Api -Method GET -Uri "/nvrs" -Token $Token
    $NVR = $List.data | Select-Object -First 1
    if ($NVR) {
        $NvrId = $NVR.id
        Write-Host "Using existing NVR: $NvrId" -ForegroundColor Green
    }
    else {
        throw "Could not create or find NVR"
    }
}

Write-Host "`n3. Testing Connection..." -ForegroundColor Cyan
try {
    $Res = Invoke-Api -Method POST -Uri "/nvrs/$NvrId`::test-connection" -Token $Token
    Write-Host "Connection Result: $($Res.status)" -ForegroundColor Yellow
}
catch {
    Write-Host "Test Connection failed (expected if no device)" -ForegroundColor Yellow
}

Write-Host "`n4. Running Discovery..." -ForegroundColor Cyan
try {
    $Res = Invoke-Api -Method POST -Uri "/nvrs/$NvrId`::discover-channels" -Token $Token
    Write-Host "Discovery Count: $($Res.count)" -ForegroundColor Yellow
}
catch {
    Write-Host "Discovery failed (expected if no device)" -ForegroundColor Yellow
}

Write-Host "`n5. Listing Channels..." -ForegroundColor Cyan
$Channels = Invoke-Api -Method GET -Uri "/nvrs/$NvrId/channels" -Token $Token
Write-Host "Found $($Channels.total) channels." -ForegroundColor Green
$Channels.data | Format-Table ChannelRef, Name, ValidationStatus

if ($Channels.data.Count -gt 0) {
    $Ch = $Channels.data[0]
    Write-Host "`n6. Validating Channel $($Ch.id)..." -ForegroundColor Cyan
    $ValBody = @{ channel_ids = @($Ch.id) }
    try {
        $ValRes = Invoke-Api -Method POST -Uri "/nvrs/$NvrId`::validate-channels" -Body $ValBody -Token $Token
        Write-Host "Validation Result: $($ValRes.results."$($Ch.id)")" -ForegroundColor Green
    }
    catch {
        Write-Host "Validation failed" -ForegroundColor Red
    }

    Write-Host "`n7. Provisioning Camera for Channel $($Ch.id)..." -ForegroundColor Cyan
    $ProvBody = @{ channel_ids = @($Ch.id) }
    try {
        $ProvRes = Invoke-Api -Method POST -Uri "/nvrs/$NvrId`::provision-cameras" -Body $ProvBody -Token $Token
        Write-Host "Provisioned Count: $($ProvRes.provisioned_count)" -ForegroundColor Green
    }
    catch {
        Write-Host "Provisioning failed" -ForegroundColor Red
    }
}
else {
    Write-Host "Skipping Provisioning (No channels found)" -ForegroundColor Yellow
}

Write-Host "`nDone." -ForegroundColor Cyan
