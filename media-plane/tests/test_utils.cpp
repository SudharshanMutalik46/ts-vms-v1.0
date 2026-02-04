#include <gtest/gtest.h>
#include "utils/logger.hpp"

using namespace ts::vms::media::utils;

TEST(LoggerTest, RedactRtspUrl) {
    EXPECT_EQ(Logger::RedactRtspUrl("rtsp://user:pass@192.168.1.1/live"), "rtsp://***:***@192.168.1.1/live");
    EXPECT_EQ(Logger::RedactRtspUrl("rtsp://192.168.1.1/live"), "rtsp://192.168.1.1/live");
    EXPECT_EQ(Logger::RedactRtspUrl("rtsps://admin:12345@camera.local:554/s0"), "rtsps://***:***@camera.local:554/s0");
}

TEST(LoggerTest, RedactRtspUrlInvalid) {
    EXPECT_EQ(Logger::RedactRtspUrl("not a url"), "not a url");
    EXPECT_EQ(Logger::RedactRtspUrl("http://user:pass@host"), "http://user:pass@host"); // Should only redact rtsp/rtsps per spec
}
