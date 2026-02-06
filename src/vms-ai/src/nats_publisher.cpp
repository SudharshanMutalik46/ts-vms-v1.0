#include "nats_publisher.h"
#include <nats/nats.h>
#include <iostream>
#include <thread>
#include <chrono>
#include "metrics_server.h"

namespace vms_ai {

NATSPublisher::NATSPublisher(const Config& config) : config_(config) {}

NATSPublisher::~NATSPublisher() {
    running_ = false;
    if (reconnect_thread_.joinable()) reconnect_thread_.join();
    if (conn_) natsConnection_Destroy(conn_);
}

bool NATSPublisher::PerformConnect() {
    natsStatus s = natsConnection_ConnectTo(&conn_, config_.nats_url.c_str());
    if (s == NATS_OK) {
        std::cout << "[NATS] Connected to " << config_.nats_url << "\n";
        connected_ = true;
        MetricsServer::SetNatsConnected(true);
        return true;
    } else {
        std::cerr << "[NATS] Connect failed: " << natsStatus_GetText(s) << "\n";
        connected_ = false;
        MetricsServer::SetNatsConnected(false);
        return false;
    }
}

void NATSPublisher::ReconnectLoop() {
    int backoff_ms = 250;
    while (running_) {
        if (!connected_) {
            if (PerformConnect()) {
                backoff_ms = 250; // Reset
            } else {
                std::this_thread::sleep_for(std::chrono::milliseconds(backoff_ms));
                backoff_ms *= 2;
                if (backoff_ms > 5000) backoff_ms = 5000; // Cap at 5s
            }
        } else {
            // Check status periodically
            if (natsConnection_Status(conn_) != NATS_CONN_STATUS_CONNECTED) {
                std::cerr << "[NATS] Connection lost. Reconnecting...\n";
                natsConnection_Destroy(conn_);
                conn_ = nullptr;
                connected_ = false;
                MetricsServer::SetNatsConnected(false);
            }
            std::this_thread::sleep_for(std::chrono::seconds(1));
        }
    }
}

void NATSPublisher::PublishDetection(const std::string& subject, const std::string& json_payload) {
    if (!connected_ || !conn_) {
        MetricsServer::IncPublishFailure();
        return; 
    }

    natsStatus s = natsConnection_PublishString(conn_, subject.c_str(), json_payload.c_str());
    if (s != NATS_OK) {
        std::cerr << "[NATS] Publish failed: " << natsStatus_GetText(s) << "\n";
        MetricsServer::IncPublishFailure();
    }
    // Flush handled by NATS client auto-flush or explicitly if needed. 
    // natsConnection_Flush(conn_); usually not needed for high throughput unless immediate delivery required.
}

bool NATSPublisher::IsConnected() const {
    return connected_;
}

} // namespace vms_ai
