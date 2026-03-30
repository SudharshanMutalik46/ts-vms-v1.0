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
        it->second->GetMetrics(),
        it->second->GetCodecString()
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
            pipeline->GetMetrics(),
            pipeline->GetCodecString()
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

IngestManager::Result IngestManager::StartSfuRtpEgress(const std::string& camera_id, const std::string& dst_ip, int dst_port, uint32_t ssrc, uint32_t pt, const std::string& codec) {
    std::lock_guard<std::mutex> lock(map_mutex_);
    auto it = pipelines_.find(camera_id);
    if (it == pipelines_.end()) {
        spdlog::warn("[IngestManager] Camera {} not ready yet — queuing SFU egress", camera_id);
        // Store for retry when pipeline is RUNNING
        pending_sfu_egress_[camera_id] = {dst_ip, dst_port, ssrc, pt, codec};
        return Result::SUCCESS; // caller treats as success, delivery is async
    }

    const auto state = it->second->GetState();
    if (state != pipeline::State::RUNNING) {
        spdlog::info(
            "[{}] Deferring SFU egress until pipeline is RUNNING (state={})",
            camera_id,
            static_cast<int>(state));
        pending_sfu_egress_[camera_id] = {dst_ip, dst_port, ssrc, pt, codec};
        return Result::SUCCESS;
    }

    if (it->second->IsSfuEgressRunning()) return Result::ALREADY_RUNNING;

    pipeline::IngestPipeline::SfuConfig config{dst_ip, dst_port, ssrc, pt, codec};
    if (it->second->StartSfuRtpEgress(config)) {
        return Result::SUCCESS;
    }

    // Pipeline returned false — codec not yet detected (tight race between
    // state=RUNNING and OnPadAdded). Store pending; MonitorLoop will drain
    // it on next 1s tick once codec is known and pipeline is stable.
    spdlog::warn("[IngestManager] Camera {} SFU egress deferred (codec UNKNOWN at RUNNING)", camera_id);
    pending_sfu_egress_[camera_id] = {dst_ip, dst_port, ssrc, pt, codec};
    return Result::SUCCESS;
}

void IngestManager::StopSfuRtpEgress(const std::string& camera_id) {
    std::lock_guard<std::mutex> lock(map_mutex_);
    auto it = pipelines_.find(camera_id);
    if (it != pipelines_.end()) {
        it->second->StopSfuRtpEgress();
    }
}

void IngestManager::OnPipelineRunning(const std::string& camera_id) {
    auto it = pending_sfu_egress_.find(camera_id);
    if (it != pending_sfu_egress_.end()) {
        auto& cfg = it->second;
        spdlog::info("[IngestManager] Applying queued SFU egress for {}", camera_id);
        if (auto pit = pipelines_.find(camera_id); pit != pipelines_.end()) {
            pit->second->StartSfuRtpEgress(cfg);
        }
        pending_sfu_egress_.erase(it);
    }
}

void IngestManager::MonitorLoop() {
    while (running_) {
        std::this_thread::sleep_for(std::chrono::milliseconds(200));
        
        std::vector<std::string> to_reconnect;
        std::vector<std::string> just_started;
        {
            std::lock_guard<std::mutex> lock(map_mutex_);
            auto now = std::chrono::steady_clock::now();
            
            for (auto const& [id, pipeline] : pipelines_) {
                auto state = pipeline->GetState();
                
                if (state == pipeline::State::RUNNING) {
                    // Check if HLS needs to be lazily initialized
                    if (pipeline->GetHlsBranchPending()) {
                        spdlog::info("[{}] MonitorLoop: Lazily setting up HLS branch", id);
                        pipeline->SetupHlsBranch();
                        pipeline->ClearHlsBranchPending();
                    }

                    // Check if it was just started or just hit RUNNING
                    if (pending_sfu_egress_.count(id)) {
                        just_started.push_back(id);
                    }

                    // Reset backoff after 30s of stable RUNNING
                    if (pipeline->GetLastFrameTimeMs() < 5000) {
                        if (reconnect_attempts_[id] > 0) {
                            auto last_reconnect = last_reconnect_ts_[id];
                            if (std::chrono::duration_cast<std::chrono::seconds>(now - last_reconnect).count() >= 30) {
                                reconnect_attempts_[id] = 0;
                                spdlog::debug("[{}] Resetting backoff after stable RUNNING", id);
                            }
                        }
                    }

                    const auto stallTimeoutMs = pipeline->IsSfuEgressRunning() ? 20000 : 5000;
                    if (pipeline->GetLastFrameTimeMs() > stallTimeoutMs) {
                        spdlog::warn("[{}] Stall detected ({}s no frames while RUNNING)", id, stallTimeoutMs / 1000);
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

        // Drain pending SFU egress for cameras that just reached RUNNING
        {
            std::lock_guard<std::mutex> lock(map_mutex_);
            for (const auto& id : just_started) {
                OnPipelineRunning(id);
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
