#pragma once
#include "FinalizedSegmentInfo.h"
#include <functional>
#include <string>
#include <vector>
#include <gst/gst.h>
#include <mutex>
#include <map>
#include <chrono>

namespace ts {
namespace vms {
namespace recording {

struct SegmentWriterOptions {
    std::string base_path = "segments";
    int segment_duration_sec = 60;
    std::string final_ext = ".mkv";
    bool enable_checksum = true;
};

class SegmentWriter {
public:
    using SegmentCallback = std::function<void(const FinalizedSegmentInfo&)>;
    using ArchiveIndexCallback = std::function<bool(const FinalizedSegmentInfo&)>;
    using ErrorCallback = std::function<void(const std::string&)>;

    SegmentWriter(const std::string& camera_id, const SegmentWriterOptions& opts);
    ~SegmentWriter();

    bool Start(GstElement* pipeline_to_monitor);
    void Stop();

    void SetCallbacks(SegmentCallback seg_cb, ArchiveIndexCallback idx_cb, ErrorCallback err_cb) {
        segment_cb_ = seg_cb;
        archive_index_cb_ = idx_cb;
        error_cb_ = err_cb;
    }

private:
    static GstPadProbeReturn OnPadProbe(GstPad* pad, GstPadProbeInfo* info, gpointer user_data);
    static gchar* OnFormatLocation(GstElement* splitmux, guint fragment_id, gpointer user_data);
    void HandleBusMessage(GstMessage* msg);

    struct PendingFragment {
        uint32_t fragment_id = 0;
        std::chrono::system_clock::time_point start_time_utc;
    };

    void FinalizeSegment(const std::string& tmp_path);
    PendingFragment TakePendingFragment(const std::string& tmp_path);
    std::string BuildSegmentId(const PendingFragment& pending) const;

    std::string camera_id_;
    SegmentWriterOptions opts_;
    
    SegmentCallback segment_cb_;
    ArchiveIndexCallback archive_index_cb_;
    ErrorCallback error_cb_;

    GstElement* pipeline_ = nullptr;
    guint bus_watch_id_ = 0;

    std::mutex pending_mu_;
    std::map<std::string, PendingFragment> pending_fragments_;
};

}
}
}
