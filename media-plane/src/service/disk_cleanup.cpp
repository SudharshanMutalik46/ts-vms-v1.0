#include "disk_cleanup.hpp"
#include "utils/logger.hpp"
#include "utils/metrics.hpp"
#include <filesystem>
#include <vector>
#include <algorithm>
#include <chrono>
#include <thread>
#include <spdlog/spdlog.h>

namespace fs = std::filesystem;

namespace ts::vms::media::service {

struct SessionInfo {
    fs::path path;
    uint64_t size_bytes;
    std::chrono::system_clock::time_point last_write_time;
};

DiskCleanupManager::DiskCleanupManager(const DiskCleanupConfig& config) : config_(config) {}

DiskCleanupManager::~DiskCleanupManager() {
    Stop();
}

void DiskCleanupManager::Start() {
    if (running_) return;
    running_ = true;
    worker_ = std::thread(&DiskCleanupManager::RunLoop, this);
    spdlog::info("DiskCleanupManager started. Root: {}, Limit: {} GB", config_.root_dir, config_.max_size_bytes / 1024 / 1024 / 1024);
}

void DiskCleanupManager::Stop() {
    running_ = false;
    if (worker_.joinable()) worker_.join();
}

void DiskCleanupManager::RunLoop() {
    while (running_) {
        std::this_thread::sleep_for(std::chrono::milliseconds(config_.cleanup_interval_ms));
        if (!running_) break;
        
        try {
            PerformCleanup();
        } catch (const std::exception& e) {
            spdlog::error("DiskCleanupManager exception: {}", e.what());
            utils::Metrics::Instance().hls_disk_cleanup_failures_total().Increment();
        }
    }
}

uint64_t DiskCleanupManager::CalculateDirectorySize(const std::string& path) {
    uint64_t size = 0;
    try {
        for (const auto& entry : fs::recursive_directory_iterator(path)) {
            if (fs::is_regular_file(entry)) {
                size += entry.file_size();
            }
        }
    } catch (...) {}
    return size;
}

void DiskCleanupManager::PerformCleanup() {
    fs::path root(config_.root_dir);
    if (!fs::exists(root)) return;

    std::vector<SessionInfo> sessions;
    uint64_t total_size = 0;
    auto now = std::chrono::system_clock::now();
    uint32_t ops_budget = config_.max_delete_per_tick;

    // Scan
    try {
        for (const auto& cam_entry : fs::directory_iterator(root / "live")) {
            if (!cam_entry.is_directory()) continue;

            for (const auto& session_entry : fs::directory_iterator(cam_entry)) {
                if (!session_entry.is_directory()) continue;
                
                // Get Last Write Time safetly
                std::chrono::system_clock::time_point lwt;
                try {
                     auto ftime = fs::last_write_time(session_entry);
                     // Conversion from file_clock to system_clock is platform dependent, 
                     // but for comparison we can use relative duration if we stick to one clock.
                     // C++20 makes this easier, but let's approximate or just use file_clock for diff.
                     // Actually, let's just use file_time_type for comparison.
                } catch(...) { continue; }
                
                // We'll calculate size on demand to save I/O if we don't need it?
                // No, we need total size for Quota.
                uint64_t size = CalculateDirectorySize(session_entry.path().string());
                total_size += size;
                
                // Use file_clock directly for comparison
                auto ftime = fs::last_write_time(session_entry);
                auto age = std::chrono::duration_cast<std::chrono::minutes>(std::chrono::file_clock::now() - ftime);

                // TTL Check
                if (age.count() > config_.retention_minutes && ops_budget > 0) {
                     // Delete immediately (TTL violation)
                     try {
                         uint64_t reclaimed = size; // Approx
                         fs::remove_all(session_entry.path());
                         utils::Metrics::Instance().hls_disk_cleanup_bytes_reclaimed_total().Increment(reclaimed);
                         spdlog::info("Deleted expired session: {}", session_entry.path().string());
                         ops_budget--;
                         continue; // Removed, don't add to list
                     } catch (const std::exception& e) {
                         spdlog::warn("Failed to delete {}: {}", session_entry.path().string(), e.what());
                         utils::Metrics::Instance().hls_disk_cleanup_failures_total().Increment();
                     }
                }
                
                // Store for Quota check
                // Need to convert file_clock to system_clock for struct? Or just make struct use file_clock
                // Simplified:
                SessionInfo info;
                info.path = session_entry.path();
                info.size_bytes = size;
                // info.last_write_time = ... // trickier without C++20 features sometimes.
                // storing age in minutes
                // hack: store age instead of time
                sessions.push_back({session_entry.path(), size, std::chrono::system_clock::now() - age}); 
            }
        }
    } catch (...) {}

    // Quota Enforcement
    if (total_size > config_.max_size_bytes) {
        // Sort by oldest first
        std::sort(sessions.begin(), sessions.end(), [](const SessionInfo& a, const SessionInfo& b) {
            return a.last_write_time < b.last_write_time;
        });

        for (const auto& session : sessions) {
            if (ops_budget == 0) break;
            if (total_size <= config_.max_size_bytes) break;
            
            // Safety: Skip if very recent (< 1 min) - Active Session Protection
            auto age = std::chrono::duration_cast<std::chrono::minutes>(now - session.last_write_time);
            if (age.count() < 1) continue; 

            try {
                fs::remove_all(session.path);
                total_size -= session.size_bytes;
                utils::Metrics::Instance().hls_disk_cleanup_bytes_reclaimed_total().Increment(session.size_bytes);
                spdlog::info("Deleted session for quota: {}", session.path.string());
                ops_budget--;
            } catch (const std::exception& e) {
                 spdlog::warn("Failed to delete {}: {}", session.path.string(), e.what());
                 utils::Metrics::Instance().hls_disk_cleanup_failures_total().Increment();
            }
        }
    }
}

} // namespace ts::vms::media::service
