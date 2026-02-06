#pragma once
#include <string>
#include <atomic>

namespace vms_ai {

class MetricsServer {
public:
    static void Start(int port);
    
    // Low-cardinality counters
    static void IncFramesDropped(const std::string& stream);
    static void IncPublishFailure();
    static void ObserveInferenceLatency(const std::string& stream, double ms);
    static void SetServiceUp(bool up);
    static void SetNatsConnected(bool connected);
    
private:
    // Simple embedded HTTP server implementation
    static void ServerLoop(int port);
};

} // namespace vms_ai
