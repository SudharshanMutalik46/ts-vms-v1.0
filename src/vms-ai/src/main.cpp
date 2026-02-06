#include "config.h"
#include "scheduler.h"
#include "metrics_server.h"
#include "nats_publisher.h"
#include "onnx_engine.h"
#include <iostream>
#include <csignal>
#include <atomic>

std::atomic<bool> g_keep_running{true};

void signal_handler(int signal) {
    if (signal == SIGINT || signal == SIGTERM) {
        std::cout << "[AI Service] Shutting down...\n";
        g_keep_running = false;
    }
}

int main() {
    std::cerr << "[AI Service] I AM ALIVE (stderr)" << std::endl;
    std::signal(SIGINT, signal_handler);
    std::signal(SIGTERM, signal_handler);

    std::cout << "--------------------------------------\n";
    std::cout << "   Techno Support VMS AI Service (C++)   \n";
    std::cout << "--------------------------------------\n";

    // 1. Config
    auto config = vms_ai::Config::LoadFromEnv();

    // 2. Metrics
    vms_ai::MetricsServer::Start(9090); // Port 9090 for metrics
    vms_ai::MetricsServer::SetServiceUp(true);

    // 3. Components
    auto nats = std::make_shared<vms_ai::NATSPublisher>(config);
    if (!nats->PerformConnect()) {
        std::cerr << "[AI Service] Initial NATS connection failed, retrying in background...\n";
    }

    auto engine = std::make_shared<vms_ai::ONNXEngine>(config);
    if (!engine->Initialize()) {
         std::cerr << "[AI Service] Failed to initialize ONNX Engine (check models). Exiting.\n";
         return 1;
    }

    // 4. Scheduler (Main Loop)
    vms_ai::Scheduler scheduler(config, nats, engine);
    
    // This blocks until shutdown
    scheduler.Run();

    std::cout << "[AI Service] Graceful exit.\n";
    vms_ai::MetricsServer::SetServiceUp(false);
    return 0;
}
