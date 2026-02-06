#pragma once
#include <string>
#include <atomic>
#include <thread>
#include "config.h"
#include <nats/nats.h>

namespace vms_ai {

class NATSPublisher {
public:
    explicit NATSPublisher(const Config& config);
    ~NATSPublisher();

    bool PerformConnect();
    void PublishDetection(const std::string& subject, const std::string& json_payload);
    
    bool IsConnected() const;

private:
    void ReconnectLoop();
    
    Config config_;
    std::atomic<bool> running_{true};
    std::atomic<bool> connected_{false};
    
    natsConnection* conn_ = nullptr;
    std::thread reconnect_thread_;
};

} // namespace vms_ai
