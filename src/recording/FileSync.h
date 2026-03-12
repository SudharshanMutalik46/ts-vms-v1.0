#pragma once
#include <string>

namespace ts {
namespace vms {
namespace recording {

class FileSync {
public:
    // Forces OS buffers for an existing file to durable storage.
    static bool FlushToDisk(const std::string& filepath);

    // Computes a real SHA-256 digest for the finalized archive file.
    // Returns empty string on failure.
    static std::string ComputeChecksum(const std::string& filepath);
};

}
}
}
