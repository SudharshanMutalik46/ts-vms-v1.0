#include "service/ingest_manager.hpp"
#include "utils/logger.hpp"
#include "utils/metrics.hpp"
#include <spdlog/spdlog.h>
#include <algorithm>
#include <random>

namespace ts::vms::media::service {

IngestManager::IngestManager(size_t max_pipelines, int max_starts_per_minute)
    : max_pipelines_(max_pipelines), max_starts_per_minute_(max_starts_per_minute), running_(true) {
    
    // Start Disk Cleanup
    disk_cleanup_ = std::make_unique<DiskCleanupManager>(DiskCleanupConfig{});
    disk_cleanup_->Start();

    monitor_thread_ = std::thread(&IngestManager::MonitorLoop, this);
}

IngestManager::~IngestManager() {
    running_ = false;
    if (monitor_thread_.joinable()) monitor_thread_.join();
    if (disk_cleanup_) disk_cleanup_->Stop();
    
    std::lock_guard<std::mutex> lock(map_mutex_);
    pipelines_.clear();
}

bool IngestManager::StartIngest(const std::string& camera_id, const std::string& rtsp_url, bool prefer_tcp) {
    {
        std::lock_guard<std::mutex> lock(rate_mutex_);
        auto now = std::chrono::steady_clock::now();
        start_times_.erase(std::remove_if(start_times_.begin(), start_times_.end(),
            [&](const auto& t) { return std::chrono::duration_cast<std::chrono::minutes>(now - t).count() >= 1; }),
            start_times_.end());

        if (start_times_.size() >= static_cast<size_t>(max_starts_per_minute_)) {
            spdlog::warn("[{}] Start rate limit exceeded", camera_id);
            utils::Metrics::Instance().errors_total("rate_limit").Increment();
            return false;
        }
        start_times_.push_back(now);
    }

    std::lock_guard<std::mutex> lock(map_mutex_);
    if (pipelines_.size() >= max_pipelines_) {
        spdlog::warn("[{}] Global pipeline cap reached ({})", camera_id, max_pipelines_);
        utils::Metrics::Instance().errors_total("cap").Increment();
        return false;
    }

    if (pipelines_.find(camera_id) != pipelines_.end()) {
        return true; // Already exists
    }

    pipeline::PipelineConfig config{camera_id, rtsp_url, prefer_tcp};
    auto pipeline = std::make_unique<pipeline::IngestPipeline>(config);
    
    if (pipeline->Start()) {
        pipelines_[camera_id] = std::move(pipeline);
        camera_urls_[camera_id] = rtsp_url;
        camera_tcp_[camera_id] = prefer_tcp;
        reconnect_attempts_[camera_id] = 0;
        utils::Metrics::Instance().pipelines_active().Increment();
        return true;
    }

    return false;
}

void IngestManager::StopIngest(const std::string& camera_id) {
    std::lock_guard<std::mutex> lock(map_mutex_);
    auto it = pipelines_.find(camera_id);
    if (it != pipelines_.end()) {
        it->second->Stop();
        pipelines_.erase(it);
        camera_urls_.erase(camera_id);
        camera_tcp_.erase(camera_id);
        reconnect_attempts_.erase(camera_id);
        last_reconnect_ts_.erase(camera_id);
        utils::Metrics::Instance().pipelines_active().Decrement();
    }
}

std::optional<CameraStatus> IngestManager::GetStatus(const std::string& camera_id) {
    std::lock_guard<std::mutex> lock(map_mutex_);
    auto it = pipelines_.find(camera_id);
    if (it == pipelines_.end()) return std::nullopt;

    return CameraStatus{
        camera_id,
        it->second->GetState(),
        it->second->GetFps(),
        it->second->GetLastFrameTimeMs(),
        reconnect_attempts_[camera_id],
        it->second->GetHlsState(),
        it->second->GetMetrics()
    };
}

std::vector<CameraStatus> IngestManager::ListIngests() {
    std::lock_guard<std::mutex> lock(map_mutex_);
    std::vector<CameraStatus> list;
    for (auto const& [id, pipeline] : pipelines_) {
        list.push_back({
            id,
            pipeline->GetState(),
            pipeline->GetFps(),
            pipeline->GetLastFrameTimeMs(),
            reconnect_attempts_[id],
            pipeline->GetHlsState(),
            pipeline->GetMetrics()
        });
    }
    return list;
}

std::optional<IngestManager::Snapshot> IngestManager::CaptureSnapshot(const std::string& camera_id) {
    std::lock_guard<std::mutex> lock(map_mutex_);
    auto it = pipelines_.find(camera_id);
    if (it == pipelines_.end()) return std::nullopt;

    auto data = it->second->CaptureSnapshot();
    if (!data) return std::nullopt;

    return Snapshot{
        *data,
        std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::system_clock::now().time_since_epoch()).count()
    };
}

IngestManager::Result IngestManager::StartSfuRtpEgress(const std::string& camera_id, const std::string& dst_ip, int dst_port, uint32_t ssrc, uint32_t pt) {
    std::lock_guard<std::mutex> lock(map_mutex_);
    auto it = pipelines_.find(camera_id);
    if (it == pipelines_.end()) {
        spdlog::warn("[IngestManager] StartSfuRtpEgress: Camera {} NOT FOUND in pipelines map. Active pipelines: {}", camera_id, pipelines_.size());
        return Result::CAMERA_NOT_FOUND;
    }

    if (it->second->IsSfuEgressRunning()) return Result::ALREADY_RUNNING;

    pipeline::IngestPipeline::SfuConfig config{dst_ip, dst_port, ssrc, pt};
    if (it->second->StartSfuRtpEgress(config)) {
        return Result::SUCCESS;
    }

    return Result::FAILED;
}

void IngestManager::StopSfuRtpEgress(const std::string& camera_id) {
    std::lock_guard<std::mutex> lock(map_mutex_);
    auto it = pipelines_.find(camera_id);
    if (it != pipelines_.end()) {
        it->second->StopSfuRtpEgress();
    }
}

void IngestManager::MonitorLoop() {
    while (running_) {
        std::this_thread::sleep_for(std::chrono::seconds(1));
        
        std::vector<std::string> to_reconnect;
        {
            std::lock_guard<std::mutex> lock(map_mutex_);
            auto now = std::chrono::steady_clock::now();
            
            for (auto const& [id, pipeline] : pipelines_) {
                auto state = pipeline->GetState();
                
                // Reset backoff after 30s of stable RUNNING
                if (state == pipeline::State::RUNNING && pipeline->GetLastFrameTimeMs() < 5000) {
                    if (reconnect_attempts_[id] > 0) {
                        auto last_reconnect = last_reconnect_ts_[id];
                        if (std::chrono::duration_cast<std::chrono::seconds>(now - last_reconnect).count() >= 30) {
                            reconnect_attempts_[id] = 0;
                            spdlog::debug("[{}] Resetting backoff after stable RUNNING", id);
                        }
                    }
                }

                // Stall detection:
                // - STARTING state: Allow 30 seconds for initial connection (slow H.265 cameras)
                // - RUNNING state: 5 seconds no frames triggers reconnect
                if (state == pipeline::State::RUNNING) {
                    if (pipeline->GetLastFrameTimeMs() > 5000) {
                        spdlog::warn("[{}] Stall detected (5s no frames while RUNNING)", id);
                        utils::Metrics::Instance().stalls_total().Increment();
                        to_reconnect.push_back(id);
                    }
                } else if (state == pipeline::State::STARTING) {
                    // Allow more time for initial connection (H.265 cameras may take 60-90s)
                    if (pipeline->GetLastFrameTimeMs() > 90000) {
                        spdlog::warn("[{}] Connection timeout (90s no frames while STARTING)", id);
                        utils::Metrics::Instance().stalls_total().Increment();
                        to_reconnect.push_back(id);
                    }
                } else if (state == pipeline::State::RECONNECTING) {
                    to_reconnect.push_back(id);
                }
            }
        }

        for (const auto& id : to_reconnect) {
            Reconnect(id);
        }

        // Aggregate FPS metric
        double total_fps = 0;
        size_t count = 0;
        {
            std::lock_guard<std::mutex> lock(map_mutex_);
            for (auto const& [id, pipeline] : pipelines_) {
                if (pipeline->GetState() == pipeline::State::RUNNING) {
                    total_fps += pipeline->GetFps();
                    count++;
                }
            }
        }
        if (count > 0) utils::Metrics::Instance().ingest_fps_avg().Set(total_fps / count);
        else utils::Metrics::Instance().ingest_fps_avg().Set(0);
    }
}

void IngestManager::Reconnect(const std::string& camera_id) {
    std::lock_guard<std::mutex> lock(map_mutex_);
    auto it = pipelines_.find(camera_id);
    if (it == pipelines_.end()) return;

    auto now = std::chrono::steady_clock::now();
    int attempts = reconnect_attempts_[camera_id];
    
    // Check if enough time has passed based on backoff
    if (last_reconnect_ts_.find(camera_id) != last_reconnect_ts_.end()) {
        int backoff_sec = CalculateBackoff(attempts);
        if (std::chrono::duration_cast<std::chrono::seconds>(now - last_reconnect_ts_[camera_id]).count() < backoff_sec) {
            return;
        }
    }

    spdlog::info("[{}] Attempting reconnection (attempt {})", camera_id, attempts + 1);
    utils::Metrics::Instance().reconnects_total().Increment();
    
    it->second->Stop();
    
    pipeline::PipelineConfig config{camera_id, camera_urls_[camera_id], camera_tcp_[camera_id]};
    it->second = std::make_unique<pipeline::IngestPipeline>(config);
    it->second->Start();

    reconnect_attempts_[camera_id]++;
    last_reconnect_ts_[camera_id] = now;
}

int IngestManager::CalculateBackoff(int attempts) {
    if (attempts <= 0) return 1;
    
    // 1, 2, 4, 8, 16, 30 cap
    int backoff = static_cast<int>(std::pow(2, attempts));
    backoff = std::min(backoff, 30);

    // +/- 10% jitter
    static std::mt19937 gen(std::random_device{}()); 
    std::uniform_real_distribution<> dis(0.9, 1.1);
    return static_cast<int>(backoff * dis(gen));
}

} // namespace ts::vms::media::service
