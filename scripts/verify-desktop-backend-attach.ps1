# scripts/verify-desktop-backend-attach.ps1
$ErrorActionPreference = "Stop"

Write-Host "=== TS-VMS Desktop Attachment Verification ===" -ForegroundColor Cyan

# 1. Restart Services
Write-Host "1. Restarting Backend Services..."
./scripts/dev-restart.ps1

# 2. Check Ports
$ports = @(8080, 8085, 50051, 6379, 4222, 5432)
foreach ($p in $ports) {
    $conn = Test-NetConnection -ComputerName 127.0.0.1 -Port $p -WarningAction SilentlyContinue
    if ($conn.TcpTestSucceeded) {
        Write-Host "   [PASS] Port $p is listening." -ForegroundColor Green
    }
    else {
        Write-Host "   [FAIL] Port $p is NOT listening." -ForegroundColor Red
        exit 1
    }
}

# 3. Test Login
Write-Host "3. Testing API Login (Admin)..."
$email = $env:VMS_EMAIL
$pass = $env:VMS_PASSWORD
$tid = $env:VMS_TENANT_ID

if (-not $email) { $email = "admin@technosupport.com"; $pass = "password"; $tid = "00000000-0000-0000-0000-000000000001" }

$body = @{
    email     = $email
    password  = $pass
    tenant_id = $tid
} | ConvertTo-Json

try {
    $resp = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/v1/auth/login" -Method Post -Body $body -ContentType "application/json"
    Write-Host "   [PASS] Login successful. Token received." -ForegroundColor Green
}
catch {
    Write-Host "   [FAIL] Login failed: $_" -ForegroundColor Red
    exit 1
}

# 4. Verify Token
Write-Host "4. Verifying Token via /debug/me..."
$headers = @{ Authorization = "Bearer $($resp.access_token)" }
try {
    $debug = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/v1/debug/me" -Headers $headers
    if ($debug -match "Tenant:") {
        Write-Host "   [PASS] Token verified: $debug" -ForegroundColor Green
    }
    else {
        throw "Invalid debug response"
    }
}
catch {
    Write-Host "   [FAIL] Debug check failed: $_" -ForegroundColor Red
    exit 1
}

Write-Host "=== VERIFICATION SUCCESSFUL ===" -ForegroundColor Green
exit 0
