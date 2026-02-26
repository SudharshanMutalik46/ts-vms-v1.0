#include "BackfillController.h"
#include <iostream>

namespace ts {
namespace vms {
namespace recording {

BackfillController::BackfillController(std::string camera_id, PreBufferRing* ring) 
    : camera_id_(camera_id), ring_(ring) {}

BackfillController::~BackfillController() { StopEventRecording(); }

bool BackfillController::StartEventRecording(int prebuffer_seconds, GstCaps* stream_caps) {
    if (pipeline_) return true; // Already recording

    std::cout << "[BackfillController] Event triggered for " << camera_id_ << ". Building pipeline...\n";

    // Phase 4.3 SegmentWriter Pipeline, but fed via appsrc!
    std::string pipe_str = "appsrc name=src ! splitmuxsink max-size-time=300000000000 muxer=mp4mux location=./out_" + camera_id_ + "_%04d.mp4";
    
    pipeline_ = gst_parse_launch(pipe_str.c_str(), NULL);
    appsrc_ = gst_bin_get_by_name(GST_BIN(pipeline_), "src");
    
    if (stream_caps) {
        gst_app_src_set_caps(GST_APP_SRC(appsrc_), stream_caps);
    }

    gst_element_set_state(pipeline_, GST_STATE_PLAYING);

    // 1. EXTRACT BACKFILL (IDR Aligned)
    auto pre_frames = ring_->ExtractBackfill(prebuffer_seconds);
    std::cout << "[BackfillController] Extracted " << pre_frames.size() << " frames for backfill.\n";

    if (pre_frames.empty()) {
        std::cout << "[BackfillController] Waiting for next live IDR to start recording...\n";
    }

    // 2. PUSH BACKFILL FAST
    for (GstBuffer* buf : pre_frames) {
        gst_app_src_push_buffer(GST_APP_SRC(appsrc_), buf); // appsrc takes ownership
    }

    std::cout << "[BackfillController] Backfill complete. Proceeding to live stream.\n";
    
    // NOTE: In full integration, the BufferedIngress OnNewSample() would now route live 
    // frames directly to appsrc_ here.
    return true;
}

void BackfillController::StopEventRecording() {
    if (pipeline_) {
        gst_app_src_end_of_stream(GST_APP_SRC(appsrc_));
        // Wait for EOS propagation omitted for brevity
        gst_element_set_state(pipeline_, GST_STATE_NULL);
        gst_object_unref(appsrc_);
        gst_object_unref(pipeline_);
        pipeline_ = nullptr;
    }
}

}
}
}
