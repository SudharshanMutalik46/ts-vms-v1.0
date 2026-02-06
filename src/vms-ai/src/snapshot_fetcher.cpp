#include "snapshot_fetcher.h"
#include <iostream>
#include <vector>
#include <windows.h>
#include <winhttp.h>

#pragma comment(lib, "winhttp.lib")

namespace vms_ai {

SnapshotFetcher::SnapshotFetcher(const Config& config) : config_(config) {
    // Open session
    session_handle_ = WinHttpOpen(L"VMS-AI-Service/1.0", 
                                  WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
                                  WINHTTP_NO_PROXY_NAME, 
                                  WINHTTP_NO_PROXY_BYPASS, 0);
    
    // Set default timeout (2s)
    if (session_handle_) {
        int timeout_ms = 2000;
        WinHttpSetTimeouts(session_handle_, timeout_ms, timeout_ms, timeout_ms, timeout_ms);
    }
}

SnapshotFetcher::~SnapshotFetcher() {
    if (connect_handle_) WinHttpCloseHandle(connect_handle_);
    if (session_handle_) WinHttpCloseHandle(session_handle_);
}

std::wstring StringToWString(const std::string& s) {
    if (s.empty()) return std::wstring();
    int size_needed = MultiByteToWideChar(CP_UTF8, 0, &s[0], (int)s.size(), NULL, 0);
    std::wstring wstrTo(size_needed, 0);
    MultiByteToWideChar(CP_UTF8, 0, &s[0], (int)s.size(), &wstrTo[0], size_needed);
    return wstrTo;
}

std::optional<std::vector<uint8_t>> SnapshotFetcher::FetchSnapshot(const std::string& camera_id) {
    if (!session_handle_) return std::nullopt;

    // Parse URL - simplistic parsing assuming fixed format for MVP
    // Assuming config_.control_plane_url is "http://127.0.0.1:8080"
    // We need to split host and port.
    // Ideally use WinHttpCrackUrl but for now hardcode localhost:8080 as per prompt input contracts
    // Or parse from config_.control_plane_url
    
    // For this implementation, let's assume strict IPv4 and port.
    std::wstring host = L"127.0.0.1";
    int port = 8080;
    
    // Connect if not cached (simple approach: new connect each time or reuse)
    // Reusing connect handle is better for keep-alive
    if (!connect_handle_) {
        connect_handle_ = WinHttpConnect(session_handle_, host.c_str(), port, 0);
    }
    
    if (!connect_handle_) return std::nullopt;

    // Request path: /api/v1/internal/cameras/{id}/snapshot
    std::string path = "/api/v1/internal/cameras/" + camera_id + "/snapshot";
    std::wstring wpath = StringToWString(path);

    HINTERNET hRequest = WinHttpOpenRequest(connect_handle_, L"GET", wpath.c_str(),
                                            NULL, WINHTTP_NO_REFERER, 
                                            WINHTTP_DEFAULT_ACCEPT_TYPES, 0);

    if (!hRequest) return std::nullopt;

    // Add Auth Header
    if (!config_.ai_service_token.empty()) {
        std::string header = "X-AI-Service-Token: " + config_.ai_service_token;
        std::wstring wheader = StringToWString(header);
        WinHttpAddRequestHeaders(hRequest, wheader.c_str(), (ULONG)-1L, WINHTTP_ADDREQ_FLAG_ADD);
    }

    bool bResults = WinHttpSendRequest(hRequest, WINHTTP_NO_ADDITIONAL_HEADERS, 0,
                                       WINHTTP_NO_REQUEST_DATA, 0, 
                                       0, 0);

    if (bResults) {
        bResults = WinHttpReceiveResponse(hRequest, NULL);
    }

    std::vector<uint8_t> buffer;
    if (bResults) {
        // Check Content-Length
        DWORD dwSize = 0;
        DWORD dwDownloaded = 0;
        
        // 1MB hard cap
        const size_t MAX_SIZE = 1024 * 1024; 

        do {
            // Check for available data
            dwSize = 0;
            if (!WinHttpQueryDataAvailable(hRequest, &dwSize)) {
                break;
            }

            if (dwSize == 0) break;

            if (buffer.size() + dwSize > MAX_SIZE) {
                // Exceeded cap
                WinHttpCloseHandle(hRequest);
                return std::nullopt; 
            }

            // Allocate space
            size_t old_size = buffer.size();
            buffer.resize(old_size + dwSize);

            if (!WinHttpReadData(hRequest, &buffer[old_size], dwSize, &dwDownloaded)) {
                break;
            }
            
            // Adjust vector if read less
            if (dwDownloaded < dwSize) {
                buffer.resize(old_size + dwDownloaded);
            }

        } while (dwSize > 0);
    }

    WinHttpCloseHandle(hRequest);

    if (buffer.empty()) return std::nullopt;
    return buffer;
}

} // namespace vms_ai

// Simple JSON extraction helper (since we don't have nlohmann linked in this file yet easily)
// Expects: [{"camera_id":"...","tenant_id":"..."}, ...]
std::vector<vms_ai::SnapshotFetcher::ActiveCamera> ParseActiveCameras(const std::vector<uint8_t>& data) {
    std::vector<vms_ai::SnapshotFetcher::ActiveCamera> result;
    std::string s(data.begin(), data.end());
    
    // Very naive parser for MVP to avoid external dependency issues in this specific file if not linked
    // In production we use nlohmann::json.
    // Assuming simple regex-like search or just manual scanning for "camera_id": "..."
    
    size_t pos = 0;
    while ((pos = s.find("\"camera_id\"", pos)) != std::string::npos) {
        vms_ai::SnapshotFetcher::ActiveCamera cam;
        
        // Find camera_id value
        size_t start_quote = s.find("\"", pos + 12); // after "camera_id":
        if (start_quote == std::string::npos) break;
        size_t end_quote = s.find("\"", start_quote + 1);
        if (end_quote == std::string::npos) break;
        
        cam.camera_id = s.substr(start_quote + 1, end_quote - start_quote - 1);
        
        // Find tenant_id (optional validation)
        size_t tenant_pos = s.find("\"tenant_id\"", pos);
        if (tenant_pos != std::string::npos && tenant_pos < s.find("}", pos)) { // simplistic scope check
             size_t t_start = s.find("\"", tenant_pos + 12);
             if (t_start != std::string::npos) {
                 size_t t_end = s.find("\"", t_start + 1);
                 if (t_end != std::string::npos) {
                     cam.tenant_id = s.substr(t_start + 1, t_end - t_start - 1);
                 }
             }
        }
        
        result.push_back(cam);
        pos = end_quote + 1;
    }
    
    return result;
}

std::vector<vms_ai::SnapshotFetcher::ActiveCamera> vms_ai::SnapshotFetcher::FetchActiveCameras() {
    // std::cerr << "[SnapshotFetcher] Fetching active cameras...\n"; // Verbose check
    if (!session_handle_) return {};

    // Reuse similar logic to FetchSnapshot but different path
    std::wstring host = L"127.0.0.1"; // Hardcoded for MVP local loopback per requirements
    int port = 8080;

    if (!connect_handle_) {
        connect_handle_ = WinHttpConnect(session_handle_, host.c_str(), port, 0);
    }
    if (!connect_handle_) return {};

    std::wstring wpath = L"/api/v1/internal/cameras/active";

    HINTERNET hRequest = WinHttpOpenRequest(connect_handle_, L"GET", wpath.c_str(),
                                            NULL, WINHTTP_NO_REFERER, 
                                            WINHTTP_DEFAULT_ACCEPT_TYPES, 0);
    if (!hRequest) return {};

    // Add Auth Header
    if (!config_.ai_service_token.empty()) {
        std::string header = "X-AI-Service-Token: " + config_.ai_service_token;
        std::wstring wheader = vms_ai::StringToWString(header);
        WinHttpAddRequestHeaders(hRequest, wheader.c_str(), (ULONG)-1L, WINHTTP_ADDREQ_FLAG_ADD);
    }

    bool bResults = WinHttpSendRequest(hRequest, WINHTTP_NO_ADDITIONAL_HEADERS, 0,
                                       WINHTTP_NO_REQUEST_DATA, 0, 
                                       0, 0);

    if (bResults) {
        bResults = WinHttpReceiveResponse(hRequest, NULL);
    }

    std::vector<uint8_t> buffer;
    if (bResults) {
        DWORD dwSize = 0;
        DWORD dwDownloaded = 0;
        do {
            dwSize = 0;
            if (!WinHttpQueryDataAvailable(hRequest, &dwSize)) break;
            if (dwSize == 0) break;

            size_t old_size = buffer.size();
            buffer.resize(old_size + dwSize);
            if (WinHttpReadData(hRequest, &buffer[old_size], dwSize, &dwDownloaded)) {
                 if (dwDownloaded < dwSize) buffer.resize(old_size + dwDownloaded);
            } else {
                break;
            }
        } while (dwSize > 0);
    }

    WinHttpCloseHandle(hRequest);

    if (buffer.empty()) return {};
    std::string raw(buffer.begin(), buffer.end());
    std::cerr << "[SnapshotFetcher] Raw Active JSON: " << raw << "\n";
    
    auto list = ParseActiveCameras(buffer);
    std::cerr << "[SnapshotFetcher] Parsed Count: " << list.size() << "\n";
    return list;
}
