#pragma once
#include <string>
#include <vector>

namespace ts {
namespace vms {
namespace recording {

struct ScanReport {
    int tmp_deleted = 0;
    int video_quarantined = 0;
    std::vector<std::string> affected_paths;
};

class StartupScanner {
public:
    static ScanReport ScanAndClean(const std::string& root_dir, int tmp_ttl_minutes);

private:
    static bool IsValidMatroskaSegment(const std::string& filepath);
    static bool IsValidLegacyMp4(const std::string& filepath);
};

}
}
}
