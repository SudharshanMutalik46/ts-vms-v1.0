#pragma once
#include <atomic>
#include <mutex>
#include <vector>
#include <iostream>

namespace ts {
namespace vms {
namespace recording {

class PreBufferRing;

class PreBufferManager {
public:
    PreBufferManager(size_t max_bytes = 2ULL * 1024 * 1024 * 1024); // 2GB
    void RequestMemory(size_t size);
    void ReleaseMemory(size_t size);
    
    void RegisterRing(PreBufferRing* ring);
    void UnregisterRing(PreBufferRing* ring);

private:
    void ForceEvict(size_t size_needed);

    size_t max_bytes_;
    std::atomic<size_t> current_bytes_{0};
    
    std::mutex rings_mu_;
    std::vector<PreBufferRing*> rings_;
};

}
}
}
