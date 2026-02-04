#include <gtest/gtest.h>
#include <gmock/gmock.h>
#include <filesystem>
#include <thread>
#include <chrono>
#include <fstream>
#include "service/disk_cleanup.hpp"
#include "service/ingest_manager.hpp"
#include "pipeline/ingest_pipeline.hpp"

namespace fs = std::filesystem;
using namespace ts::vms::media::service;
using namespace ts::vms::media::pipeline;

// Helper to create dummy files
void CreateDummySession(const fs::path& root, const std::string& cam, const std::string& session, uint64_t size_mb, int age_min) {
    fs::path dir = root / "live" / cam / session;
    fs::create_directories(dir);
    
    fs::path file = dir / "segment_0.m4s";
    std::ofstream ofs(file, std::ios::binary);
    std::vector<char> data(1024 * 1024); // 1MB
    for(uint64_t i=0; i<size_mb; ++i) ofs.write(data.data(), data.size());
    ofs.close();

    // Set modification time
    auto time = std::chrono::file_clock::now() - std::chrono::minutes(age_min);
    fs::last_write_time(dir, time);
    fs::last_write_time(file, time);
}

class DiskCleanupTest : public ::testing::Test {
protected:
    void SetUp() override {
        test_root_ = "test_hls_cleanup";
        if (fs::exists(test_root_)) fs::remove_all(test_root_);
        fs::create_directories(test_root_);
    }

    void TearDown() override {
        if (fs::exists(test_root_)) fs::remove_all(test_root_);
    }

    std::string test_root_;
};

TEST_F(DiskCleanupTest, EnforcesTTL) {
    DiskCleanupConfig config;
    config.root_dir = test_root_;
    config.retention_minutes = 10;
    config.cleanup_interval_ms = 100;
    
    CreateDummySession(test_root_, "cam1", "sess1", 1, 20); // Old
    CreateDummySession(test_root_, "cam1", "sess2", 1, 5);  // New
    
    DiskCleanupManager manager(config);
    manager.Start();
    std::this_thread::sleep_for(std::chrono::milliseconds(200));
    manager.Stop();
    
    EXPECT_FALSE(fs::exists(fs::path(test_root_) / "live" / "cam1" / "sess1"));
    EXPECT_TRUE(fs::exists(fs::path(test_root_) / "live" / "cam1" / "sess2"));
}

TEST_F(DiskCleanupTest, EnforcesQuota) {
    DiskCleanupConfig config;
    config.root_dir = test_root_;
    config.max_size_bytes = 5 * 1024 * 1024; // 5 MB
    config.retention_minutes = 60;
    config.cleanup_interval_ms = 100;
    
    // Create 3 sessions of 2MB each = 6MB total > 5MB limit
    // Oldest should go.
    CreateDummySession(test_root_, "cam1", "sess1", 2, 30); // Oldest
    std::this_thread::sleep_for(std::chrono::milliseconds(100)); // Ensure diff timestamps if FS has low res
    CreateDummySession(test_root_, "cam1", "sess2", 2, 20);
    CreateDummySession(test_root_, "cam1", "sess3", 2, 10);
    
    DiskCleanupManager manager(config);
    manager.Start();
    std::this_thread::sleep_for(std::chrono::milliseconds(200));
    manager.Stop();
    
    EXPECT_FALSE(fs::exists(fs::path(test_root_) / "live" / "cam1" / "sess1")); // Deleted
    EXPECT_TRUE(fs::exists(fs::path(test_root_) / "live" / "cam1" / "sess2"));
    EXPECT_TRUE(fs::exists(fs::path(test_root_) / "live" / "cam1" / "sess3"));
}

TEST_F(DiskCleanupTest, NeverDeletesActiveSession) {
    DiskCleanupConfig config;
    config.root_dir = test_root_;
    config.max_size_bytes = 1; // Super low limit
    config.cleanup_interval_ms = 100;
    
    // Create session that looks "active" (last write < 1 min)
    CreateDummySession(test_root_, "cam1", "sess_active", 1, 0); 
    
    DiskCleanupManager manager(config);
    manager.Start();
    std::this_thread::sleep_for(std::chrono::milliseconds(200));
    manager.Stop();
    
    EXPECT_TRUE(fs::exists(fs::path(test_root_) / "live" / "cam1" / "sess_active"));
}

// Basic pipeline test
class IngestPipelineTest : public ::testing::Test {
protected:
    void SetUp() override {
        gst_init(nullptr, nullptr);
    }
};

TEST_F(IngestPipelineTest, GeneratesSessionId) {
    PipelineConfig config{"cam_test", "rtsp://test", false};
    IngestPipeline pipeline(config);
    // Basic construction test + HLS session ID generation implicit in ctor? 
    // Actually Session ID is generated on Start() or CreateHlsSession().
    // We can't easily test internal state without friendship or getters.
    // But this test mainly ensures no crash on instantiation.
}
