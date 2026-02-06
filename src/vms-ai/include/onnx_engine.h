#pragma once
#include <string>
#include <vector>
#include <optional>
#include "config.h"
#include "image_processor.h"

// Forward decl for internal ORT types
namespace Ort { class Session; class Env; class Value; }

namespace vms_ai {

struct Detection {
    std::string label;
    float confidence;
    struct BBox {
        float x, y, w, h;
    } bbox;
};

class ONNXEngine {
public:
    explicit ONNXEngine(const Config& config);
    ~ONNXEngine();

    bool Initialize();

    // Runs inference with adaptive timeouts
    // Returns detections or empty on failure/timeout
    std::vector<Detection> RunInference(const ImageTensor& tensor, const std::string& stream_type);

private:
    Config config_;
    struct Impl;
    Impl* impl_; // PIMPL to hide ORT headers
};

} // namespace vms_ai
