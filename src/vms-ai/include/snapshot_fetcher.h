#pragma once
#include <string>
#include <vector>
#include <optional>
#include "config.h"

namespace vms_ai {

class SnapshotFetcher {
public:
    explicit SnapshotFetcher(const Config& config);
    ~SnapshotFetcher();

    // Returns JPEG bytes or empty optional on failure/timeout
    std::optional<std::vector<uint8_t>> FetchSnapshot(const std::string& camera_id);

    struct ActiveCamera {
        std::string camera_id;
        std::string tenant_id;
    };

    // Fetches list of cameras requiring AI processing
    std::vector<ActiveCamera> FetchActiveCameras();

private:
    Config config_;
    // WinHTTP session handle
    void* session_handle_ = nullptr;
    void* connect_handle_ = nullptr;
};

} // namespace vms_ai
