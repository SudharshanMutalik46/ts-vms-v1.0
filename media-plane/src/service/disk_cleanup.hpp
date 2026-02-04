#pragma once
#include <string>
#include <thread>
#include <atomic>
#include <cstdint>

namespace ts::vms::media::service {

struct DiskCleanupConfig {
    std::string root_dir = "C:\\ProgramData\\TechnoSupport\\VMS\\hls";
    uint64_t max_size_bytes = 20ULL * 1024 * 1024 * 1024; // 20 GB
    uint32_t retention_minutes = 60;
    uint32_t cleanup_interval_ms = 10000;
    uint32_t max_delete_per_tick = 50;
};

class DiskCleanupManager {
public:
    explicit DiskCleanupManager(const DiskCleanupConfig& config);
    ~DiskCleanupManager();

    void Start();
    void Stop();

private:
    void RunLoop();
    void PerformCleanup();
    uint64_t CalculateDirectorySize(const std::string& path);
    
    DiskCleanupConfig config_;
    std::thread worker_;
    std::atomic<bool> running_{false};
};

} // namespace ts::vms::media::service
