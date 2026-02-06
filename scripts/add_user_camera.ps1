$ErrorActionPreference = "Stop"

$Token = Get-Content "token.txt" -Raw
$Token = $Token.Trim()
$Headers = @{ "Authorization" = "Bearer $Token"; "Content-Type" = "application/json" }

# 1. Create Camera
Write-Host "Creating Camera..."
$siteId = "00000000-0000-0000-0000-000000000001"
$camBody = @{ 
    site_id    = $siteId
    name       = "Real IP Cam"
    ip_address = "192.168.1.7"
    port       = 554
    tags       = @("manual_override")
} | ConvertTo-Json

try {
    $cam = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/cameras" -Method Post -Headers $Headers -Body $camBody
    $camId = $cam.id
    Write-Host "Created Camera ID: $camId"
}
catch {
    Write-Host "Create failed/skipped: $_"
    # Try to find it if duplicate
    $list = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/cameras" -Method Get -Headers $Headers
    $found = $list.data | Where-Object { $_.ip_address -eq "192.168.1.7" }
    if ($found) {
        $camId = if ($found -is [array]) { $found[0].id } else { $found.id }
        Write-Host "Found existing ID: $camId"
    }
    else {
        exit
    }
}

# 2. Force Media Selection
Write-Host "Setting RTSP URL..."
$env:DB_HOST = "localhost"
$env:DB_USER = "postgres"
$env:DB_PASSWORD = "ts1234"
$env:DB_NAME = "ts_vms"
$env:CAM_ID = $camId

go run scripts/force_rtsp_standalone.go

Write-Host "Done. URL set to rtsp://192.168.1.7:554/live/stream1"
Write-Host "RTSP_URL=rtsp://192.168.1.7:554/live/stream1"
Write-Host "CAMERA_ID=$camId"
