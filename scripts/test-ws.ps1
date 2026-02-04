$ErrorActionPreference = "Stop"

try {
    $ws = New-Object System.Net.WebSockets.ClientWebSocket
    $uri = New-Object System.Uri("ws://127.0.0.1:8085/?roomId=test-room&sessionId=test-session")
    $cts = New-Object System.Threading.CancellationTokenSource
    $ws.ConnectAsync($uri, $cts.Token).Wait()

    if ($ws.State -eq 'Open') {
        Write-Host "SUCCESS: WebSocket Connected!"
        $ws.CloseAsync([System.Net.WebSockets.WebSocketCloseStatus]::NormalClosure, "Test finish", $cts.Token).Wait()
    }
    else {
        Write-Error "WebSocket failed to connect. State: $($ws.State)"
    }
}
catch {
    Write-Error "Connection Error: $_"
}
