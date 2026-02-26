#pragma once
#include <gst/gst.h>
#include <gst/app/gstappsrc.h>
#include "PreBufferRing.h"
#include <string>

namespace ts {
namespace vms {
namespace recording {

class BackfillController {
public:
    BackfillController(std::string camera_id, PreBufferRing* ring);
    ~BackfillController();

    // Triggered by Phase 4.2 Event Logic
    bool StartEventRecording(int prebuffer_seconds, GstCaps* stream_caps);
    void StopEventRecording();

private:
    std::string camera_id_;
    PreBufferRing* ring_;
    GstElement* pipeline_ = nullptr;
    GstElement* appsrc_ = nullptr;
};

}
}
}
