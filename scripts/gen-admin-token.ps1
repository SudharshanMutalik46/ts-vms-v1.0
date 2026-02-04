$ErrorActionPreference = "Stop"

Write-Host "Generating Admin Token..." -ForegroundColor Cyan

# Run the Go script and capture output
# We use Invoke-Expression or just run it.
# Ensure we are in root
$Root = "c:\Users\sudha\Desktop\ts_vms_1.0"
Set-Location $Root

$Output = go run scripts/gen-dev-token.go
$TokenLine = $Output | Select-String "Token: "
if ($TokenLine) {
    $Token = $TokenLine.ToString().Replace("Token: ", "").Trim()
    $Token | Out-File "token.txt" -Force -Encoding ascii
    Write-Host "Token saved to 'token.txt'" -ForegroundColor Green
    Write-Host "Token: $Token" -ForegroundColor Gray
    Set-Clipboard -Value $Token
    Write-Host "(Token copied to clipboard)" -ForegroundColor Yellow
}
else {
    Write-Error "Failed to generate token. Output: $Output"
}
