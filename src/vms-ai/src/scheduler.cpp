#include "scheduler.h"
#include "post_processor.h"
#include "metrics_server.h"
#include <iostream>
#include <thread>
#include <chrono>
#include <future>
#include <winhttp.h> // For WinHTTP cleanup if needed

// Simplified JSON parsing to get camera list
// In a real project we'd use nlohmann/json parser on the response from WinHTTP.
// For now, naive string search for MVP or basic json parsing.
#include <nlohmann/json.hpp>

#include <set>

namespace vms_ai {

Scheduler::Scheduler(Config& config, 
                     std::shared_ptr<NATSPublisher> nats,
                     std::shared_ptr<ONNXEngine> engine)
    : config_(config), nats_(nats), engine_(engine) {
    fetcher_ = std::make_unique<SnapshotFetcher>(config);
    processor_ = std::make_unique<ImageProcessor>();
}

Scheduler::~Scheduler() {}

void Scheduler::Run() {
    // Thread pool (fixed 4 threads)
    const int NUM_THREADS = 4;
    // We can use a semaphore or simple counter + std::async
    
    std::cout << "[Scheduler] Starting loop. Max Cameras=" << config_.max_cameras << "\n";

    while (true) {
        auto start_loop = std::chrono::steady_clock::now();

        // 1. Poll Active Cameras (from Control Plane)
        // In a real implementation we call API. 
        // For MVP structure, we'll iterate a known list or assume demand comes from somewhere.
        // Let's assume we implement `PollActiveCameras` properly below.
        // 1. Poll Active Cameras (from Control Plane)
        PollActiveCameras();
        
        // 2. Schedule Jobs
        // "One job per camera per tick" rule.
        int active_jobs = 0;
        std::vector<std::future<void>> futures;

        for (auto& [id, state] : cameras_) {
            if (active_jobs >= NUM_THREADS) break; // Simple concurrency cap for this tick
            
            // Check concurrency limit globally (max 8 cameras active)
            // Already filtered by PollActiveCameras ideally.

            // Check intervals
            auto now = std::chrono::steady_clock::now();
            int64_t now_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now.time_since_epoch()).count();
            
            bool due_basic = (now_ms - state.last_basic_ms >= config_.basic_interval_ms);
            bool due_weapon = config_.enable_weapon_ai && (now_ms - state.last_weapon_ms >= config_.weapon_interval_ms);

            if (due_basic || due_weapon) {
                state.last_basic_ms = now_ms;
                if (due_weapon) state.last_weapon_ms = now_ms;
                
                // Spawn Job
                futures.push_back(std::async(std::launch::async, [this, id, due_weapon]() {
                    ProcessCamera(id);
                }));
                active_jobs++;
            }
        }

        // Wait for tick completion
        for (auto& f : futures) {
            f.wait();
        }
        
        // Sleep remainder of tick (e.g., 100ms polling rate)
        std::this_thread::sleep_for(std::chrono::milliseconds(100)); 
    }
}

void Scheduler::PollActiveCameras() {
    auto active_list = fetcher_->FetchActiveCameras();
    
    // Mark all existing as not seen
    std::set<std::string> seen_ids;

    // Update/Add
    for (const auto& cam : active_list) {
        seen_ids.insert(cam.camera_id);
        
        if (cameras_.find(cam.camera_id) == cameras_.end()) {
             // New camera
             CameraState state;
             state.id = cam.camera_id;
             // active_cameras_ map uses string ID as key
             cameras_[cam.camera_id] = state;
             std::cout << "[Scheduler] Added camera: " << cam.camera_id << "\n";
        }
    }

    // Remove stale
    for (auto it = cameras_.begin(); it != cameras_.end(); ) {
        if (seen_ids.find(it->first) == seen_ids.end()) {
             std::cout << "[Scheduler] Removed camera: " << it->first << "\n";
             it = cameras_.erase(it);
        } else {
             ++it;
        }
    }
}

void Scheduler::ProcessCamera(const std::string& camera_id) {
    // 1. Check NATS
    if (!nats_->IsConnected()) return;

    // 2. Fetch Snapshot
    auto jpeg = fetcher_->FetchSnapshot(camera_id);
    if (!jpeg) {
        MetricsServer::IncFramesDropped("snapshot_fail");
        return;
    }

    // 3. Decode
    // Model expects 1200x1200 (based on debug logs)
    auto tensor = processor_->DecodeAndPreprocess(*jpeg, 1200, 1200);
    if (!tensor) {
        MetricsServer::IncFramesDropped("decode_fail");
        return;
    }

    // 4. Inference & Publish (Basic)
    auto output = engine_->RunInference(*tensor, "basic");
    if (!output.empty()) {
        int64_t now = std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::system_clock::now().time_since_epoch()).count();
        nlohmann::json json = PostProcessor::FormatDetection(camera_id, "basic", output, now);
        nats_->PublishDetection("detections.basic." + camera_id, json.dump());
    }

    // 5. Inference & Publish (Weapon) - if enabled
    if (config_.enable_weapon_ai) {
         // Re-run inference or use separate pass
         // engine_->RunInference(*tensor, "weapon");
    }
}

} // namespace vms_ai
