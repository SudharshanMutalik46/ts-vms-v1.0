#pragma once
#include <string>
#include <vector>
#include <atomic>
#include <mutex>
#include <condition_variable>
#include <thread>
#include <chrono>

namespace ts { namespace vms { namespace diskio {

class AsyncFileWriter {
public:
    AsyncFileWriter(const std::string& camera_id, bool simulate_slow = false);
    ~AsyncFileWriter();

    bool Open(const std::string& path);
    void EnqueueWrite(const uint8_t* data, size_t len);
    void FlushAndWait();
    void Close();

private:
    void SubmitBatch();
    void IocpWorker();

    std::string camera_id_;
    bool simulate_slow_;
    
#ifdef _WIN32
    void* hFile_ = nullptr;
    void* hIocp_ = nullptr;
#endif

    std::thread worker_thread_;
    std::atomic<bool> running_{false};
    std::atomic<int> inflight_count_{0};
    
    std::mutex mu_;
    std::condition_variable cv_;
    
    std::vector<uint8_t> batch_buffer_;
    uint64_t file_offset_ = 0;
    const size_t BLOCK_SIZE = 4 * 1024 * 1024; // 4MB
};
}}}
