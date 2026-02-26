#pragma once
#include <string>
#include <vector>

namespace ts {
namespace vms {
namespace recording {

struct ScanReport {
    int tmp_deleted = 0;
    int mp4_quarantined = 0;
    std::vector<std::string> affected_paths;
};

class StartupScanner {
public:
    // Scans a directory recursively for orphaned .tmp and corrupt .mp4
    static ScanReport ScanAndClean(const std::string& root_dir, int tmp_ttl_minutes);

private:
    static bool IsValidMP4(const std::string& filepath);
};

}
}
}
