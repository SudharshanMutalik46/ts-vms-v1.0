#pragma once
#include <string>
#include <chrono>

namespace ts {
namespace vms {
namespace recording {

struct FinalizedSegmentInfo {
    std::string segment_id;
    std::string camera_id;
    std::string final_path;
    std::string container;
    std::string checksum_sha256;
    std::chrono::system_clock::time_point start_time_utc;
    std::chrono::system_clock::time_point end_time_utc;
    int64_t duration_ms = 0;
    uint64_t size_bytes = 0;
};

}
}
}
