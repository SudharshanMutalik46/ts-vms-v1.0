#include "StartupScanner.h"
#include <filesystem>
#include <fstream>
#include <iostream>
#include <chrono>

namespace fs = std::filesystem;

namespace ts {
namespace vms {
namespace recording {

bool StartupScanner::IsValidMP4(const std::string& filepath) {
    std::ifstream file(filepath, std::ios::binary | std::ios::ate);
    if (!file.is_open()) return false;
    
    std::streamsize size = file.tellg();
    if (size == 0) return false; // 0-byte file is corrupt

    file.seekg(0, std::ios::beg);
    char header[8];
    if (file.read(header, 8)) {
        // ISO Base Media Format signature check: bytes 4-7 should be 'ftyp'
        if (header[4] == 'f' && header[5] == 't' && header[6] == 'y' && header[7] == 'p') {
            return true;
        }
    }
    return false;
}

bool IsValidMKV(const std::string& filepath) {
    std::ifstream file(filepath, std::ios::binary);
    if (!file.is_open()) return false;
    
    unsigned char header[4];
    file.read(reinterpret_cast<char*>(header), 4);
    
    // Check for standard MKV EBML Header: 1A 45 DF A3
    if (file.gcount() == 4 && 
        header[0] == 0x1A && header[1] == 0x45 && 
        header[2] == 0xDF && header[3] == 0xA3) {
        
        // Ensure file isn't 0 bytes
        file.seekg(0, std::ios::end);
        return file.tellg() > 4; 
    }
    return false;
}

ScanReport StartupScanner::ScanAndClean(const std::string& root_dir, int tmp_ttl_minutes) {
    ScanReport report;
    if (!fs::exists(root_dir)) return report;

    auto now = fs::file_time_type::clock::now();

    for (const auto& entry : fs::recursive_directory_iterator(root_dir)) {
        if (!entry.is_regular_file()) continue;

        std::string ext = entry.path().extension().string();
        std::string path = entry.path().string();

        if (ext == ".tmp") {
            auto ftime = fs::last_write_time(entry);
            auto age_mins = std::chrono::duration_cast<std::chrono::minutes>(now - ftime).count();
            
            if (age_mins > tmp_ttl_minutes) {
                fs::remove(entry.path());
                report.tmp_deleted++;
                report.affected_paths.push_back("DELETED TMP: " + path);
            }
        } else if (ext == ".mkv") {
            if (!IsValidMKV(path)) {
                fs::path corrupt_dir = fs::path(root_dir) / "corrupt";
                fs::create_directories(corrupt_dir);
                fs::path dest = corrupt_dir / entry.path().filename();
                
                fs::rename(entry.path(), dest);
                report.mp4_quarantined++;
                report.affected_paths.push_back("QUARANTINED: " + path);
            }
        } else if (ext == ".mp4") {
            if (!IsValidMP4(path)) {
                fs::path corrupt_dir = fs::path(root_dir) / "corrupt";
                fs::create_directories(corrupt_dir);
                fs::path dest = corrupt_dir / entry.path().filename();
                
                fs::rename(entry.path(), dest);
                report.mp4_quarantined++;
                report.affected_paths.push_back("QUARANTINED: " + path);
            }
        }
    }
    return report;
}

}
}
}
