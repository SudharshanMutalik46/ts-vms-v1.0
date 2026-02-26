#include "PreBufferRing.h"
#include "PreBufferManager.h"
#include <iostream>

namespace ts {
namespace vms {
namespace recording {

PreBufferRing::PreBufferRing(std::string camera_id, int target_seconds, PreBufferManager* manager)
    : camera_id_(camera_id), target_seconds_(target_seconds), manager_(manager) {
    manager_->RegisterRing(this);
}

PreBufferRing::~PreBufferRing() {
    manager_->UnregisterRing(this);
    std::lock_guard<std::mutex> lock(mu_);
    for (auto& f : ring_) {
        gst_buffer_unref(f.buffer);
        manager_->ReleaseMemory(f.size_bytes);
    }
    ring_.clear();
}

void PreBufferRing::PushFrame(GstBuffer* buf) {
    size_t size = gst_buffer_get_size(buf);
    
    // Ask manager for budget allocation
    manager_->RequestMemory(size);

    BufferedFrame frame;
    frame.buffer = gst_buffer_ref(buf);
    frame.is_keyframe = !GST_BUFFER_FLAG_IS_SET(buf, GST_BUFFER_FLAG_DELTA_UNIT);
    frame.timestamp = std::chrono::system_clock::now();
    frame.size_bytes = size;

    {
        std::lock_guard<std::mutex> lock(mu_);
        ring_.push_back(frame);
    }
    EnforceTimeLimit();
}

void PreBufferRing::EnforceTimeLimit() {
    std::lock_guard<std::mutex> lock(mu_);
    if (ring_.empty()) return;

    auto now = std::chrono::system_clock::now();
    auto limit = now - std::chrono::seconds(target_seconds_);

    while (!ring_.empty() && ring_.front().timestamp < limit) {
        gst_buffer_unref(ring_.front().buffer);
        manager_->ReleaseMemory(ring_.front().size_bytes);
        ring_.pop_front();
    }
}

size_t PreBufferRing::EvictOldest() {
    std::lock_guard<std::mutex> lock(mu_);
    if (ring_.empty()) return 0;
    
    size_t freed = ring_.front().size_bytes;
    gst_buffer_unref(ring_.front().buffer);
    ring_.pop_front();
    return freed;
}

std::vector<GstBuffer*> PreBufferRing::ExtractBackfill(int seconds_to_backfill) {
    std::lock_guard<std::mutex> lock(mu_);
    std::vector<GstBuffer*> backfill;
    if (ring_.empty()) return backfill;

    auto target_time = std::chrono::system_clock::now() - std::chrono::seconds(seconds_to_backfill);
    
    // 1. Find iterator near target time
    auto it = ring_.begin();
    while (it != ring_.end() && it->timestamp < target_time) {
        ++it;
    }

    // 2. IDR Alignment Rule (MANDATORY): Step backwards to find an IDR
    while (it != ring_.begin() && !it->is_keyframe) {
        --it;
    }

    if (!it->is_keyframe) {
        std::cerr << "[PreBuffer] WARNING: No IDR found in backfill window for " << camera_id_ << "\n";
        return backfill; // Option B: Return empty, wait for next live IDR.
    }

    // 3. Clone buffers for writing
    for (; it != ring_.end(); ++it) {
        backfill.push_back(gst_buffer_ref(it->buffer));
    }
    
    return backfill;
}

}
}
}
