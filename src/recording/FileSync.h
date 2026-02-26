#pragma once
#include <string>

namespace ts {
namespace vms {
namespace recording {

class FileSync {
public:
    // Forces the OS to flush file buffers to physical disk
    static bool FlushToDisk(const std::string& filepath);
    
    // Computes a basic SHA256 checksum (stubbed for minimal dependencies)
    static std::string ComputeChecksum(const std::string& filepath);
};

}
}
}
