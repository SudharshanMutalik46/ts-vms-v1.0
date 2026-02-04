param(
    [string]$CameraId
)

$ErrorActionPreference = "Stop"
$BaseUrl = "http://localhost:8082/api/v1"

# 1. Generate Dev Token
Write-Host "Generating Admin Token..."
$env:JWT_SIGNING_KEY = "dev-secret-do-not-use-in-prod"
$TokenRaw = (go run scripts/gen-dev-token.go) -join "`n"
# Extract the token using regex
if ($TokenRaw -match "Token: ([^\s\r\n]+)") {
    $Token = $Matches[1]
}
else {
    Write-Error "Failed to parse token from output: $TokenRaw"
    exit 1
}
Write-Host "Token extracted successfully."
if (-not $Token) { Write-Error "Failed to generate token"; exit 1 }

# 2. List Cameras (or specific one if ID provided)
$Headers = @{ "Authorization" = "Bearer $Token" }

try {
    if ($CameraId) {
        Write-Host "Checking Camera: $CameraId"
        $Response = Invoke-RestMethod -Uri "$BaseUrl/cameras/$CameraId" -Headers $Headers -Method Get
        $Response | Format-List
    }
    else {
        Write-Host "Listing All Cameras..."
        $Response = Invoke-RestMethod -Uri "$BaseUrl/cameras" -Headers $Headers -Method Get
        if ($Response.data) {
            $Response.data | Format-Table -Property id, name, ip_address, port, is_enabled -AutoSize
        }
        else {
            Write-Host "No cameras found for this tenant."
        }
    }
}
catch {
    Write-Error "API Request Failed: $_"
}
