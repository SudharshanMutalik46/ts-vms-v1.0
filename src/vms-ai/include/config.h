#pragma once
#include <string>
#include <vector>

namespace vms_ai {

struct Config {
    std::string nats_url = "nats://127.0.0.1:4222";
    std::string control_plane_url = "http://127.0.0.1:8080";
    std::string ai_service_token;
    
    int max_cameras = 8;
    int basic_interval_ms = 2000;
    int weapon_interval_ms = 4000;
    
    bool enable_weapon_ai = false;
    
    // Model paths
    std::string model_basic_path = "models/basic/mobilenet_ssd_v2.onnx";
    std::string model_weapon_path = "models/weapon/weapon_detector.onnx";

    static Config LoadFromEnv();
};

} // namespace vms_ai
