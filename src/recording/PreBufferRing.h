#pragma once
#include <gst/gst.h>
#include <deque>
#include <mutex>
#include <chrono>
#include <vector>
#include <string>

namespace ts {
namespace vms {
namespace recording {

struct BufferedFrame {
    GstBuffer* buffer;
    bool is_keyframe;
    std::chrono::system_clock::time_point timestamp;
    size_t size_bytes;
};

class PreBufferManager;

class PreBufferRing {
public:
    PreBufferRing(std::string camera_id, int target_seconds, PreBufferManager* manager);
    ~PreBufferRing();

    void PushFrame(GstBuffer* buf);
    
    // Evicts the absolute oldest frame in this specific ring
    size_t EvictOldest();
    
    // Extracts frames for event backfill, starting from the nearest IDR
    std::vector<GstBuffer*> ExtractBackfill(int seconds_to_backfill);

    std::string GetCameraId() const { return camera_id_; }

private:
    void EnforceTimeLimit();

    std::string camera_id_;
    int target_seconds_;
    PreBufferManager* manager_;
    
    std::deque<BufferedFrame> ring_;
    std::mutex mu_;
};

}
}
}
