# NVR Integration Verification Script

$ErrorActionPreference = "Stop"

# Configuration
$BaseUrl = "http://localhost:8080/api/v1"
$AdminEmail = "admin@technosupport.com"
$AdminPassword = "adminpassword" # Default from seed

# 1. Login (Bypassed using Token Gen)
Write-Host "1. Generating Token..." -ForegroundColor Cyan
$Token = go run cmd/token_gen/main.go
$Token = $Token.Trim() # Remove whitespace
$Headers = @{ Authorization = "Bearer $Token" }
Write-Host "   Token Generated." -ForegroundColor Green

# 2. Setup: Get a Site and Camera
Write-Host "`n2. Fetching Site and Camera..." -ForegroundColor Cyan
# skipped debug check
# Wait, we don't have a public List Sites endpoint easily accessible without iterating?
# We hardcode Site ID from seed or assume existing one.
# Seed site: "00000000-0000-0000-0000-000000000001"
$SiteID = "950fe99a-a4eb-4d7c-ac58-aaff6ea03d1d"

# Create a Dummy Camera for testing linking
$CamBody = @{
    site_id    = $SiteID
    name       = "NVR Test Camera"
    ip_address = "192.168.1.199"
    port       = 554
    username   = "admin"
    password   = "password"
} | ConvertTo-Json
$Camera = Invoke-RestMethod -Uri "$BaseUrl/cameras" -Method Post -Body $CamBody -Headers $Headers -ContentType "application/json"
$CameraID = $Camera.id
Write-Host "   Created Camera: $CameraID" -ForegroundColor Green

# 3. Create NVR
Write-Host "`n3. Creating NVR..." -ForegroundColor Cyan
$NVRBody = @{
    site_id    = $SiteID
    name       = "Test NVR 1"
    vendor     = "hikvision"
    ip_address = "10.0.0.100"
    port       = 8000
} | ConvertTo-Json
$NVR = Invoke-RestMethod -Uri "$BaseUrl/nvrs" -Method Post -Body $NVRBody -Headers $Headers -ContentType "application/json"
$NVRId = $NVR.id
Write-Host "   Created NVR: $NVRId (Status: $($NVR.status))" -ForegroundColor Green

# 4. Link Camera
Write-Host "`n4. Linking Camera to NVR..." -ForegroundColor Cyan
$LinkBody = @{
    links = @(
        @{
            camera_id       = $CameraID
            recording_mode  = "vms"
            nvr_channel_ref = "1"
        }
    )
} | ConvertTo-Json
Invoke-RestMethod -Uri "$BaseUrl/nvrs/$NVRId/cameras" -Method Put -Body $LinkBody -Headers $Headers -ContentType "application/json" | Out-Null
Write-Host "   Linked Camera." -ForegroundColor Green

# Verify Link
$Links = Invoke-RestMethod -Uri "$BaseUrl/nvrs/$NVRId/cameras" -Headers $Headers
if ($Links.Count -eq 1 -and $Links[0].camera_id -eq $CameraID) {
    Write-Host "   Link Verification Passed." -ForegroundColor Green
}
else {
    Write-Error "   Link Verification Failed."
}

# 5. Credentials
Write-Host "`n5. Setting Credentials..." -ForegroundColor Cyan
$CredBody = @{
    username = "nvr_user"
    password = "nvr_password"
} | ConvertTo-Json
Invoke-RestMethod -Uri "$BaseUrl/nvrs/$NVRId/credentials" -Method Put -Body $CredBody -Headers $Headers -ContentType "application/json" | Out-Null
Write-Host "   Credentials Set." -ForegroundColor Green

# Get Credentials
try {
    $Creds = Invoke-RestMethod -Uri "$BaseUrl/nvrs/$NVRId/credentials" -Headers $Headers
    if ($Creds.username -eq "nvr_user" -and $Creds.password -eq "nvr_password") {
        Write-Host "   Credentials Retrieval Passed (Decrypted)." -ForegroundColor Green
    }
    else {
        Write-Error "   Credentials Retrieval Failed: Mismatch."
    }
}
catch {
    $stream = $_.Exception.Response.GetResponseStream()
    $reader = New-Object System.IO.StreamReader($stream)
    $body = $reader.ReadToEnd()
    Write-Error "   GetCredentials Failed: $body"
}

# 6. Status Propagation (Online)
Write-Host "`n6. Checking Status Propagation (NVR Unknown/Online)..." -ForegroundColor Cyan
# NVR created as 'unknown' (which defaults to passing through camera status)
# Seed Health Record (Simulating Prober)
$env:PAGER = ""; $env:PGPASSWORD = "ts1234"; psql -U postgres -h localhost -d ts_vms -c "INSERT INTO camera_health_current (camera_id, tenant_id, status, last_checked_at, updated_at, consecutive_failures) VALUES ('$CameraID', '00000000-0000-0000-0000-000000000001', 'ONLINE', NOW(), NOW(), 0);" | Out-Null

$Health = Invoke-RestMethod -Uri "$BaseUrl/cameras/$CameraID/health" -Headers $Headers
Write-Host "   Camera Health: $($Health.status)"
Write-Host "   NVR Linked: $($Health.nvr_linked)"
Write-Host "   Effective Status: $($Health.effective_status)"

if ($Health.nvr_linked -eq $true -and $Health.effective_status -eq $Health.status) {
    Write-Host "   Propagation Logic Passed (Normal)." -ForegroundColor Green
}
else {
    Write-Error "   Propagation Logic Failed."
}

# 7. Status Propagation (Offline)
Write-Host "`n7. Checking Status Propagation (NVR Offline)..." -ForegroundColor Cyan
# Manually update NVR to offline via API
$UpdateBody = @{
    status = "offline"
} | ConvertTo-Json
$NVR = Invoke-RestMethod -Uri "$BaseUrl/nvrs/$NVRId" -Method Put -Body $UpdateBody -Headers $Headers -ContentType "application/json"
Write-Host "   Set NVR to OFFLINE."

$HealthOffline = Invoke-RestMethod -Uri "$BaseUrl/cameras/$CameraID/health" -Headers $Headers
Write-Host "   NVR Status: $($HealthOffline.nvr_status)"
Write-Host "   Effective Status: $($HealthOffline.effective_status)"
Write-Host "   Effective Reason: $($HealthOffline.effective_reason)"

if ($HealthOffline.effective_status -eq "OFFLINE" -and $HealthOffline.effective_reason -eq "nvr_offline") {
    Write-Host "   Propagation Logic Passed (Offline Forced)." -ForegroundColor Green
}
else {
    Write-Error "   Propagation Logic Failed (Expected OFFLINE/nvr_offline)."
}

# 8. Cleanup
Write-Host "`n8. Cleanup..." -ForegroundColor Cyan
Invoke-RestMethod -Uri "$BaseUrl/nvrs/$NVRId" -Method Delete -Headers $Headers | Out-Null
# Camera delete?
# Invoke-RestMethod -Uri "$BaseUrl/cameras/$CameraID/disable" -Method Post -Headers $Headers | Out-Null
Write-Host "   Cleanup Complete." -ForegroundColor Green

Write-Host "`n--- VERIFICATION SUCCESSFUL ---" -ForegroundColor Green
