# verify-latency.ps1
# Verifies VMS Phase 3.5 Metrics and Latency Targets

$ControlURL = "http://localhost:8080"
$MaxLatencyMs = 500

Write-Host "Verifying VMS Metrics at $ControlURL/metrics..." -ForegroundColor Cyan

try {
    $response = Invoke-WebRequest -Uri "$ControlURL/metrics" -UseBasicParsing
    $content = $response.Content
}
catch {
    Write-Error "Failed to fetch metrics: $_"
    exit 1
}

# Check for UP metrics
if ($content -match 'vms_metrics_up{component="media"} 1') {
    Write-Host "[OK] Media Plane Metrics UP" -ForegroundColor Green
}
else {
    Write-Host "[FAIL] Media Plane Metrics DOWN or Missing" -ForegroundColor Red
}

if ($content -match 'vms_metrics_up{component="sfu"} 1') {
    Write-Host "[OK] SFU Metrics UP" -ForegroundColor Green
}
else {
    Write-Host "[FAIL] SFU Metrics DOWN (or Auth failed)" -ForegroundColor Red
}

# Parse Latency
# Regex to find: vms_media_ingest_latency_ms{camera_id="<id>"} <value>
$latencyPattern = 'vms_media_ingest_latency_ms\{camera_id="([^"]+)"\}\s+([0-9\.]+)'
$matches = [regex]::Matches($content, $latencyPattern)

if ($matches.Count -eq 0) {
    Write-Host "[WARN] No active cameras found in metrics." -ForegroundColor Yellow
}
else {
    foreach ($match in $matches) {
        $camId = $match.Groups[1].Value
        $val = [double]$match.Groups[2].Value
        
        if ($val -lt $MaxLatencyMs) {
            Write-Host "[PASS] Camera $camId Latency: ${val}ms (< $MaxLatencyMs ms)" -ForegroundColor Green
        }
        else {
            Write-Host "[FAIL] Camera $camId Latency: ${val}ms (> $MaxLatencyMs ms)" -ForegroundColor Red
        }
    }
}

# Parse SFU Stats
if ($content -match 'vms_sfu_rooms ([0-9]+)') {
    Write-Host "SFU Rooms: $($matches[0])"
}
