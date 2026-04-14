# gen-admin-token.ps1
$ErrorActionPreference = "Stop"

Write-Host "Generating Admin Token..." -ForegroundColor Cyan

# Use PSScriptRoot to find the project root
$Root = Split-Path $PSScriptRoot -Parent
Push-Location $Root

try {
    # Run the generator
    $Output = go run scripts/gen-dev-token.go
    $TokenLine = $Output | Select-String "Token: "
    if ($TokenLine) {
        $Token = $TokenLine.ToString().Replace("Token: ", "").Trim()
        $Token | Out-File "token.txt" -Force -Encoding ascii
        Write-Host "Token saved to 'token.txt'" -ForegroundColor Green
        Write-Host "Token: $Token" -ForegroundColor Gray
        
        # Clipboard check for CI/non-interactive envs
        try {
            Set-Clipboard -Value $Token -ErrorAction SilentlyContinue
            Write-Host "(Token copied to clipboard)" -ForegroundColor Yellow
        } catch {}
    }
    else {
        Write-Error "Failed to generate token. Output: $Output"
    }
}
finally {
    Pop-Location
}
