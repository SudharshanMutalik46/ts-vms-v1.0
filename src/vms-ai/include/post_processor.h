#pragma once
#include <vector>
#include <string>
#include "onnx_engine.h"
#include <nlohmann/json.hpp>

namespace vms_ai {

class PostProcessor {
public:
    // Enhances raw detections:
    // 1. Validates BBox [0..1]
    // 2. Maps label IDs to strings
    // 3. Enforces objects cast <= 50
    // 4. Formats as JSON with size guard <= 8KB
    static nlohmann::json FormatDetection(
        const std::string& camera_id,
        const std::string& stream_type,
        const std::vector<Detection>& raw_detections,
        int64_t ts_ms
    );
};

} // namespace vms_ai
