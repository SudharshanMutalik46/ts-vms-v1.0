#include <iostream>
#include <string>
#include <memory>
#include <grpcpp/grpcpp.h>
#include <gst/gst.h>
#include <spdlog/spdlog.h>
#include "service/media_service.hpp"
#include "utils/logger.hpp"
#include "utils/metrics.hpp"

// Simple CLI parser
struct Config {
    std::string grpc_addr = "0.0.0.0:50051";
    std::string metrics_addr = "0.0.0.0:9091";
    std::string log_level = "info";
    size_t max_pipelines = 256;
    int max_starts_per_minute = 60;
};

Config ParseArgs(int argc, char** argv) {
    Config cfg;
    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];
        if (arg == "--grpc-addr" && i + 1 < argc) cfg.grpc_addr = argv[++i];
        else if (arg == "--metrics-addr" && i + 1 < argc) cfg.metrics_addr = argv[++i];
        else if (arg == "--log-level" && i + 1 < argc) cfg.log_level = argv[++i];
        else if (arg == "--max-pipelines" && i + 1 < argc) cfg.max_pipelines = std::stoul(argv[++i]);
        else if (arg == "--max-starts-per-minute" && i + 1 < argc) cfg.max_starts_per_minute = std::stoi(argv[++i]);
    }
    return cfg;
}

int main(int argc, char** argv) {
    Config cfg = ParseArgs(argc, argv);

    // Initialize GStreamer
    gst_init(&argc, &argv);

    // Initialize Utilities
    ts::vms::media::utils::Logger::Init(cfg.log_level);
    ts::vms::media::utils::Metrics::Instance().Init(cfg.metrics_addr);

    spdlog::info("Starting Techno Support VMS Media Plane Service");
    spdlog::info("gRPC address: {}", cfg.grpc_addr);
    spdlog::info("Metrics address: {}", cfg.metrics_addr);

    // Initialize Manager and Service
    auto manager = std::make_shared<ts::vms::media::service::IngestManager>(cfg.max_pipelines, cfg.max_starts_per_minute);
    ts::vms::media::service::MediaServiceImpl service(manager);

    // Build Server
    grpc::ServerBuilder builder;
    builder.AddListeningPort(cfg.grpc_addr, grpc::InsecureServerCredentials());
    builder.RegisterService(&service);

    std::unique_ptr<grpc::Server> server(builder.BuildAndStart());
    if (!server) {
        spdlog::error("Failed to start gRPC server");
        return 1;
    }

    spdlog::info("Media Plane Service is running");
    server->Wait();

    return 0;
}
