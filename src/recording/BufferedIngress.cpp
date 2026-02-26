#include "BufferedIngress.h"
#include <iostream>

namespace ts {
namespace vms {
namespace recording {

static GstFlowReturn new_sample_cb(GstAppSink* appsink, gpointer user_data) {
    return static_cast<BufferedIngress*>(user_data)->OnNewSample(appsink);
}

BufferedIngress::BufferedIngress(std::string camera_id, PreBufferRing* ring) 
    : camera_id_(camera_id), ring_(ring) {}

BufferedIngress::~BufferedIngress() { Stop(); }

bool BufferedIngress::Start(const std::string& rtsp_url) {
    std::string pipe_str = "rtspsrc location=" + rtsp_url + 
                           " protocols=tcp latency=200 ! rtph265depay ! h265parse config-interval=1 ! appsink name=sink emit-signals=true max-buffers=5 drop=false";
    
    GError* err = nullptr;
    pipeline_ = gst_parse_launch(pipe_str.c_str(), &err);
    if (err) { std::cerr << "Pipeline Error: " << err->message << "\n"; return false; }

    GstElement* sink = gst_bin_get_by_name(GST_BIN(pipeline_), "sink");
    g_signal_connect(sink, "new-sample", G_CALLBACK(new_sample_cb), this);
    gst_object_unref(sink);

    gst_element_set_state(pipeline_, GST_STATE_PLAYING);
    return true;
}

void BufferedIngress::Stop() {
    if (pipeline_) {
        gst_element_set_state(pipeline_, GST_STATE_NULL);
        gst_object_unref(pipeline_);
        pipeline_ = nullptr;
    }
    if (caps_) { gst_caps_unref(caps_); caps_ = nullptr; }
}

GstFlowReturn BufferedIngress::OnNewSample(GstAppSink* appsink) {
    GstSample* sample = gst_app_sink_pull_sample(appsink);
    if (!sample) return GST_FLOW_ERROR;

    // Capture SPS/PPS caps on first frame
    if (!caps_) {
        GstCaps* c = gst_sample_get_caps(sample);
        if (c) caps_ = gst_caps_copy(c);
    }

    GstBuffer* buf = gst_sample_get_buffer(sample);
    if (buf) {
        ring_->PushFrame(buf);
    }

    gst_sample_unref(sample);
    return GST_FLOW_OK;
}

}
}
}
