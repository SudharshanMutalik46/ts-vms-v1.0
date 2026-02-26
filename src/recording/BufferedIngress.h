#pragma once
#include <gst/gst.h>
#include <gst/app/gstappsink.h>
#include "PreBufferRing.h"
#include <string>

namespace ts {
namespace vms {
namespace recording {

class BufferedIngress {
public:
    BufferedIngress(std::string camera_id, PreBufferRing* ring);
    ~BufferedIngress();

    bool Start(const std::string& rtsp_url);
    void Stop();

    // Callback for appsink
    GstFlowReturn OnNewSample(GstAppSink* appsink);

    // Provide the Caps (SPS/PPS) to downstream writers
    GstCaps* GetCaps() { return caps_; }

private:
    std::string camera_id_;
    PreBufferRing* ring_;
    GstElement* pipeline_ = nullptr;
    GstCaps* caps_ = nullptr;
};

}
}
}
