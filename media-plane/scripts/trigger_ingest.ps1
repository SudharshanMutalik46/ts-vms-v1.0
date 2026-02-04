param(
    [string]$CameraID = "cam_real_1",
    [string]$RtspUrl = "rtsp://192.168.1.181/live/0/SUB",
    [string]$HostAddr = "localhost:50051"
)

# Try to find grpc_cli
$grpcCli = "grpc_cli" # Assume in path
# Check common vcpkg locations if not in path
if (!(Get-Command $grpcCli -ErrorAction SilentlyContinue)) {
    $vcpkgTools = "C:\vcpkg\installed\x64-windows\tools\grpc\grpc_cli.exe"
    if (Test-Path $vcpkgTools) {
        $grpcCli = $vcpkgTools
    }
    else {
        Write-Error "grpc_cli not found. Please install it or provide full path."
        return
    }
}

$protoPath = "proto"
$service = "ts.vms.media.v1.MediaService"
$method = "StartIngest"
$data = "{`"camera_id`": `"$CameraID`", `"rtsp_url`": `"$RtspUrl`"}"

Write-Host "Triggering ingest for $CameraID ..." -ForegroundColor Cyan
& $grpcCli call $HostAddr $service.$method $data --proto_path=$protoPath
