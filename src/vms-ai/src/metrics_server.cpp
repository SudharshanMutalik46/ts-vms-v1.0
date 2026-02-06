#include "metrics_server.h"
#include <iostream>
#include <sstream>
#include <map>
#include <mutex>

// For valid simple HTTP, we'd implementation a tiny socket listener
// For this MVP phase, we will implement a very basic single-threaded socket listener
// using WinSock2.

#include <winsock2.h>
#include <ws2tcpip.h>

#pragma comment(lib, "Ws2_32.lib")

namespace vms_ai {

// Global metrics state
static struct {
    std::mutex mu;
    std::map<std::string, int> counters;
    std::map<std::string, double> gauges;
} g_metrics;

void MetricsServer::Start(int port) {
    std::thread([port]() { ServerLoop(port); }).detach();
}

void MetricsServer::IncFramesDropped(const std::string& stream) {
    std::lock_guard<std::mutex> lock(g_metrics.mu);
    g_metrics.counters["ai_frames_dropped_total{stream=\"" + stream + "\"}"]++;
}

void MetricsServer::IncPublishFailure() {
    std::lock_guard<std::mutex> lock(g_metrics.mu);
    g_metrics.counters["ai_publish_failures_total"]++;
}

void MetricsServer::SetServiceUp(bool up) {
    std::lock_guard<std::mutex> lock(g_metrics.mu);
    g_metrics.gauges["ai_service_up"] = up ? 1.0 : 0.0;
}

void MetricsServer::SetNatsConnected(bool connected) {
    std::lock_guard<std::mutex> lock(g_metrics.mu);
    g_metrics.gauges["ai_nats_connected"] = connected ? 1.0 : 0.0;
}

void MetricsServer::ObserveInferenceLatency(const std::string& stream, double ms) {
    // Histogram is complex for minimal implementation, using summary counter/sum for now
    std::lock_guard<std::mutex> lock(g_metrics.mu);
    g_metrics.counters["ai_inference_count{stream=\"" + stream + "\"}"]++;
    g_metrics.gauges["ai_inference_latest_ms{stream=\"" + stream + "\"}"] = ms; // Approximate
}

void MetricsServer::ServerLoop(int port) {
    WSADATA wsaData;
    WSAStartup(MAKEWORD(2, 2), &wsaData);

    SOCKET ListenSocket = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    sockaddr_in service;
    service.sin_family = AF_INET;
    service.sin_addr.s_addr = INADDR_ANY;
    service.sin_port = htons(port);

    if (bind(ListenSocket, (SOCKADDR*)&service, sizeof(service)) == SOCKET_ERROR) {
        std::cerr << "[Metrics] Bind failed.\n";
        return;
    }

    if (listen(ListenSocket, 5) == SOCKET_ERROR) { // Support slightly more concurrency
        std::cerr << "[Metrics] Listen failed.\n";
        return;
    }
    
    std::cout << "[Metrics] Listening on port " << port << std::endl;

    while (true) {
        SOCKET ClientSocket = accept(ListenSocket, NULL, NULL);
        if (ClientSocket == INVALID_SOCKET) continue;
        
        // simple timeout for recv to prevent hanging
        DWORD timeout = 1000;
        setsockopt(ClientSocket, SOL_SOCKET, SO_RCVTIMEO, (const char*)&timeout, sizeof(timeout));

        // Read request (naive)
        char recvbuf[1024];
        int bytesReceived = recv(ClientSocket, recvbuf, 1024, 0);
        
        if (bytesReceived > 0) {
             // Generate response
            std::stringstream ss;
            ss << "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\n";
            
            {
                std::lock_guard<std::mutex> lock(g_metrics.mu);
                for (auto const& [key, val] : g_metrics.counters) {
                    ss << key << " " << val << "\n";
                }
                for (auto const& [key, val] : g_metrics.gauges) {
                    ss << key << " " << val << "\n";
                }
            }

            std::string resp = ss.str();
            send(ClientSocket, resp.c_str(), (int)resp.length(), 0);
        }
        
        closesocket(ClientSocket);
    }
    WSACleanup();
}

} // namespace vms_ai
