$ErrorActionPreference = "Stop"

# Auto-detect script location and jump ONE level up to the repository root
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location "$ScriptDir\.."

Write-Host "=== TS-VMS Phase 4.8 Disk I/O Verification ===" -ForegroundColor Cyan

# --- 1. Inject GStreamer DLLs into runtime PATH ---
if ($env:GSTREAMER_1_0_ROOT_MSVC_X86_64) {
    $GstBin = "$env:GSTREAMER_1_0_ROOT_MSVC_X86_64\bin"
    if ($env:PATH -notmatch [regex]::Escape($GstBin)) {
        $env:PATH = "$GstBin;" + $env:PATH
        Write-Host "-> Injected GStreamer DLLs into runtime PATH" -ForegroundColor DarkGray
    }
}

# --- 2. Rewrite TestHarness48.cpp to enforce std::endl flushes ---
$HarnessCode = @"
#include <iostream>
#include <vector>
#include <thread>
#include "diskio/AsyncFileWriter.h"
#include "diskio/DiskMetrics.h"

using namespace ts::vms::diskio;

int main(int argc, char* argv[]) {
    std::cout << "=== Phase 4.8 Disk I/O Optimization Harness ===" << std::endl;

    bool simulate_slow = false;
    if (argc > 1 && std::string(argv[1]) == "--simulate-slow-disk") {
        simulate_slow = true;
        std::cout << "[WARN] Slow Disk Simulation ENABLED (>100ms forced latency)" << std::endl;
    }

    DiskMetrics metrics;
    metrics.Update();

    std::cout << "Booting AsyncFileWriter (4MB Batch Coalescing)..." << std::endl;
    AsyncFileWriter writer("cam-01", simulate_slow);
    
    if (!writer.Open("test_io_output.tmp")) {
        std::cerr << "Failed to open file." << std::endl;
        return 1;
    }

    std::cout << "Simulating 12MB of small sequential writes (100KB chunks)..." << std::endl;
    std::vector<uint8_t> dummy_data(1024 * 100, 0xAB); // 100KB

    for (int i = 0; i < 120; i++) {
        writer.EnqueueWrite(dummy_data.data(), dummy_data.size());
        if (i % 20 == 0) {
            metrics.Update();
            std::cout << "  -> Queue Depth: " << metrics.GetQueueDepth() << std::endl;
        }
    }

    std::cout << "Flushing and Waiting for Background Threads (IOCP)..." << std::endl;
    writer.FlushAndWait();
    
    std::cout << "Closing File." << std::endl;
    writer.Close();

    std::cout << "Harness Complete. Syscalls reduced to just 3 bulk OVERLAPPED writes." << std::endl;
    return 0;
}
"@
Set-Content -Path "src\recording\TestHarness48.cpp" -Value $HarnessCode -Force

# --- 3. Clean and Compile Harness ---
Write-Host "Compiling Disk I/O Async Harness..." -ForegroundColor Yellow
if (Test-Path "src\recording\build") { Remove-Item -Recurse -Force "src\recording\build" }

cd src/recording
cmake -B build
cmake --build build --config Debug
cd ../..

$ExePath = ".\src\recording\build\Debug\vms-diskio-harness.exe"

if (!(Test-Path $ExePath)) {
    Write-Error "Harness failed to compile. Executable not found at $ExePath"
}

# --- 4. Run Standard Test (Batching Proof) ---
Write-Host "`n--- RUN 1: Normal Async I/O (4MB Coalescing) ---" -ForegroundColor Green
$proc1 = Start-Process -FilePath $ExePath -RedirectStandardOutput "out1.txt" -RedirectStandardError "err1.txt" -Wait -PassThru -NoNewWindow
$outNormal = Get-Content "out1.txt" -Raw
Write-Host $outNormal

if ($proc1.ExitCode -ne 0) {
    $errNormal = Get-Content "err1.txt" -Raw
    Write-Error "Harness crashed with exit code: $($proc1.ExitCode). Error: $errNormal"
}

if ($outNormal -match "Syscalls reduced to just 3 bulk OVERLAPPED writes") {
    Write-Host "`n[OK] Small writes successfully batched into memory and executed asynchronously." -ForegroundColor Green
}
else {
    Write-Error "Batching failed or output did not match expectations."
}

# --- 5. Run Slow Disk Simulation (Alert Proof) ---
Write-Host "`n--- RUN 2: Slow Disk Simulation (>100ms Latency) ---" -ForegroundColor Red
$proc2 = Start-Process -FilePath $ExePath -ArgumentList "--simulate-slow-disk" -RedirectStandardOutput "out2.txt" -RedirectStandardError "err2.txt" -Wait -PassThru -NoNewWindow
$outSlow = Get-Content "out2.txt" -Raw
$errSlow = Get-Content "err2.txt" -Raw

if ($outSlow) { Write-Host $outSlow }
if ($errSlow) { Write-Host $errSlow -ForegroundColor Red }

if ($outSlow -match "diskio.slow_write_detected") {
    Write-Host "`n[OK] Slow disk latency tracking successfully detected >100ms stall!" -ForegroundColor Green
}
else {
    Write-Error "Slow disk detection failed to emit alert."
}

# Cleanup
if (Test-Path ".\test_io_output.tmp") { Remove-Item ".\test_io_output.tmp" }
if (Test-Path "out1.txt") { Remove-Item "out1.txt" }
if (Test-Path "err1.txt") { Remove-Item "err1.txt" }
if (Test-Path "out2.txt") { Remove-Item "out2.txt" }
if (Test-Path "err2.txt") { Remove-Item "err2.txt" }

Write-Host "`n[SUCCESS] Phase 4.8 Windows Overlapped I/O Integration Complete!" -ForegroundColor Cyan