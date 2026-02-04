#pragma once
#include <string>
#include <unordered_map>
#include <mutex>
#include <thread>
#include <atomic>
#include <optional>
#include <vector>
#include "pipeline/ingest_pipeline.hpp"
#include "service/disk_cleanup.hpp"

namespace ts::vms::media::service {

struct CameraStatus {
    std::string camera_id;
    pipeline::State state;
    double fps;
    int64_t last_frame_age_ms;
    int reconnect_attempts;
    pipeline::IngestPipeline::HlsState hls_state;
};

class IngestManager {
public:
    IngestManager(size_t max_pipelines, int max_starts_per_minute);
    ~IngestManager();

    bool StartIngest(const std::string& camera_id, const std::string& rtsp_url, bool prefer_tcp);
    void StopIngest(const std::string& camera_id);
    
    std::optional<CameraStatus> GetStatus(const std::string& camera_id);
    std::vector<CameraStatus> ListIngests();

    // Snapshot (Phase 2.x/3.x)
    struct Snapshot {
        std::vector<uint8_t> data;
        int64_t timestamp;
    };
    std::optional<Snapshot> CaptureSnapshot(const std::string& camera_id);

    // SFU Egress (Phase 3.4)
    enum class Result { SUCCESS, ALREADY_RUNNING, FAILED, CAMERA_NOT_FOUND };
    Result StartSfuRtpEgress(const std::string& camera_id, const std::string& dst_ip, int dst_port, uint32_t ssrc, uint32_t pt);
    void StopSfuRtpEgress(const std::string& camera_id);

private:
    void MonitorLoop();
    void Reconnect(const std::string& camera_id);
    int CalculateBackoff(int attempts);

    size_t max_pipelines_;
    int max_starts_per_minute_;
    
    std::mutex map_mutex_;
    std::unordered_map<std::string, std::unique_ptr<pipeline::IngestPipeline>> pipelines_;
    std::unordered_map<std::string, int> reconnect_attempts_;
    std::unordered_map<std::string, std::chrono::steady_clock::time_point> last_reconnect_ts_;
    std::unordered_map<std::string, std::string> camera_urls_; // Store URLs for reconnection
    std::unordered_map<std::string, bool> camera_tcp_;

    std::unique_ptr<DiskCleanupManager> disk_cleanup_;

    std::atomic<bool> running_;
    std::thread monitor_thread_;

    // Rate limiting
    std::mutex rate_mutex_;
    std::vector<std::chrono::steady_clock::time_point> start_times_;
};

} // namespace ts::vms::media::service
