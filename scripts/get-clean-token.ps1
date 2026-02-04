$env:JWT_SIGNING_KEY = "dev-secret-do-not-use-in-prod"
$TokenRaw = go run scripts/gen-dev-token.go
if ($TokenRaw -match "Token: ([^\s\r\n]+)") {
    $Token = $Matches[1]
    # Verify via curl
    try {
        $resp = Invoke-WebRequest -Uri "http://localhost:8082/api/v1/cameras" -Headers @{Authorization = "Bearer $Token" } -Method Get -ErrorAction Stop
        if ($resp.StatusCode -eq 200) {
            Write-Host "`nCLEAN_TOKEN_START"
            Write-Host $Token
            Write-Host "CLEAN_TOKEN_END"
        }
        else {
            Write-Error "Verification failed with status: $($resp.StatusCode)"
        }
    }
    catch {
        Write-Error "Verification request failed: $_"
    }
}
else {
    Write-Error "Failed to parse token from script output."
}
