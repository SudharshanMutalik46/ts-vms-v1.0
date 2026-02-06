$Token = "eyJhbGciOiJIUzI1NiIsImtpZCI6InYxIiwidHlwIjoiSldUIn0.eyJleHAiOjE3NzAyNzU2NTQsImlhdCI6MTc3MDE4OTI1NCwianRpIjoiZjMyODg1ZTEtYWY0YS00YzU1LTg2YTQtM2NjMDBjMTY3NzY0IiwibmJmIjoxNzcwMTg5MjU0LCJzdWIiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDEiLCJ0ZW5hbnRfaWQiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDEiLCJ0b2tlbl90eXBlIjoiYWNjZXNzIn0.AVHHEHfkz6NmIGzlBc6nLG3VuC4H_E8ELd6wcBA1wdo"
$Headers = @{ "Authorization" = "Bearer $Token"; "Content-Type" = "application/json" }

Write-Host "1. Checking Health..."
$health = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/healthz" -Method Get
Write-Host "Health: $($health | ConvertTo-Json -Depth 2)"

Write-Host "`n2. Listing Cameras..."
$cameras = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/cameras" -Method Get -Headers $Headers
# Response is { data: [...], meta: {...} }
if ($null -eq $cameras.data -or $cameras.data.Count -eq 0) {
    Write-Host "No cameras found. Adding one..."
    # Using a dummy UUID for Site ID (hoping FK not enforced or seeded). If fails, we might need a site.
    $siteId = "00000000-0000-0000-0000-000000000001"
    $camBody = @{ 
        site_id    = $siteId
        name       = "Test Cam"
        ip_address = "127.0.0.1"
        port       = 8554
        tags       = @("test")
    } | ConvertTo-Json
    $newCam = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/cameras" -Method Post -Headers $Headers -Body $camBody
    $camId = $newCam.id
    Write-Host "Created Camera ID: $camId"
}
else {
    $camId = $cameras.data[0].id
    Write-Host "Found Camera ID: $camId"
}

Write-Host "`n3. Checking Live Debug..."
try {
    $debug = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/debug/live/$camId" -Method Get -Headers $Headers
    Write-Host "Debug Info: $($debug | ConvertTo-Json -Depth 3)"
}
catch {
    Write-Host "Debug failed: $_"
}

Write-Host "`n4. Attempting Join Room..."
try {
    $body = @{ sessionId = "test-session-manual" } | ConvertTo-Json
    $join = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/sfu/rooms/$camId/join" -Method Post -Headers $Headers -Body $body
    Write-Host "Join Success! Caps received."
}
catch {
    Write-Host "Join Failed (Expected if SFU issues?):"
    # PowerShell 7 puts the response in valid specific property, PS 5 in Exception.Response
    if ($_.Exception.Response) {
        $reader = New-Object System.IO.StreamReader $_.Exception.Response.GetResponseStream()
        $errBody = $reader.ReadToEnd()
        Write-Host "Error Body: $errBody"
    }
}
