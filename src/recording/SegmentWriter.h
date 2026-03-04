#pragma once
#include <string>
#include <functional>
#include <gst/gst.h>

namespace ts {
namespace vms {
namespace recording {

struct WriterOptions {
    int segment_duration_sec = 300;
    std::string tmp_ext = ".tmp";
    std::string final_ext = ".mkv";
    bool enable_checksum = false;
    bool prefer_tcp = true;
    int latency_ms = 200;
};

class SegmentWriter {
public:
    using SegmentCallback = std::function<void(const std::string& final_path, uint64_t size_bytes, const std::string& checksum)>;
    using ErrorCallback = std::function<void(const std::string& error_msg)>;

    SegmentWriter();
    ~SegmentWriter();

    bool Start(const std::string& camera_id, const std::string& rtsp_url, const std::string& out_dir, const WriterOptions& opts);
    void Stop();

    void OnSegmentFinalized(SegmentCallback cb) { segment_cb_ = cb; }
    void OnError(ErrorCallback cb) { error_cb_ = cb; }

    // Internal callbacks
    gchar* FormatLocation(guint fragment_id);
    void HandleBusMessage(GstMessage* msg);

private:
    void FinalizeSegment(const std::string& tmp_path);

    std::string camera_id_;
    std::string out_dir_;
    WriterOptions opts_;
    
    GstElement* pipeline_ = nullptr;
    GstBus* bus_ = nullptr;
    guint bus_watch_id_ = 0;

    SegmentCallback segment_cb_;
    ErrorCallback error_cb_;
};

}
}
}
