# HLS Token Generator for vms-hlsd Manual Verification
param(
    [string]$CameraID = "cam1",
    [string]$SessionID = "sess1",
    [string]$Secret = "dev-hls-secret",
    [string]$Kid = "v1",
    [int]$ExpiryMinutes = 60
)

$exp = [DateTimeOffset]::Now.AddMinutes($ExpiryMinutes).ToUnixTimeSeconds()
$canonical = "hls|$CameraID|$SessionID|$exp"

$hmacsha = New-Object System.Security.Cryptography.HMACSHA256
$hmacsha.Key = [System.Text.Encoding]::UTF8.GetBytes($Secret)
$signatureBytes = $hmacsha.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($canonical))
$sig = [System.BitConverter]::ToString($signatureBytes).Replace("-", "").ToLower()

$query = "sub=$CameraID&sid=$SessionID&exp=$exp&scope=hls&kid=$Kid&sig=$sig"
$url = "http://localhost:8081/hls/live/tenant1/$CameraID/$SessionID/index.m3u8?$query"

Write-Host "`n--- HLS Verification Info ---" -ForegroundColor Cyan
Write-Host "Canonical String: $canonical"
Write-Host "HMAC Secret:      $Secret"
Write-Host "Signature:        $sig"
Write-Host "`nSigned URL (Playlist):" -ForegroundColor Green
Write-Host $url
Write-Host "`nTo verify with curl (Cookie check):" -ForegroundColor Yellow
Write-Host "1. Fetch Playlist & Save Cookie:"
Write-Host "   curl -v -c hls_cookies.txt `"$url`" -H `"Authorization: Bearer <YOUR_JWT_HERE>`""
Write-Host "2. Fetch Segment with Cookie:"
Write-Host "   curl -v -b hls_cookies.txt `"http://localhost:8081/hls/live/tenant1/$CameraID/$SessionID/segment_0.m4s`" -H `"Authorization: Bearer <YOUR_JWT_HERE>`""
