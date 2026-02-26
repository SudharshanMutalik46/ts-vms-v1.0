#include "AsyncFileWriter.h"
#include <iostream>
#include <cstring>
#ifdef _WIN32
#include <windows.h>
#endif

namespace ts { namespace vms { namespace diskio {

static uint64_t GetTimeMs() {
    return std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::system_clock::now().time_since_epoch()).count();
}

AsyncFileWriter::AsyncFileWriter(const std::string& camera_id, bool simulate_slow) 
    : camera_id_(camera_id), simulate_slow_(simulate_slow) {
    batch_buffer_.reserve(BLOCK_SIZE + 1024 * 1024);
}

AsyncFileWriter::~AsyncFileWriter() {
    Close();
}

bool AsyncFileWriter::Open(const std::string& path) {
    std::lock_guard<std::mutex> lock(mu_);
#ifdef _WIN32
    if (hFile_ != nullptr) return false;

    hFile_ = CreateFileA(path.c_str(), 
                        GENERIC_WRITE, 
                        FILE_SHARE_READ, 
                        NULL, 
                        CREATE_ALWAYS, 
                        FILE_ATTRIBUTE_NORMAL | FILE_FLAG_OVERLAPPED | FILE_FLAG_WRITE_THROUGH, 
                        NULL);

    if (hFile_ == INVALID_HANDLE_VALUE) {
        std::cerr << "[AsyncFileWriter] Failed to open " << path << " Error: " << GetLastError() << "\n";
        hFile_ = nullptr;
        return false;
    }

    hIocp_ = CreateIoCompletionPort(hFile_, NULL, (ULONG_PTR)this, 1);
#endif

    running_ = true;
    worker_thread_ = std::thread(&AsyncFileWriter::IocpWorker, this);

    return true;
}

void AsyncFileWriter::EnqueueWrite(const uint8_t* data, size_t len) {
    std::lock_guard<std::mutex> lock(mu_);
    batch_buffer_.insert(batch_buffer_.end(), data, data + len);

    if (batch_buffer_.size() >= BLOCK_SIZE) {
        SubmitBatch();
    }
}

struct WriteContext {
#ifdef _WIN32
    OVERLAPPED overlapped;
#endif
    uint64_t start_time_ms;
    void* buffer_copy;
    bool simulate_slow;
    std::string camera_id;
};

void AsyncFileWriter::SubmitBatch() {
    if (batch_buffer_.empty()) return;

    WriteContext* ctx = new WriteContext();
#ifdef _WIN32
    memset(&ctx->overlapped, 0, sizeof(OVERLAPPED));
    ctx->overlapped.Offset = file_offset_ & 0xFFFFFFFF;
    ctx->overlapped.OffsetHigh = (file_offset_ >> 32) & 0xFFFFFFFF;
#endif
    ctx->start_time_ms = GetTimeMs();
    ctx->simulate_slow = simulate_slow_;
    ctx->camera_id = camera_id_;
    
    // Copy the buffer
    size_t size = batch_buffer_.size();
    ctx->buffer_copy = malloc(size);
    memcpy(ctx->buffer_copy, batch_buffer_.data(), size);

    inflight_count_++;

#ifdef _WIN32
    if (!WriteFile(hFile_, ctx->buffer_copy, (DWORD)size, NULL, &ctx->overlapped)) {
        DWORD err = GetLastError();
        if (err != ERROR_IO_PENDING) {
            inflight_count_--;
            free(ctx->buffer_copy);
            delete ctx;
            std::cerr << "[AsyncFileWriter] WriteFile Failed immediately. Error: " << err << "\n";
        }
    }
#endif

    file_offset_ += size;
    batch_buffer_.clear();
}


void AsyncFileWriter::FlushAndWait() {
    {
        std::lock_guard<std::mutex> lock(mu_);
        SubmitBatch();
    }

    // Wait for all inflight
    while (inflight_count_ > 0) {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
    }
    
#ifdef _WIN32
    if (hFile_) {
        FlushFileBuffers(hFile_);
    }
#endif
}

void AsyncFileWriter::Close() {
    FlushAndWait();

    running_ = false;
#ifdef _WIN32
    if (hIocp_) {
        // Wake thread
        PostQueuedCompletionStatus((HANDLE)hIocp_, 0, (ULONG_PTR)this, NULL);
    }
#else
    cv_.notify_all();
#endif

    if (worker_thread_.joinable()) {
        worker_thread_.join();
    }

#ifdef _WIN32
    if (hIocp_) { CloseHandle((HANDLE)hIocp_); hIocp_ = nullptr; }
    if (hFile_) { CloseHandle((HANDLE)hFile_); hFile_ = nullptr; }
#endif
}

void AsyncFileWriter::IocpWorker() {
#ifdef _WIN32
    DWORD bytesTransferred = 0;
    ULONG_PTR completionKey = 0;
    LPOVERLAPPED overlapped = nullptr;

    while (running_) {
        BOOL bRet = GetQueuedCompletionStatus((HANDLE)hIocp_, &bytesTransferred, &completionKey, &overlapped, INFINITE);

        if (!running_ && !overlapped) break;

        if (overlapped) {
            WriteContext* ctx = (WriteContext*)overlapped;
            uint64_t latency = GetTimeMs() - ctx->start_time_ms;
            
            if (ctx->simulate_slow) {
                latency += 150; // force a slow write
            }

            if (latency > 100) {
                std::cout << "[ALERT] diskio.slow_write_detected | Camera: " << ctx->camera_id << " | Latency: " << latency << "ms\n";
            }

            free(ctx->buffer_copy);
            delete ctx;
            inflight_count_--;
        }
    }
#endif
}

}}}
