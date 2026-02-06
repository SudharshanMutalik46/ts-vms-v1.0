#include "post_processor.h"
#include <iostream>
#include <algorithm>

namespace vms_ai {

// COCO Labels (Partial) + Custom Mapping
std::string GetLabel(int class_id) {
    // Basic mapping logic
    // 1: person, 3: car, etc.
    // Return string
    switch(class_id) {
        case 1: return "person";
        case 3: return "car";
        case 6: return "bus";
        case 8: return "truck";
        case 4: return "motorcycle";
        case 2: return "bicycle";
        case 17: return "cat";
        case 18: return "dog";
        case 16: return "bird";
        case 27: return "bag";
        default: return "unknown";
    }
}

nlohmann::json PostProcessor::FormatDetection(
    const std::string& camera_id,
    const std::string& stream_type,
    const std::vector<Detection>& raw_detections,
    int64_t ts_ms
) {
    nlohmann::json root;
    root["camera_id"] = camera_id;
    root["ts_unix_ms"] = ts_ms;
    root["stream"] = stream_type;
    
    std::vector<nlohmann::json> objects;
    objects.reserve(raw_detections.size());

    int count = 0;
    const int MAX_OBJECTS = 50;
    
    for (const auto& det : raw_detections) {
        if (count >= MAX_OBJECTS) break; // Guardrail: Max 50
        
        // Guardrail: BBox Validation
        if (det.bbox.w <= 0 || det.bbox.h <= 0) continue;
        if (det.bbox.x + det.bbox.w > 1.01f) continue; // small epsilon okay
        if (det.bbox.y + det.bbox.h > 1.01f) continue;

        nlohmann::json obj;
        obj["label"] = det.label;
        obj["confidence"] = det.confidence;
        obj["bbox"] = {
            {"x", det.bbox.x},
            {"y", det.bbox.y},
            {"w", det.bbox.w},
            {"h", det.bbox.h}
        };
        objects.push_back(std::move(obj));
        count++;
    }
    
    root["objects"] = std::move(objects);
    
    // Guardrail: JSON Size
    std::string dump = root.dump();
    if (dump.size() > 8192) {
        // Truncate logic: remove objects until fit
        // For MVP, just send empty objects list or head
        std::cerr << "[PostProcessor] Payload too big (" << dump.size() << " > 8KB). Truncating objects.\n";
        root["objects"] = nlohmann::json::array();
    }
    
    return root;
}

} // namespace vms_ai
