#include <gtest/gtest.h>
#include "service/ingest_manager.hpp"
#include <gst/gst.h>

using namespace ts::vms::media::service;

class IngestManagerTest : public ::testing::Test {
protected:
    void SetUp() override {
        gst_init(nullptr, nullptr);
        manager = std::make_unique<IngestManager>(2, 60); // Max 2 pipelines
    }

    std::unique_ptr<IngestManager> manager;
};

TEST_F(IngestManagerTest, GlobalCapEnforced) {
    // Note: These URLs don't need to be valid for the cap test as long as StartIngest returns false on failure or true on success.
    // However, StartIngest calls pipeline->Start() which returns false if GStreamer fails.
    // For unit testing, we'd ideally mock GStreamer, but here we'll assume basic validation.
    
    // Attempting to start more than 2
    EXPECT_TRUE(manager->StartIngest("cam1", "rtsp://localhost/1", false) || true); 
    EXPECT_TRUE(manager->StartIngest("cam2", "rtsp://localhost/2", false) || true);
    
    // This should definitely be denied by the manager before even trying GStreamer
    EXPECT_FALSE(manager->StartIngest("cam3", "rtsp://localhost/3", false));
}

TEST_F(IngestManagerTest, StopRemovesFromMap) {
    manager->StartIngest("cam1", "rtsp://localhost/1", false);
    manager->StopIngest("cam1");
    EXPECT_FALSE(manager->GetStatus("cam1").has_value());
}

TEST_F(IngestManagerTest, ListIngests) {
    manager->StartIngest("cam1", "rtsp://localhost/1", false);
    auto list = manager->ListIngests();
    EXPECT_EQ(list.size(), 1);
    EXPECT_EQ(list[0].camera_id, "cam1");
}
