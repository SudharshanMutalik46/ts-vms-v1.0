#pragma once
#include <vector>
#include <cstdint>
#include <optional>

namespace vms_ai {

struct ImageTensor {
    std::vector<float> data; // Normalized CHW or HWC depending on model
    int width;
    int height;
    int channels;
};

class ImageProcessor {
public:
    ImageProcessor();
    ~ImageProcessor();

    // Decodes JPEG bytes to normalized tensor (1x3x300x300 for MobileNet SSD)
    // Returns empty optional on failure (invalid image, truncated)
    std::optional<ImageTensor> DecodeAndPreprocess(const std::vector<uint8_t>& jpeg_bytes, int target_w, int target_h);

private:
    // WIC factory pointer
    struct Impl;
    Impl* impl_;
};

} // namespace vms_ai
