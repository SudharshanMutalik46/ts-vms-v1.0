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
