#pragma once
#include <vector>
#include <memory>
#include <map>
#include "config.h"
#include "nats_publisher.h"
#include "onnx_engine.h"
#include "snapshot_fetcher.h"
#include "image_processor.h"

namespace vms_ai {

class Scheduler {
public:
    Scheduler(Config& config, 
              std::shared_ptr<NATSPublisher> nats,
              std::shared_ptr<ONNXEngine> engine);
    ~Scheduler();

    void Run(); // Main blocking loop

private:
    struct CameraState {
        std::string id;
        int64_t last_basic_ms = 0;
        int64_t last_weapon_ms = 0;
        bool processing = false; // "One job per camera" rule
    };

    void PollActiveCameras();
    void ProcessCamera(const std::string& camera_id);

    Config config_;
    std::shared_ptr<NATSPublisher> nats_;
    std::shared_ptr<ONNXEngine> engine_;
    std::unique_ptr<SnapshotFetcher> fetcher_;
    std::unique_ptr<ImageProcessor> processor_;

    std::map<std::string, CameraState> cameras_;
};

} // namespace vms_ai
