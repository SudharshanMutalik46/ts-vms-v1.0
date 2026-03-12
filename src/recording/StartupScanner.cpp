#include "StartupScanner.h"

#include <chrono>
#include <filesystem>
#include <fstream>

namespace fs = std::filesystem;

namespace ts {
namespace vms {
namespace recording {

bool StartupScanner::IsValidMatroskaSegment(const std::string& filepath) {
    std::ifstream file(filepath, std::ios::binary);
    if (!file.is_open()) {
        return false;
    }

    unsigned char header[4]{};
    file.read(reinterpret_cast<char*>(header), sizeof(header));
    if (file.gcount() != 4) {
        return false;
    }

    const bool hasEbmlHeader =
        header[0] == 0x1A &&
        header[1] == 0x45 &&
        header[2] == 0xDF &&
        header[3] == 0xA3;

    if (!hasEbmlHeader) {
        return false;
    }

    file.seekg(0, std::ios::end);
    return file.tellg() > 4;
}

bool StartupScanner::IsValidLegacyMp4(const std::string& filepath) {
    std::ifstream file(filepath, std::ios::binary);
    if (!file.is_open()) {
        return false;
    }

    unsigned char header[32]{};
    file.read(reinterpret_cast<char*>(header), sizeof(header));
    const std::streamsize read = file.gcount();
    if (read < 12) {
        return false;
    }

    for (int i = 4; i <= static_cast<int>(read) - 4; ++i) {
        if (header[i] == 'f' && header[i + 1] == 't' && header[i + 2] == 'y' && header[i + 3] == 'p') {
            file.seekg(0, std::ios::end);
            return file.tellg() > 16;
        }
    }

    return false;
}

ScanReport StartupScanner::ScanAndClean(const std::string& root_dir, int tmp_ttl_minutes) {
    ScanReport report;
    if (!fs::exists(root_dir)) {
        return report;
    }

    const auto now = fs::file_time_type::clock::now();

    for (const auto& entry : fs::recursive_directory_iterator(root_dir)) {
        if (!entry.is_regular_file()) {
            continue;
        }

        const std::string ext = entry.path().extension().string();
        const std::string path = entry.path().string();

        if (ext == ".tmp") {
            const auto ftime = fs::last_write_time(entry);
            const auto age_mins = std::chrono::duration_cast<std::chrono::minutes>(now - ftime).count();

            if (age_mins > tmp_ttl_minutes) {
                fs::remove(entry.path());
                report.tmp_deleted++;
                report.affected_paths.push_back("DELETED TMP: " + path);
            }
            continue;
        }

        if (ext != ".mkv" && ext != ".mp4") {
            continue;
        }

        const bool valid = (ext == ".mkv")
            ? IsValidMatroskaSegment(path)
            : IsValidLegacyMp4(path);

        if (valid) {
            continue;
        }

        const fs::path corrupt_dir = fs::path(root_dir) / "corrupt";
        fs::create_directories(corrupt_dir);

        const fs::path dest = corrupt_dir / entry.path().filename();
        std::error_code ec;
        fs::rename(entry.path(), dest, ec);

        if (!ec) {
            report.video_quarantined++;
            report.affected_paths.push_back("QUARANTINED: " + path);
        }
    }

    return report;
}

}
}
}
