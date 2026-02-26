#pragma once
#include <atomic>
#include <string>

namespace ts { namespace vms { namespace diskio {
class DiskMetrics {
public:
    DiskMetrics();
    ~DiskMetrics();
    void Update();
    double GetQueueDepth() const { return current_queue_depth_.load(); }
private:
    std::atomic<double> current_queue_depth_{0};
#ifdef _WIN32
    void* pdh_query_ = nullptr;
    void* pdh_counter_ = nullptr;
#endif
};
}}}
