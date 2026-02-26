#include "PreBufferManager.h"
#include "PreBufferRing.h"
#include <algorithm>

namespace ts {
namespace vms {
namespace recording {

PreBufferManager::PreBufferManager(size_t max_bytes) : max_bytes_(max_bytes) {}

void PreBufferManager::RegisterRing(PreBufferRing* ring) {
    std::lock_guard<std::mutex> lock(rings_mu_);
    rings_.push_back(ring);
}

void PreBufferManager::UnregisterRing(PreBufferRing* ring) {
    std::lock_guard<std::mutex> lock(rings_mu_);
    rings_.erase(std::remove(rings_.begin(), rings_.end(), ring), rings_.end());
}

void PreBufferManager::RequestMemory(size_t size) {
    if (current_bytes_ + size > max_bytes_) {
        ForceEvict(size);
    }
    current_bytes_ += size;
}

void PreBufferManager::ReleaseMemory(size_t size) {
    current_bytes_ -= size;
}

void PreBufferManager::ForceEvict(size_t size_needed) {
    std::lock_guard<std::mutex> lock(rings_mu_);
    size_t freed = 0;
    
    // Round-robin eviction to be fair across all cameras
    size_t ring_idx = 0;
    while (freed < size_needed && !rings_.empty()) {
        size_t f = rings_[ring_idx]->EvictOldest();
        if (f == 0) break; // All rings empty
        freed += f;
        ReleaseMemory(f);
        ring_idx = (ring_idx + 1) % rings_.size();
    }
    
    if (freed > 0) {
        std::cerr << "[PreBufferManager] GLOBAL EVICTION TRIGGERED: Freed " << freed << " bytes.\n";
    }
}

}
}
}
